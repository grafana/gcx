package setup_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/setup"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// These tests pin the agent output contract for `gcx setup status`. Before
// the migration the command bypassed output.Options entirely and printed a
// fixed ASCII table on stdout even in agent mode; it now routes the status
// document through the codec system: the default text codec renders the
// byte-identical table, agent mode gets exactly one JSON value, and explicit
// -o json/yaml always win.

// statusTableEnabled is the byte-exact human table for an enabled
// instrumentation product with 3 clusters, behind a healthy collector app
// plugin.
const statusTableEnabled = "PRODUCT           ENABLED  HEALTH   DETAILS\n" +
	"fleet-management  yes      healthy  read and admin routes\n" +
	"instrumentation   yes      healthy  3 clusters\n"

// statusTableDisabled is the byte-exact human table when no clusters exist.
const statusTableDisabled = "PRODUCT           ENABLED  HEALTH   DETAILS\n" +
	"fleet-management  yes      healthy  read and admin routes\n" +
	"instrumentation   no       healthy  0 clusters\n"

func TestSetupStatus_OutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		output     string // explicit -o value; empty = default
		enabled    bool
		clusters   int
		wantStdout string // exact stdout; empty = use check
		check      func(t *testing.T, stdout string)
	}{
		{
			name:       "human default enabled table is byte-identical",
			enabled:    true,
			clusters:   3,
			wantStdout: statusTableEnabled,
		},
		{
			name:       "human default disabled table is byte-identical",
			enabled:    false,
			clusters:   0,
			wantStdout: statusTableDisabled,
		},
		{
			name:      "agent mode emits exactly one JSON document",
			agentMode: true,
			enabled:   true,
			clusters:  3,
			check: func(t *testing.T, stdout string) {
				t.Helper()
				doc := decodeSingleJSONValue(t, stdout)
				if doc["type"] != "gcx.setup.status" {
					t.Fatalf("type = %v, want gcx.setup.status", doc["type"])
				}
				if doc["schema_version"] != "1" {
					t.Fatalf("schema_version = %v, want 1", doc["schema_version"])
				}
				products, ok := doc["products"].([]any)
				if !ok || len(products) != 2 {
					t.Fatalf("products = %v, want two entries", doc["products"])
				}
				plugin, ok := products[0].(map[string]any)
				if !ok {
					t.Fatalf("products[0] is %T, want object", products[0])
				}
				if plugin["product"] != "fleet-management" || plugin["health"] != "healthy" {
					t.Fatalf("unexpected plugin row: %v", plugin)
				}
				product, ok := products[1].(map[string]any)
				if !ok {
					t.Fatalf("products[1] is %T, want object", products[1])
				}
				if product["product"] != "instrumentation" || product["enabled"] != true {
					t.Fatalf("unexpected product row: %v", product)
				}
			},
		},
		{
			name:     "explicit -o json wins in human mode",
			enabled:  true,
			clusters: 3,
			output:   "json",
			check: func(t *testing.T, stdout string) {
				t.Helper()
				doc := decodeSingleJSONValue(t, stdout)
				if doc["type"] != "gcx.setup.status" {
					t.Fatalf("type = %v, want gcx.setup.status", doc["type"])
				}
			},
		},
		{
			name:      "explicit -o yaml wins in agent mode",
			agentMode: true,
			enabled:   true,
			clusters:  3,
			output:    "yaml",
			check: func(t *testing.T, stdout string) {
				t.Helper()
				if !strings.Contains(stdout, "type: gcx.setup.status") ||
					!strings.Contains(stdout, "product: instrumentation") {
					t.Fatalf("yaml output missing expected fields:\n%s", stdout)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			flags := pflag.NewFlagSet("status", pflag.ContinueOnError)
			opts := setup.NewStatusOptsForTest(flags)
			if tc.output != "" {
				if err := flags.Set("output", tc.output); err != nil {
					t.Fatalf("set -o %s: %v", tc.output, err)
				}
			}
			if err := opts.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}

			var stdout, stderr bytes.Buffer
			opts.IO.ErrWriter = &stderr

			doc := setup.StatusDocForTest(tc.enabled, tc.clusters)
			if err := opts.IO.Encode(&stdout, doc); err != nil {
				t.Fatalf("Encode() = %v", err)
			}

			if tc.wantStdout != "" {
				if stdout.String() != tc.wantStdout {
					t.Fatalf("stdout not byte-identical:\ngot:  %q\nwant: %q", stdout.String(), tc.wantStdout)
				}
				return
			}
			tc.check(t, stdout.String())
		})
	}
}

// decodeSingleJSONValue asserts that raw holds exactly one JSON object
// followed by EOF, and returns it decoded.
func decodeSingleJSONValue(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var first any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, raw)
	}
	var second any
	if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout must contain exactly one JSON value, second decode = %v\n%s", err, raw)
	}
	doc, ok := first.(map[string]any)
	if !ok {
		t.Fatalf("document is %T, want object", first)
	}
	return doc
}

// runStatus mounts the setup command under a bare root and runs `setup status`
// against the given handler. It returns the stdout of the command and the error
// of the run.
func runStatus(t *testing.T, handler http.HandlerFunc) (string, error) {
	t.Helper()
	stdout, _, err := runStatusMode(t, handler, "json", false)
	return stdout, err
}

func runStatusMode(t *testing.T, handler http.HandlerFunc, output string, agentMode bool, extraArgs ...string) (string, string, error) {
	t.Helper()
	agent.SetFlag(agentMode)
	t.Cleanup(func() { agent.SetFlag(false) })

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	cfg := "version: 1\n" +
		"stacks:\n" +
		"  test:\n" +
		"    grafana:\n" +
		"      server: " + server.URL + "\n" +
		"      token: test-token\n" +
		"      org-id: 1\n" +
		"contexts:\n" +
		"  test:\n" +
		"    stack: test\n" +
		"current-context: test\n"
	if err := os.WriteFile(cfgFile, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	root := &cobra.Command{Use: "gcx", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(setup.Command())

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	args := []string{"setup", "status", "--config", cfgFile}
	if output != "" {
		args = append(args, "-o", output)
	}
	root.SetArgs(append(args, extraArgs...))

	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

// The Fleet Management preflight row is the reason the preflight exists. A
// failed instrumentation check must not discard it.
func TestSetupStatus_KeepsPreflightRowWhenInstrumentationFails(t *testing.T) {
	tests := []struct {
		name     string
		wantCode int
		handler  http.HandlerFunc
	}{
		{
			name:     "stack lookup fails",
			wantCode: gcxerrors.ExitAuthFailure,
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/plugins/grafana-collector-app/settings":
					writeJSON(w, map[string]any{"id": "grafana-collector-app", "enabled": true})
				case "/api/access-control/user/actions":
					writeJSON(w, map[string]bool{
						"grafana-collector-app:read":  true,
						"grafana-collector-app:admin": true,
					})
				default:
					w.WriteHeader(http.StatusForbidden)
				}
			},
		},
		{
			name:     "monitoring request fails",
			wantCode: gcxerrors.ExitGeneralError,
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/plugins/grafana-collector-app/settings":
					writeJSON(w, map[string]any{"id": "grafana-collector-app", "enabled": true})
				case "/api/access-control/user/actions":
					writeJSON(w, map[string]bool{
						"grafana-collector-app:read":  true,
						"grafana-collector-app:admin": true,
					})
				case "/api/plugin-proxy/grafana-collector-app/grafanacom-api/instances/":
					writeJSON(w, map[string]any{
						"hmInstancePromId":        123,
						"hmInstancePromClusterId": 42,
					})
				case "/api/plugin-proxy/grafana-collector-app/fleet-management-api/discovery.v1.DiscoveryService/RunK8sMonitoring":
					w.WriteHeader(http.StatusInternalServerError)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, err := runStatus(t, tt.handler)
			if err == nil {
				t.Fatal("Execute() = nil, want the failure of the instrumentation check")
			}

			var emitted *gcxerrors.EmittedError
			if !errors.As(err, &emitted) {
				t.Fatalf("error = %T (%v), want *gcxerrors.EmittedError", err, err)
			}
			if emitted.Code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d", emitted.Code, tt.wantCode)
			}

			doc := decodeSingleJSONValue(t, stdout)
			products, ok := doc["products"].([]any)
			if !ok || len(products) != 2 {
				t.Fatalf("products = %v, want two entries", doc["products"])
			}
			plugin, ok := products[0].(map[string]any)
			if !ok {
				t.Fatalf("products[0] is %T, want object", products[0])
			}
			if plugin["product"] != "fleet-management" || plugin["health"] != "healthy" {
				t.Fatalf("the preflight row is lost: %v", plugin)
			}
			instrumentation, ok := products[1].(map[string]any)
			if !ok {
				t.Fatalf("products[1] is %T, want object", products[1])
			}
			if instrumentation["product"] != "instrumentation" || instrumentation["health"] != "unknown" || instrumentation["enabled"] != nil {
				t.Fatalf("unexpected instrumentation row: %v", instrumentation)
			}
			details, _ := instrumentation["details"].(string)
			if !strings.Contains(details, "the check failed") {
				t.Fatalf("details = %q, want the reason of the failure", details)
			}
		})
	}
}

// An unavailable plugin emits one complete status document and returns a
// general error. The action check must not hide the known plugin state.
func TestSetupStatus_UnavailablePluginEndsWithGeneralError(t *testing.T) {
	tests := []struct {
		name     string
		settings func(http.ResponseWriter)
	}{
		{
			name: "plugin is absent",
			settings: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusNotFound)
			},
		},
		{
			name: "plugin is disabled",
			settings: func(w http.ResponseWriter) {
				writeJSON(w, map[string]any{"id": "grafana-collector-app", "enabled": false})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actionsCalled := false
			stdout, err := runStatus(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/plugins/grafana-collector-app/settings":
					tt.settings(w)
				case "/api/access-control/user/actions":
					actionsCalled = true
					w.WriteHeader(http.StatusInternalServerError)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})

			var emitted *gcxerrors.EmittedError
			if !errors.As(err, &emitted) {
				t.Fatalf("error = %T (%v), want *gcxerrors.EmittedError", err, err)
			}
			if emitted.Code != gcxerrors.ExitGeneralError {
				t.Fatalf("exit code = %d, want %d", emitted.Code, gcxerrors.ExitGeneralError)
			}
			if actionsCalled {
				t.Fatal("the action endpoint was called for an unavailable plugin")
			}

			doc := decodeSingleJSONValue(t, stdout)
			products, ok := doc["products"].([]any)
			if !ok || len(products) != 2 {
				t.Fatalf("products = %v, want two entries", doc["products"])
			}
			plugin, _ := products[0].(map[string]any)
			if plugin["product"] != "fleet-management" || plugin["health"] != "unhealthy" {
				t.Fatalf("unexpected plugin row: %v", plugin)
			}
			instrumentation, _ := products[1].(map[string]any)
			if instrumentation["health"] != "unknown" {
				t.Fatalf("unexpected instrumentation row: %v", instrumentation)
			}
		})
	}
}

// A 403 on the plugin settings route must not read as "not installed". The
// command still tries the instrumentation check, so the real error reaches the
// user.
func TestSetupStatus_UnknownPluginStateStillTriesTheCheck(t *testing.T) {
	stdout, err := runStatus(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-collector-app/settings":
			w.WriteHeader(http.StatusForbidden)
		case "/api/access-control/user/actions":
			writeJSON(w, map[string]bool{})
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	})
	if err == nil {
		t.Fatal("Execute() = nil, want the failure of the instrumentation check")
	}

	doc := decodeSingleJSONValue(t, stdout)
	products, _ := doc["products"].([]any)
	if len(products) != 2 {
		t.Fatalf("products = %v, want two entries", doc["products"])
	}
	plugin, _ := products[0].(map[string]any)
	require.Contains(t, plugin, "enabled")
	assert.Nil(t, plugin["enabled"])
	if plugin["health"] != "unknown" {
		t.Fatalf("plugin health = %v, want unknown", plugin["health"])
	}
	details, _ := plugin["details"].(string)
	if !strings.Contains(details, "HTTP 403") {
		t.Fatalf("details = %q, want the status in the text", details)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Drive the public command so preflight, typed errors, encoding and process exit
// classification are exercised together in every supported default output mode.
func TestSetupStatus_InstrumentationProbeOutcome(t *testing.T) {
	for _, mode := range []struct {
		name           string
		output         string
		agent          bool
		args           []string
		wantInBand     bool
		wantDiagnostic bool
		wantHint       bool
	}{
		{name: "text", output: "text", wantDiagnostic: true},
		{name: "json", output: "json", wantDiagnostic: true},
		{name: "yaml", output: "yaml", wantDiagnostic: true},
		{name: "agent", agent: true, wantInBand: true, wantHint: true},
		{name: "agents", output: "agents", wantDiagnostic: true},
		{name: "agent json", output: "json", agent: true, wantInBand: true, wantHint: true},
		{name: "agent yaml", output: "yaml", agent: true, wantDiagnostic: true},
		{name: "agent text", output: "text", agent: true, wantDiagnostic: true},
		{name: "json flag", args: []string{"--json", ""}, wantInBand: true},
		{name: "json fields", args: []string{"--json", "type,schema_version,products,error"}, wantInBand: true},
		{name: "agent json fields", agent: true, args: []string{"--json", "type,schema_version,products,error"}, wantInBand: true, wantHint: true},
		{name: "jq document", args: []string{"--jq", "."}, wantDiagnostic: true},
		{name: "jq", agent: true, args: []string{"--jq", ".products"}, wantDiagnostic: true},
		{name: "json products", agent: true, args: []string{"--json", "products"}, wantDiagnostic: true, wantHint: true},
	} {
		for _, tc := range []struct {
			name         string
			status       int
			actionsKnown bool
			canAdmin     bool
			wantCode     int
			wantSummary  string
			body         string
			wantCause    string
		}{
			{name: "read-only token forbidden", status: 403, actionsKnown: true, wantCode: 3, wantSummary: gcxerrors.SummaryAuthorizationFailed},
			{name: "unknown actions forbidden", status: 403, wantCode: 3, wantSummary: gcxerrors.SummaryAuthorizationFailed},
			{name: "admin token forbidden", status: 403, actionsKnown: true, canAdmin: true, wantCode: 3, wantSummary: gcxerrors.SummaryAuthorizationFailed},
			{name: "backend forbidden", status: 403, body: `{"message":"backend access denied"}`, wantCode: 3, wantSummary: gcxerrors.SummaryAuthorizationFailed},
			{name: "endpoint missing", status: 404, body: `{"message":"plugin route match not found"}`, wantCode: 1, wantSummary: gcxerrors.SummaryEndpointNotAvailable},
			{name: "credentials rejected", status: 401, wantCode: 3, wantSummary: gcxerrors.SummaryAuthenticationFailed},
			{name: "server error with admin", status: 500, actionsKnown: true, canAdmin: true, wantCode: 1, wantSummary: gcxerrors.SummaryAPIError},
			{name: "monitoring connection closed", wantCode: 1, wantSummary: gcxerrors.SummaryNetworkError, wantCause: "EOF"},
			{name: "no clusters", status: 200, actionsKnown: true, canAdmin: true},
		} {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				var monitoringCalled atomic.Bool
				stdout, stderr, err := runStatusMode(t, func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/bootdata":
						w.WriteHeader(http.StatusNotFound)
					case "/api/plugins/grafana-collector-app/settings":
						writeJSON(w, map[string]any{"id": "grafana-collector-app", "enabled": true})
					case "/api/access-control/user/actions":
						if !tc.actionsKnown {
							w.WriteHeader(http.StatusForbidden)
							return
						}
						writeJSON(w, map[string]bool{"grafana-collector-app:read": true, "grafana-collector-app:admin": tc.canAdmin})
					case "/api/plugin-proxy/grafana-collector-app/grafanacom-api/instances/":
						writeJSON(w, map[string]any{"hmInstancePromId": 123, "hmInstancePromClusterId": 42})
					case "/api/plugin-proxy/grafana-collector-app/fleet-management-api/discovery.v1.DiscoveryService/RunK8sMonitoring":
						monitoringCalled.Store(true)
						if tc.wantCause != "" {
							hijacker, ok := w.(http.Hijacker)
							if !assert.True(t, ok) {
								return
							}
							conn, _, hijackErr := hijacker.Hijack()
							if assert.NoError(t, hijackErr) {
								_ = conn.Close()
							}
							return
						}
						if tc.status == http.StatusOK {
							writeJSON(w, map[string]any{"clusters": []any{}})
							return
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(tc.status)
						body := tc.body
						if body == "" {
							body = `{"message":"plugin proxy route access denied","extra":"raw-response-marker"}`
						}
						_, _ = io.WriteString(w, body)
					default:
						t.Errorf("unexpected request: %s", r.URL.Path)
						w.WriteHeader(http.StatusNotFound)
					}
				}, mode.output, mode.agent, mode.args...)
				require.True(t, monitoringCalled.Load())
				if mode.wantHint {
					hintLine, remaining, found := strings.Cut(stderr, "\n")
					require.True(t, found)
					hint := decodeSingleJSONValue(t, hintLine)
					assert.Equal(t, "hint", hint["class"])
					assert.Contains(t, hint["summary"], "--jq")
					stderr = remaining
				}
				if tc.wantCode == 0 {
					require.NoError(t, err)
					assert.Empty(t, stderr)
				} else {
					var emitted *gcxerrors.EmittedError
					require.ErrorAs(t, err, &emitted)
					assert.Equal(t, tc.wantCode, emitted.Code)
					assert.Contains(t, stdout, tc.wantSummary)
					if tc.wantCause != "" {
						assert.Contains(t, stdout, tc.wantCause)
					}
					if mode.wantDiagnostic {
						assertStatusDiagnostic(t, stderr, tc.wantSummary, mode.agent)
					} else {
						assert.Empty(t, stderr)
					}
				}
				switch {
				case mode.name == "jq" || mode.name == "json products":
					var products []map[string]any
					if mode.name == "json products" {
						var selected struct {
							Products []map[string]any `json:"products"`
						}
						require.NoError(t, json.Unmarshal([]byte(stdout), &selected))
						products = selected.Products
						assert.Len(t, decodeSingleJSONValue(t, stdout), 1)
					} else {
						require.NoError(t, json.Unmarshal([]byte(stdout), &products))
					}
					require.Len(t, products, 2)
					assert.Equal(t, "instrumentation", products[1]["product"])
					assert.NotContains(t, stdout, `"error"`)
					if tc.wantCode != 0 {
						assert.Nil(t, products[1]["enabled"])
						assert.Equal(t, "unknown", products[1]["health"])
					}
				case mode.output == "text":
					if tc.wantCode != 0 {
						assert.Regexp(t, `instrumentation\s+unknown\s+unknown`, stdout)
						assert.NotContains(t, stdout, "Error:")
						assert.NotContains(t, stdout, "Suggestions:")
					} else {
						assert.Regexp(t, `instrumentation\s+no\s+healthy`, stdout)
					}
				default:
					jsonOutput := stdout
					if mode.output == "yaml" {
						data, convertErr := yaml.YAMLToJSON([]byte(stdout))
						require.NoError(t, convertErr)
						jsonOutput = string(data)
					}
					assertStatusDocument(t, jsonOutput, tc.wantSummary, tc.wantCode, tc.status, mode.wantInBand)
					if mode.output == "yaml" && tc.wantCode != 0 {
						assert.Contains(t, stdout, "enabled: null")
					}
				}
				assert.NotContains(t, stdout+stderr, "gcx setup status")
				if tc.status == 401 || tc.status == 403 {
					assertStatusAuthDiagnostic(t, stdout, stderr, tc.status, tc.body, mode.wantDiagnostic && !mode.agent)
				}
			})
		}
	}
}

func assertStatusDocument(t *testing.T, raw, summary string, code, status int, inBand bool) {
	t.Helper()
	doc := decodeSingleJSONValue(t, raw)
	assert.Equal(t, "gcx.setup.status", doc["type"])
	assert.Equal(t, "1", doc["schema_version"])
	products, ok := doc["products"].([]any)
	require.True(t, ok)
	require.Len(t, products, 2)
	row, ok := products[1].(map[string]any)
	require.True(t, ok)
	require.Contains(t, row, "enabled")
	if code == 0 {
		assert.Equal(t, false, row["enabled"])
		assert.Equal(t, "healthy", row["health"])
		assert.Nil(t, doc["error"])
		return
	}
	assert.Nil(t, row["enabled"])
	assert.Equal(t, "unknown", row["health"])
	details, ok := row["details"].(string)
	require.True(t, ok)
	assert.Equal(t, strings.TrimSpace(details), details)
	if !inBand {
		assert.NotContains(t, doc, "error")
		return
	}
	assertStatusFailure(t, doc, details, summary, code, status)
}

func assertStatusFailure(t *testing.T, doc map[string]any, details, summary string, code, status int) {
	t.Helper()
	failure, ok := doc["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, summary, failure["summary"])
	assert.EqualValues(t, code, failure["exitCode"])
	cause, ok := failure["details"].(string)
	require.True(t, ok)
	assert.Equal(t, "the check failed: "+summary+": "+cause, details)
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound {
		require.Contains(t, failure, "docsLink")
	}
	if link, ok := failure["docsLink"].(string); ok {
		assert.Contains(t, failure["suggestions"], gcxerrors.DocsFetchSuggestion(link))
	}
}

func assertStatusDiagnostic(t *testing.T, stderr, summary string, agentMode bool) {
	t.Helper()
	if !agentMode {
		assert.Contains(t, stderr, "Error: "+summary)
		return
	}
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	require.Len(t, lines, 2)
	for i, line := range lines {
		diagnostic := decodeSingleJSONValue(t, line)
		assert.Equal(t, "warning", diagnostic["class"])
		assert.NotContains(t, diagnostic, "error")
		assert.NotContains(t, diagnostic, "type")
		if i == 0 {
			assert.Equal(t, summary, diagnostic["summary"])
		} else {
			assert.NotEmpty(t, diagnostic["summary"])
		}
	}
}

func assertStatusAuthDiagnostic(t *testing.T, stdout, stderr string, status int, body string, humanDiagnostic bool) {
	t.Helper()
	assert.NotContains(t, stdout+stderr, "/discovery.v1.DiscoveryService/RunK8sMonitoring")
	assert.NotContains(t, stdout+stderr, "raw-response-marker")
	if status != http.StatusForbidden {
		return
	}
	if humanDiagnostic {
		assert.Contains(t, stderr, "Named read routes")
		assert.Contains(t, stderr, "Wildcard routes")
		assert.Contains(t, stderr, "gcx api /api/access-control/user/actions")
	}
	if body == "" {
		assert.Contains(t, stdout+stderr, "requires the grafana-collector-app:admin action")
	} else {
		assert.NotContains(t, stdout+stderr, "requires the")
		assert.NotContains(t, stderr, "Ask a stack admin to grant")
	}
}
