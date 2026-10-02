package assistant_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	assistantcmd "github.com/grafana/gcx/internal/providers/assistant"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeMemorySearch(t *testing.T, cfg string, args ...string) (string, string, error) {
	t.Helper()
	cmd := assistantcmd.Command()
	cmd.SetContext(t.Context())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"search", "--config", cfg}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestMemorySearchRequest(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	for _, oauth := range []bool{false, true} {
		t.Run(fmt.Sprintf("oauth=%t", oauth), func(t *testing.T) {
			testutils.SetAgentMode(t, true)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				path := "/api/plugins/grafana-assistant-app/resources/api/v1/assistant-search"
				token := "test-token"
				if oauth {
					path = "/api/cli/v1/proxy" + path
					token = "gat_test-access-token"
				}
				assert.Equal(t, path, r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "cli", r.Header.Get("X-App-Source"))
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.JSONEq(t, `{"query":"checkout latency","collections":["dashboards","alertRules","infrastructure"],"retrievalOnly":true,"startTime":"2025-01-01T00:00:00Z","endTime":"2026-01-01T00:00:00Z"}`, string(body))
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":{"collections":[{"collection":"dashboards","total":1,"results":[{"score":0.9,"title":"Checkout","summary":"Latency panels","sourceId":"checkout","sourceUrl":"https://example.grafana.net/d/checkout","metadata":{"datasourceUids":["prom-1"]}}]},{"collection":"alertRules","total":0,"results":[]}],"searchFocus":["dashboards","alertRules"],"suppressedCollections":["infrastructure"]}}`)
			}))
			t.Cleanup(server.Close)
			cfg := writeAssistantTestConfig(t, server.URL)
			if oauth {
				cfg = filepath.Join(t.TempDir(), "config.yaml")
				contents := fmt.Sprintf(`current-context: test
contexts:
  test:
    grafana:
      server: https://example.grafana.net
      stack-id: 12345
      auth-method: oauth
      proxy-endpoint: %s
      oauth-token: gat_test-access-token
`, server.URL)
				require.NoError(t, os.WriteFile(cfg, []byte(contents), 0o600))
			}
			stdout, stderr, err := executeMemorySearch(t, cfg, " checkout latency ", "--collections", "DASHBOARDS,AlertRules,dashboards,infrastructure", "--from", "2025-01-01T00:00:00Z", "--to", "2026-01-01T00:00:00Z")
			require.NoError(t, err)
			assert.EqualValues(t, 1, calls.Load())
			assert.Empty(t, stderr)
			var result map[string]any
			decoder := json.NewDecoder(strings.NewReader(stdout))
			require.NoError(t, decoder.Decode(&result))
			require.ErrorIs(t, decoder.Decode(new(any)), io.EOF)
			assert.Equal(t, "gcx.assistant_search", result["type"])
			assert.Equal(t, "1", result["schema_version"])
			assert.Equal(t, false, result["exhaustive"])
			assert.Equal(t, []any{"dashboards", "alertRules"}, result["searchFocus"])
			assert.Equal(t, []any{"infrastructure"}, result["suppressedCollections"])
			assert.Contains(t, stdout, "prom-1")
			assert.Contains(t, stdout, "Latency panels")
		})
	}
}

func TestMemorySearchValidationBeforeRequest(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing query", []string{"--collections", "dashboards"}, "accepts 1 arg"},
		{"extra query", []string{"one", "two", "--collections", "dashboards"}, "accepts 1 arg"},
		{"blank query", []string{"  ", "--collections", "dashboards"}, "query must not be empty"},
		{"missing collections", []string{"checkout"}, "--collections is required"},
		{"empty collections", []string{"checkout", "--collections="}, "--collections is required"},
		{"blank collection", []string{"checkout", "--collections", " "}, "invalid collection"},
		{"unknown collection", []string{"checkout", "--collections", "dashboards,logs"}, `invalid collection "logs"`},
		{"empty member", []string{"checkout", "--collections", "dashboards,"}, "invalid collection"},
		{"empty from", []string{"checkout", "--collections", "incidents", "--from="}, "--from must not be empty"},
		{"invalid from", []string{"checkout", "--collections", "incidents", "--from", "last month"}, `invalid --from "last month"`},
		{"empty to", []string{"checkout", "--collections", "incidents", "--to="}, "--to must not be empty"},
		{"invalid to", []string{"checkout", "--collections", "incidents", "--to", "tomorrow"}, `invalid --to "tomorrow"`},
		{"reversed range", []string{"checkout", "--collections", "incidents", "--from", "2026-02-01T00:00:00Z", "--to", "2026-01-01T00:00:00Z"}, "must be before"},
		{"empty range", []string{"checkout", "--collections", "incidents", "--from", "2026-01-01T00:00:00Z", "--to", "2026-01-01T00:00:00Z"}, "must be before"},
		{"zero timeout", []string{"checkout", "--collections", "dashboards", "--timeout", "0"}, "--timeout must be positive"},
		{"negative timeout", []string{"checkout", "--collections", "dashboards", "--timeout", "-1"}, "--timeout must be positive"},
		{"overflow timeout", []string{"checkout", "--collections", "dashboards", "--timeout", "9223372037"}, "must not exceed 9223372036 seconds"},
		{"overflow relative time", []string{"checkout", "--collections", "incidents", "--from", "now-999999999999999999999y"}, "out of range"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			missingConfig := filepath.Join(t.TempDir(), "missing-config.yaml")
			stdout, _, err := executeMemorySearch(t, missingConfig, tt.args...)
			require.ErrorContains(t, err, tt.want)
			assert.Empty(t, stdout)
			assert.NotContains(t, err.Error(), "could not read")
		})
	}
}

func TestMemorySearchDefaultHistoryWindowIsOmitted(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.NotContains(t, body, "startTime")
		assert.NotContains(t, body, "endTime")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"collections":[]}}`)
	}))
	t.Cleanup(server.Close)
	stdout, _, err := executeMemorySearch(t, writeAssistantTestConfig(t, server.URL), "checkout", "--collections", "investigations,incidents", "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, stdout, `"collections": []`)
}

func TestMemorySearchResults(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	tests := []struct {
		name, body, wantText string
		code                 int
	}{
		{"empty", `{"collections":[{"collection":"dashboards","results":[],"total":0}]}`, "No matching results", 0},
		{"suppressed", `{"collections":[],"suppressedCollections":["infrastructure"]}`, "Not searched (disabled by workspace policy): infrastructure", 0},
		{"partial failure", `{"collections":[{"collection":"dashboards","total":1,"results":[{"title":"Checkout","sourceId":"abc","summary":"Useful evidence","sourceUrl":"https://example.grafana.net/d/abc"}]},{"collection":"incidents","results":null,"total":0,"error":"service unavailable"}]}`, "Search failed: service unavailable", 4},
		{"all failed", `{"collections":[{"collection":"dashboards","results":null,"total":0,"error":"service unavailable"}]}`, "Search failed: service unavailable", 1},
	}
	for _, tt := range tests {
		for _, output := range []string{"json", "yaml", "text", "agents"} {
			t.Run(tt.name+"/"+output, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(w, `{"data":%s}`, tt.body)
				}))
				t.Cleanup(server.Close)
				stdout, _, err := executeMemorySearch(t, writeAssistantTestConfig(t, server.URL), "checkout", "--collections", "dashboards,incidents", "-o", output)
				if tt.code != 0 {
					var emitted *gcxerrors.EmittedError
					require.ErrorAs(t, err, &emitted)
					assert.Equal(t, tt.code, emitted.Code)
				} else {
					require.NoError(t, err)
				}
				if output == "text" {
					assert.Contains(t, stdout, tt.wantText)
					assert.Contains(t, stdout, "not exhaustive")
				} else {
					assert.Contains(t, stdout, "exhaustive")
					assert.NotContains(t, stdout, "null")
					if tt.code != 0 {
						assert.Contains(t, stdout, "service unavailable")
					}
				}
				if output == "json" || output == "agents" {
					var result any
					decoder := json.NewDecoder(strings.NewReader(stdout))
					require.NoError(t, decoder.Decode(&result))
					require.ErrorIs(t, decoder.Decode(new(any)), io.EOF)
				}
				if tt.name == "partial failure" {
					assert.Contains(t, stdout, "Useful evidence")
					assert.Contains(t, stdout, "https://example.grafana.net/d/abc")
				}
			})
		}
	}
}

func TestMemorySearchTextEscapesServerControlledContent(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"collections":[{"collection":"dashboards\u001b[31m","total":1,"results":[{"title":"Checkout\u001b]52;c;Y29weQ==\u0007","sourceId":"line\nbreak","sourceUrl":"https://example.test/\u0000","summary":"first\nsecond"}]}],"suppressedCollections":["infra\u000dstructure"]}}`)
	}))
	t.Cleanup(server.Close)

	stdout, _, err := executeMemorySearch(t, writeAssistantTestConfig(t, server.URL), "checkout", "--collections", "dashboards", "-o", "text")
	require.NoError(t, err)
	assert.NotContains(t, stdout, "\x1b")
	assert.NotContains(t, stdout, "\x00")
	assert.NotContains(t, stdout, "\r")
	assert.Contains(t, stdout, `dashboards\u001b[31m`)
	assert.Contains(t, stdout, `Checkout\u001b]52;c;Y29weQ==\u0007`)
	assert.Contains(t, stdout, `line\nbreak`)
	assert.Contains(t, stdout, `https://example.test/\u0000`)
	assert.Contains(t, stdout, `first\nsecond`)
	assert.Contains(t, stdout, `infra\rstructure`)
}

func TestMemorySearchBackendErrors(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	tests := []struct {
		name                    string
		status                  int
		body, contentType, want string
	}{
		{"unauthorized", 401, "expired token", "text/plain", "expired token"},
		{"forbidden", 403, `{"message":"missing scope"}`, "application/json", "missing scope"},
		{"not deployed", 404, "not found", "text/plain", "HTTP 404"},
		{"HTML error", 502, "<html>do not expose this payload</html>", "text/html", "HTTP 502"},
		{"bad response", 200, "not json", "application/json", "decode assistant search response"},
		{"missing data", 200, `{}`, "application/json", "missing data"},
		{"null data", 200, `{"data":null}`, "application/json", "missing data"},
		{"missing collections", 200, `{"data":{}}`, "application/json", "missing collections"},
		{"null collections", 200, `{"data":{"collections":null}}`, "application/json", "missing collections"},
		{"large error", 502, strings.Repeat("x", 5000) + "do not expose this payload", "text/plain", "HTTP 502"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(server.Close)
			stdout, _, err := executeMemorySearch(t, writeAssistantTestConfig(t, server.URL), "checkout", "--collections", "dashboards")
			require.ErrorContains(t, err, tt.want)
			assert.Empty(t, stdout)
			assert.NotContains(t, err.Error(), "do not expose this payload")
			assert.Less(t, len(err.Error()), 4300)
			if tt.status != 200 {
				var apiErr interface{ HTTPStatusCode() int }
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, tt.status, apiErr.HTTPStatusCode())
			}
		})
	}
}

func TestMemorySearchTimeout(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		assert.NoError(t, http.NewResponseController(w).Flush())
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	stdout, _, err := executeMemorySearch(t, writeAssistantTestConfig(t, server.URL), "checkout", "--collections", "dashboards", "--timeout", "1")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, stdout)
}
