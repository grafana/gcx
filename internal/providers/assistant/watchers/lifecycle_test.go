package watchers //nolint:testpackage // Exercise the public command tree and wire contract together.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clientwatchers "github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const lifecycleAPIPath = "/api/plugins/grafana-assistant-app/resources/api/v1/watcher-agents"

type lifecycleHTTPRequest struct {
	Method, Path, Query, Body string
}

type lifecycleHTTPLog struct {
	mu       sync.Mutex
	requests []lifecycleHTTPRequest
}

func (l *lifecycleHTTPLog) snapshot() []lifecycleHTTPRequest {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]lifecycleHTTPRequest(nil), l.requests...)
}

func lifecycleEnvelope(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": value}); err != nil {
		http.Error(w, "cannot encode fixture response", http.StatusInternalServerError)
	}
}

// The optional handler overrides actions and readback; ordinary identity reads
// still pass through the same config and HTTP client as a user command.
func lifecycleCommandFixture(t *testing.T, item clientwatchers.Watcher, handle func(http.ResponseWriter, *http.Request) bool) (*providers.ConfigLoader, *lifecycleHTTPLog) {
	t.Helper()
	log := &lifecycleHTTPLog{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, "Bearer fixture-token", r.Header.Get("Authorization"))
		log.mu.Lock()
		log.requests = append(log.requests, lifecycleHTTPRequest{r.Method, r.URL.Path, r.URL.RawQuery, string(body)})
		log.mu.Unlock()
		if handle != nil && handle(w, r) {
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == lifecycleAPIPath+"/"+item.ID {
			lifecycleEnvelope(w, item)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == lifecycleAPIPath {
			items := []clientwatchers.Watcher{}
			if (r.URL.Query().Get("archived") == "true") == (item.ArchivedAt != nil) {
				items = append(items, item)
			}
			lifecycleEnvelope(w, map[string]any{"agents": items})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("current-context: test\ncontexts:\n  test:\n    grafana:\n      server: "+server.URL+"\n      token: fixture-token\n      stack-id: 12345\n"), 0600))
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(path)
	return loader, log
}

func decodeLifecycleOutput(t *testing.T, out *bytes.Buffer, value any) {
	t.Helper()
	decoder := yaml.NewYAMLOrJSONDecoder(out, 4096)
	require.NoError(t, decoder.Decode(value))
	var extra any
	require.ErrorIs(t, decoder.Decode(&extra), io.EOF, "finite commands emit exactly one result")
}

func TestPublicLifecycleRoutes(t *testing.T) {
	testutils.SetAgentMode(t, false)
	actions := map[string]string{"start": "started", "pause": "paused", "run": "run-requested", "archive": "archived", "unarchive": "unarchived"}
	for _, verb := range []string{"start", "pause", "run", "archive", "unarchive", "list-runs"} {
		t.Run(verb, func(t *testing.T) {
			item := savedCommandFixture()
			item.Status = "paused"
			if verb == "unarchive" {
				at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
				item.ArchivedAt = &at
			}
			suffix, method := verb, http.MethodPost
			switch verb {
			case "unarchive":
				suffix = "restore"
			case "run":
				suffix = "runs"
			case "list-runs":
				suffix, method = "runs", http.MethodGet
			}
			response := item
			response.ArchivedAt = nil
			if verb == "start" {
				response.Status = "running"
			}
			if verb == "archive" {
				at := time.Now().UTC()
				response.ArchivedAt = &at
			}
			loader, log := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != lifecycleAPIPath+"/"+item.ID+"/"+suffix {
					return false
				}
				switch verb {
				case "run":
					lifecycleEnvelope(w, map[string]any{"runId": "queued-task"})
				case "list-runs":
					lifecycleEnvelope(w, map[string]any{"runs": []any{}})
				default:
					lifecycleEnvelope(w, response)
				}
				return true
			})
			ref := item.ID
			if verb == "start" {
				ref = "checkout-health"
			}
			cmd, out, _ := mutationCommand(t, loader, []string{verb, ref, "-o", "json"}, "")
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			requests := log.snapshot()
			last := requests[len(requests)-1]
			require.Equal(t, method, last.Method)
			require.Equal(t, lifecycleAPIPath+"/"+item.ID+"/"+suffix, last.Path)
			require.Empty(t, last.Body, "lifecycle actions have no body")
			if verb == "list-runs" {
				require.Equal(t, "limit=21&offset=0", last.Query)
				var result historyResult
				decodeLifecycleOutput(t, out, &result)
				require.NotNil(t, result.Items)
				require.Empty(t, result.Items)
				require.Equal(t, 20, result.Coverage.Limit)
				return
			}
			var result lifecycleResult
			decodeLifecycleOutput(t, out, &result)
			require.Equal(t, "gcx.mutation", result.Type)
			require.Equal(t, "1", result.SchemaVersion)
			require.Equal(t, actions[verb], result.Action)
			require.Equal(t, item.ID, result.Target.ID)
			require.Equal(t, "checkout-health", result.Target.Name)
			require.Equal(t, "accepted", result.Outcome)
			require.Nil(t, result.Changed, "acceptance alone does not establish a state change")
			if verb == "run" {
				require.Equal(t, "queued-task", result.TaskID)
				require.Equal(t, "unavailable", result.Correlation)
				require.Empty(t, result.Lifecycle, "enqueue acceptance does not establish execution state")
			} else {
				require.Equal(t, response.Status, result.Lifecycle)
				require.NotNil(t, result.Archived)
				require.Equal(t, response.ArchivedAt != nil, *result.Archived)
				require.Equal(t, "response", result.Observation)
			}
		})
	}
}

func TestPublicLifecycleRejectsWrongReadIdentity(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, verb := range []string{"start", "pause", "run", "archive", "unarchive", "list-runs"} {
		t.Run(verb, func(t *testing.T) {
			item := savedCommandFixture()
			loader, log := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method == http.MethodGet && r.URL.Path == lifecycleAPIPath+"/"+item.ID {
					wrong := item
					wrong.ID = "22222222-2222-4222-8222-222222222222"
					lifecycleEnvelope(w, wrong)
					return true
				}
				return false
			})
			cmd, out, _ := mutationCommand(t, loader, []string{verb, item.ID, "-o", "json"}, "")
			require.ErrorContains(t, cmd.ExecuteContext(t.Context()), "unexpected resource identity")
			require.Empty(t, out.String())
			require.Len(t, log.snapshot(), 1, "mismatched reads must never dispatch a domain operation")
		})
	}
}

func TestPublicLifecycleStaticValidationBeforeIO(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, verb := range []string{"start", "pause", "run", "archive", "unarchive", "list-runs"} {
		for _, tc := range []struct {
			name string
			args []string
		}{
			{"missing reference", nil},
			{"extra reference", []string{"one", "two"}},
			{"empty reference", []string{""}},
			{"blank reference", []string{" \t"}},
			{"invalid output", []string{"checkout-health", "-o", "invalid"}},
			{"empty output", []string{"checkout-health", "--output="}},
		} {
			t.Run(verb+"/"+tc.name, func(t *testing.T) {
				loader, log := lifecycleCommandFixture(t, savedCommandFixture(), nil)
				cmd, out, _ := mutationCommand(t, loader, append([]string{verb}, tc.args...), "")
				require.Error(t, cmd.ExecuteContext(t.Context()))
				require.Empty(t, out.String())
				require.Empty(t, log.snapshot(), "invalid static inputs must not load a remote identity")
			})
		}
	}
	for _, limit := range []string{"0", "-1", "101", "", "invalid"} {
		t.Run("limit="+limit, func(t *testing.T) {
			loader, log := lifecycleCommandFixture(t, savedCommandFixture(), nil)
			cmd, out, _ := mutationCommand(t, loader, []string{"list-runs", "checkout-health", "--limit=" + limit}, "")
			err := cmd.ExecuteContext(t.Context())
			require.Error(t, err)
			if limit == "0" || limit == "-1" || limit == "101" {
				var detailed *gcxerrors.DetailedError
				require.ErrorAs(t, err, &detailed)
				require.NotNil(t, detailed.ExitCode)
				require.Equal(t, gcxerrors.ExitUsageError, *detailed.ExitCode)
			}
			require.Empty(t, out.String())
			require.Empty(t, log.snapshot())
		})
	}
}

func TestPublicLifecycleCancellationDuringRequest(t *testing.T) {
	testutils.SetAgentMode(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	item := savedCommandFixture()
	item.Status = "paused"
	loader, log := lifecycleCommandFixture(t, item, func(_ http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPost {
			return false
		}
		cancel()
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
			t.Error("canceling the command did not cancel its HTTP request")
		}
		return true
	})
	cmd, out, _ := mutationCommand(t, loader, []string{"start", item.ID, "-o", "json"}, "")
	err := cmd.ExecuteContext(ctx)
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	require.Equal(t, gcxerrors.ExitCancelled, emitted.Code)
	require.ErrorIs(t, err, context.Canceled)
	var result lifecycleResult
	decodeLifecycleOutput(t, out, &result)
	require.Equal(t, "unknown", result.Outcome, "cancellation does not establish whether the server committed the action")
	require.Equal(t, item.ID, result.Target.ID)
	require.Equal(t, "checkout-health", result.Target.Name)
	require.Equal(t, "unavailable", result.Observation)
	require.Empty(t, result.Lifecycle)
	require.NotEmpty(t, result.Error)
	requests := log.snapshot()
	require.Len(t, requests, 2, "cancellation must not retry or perform a remote readback")
	require.Equal(t, http.MethodPost, requests[1].Method)
	require.Equal(t, lifecycleAPIPath+"/"+item.ID+"/start", requests[1].Path)
}

func TestPublicLifecycleRefusesUnsafePreconditions(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, tc := range []struct{ verb, state string }{{"archive", "running"}, {"archive", "draft"}, {"archive", "calibrating"}, {"unarchive", "running"}, {"unarchive", "paused"}} {
		t.Run(tc.verb+"/"+tc.state, func(t *testing.T) {
			item := savedCommandFixture()
			item.Status = tc.state
			loader, log := lifecycleCommandFixture(t, item, nil)
			cmd, out, _ := mutationCommand(t, loader, []string{tc.verb, item.ID, "-o", "json"}, "")
			require.ErrorContains(t, cmd.ExecuteContext(t.Context()), "requires")
			require.Empty(t, out.String())
			require.Equal(t, []lifecycleHTTPRequest{{Method: http.MethodGet, Path: lifecycleAPIPath + "/" + item.ID}}, log.snapshot(), "refusal must neither mutate nor repair state")
		})
	}
}

func TestPublicRunAcceptanceIsNotCompletion(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, tc := range []struct {
		name, response, outcome, taskID string
		failure                         bool
	}{
		{"accepted", `{"runId":"enqueue-task"}`, "accepted", "enqueue-task", false},
		{"skipped", `{"skipped":true}`, "skipped", "", false},
		{"unconfirmed", `{}`, "unknown", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := savedCommandFixture()
			item.Status = "paused" // Readiness remains a server decision.
			loader, log := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method != http.MethodPost {
					return false
				}
				lifecycleEnvelope(w, json.RawMessage(tc.response))
				return true
			})
			cmd, out, _ := mutationCommand(t, loader, []string{"run", item.ID, "-o", "json"}, "")
			err := cmd.ExecuteContext(t.Context())
			if tc.failure {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			var result lifecycleResult
			decodeLifecycleOutput(t, out, &result)
			require.Equal(t, "run-requested", result.Action)
			require.Equal(t, tc.outcome, result.Outcome)
			require.Equal(t, tc.taskID, result.TaskID)
			require.Equal(t, "unavailable", result.Correlation)
			require.Empty(t, result.Lifecycle)
			require.Nil(t, result.Archived)
			require.Contains(t, result.Guidance, map[bool]string{false: "not scan completion", true: "before retrying"}[tc.failure])
			require.Len(t, log.snapshot(), 2, "a run request must not poll or guess which history entry belongs to it")
		})
	}
}

func TestPublicLifecycleFailuresPreserveObservedState(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, tc := range []struct {
		name, response, outcome string
		status, exit            int
	}{
		{"start failed after activation", `{"message":"sentinel-secret"}`, "unknown", 500, gcxerrors.ExitGeneralError},
		{"permission denied", `{"message":"sentinel-secret"}`, "rejected", 403, gcxerrors.ExitAuthFailure},
		{"malformed success", `{"data":`, "unknown", 200, gcxerrors.ExitGeneralError},
		{"contradictory success", `{"data":{"id":"11111111-1111-4111-8111-111111111111","name":"Checkout health","status":"paused"}}`, "unknown", 200, gcxerrors.ExitGeneralError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := savedCommandFixture()
			item.Status = "paused"
			var requested atomic.Bool
			loader, log := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method == http.MethodPost {
					requested.Store(true)
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.response)
					return true
				}
				if requested.Load() && r.URL.Path == lifecycleAPIPath+"/"+item.ID {
					observed := item
					observed.Status = "running"
					lifecycleEnvelope(w, observed)
					return true
				}
				return false
			})
			cmd, out, diagnostic := mutationCommand(t, loader, []string{"start", item.ID, "-o", "json"}, "")
			err := cmd.ExecuteContext(t.Context())
			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted)
			require.Equal(t, tc.exit, emitted.Code)
			require.NotContains(t, out.String()+diagnostic.String()+err.Error(), "sentinel-secret")
			var result lifecycleResult
			decodeLifecycleOutput(t, out, &result)
			require.Equal(t, tc.outcome, result.Outcome)
			require.Equal(t, item.ID, result.Target.ID)
			require.Equal(t, "running", result.Lifecycle)
			require.Equal(t, "readback", result.Observation)
			require.NotEmpty(t, result.Error)
			require.Contains(t, result.Guidance, "before retrying")
			require.Len(t, log.snapshot(), 3, "one action and one best-effort readback; no retry or rollback")
		})
	}
}

func TestPublicRunHistorySelectionRetainsCoverage(t *testing.T) {
	testutils.SetAgentMode(t, false)
	item := savedCommandFixture()
	loader, log := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/runs") {
			return false
		}
		lifecycleEnvelope(w, map[string]any{"runs": []map[string]any{
			{"id": "failed-run", "status": "failed", "assessment": "ok", "startedAt": "2026-01-02T03:04:05Z", "definitionVersion": 7},
			{"id": "running-run", "status": "running", "assessment": "ok", "startedAt": "2026-01-02T02:04:05Z", "definitionVersion": 6},
			{"id": "older-run", "status": "completed", "assessment": "ok", "startedAt": "2026-01-02T01:04:05Z", "definitionVersion": 5},
		}})
		return true
	})
	cmd, out, _ := mutationCommand(t, loader, []string{"list-runs", item.ID, "--limit", "2", "--json", "id,status,assessment"}, "")
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	var result struct {
		Items       []map[string]any `json:"items"`
		Coverage    historyCoverage  `json:"coverage"`
		Correlation string           `json:"correlation"`
	}
	decodeLifecycleOutput(t, out, &result)
	require.Len(t, result.Items, 2)
	require.Equal(t, "failed", result.Items[0]["status"])
	require.Equal(t, "running", result.Items[1]["status"])
	for _, run := range result.Items {
		require.Empty(t, run["assessment"], "unfinished or failed runs cannot reuse a stale healthy assessment")
		require.NotContains(t, run, "startedAt", "field selection applies to items")
	}
	require.Equal(t, historyCoverage{Scope: "caller-visible-retained", Limit: 2, Returned: 2, HasMore: true, Snapshot: false}, result.Coverage)
	require.Equal(t, "unavailable", result.Correlation)
	requests := log.snapshot()
	require.Len(t, requests, 2)
	require.Equal(t, "limit=3&offset=0", requests[1].Query, "the CLI limit controls bounded lookahead")
}

func TestPublicRunPermissionDenial(t *testing.T) {
	testutils.SetAgentMode(t, false)
	item := savedCommandFixture()
	loader, log := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPost {
			return false
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"sentinel-secret"}`)
		return true
	})
	cmd, out, diagnostic := mutationCommand(t, loader, []string{"run", item.ID, "-o", "json"}, "")
	err := cmd.ExecuteContext(t.Context())
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	require.Equal(t, gcxerrors.ExitAuthFailure, emitted.Code)
	require.NotContains(t, out.String()+diagnostic.String()+err.Error(), "sentinel-secret")
	var result lifecycleResult
	decodeLifecycleOutput(t, out, &result)
	require.Equal(t, "rejected", result.Outcome)
	require.Contains(t, result.Error, "create and write permissions")
	require.Empty(t, result.Lifecycle)
	require.Len(t, log.snapshot(), 2)
}

func TestPublicUnarchiveConcurrentActivation(t *testing.T) {
	testutils.SetAgentMode(t, false)
	item := savedCommandFixture()
	item.Status = "paused"
	at := time.Now().UTC()
	item.ArchivedAt = &at
	var requested atomic.Bool
	loader, log := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodPost {
			requested.Store(true)
		} else if !requested.Load() {
			return false
		}
		active := item
		active.Status, active.ArchivedAt = "running", nil
		lifecycleEnvelope(w, active)
		return true
	})
	cmd, out, _ := mutationCommand(t, loader, []string{"unarchive", item.ID, "-o", "json"}, "")
	require.Error(t, cmd.ExecuteContext(t.Context()), "recovery must not claim inactive monitoring after a concurrent start")
	var result lifecycleResult
	decodeLifecycleOutput(t, out, &result)
	require.Equal(t, "unknown", result.Outcome)
	require.Equal(t, "running", result.Lifecycle)
	require.NotNil(t, result.Archived)
	require.False(t, *result.Archived)
	requests := log.snapshot()
	require.Len(t, requests, 3)
	require.Equal(t, lifecycleAPIPath+"/"+item.ID+"/restore", requests[1].Path)
	require.Equal(t, http.MethodGet, requests[2].Method, "never pause to repair a concurrent activation")
}

func TestPublicEmptyRunHistoryMachineCodecs(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, output := range []string{"json", "yaml", "agents"} {
		t.Run(output, func(t *testing.T) {
			item := savedCommandFixture()
			loader, _ := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
				if !strings.HasSuffix(r.URL.Path, "/runs") {
					return false
				}
				lifecycleEnvelope(w, map[string]any{"runs": []any{}})
				return true
			})
			cmd, out, _ := mutationCommand(t, loader, []string{"list-runs", item.ID, "-o", output}, "")
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			var result historyResult
			decodeLifecycleOutput(t, out, &result)
			require.NotNil(t, result.Items, "empty collections must remain [] in every machine codec")
			require.Empty(t, result.Items)
			require.Zero(t, result.Coverage.Returned)
			require.False(t, result.Coverage.HasMore)
		})
	}
}

func TestPublicLifecycleAndHistoryCodecs(t *testing.T) {
	for _, command := range []string{"pause", "list-runs"} {
		var baseline map[string]any
		for _, output := range []string{"json", "yaml", "agents", "text", "table", "wide", "agent-default"} {
			t.Run(command+"/"+output, func(t *testing.T) {
				testutils.SetAgentMode(t, output == "agent-default")
				item := savedCommandFixture()
				item.Status = "running"
				loader, _ := lifecycleCommandFixture(t, item, func(w http.ResponseWriter, r *http.Request) bool {
					if strings.HasSuffix(r.URL.Path, "/pause") {
						paused := item
						paused.Status = "paused"
						lifecycleEnvelope(w, paused)
						return true
					}
					if strings.HasSuffix(r.URL.Path, "/runs") {
						lifecycleEnvelope(w, map[string]any{"runs": []map[string]any{{"id": "failed-run", "status": "failed", "assessment": "ok", "startedAt": "2026-01-02T03:04:05Z", "definitionVersion": 7}}})
						return true
					}
					return false
				})
				args := []string{command, item.ID}
				if output != "agent-default" {
					args = append(args, "-o", output)
				}
				cmd, out, _ := mutationCommand(t, loader, args, "")
				require.NoError(t, cmd.ExecuteContext(t.Context()))
				if output == "text" || output == "table" || output == "wide" {
					if command == "pause" {
						require.Contains(t, out.String(), "accepted")
						require.Contains(t, out.String(), "paused")
						require.Contains(t, out.String(), item.ID)
					} else {
						require.Contains(t, out.String(), "Watcher: checkout-health (ID "+item.ID+")")
						require.Contains(t, out.String(), "failed-run")
						require.Contains(t, out.String(), "failed")
						require.Contains(t, out.String(), "Coverage:")
						require.Contains(t, out.String(), "not an atomic snapshot")
						require.NotContains(t, out.String(), "ok")
					}
					return
				}
				var got map[string]any
				decodeLifecycleOutput(t, out, &got)
				if baseline == nil {
					baseline = got
				} else {
					require.Equal(t, baseline, got, "machine formats must expose the same model")
				}
			})
		}
	}
}
