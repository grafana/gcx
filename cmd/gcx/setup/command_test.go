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
	"testing"

	"github.com/grafana/gcx/cmd/gcx/setup"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"setup", "status", "--config", cfgFile, "-o", "json"})

	err := root.Execute()
	return stdout.String(), err
}

// The Fleet Management preflight row is the reason the preflight exists. A
// failed instrumentation check must not discard it.
func TestSetupStatus_KeepsPreflightRowWhenInstrumentationFails(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "stack lookup fails",
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
			name: "monitoring request fails",
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
			if emitted.Code != gcxerrors.ExitPartialFailure {
				t.Fatalf("exit code = %d, want %d", emitted.Code, gcxerrors.ExitPartialFailure)
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
			if instrumentation["product"] != "instrumentation" || instrumentation["health"] != "unknown" {
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
