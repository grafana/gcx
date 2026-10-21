package watchers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

type lifecycleMethod func(*watchers.Client, context.Context, string) (*watchers.Watcher, error)

type lifecycleTestCase struct {
	name, suffix, status string
	archived             bool
	call                 lifecycleMethod
}

func lifecycleMethods() []lifecycleTestCase {
	return []lifecycleTestCase{
		{"start", "start", "running", false, (*watchers.Client).Start},
		{"pause", "pause", "paused", false, (*watchers.Client).Pause},
		{"archive", "archive", "paused", true, (*watchers.Client).Archive},
		{"unarchive", "restore", "paused", false, (*watchers.Client).Unarchive},
	}
}

func TestLifecycleTransportAndResponse(t *testing.T) {
	for _, tc := range lifecycleMethods() {
		t.Run(tc.name, func(t *testing.T) {
			const id = "id/with?characters"
			var calls atomic.Int32
			item := watchers.Watcher{ID: id, Name: "Example", Status: tc.status}
			if tc.archived {
				item.ArchivedAt = new(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
			}
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, basePath+"/id%2Fwith%3Fcharacters/"+tc.suffix, r.URL.EscapedPath())
				assert.Empty(t, r.URL.RawQuery)
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.Empty(t, body)
				writeData(t, w, item)
			}))
			result, err := tc.call(client, t.Context(), id)
			require.NoError(t, err)
			assert.Equal(t, item, *result)
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestLifecycleKeepsUnexpectedObservation(t *testing.T) {
	for _, method := range lifecycleMethods() {
		for _, change := range []string{"different ID", "missing ID", "missing name", "unexpected status", "unexpected archive state"} {
			t.Run(method.name+"/"+change, func(t *testing.T) {
				item := watchers.Watcher{ID: "id", Name: "Example", Status: method.status}
				if method.archived {
					item.ArchivedAt = new(time.Now())
				}
				switch change {
				case "different ID":
					item.ID = "other"
				case "missing ID":
					item.ID = ""
				case "missing name":
					item.Name = " "
				case "unexpected status":
					item.Status = "future-state"
				case "unexpected archive state":
					if method.archived {
						item.ArchivedAt = nil
					} else {
						item.ArchivedAt = new(time.Now())
					}
				}
				client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeData(t, w, item) }))
				result, err := method.call(client, t.Context(), "id")
				require.Error(t, err)
				require.NotNil(t, result)
				assert.Equal(t, item.ID, result.ID)
				assert.Equal(t, item.Status, result.Status)
			})
		}
	}
}

func TestRunRequestAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       watchers.RunRequest
	}{
		{"accepted", `{"runId":"task-id"}`, watchers.RunRequest{TaskID: "task-id"}},
		{"explicit accepted", `{"runId":"task-id","skipped":false}`, watchers.RunRequest{TaskID: "task-id"}},
		{"skipped", `{"skipped":true}`, watchers.RunRequest{Skipped: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, basePath+"/id%2Fwith%3Fcharacters/runs", r.URL.EscapedPath())
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.Empty(t, body)
				_, _ = fmt.Fprintf(w, `{"data":%s}`, tc.body)
			}))
			result, err := client.RequestRun(t.Context(), "id/with?characters")
			require.NoError(t, err)
			assert.Equal(t, tc.want, *result)
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

type mutationTestCase struct {
	name string
	call func(*watchers.Client, context.Context, string) error
}

func mutationCalls() []mutationTestCase {
	methods := make([]mutationTestCase, 0, len(lifecycleMethods())+1)
	for _, method := range lifecycleMethods() {
		methods = append(methods, mutationTestCase{method.name, func(c *watchers.Client, ctx context.Context, id string) error {
			_, err := method.call(c, ctx, id)
			return err
		}})
	}
	return append(methods, mutationTestCase{"run", func(c *watchers.Client, ctx context.Context, id string) error {
		_, err := c.RequestRun(ctx, id)
		return err
	}})
}

func TestLifecycleAndRunMalformedResponses(t *testing.T) {
	for _, method := range mutationCalls() {
		for _, body := range []string{`SENTINEL`, `{}`, `{"data":null}`, `{"data":{}}`, `{"data":[]}`, `{"data":{"runId":" ","skipped":false}}`, `{"data":{"skipped":"SENTINEL","status":123}}`} {
			t.Run(method.name+"/"+body, func(t *testing.T) {
				var calls atomic.Int32
				client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					_, _ = io.WriteString(w, body)
				}))
				err := method.call(client, t.Context(), "id")
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "SENTINEL")
				assert.Equal(t, int32(1), calls.Load())
			})
		}
	}
}

func TestLifecycleAndRunNoRetries(t *testing.T) {
	for _, method := range mutationCalls() {
		for _, status := range []int{400, 401, 403, 404, 409, 429, 500, 501, 503} {
			t.Run(method.name+"/"+strconv.Itoa(status), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"message":"SENTINEL","details":"SENTINEL"}`)
				}))
				t.Cleanup(server.Close)
				base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{
					Host: server.URL,
					WrapTransport: func(base http.RoundTripper) http.RoundTripper {
						return &retry.Transport{Base: base, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
					},
				}})
				require.NoError(t, err)
				err = method.call(watchers.NewClient(base), t.Context(), "id")
				require.Error(t, err)
				apiErr, direct := err.(*watchers.APIError) //nolint:errorlint // API errors must remain direct for the shared error converter.
				require.True(t, direct)
				assert.Equal(t, status, apiErr.HTTPStatusCode())
				assert.NotContains(t, err.Error(), "SENTINEL")
				encoded, encodeErr := json.Marshal(err)
				require.NoError(t, encodeErr)
				assert.NotContains(t, string(encoded), "SENTINEL")
				assert.Equal(t, int32(1), calls.Load())
			})
		}
		t.Run(method.name+"/transport", func(t *testing.T) {
			var calls atomic.Int32
			base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{
				Host: "https://example.invalid",
				WrapTransport: func(http.RoundTripper) http.RoundTripper {
					return &retry.Transport{Base: calibrationRoundTripper(func(*http.Request) (*http.Response, error) {
						calls.Add(1)
						return nil, io.ErrUnexpectedEOF
					}), MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
				},
			}})
			require.NoError(t, err)
			err = method.call(watchers.NewClient(base), t.Context(), "id")
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func historyRun(id int) watchers.Run {
	return watchers.Run{ID: fmt.Sprintf("run-%d", id), Status: "completed", Assessment: "ok", StartedAt: time.Date(2030, 1, 1, 0, 0, id, 0, time.UTC), DefinitionVersion: 2}
}

func TestRunHistoryBoundedPagination(t *testing.T) {
	for _, tc := range []struct {
		name           string
		limit, total   int
		wantMore       bool
		wantPageLimits []string
	}{
		{"empty", 20, 0, false, []string{"21:0"}},
		{"less than N", 20, 19, false, []string{"21:0"}},
		{"exact N", 20, 20, false, []string{"21:0"}},
		{"N plus one", 20, 21, true, []string{"21:0"}},
		{"minimum", 1, 2, true, []string{"2:0"}},
		{"below maximum", 99, 100, true, []string{"100:0"}},
		{"maximum short", 100, 99, false, []string{"100:0"}},
		{"maximum exact", 100, 100, false, []string{"100:0", "1:100"}},
		{"maximum with more", 100, 101, true, []string{"100:0", "1:100"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []string
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, basePath+"/id%2Fwith%3Fcharacters/runs", r.URL.EscapedPath())
				assert.Len(t, r.URL.Query(), 2)
				limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
				assert.NoError(t, err)
				offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
				assert.NoError(t, err)
				requests = append(requests, fmt.Sprintf("%d:%d", limit, offset))
				items := make([]watchers.Run, 0)
				for i := offset; i < min(tc.total, offset+limit); i++ {
					items = append(items, historyRun(i))
				}
				writeData(t, w, map[string]any{"runs": items})
			}))
			result, err := client.ListRuns(t.Context(), "id/with?characters", tc.limit)
			require.NoError(t, err)
			require.NotNil(t, result.Items)
			assert.Len(t, result.Items, min(tc.limit, tc.total))
			assert.Equal(t, tc.wantMore, result.HasMore)
			assert.Equal(t, tc.wantPageLimits, requests)
		})
	}
}

func TestRunHistoryScalarProjection(t *testing.T) {
	client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeData(t, w, map[string]any{"runs": []map[string]any{
			{"id": "done", "status": "completed", "assessment": "warning", "startedAt": "2030-01-01T00:00:00Z", "finishedAt": "2030-01-01T00:01:00Z", "trigger": "manual", "definitionVersion": 4, "report": "SENTINEL", "error": "SENTINEL", "actions": map[string]any{"secret": "SENTINEL"}},
			{"id": "failed", "status": "failed", "assessment": "ok", "startedAt": "2030-01-01T00:00:00Z", "error": "SENTINEL"},
			{"id": "running", "status": "running", "assessment": "ok", "startedAt": "2030-01-01T00:00:00Z"},
			{"id": "unknown", "status": "future-state", "assessment": "ok", "startedAt": "2030-01-01T00:00:00Z"},
			{"id": "unknown assessment", "status": "completed", "assessment": "future-assessment", "startedAt": "2030-01-01T00:00:00Z"},
		}})
	}))
	result, err := client.ListRuns(t.Context(), "id", 20)
	require.NoError(t, err)
	require.Len(t, result.Items, 5)
	assert.Equal(t, "warning", result.Items[0].Assessment)
	assert.Equal(t, "manual", result.Items[0].Trigger)
	assert.Equal(t, int64(4), result.Items[0].DefinitionVersion)
	require.NotNil(t, result.Items[0].FinishedAt)
	for _, item := range result.Items[1:] {
		assert.Empty(t, item.Assessment)
	}
	assert.Equal(t, "future-state", result.Items[3].Status)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "SENTINEL")
	assert.NotContains(t, string(encoded), "report")
	assert.NotContains(t, string(encoded), "error")
	assert.NotContains(t, string(encoded), "actions")
}

func TestRunHistoryRejectsIncompleteResponse(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"data":null}`, `{"data":{}}`, `{"data":{"runs":null}}`, `{"data":{"runs":{}}}`,
		`{"data":{"runs":[{}]}}`,
		`{"data":{"runs":[{"id":"id","status":"completed"}]}}`,
		`{"data":{"runs":[{"id":" ","status":"completed","startedAt":"2030-01-01T00:00:00Z"}]}}`,
		`{"data":{"runs":[{"id":"id","status":" ","startedAt":"2030-01-01T00:00:00Z"}]}}`,
		`{"data":{"runs":[{"id":"id","status":"completed","startedAt":"SENTINEL"}]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
			result, err := client.ListRuns(t.Context(), "id", 20)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.NotContains(t, err.Error(), "SENTINEL")
		})
	}
}

func TestRunHistoryFailedProbeCannotClaimCompleteness(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"data":{}}`, 200}, {`{"data":{"runs":null}}`, 200}, {`SENTINEL`, 200}, {`{"message":"SENTINEL"}`, 403},
		{`{"data":{"runs":[{}]}}`, 200},
		{`{"data":{"runs":[{},{}]}}`, 200},
	} {
		t.Run(tc.body, func(t *testing.T) {
			var calls atomic.Int32
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Query().Get("offset") == "100" {
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
					return
				}
				items := make([]watchers.Run, 100)
				for i := range items {
					items[i] = historyRun(i)
				}
				writeData(t, w, map[string]any{"runs": items})
			}))
			result, err := client.ListRuns(t.Context(), "id", 100)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Equal(t, int32(2), calls.Load())
		})
	}
}

func TestLifecycleRunInputsSendNothing(t *testing.T) {
	var calls atomic.Int32
	client := mutationClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	for _, method := range mutationCalls() {
		for _, id := range []string{"", " "} {
			require.Error(t, method.call(client, t.Context(), id))
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.ErrorIs(t, method.call(client, ctx, "id"), context.Canceled)
	}
	for _, limit := range []int{-1, 0, 101} {
		_, err := client.ListRuns(t.Context(), "id", limit)
		require.Error(t, err)
	}
	for _, id := range []string{"", " "} {
		_, err := client.ListRuns(t.Context(), id, 20)
		require.Error(t, err)
	}
	assert.Zero(t, calls.Load())
}

func TestLifecycleOperationSpecificErrors(t *testing.T) {
	for _, tc := range []struct {
		name, operation string
		status          int
		kind            error
		want            string
	}{
		{"read permission", "list", 403, watchers.ErrPermissionDenied, "Watcher read permission"},
		{"write permission", "start", 403, watchers.ErrPermissionDenied, "Watcher write permission"},
		{"run permission", "run", 403, watchers.ErrPermissionDenied, "Watcher create and write permissions"},
		{"start readiness", "start", 400, watchers.ErrInvalidConfiguration, "readiness or request validation failed"},
		{"run readiness", "run", 422, watchers.ErrInvalidConfiguration, "readiness or request validation failed"},
		{"pause conflict", "pause", 409, watchers.ErrConflict, "finish or cancel calibration"},
		{"restore conflict", "unarchive", 409, watchers.ErrConflict, "inspect current status"},
		{"unavailable", "start", 501, watchers.ErrCapabilityUnavailable, "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"message":"SENTINEL","details":"SENTINEL"}`)
			}))
			var err error
			if tc.operation == "list" {
				_, err = client.ListRuns(t.Context(), "id", 20)
			} else {
				for _, method := range mutationCalls() {
					if method.name == tc.operation {
						err = method.call(client, t.Context(), "id")
					}
				}
			}
			require.ErrorIs(t, err, tc.kind)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), "before editing")
			assert.NotContains(t, err.Error(), "SENTINEL")
		})
	}
}

func TestLifecycleKnownRefusalsUseFixedReasons(t *testing.T) {
	for _, tc := range []struct {
		name, operation, message, want string
		status                         int
	}{
		{"draft", "start", "watcher agent must be calibrated with at least one enabled query before starting: agent is draft", "Watcher is a draft", 400},
		{"uncalibrated", "start", "watcher agent must be calibrated with at least one enabled query before starting: it must be calibrated at least once", "Watcher has not been calibrated", 400},
		{"no enabled checks", "start", "watcher agent must be calibrated with at least one enabled query before starting: at least one enabled query is required", "Watcher has no enabled checks", 400},
		{"expired", "start", "stop time has passed; extend or remove it before starting", "automatic-stop deadline has expired", 409},
		{"calibrating start", "start", "agent is calibrating", "wait for calibration to finish", 400},
		{"calibrating pause", "pause", "the watcher is calibrating and does not scan until calibration ends", "wait for calibration to finish", 409},
		{"archived", "run", "restore the archived watcher first", "unarchive the Watcher", 409},
		{"calibrating run", "run", "agent is calibrating and cannot be run", "wait for calibration to finish", 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"message": tc.message + " SENTINEL", "details": "SENTINEL"}))
			}))
			var err error
			for _, method := range mutationCalls() {
				if method.name == tc.operation {
					err = method.call(client, t.Context(), "id")
				}
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), "SENTINEL")
		})
	}
}

func TestLifecycleUnknownRefusalVariationsUseSafeFallback(t *testing.T) {
	for _, message := range []string{
		"watcher is draft",
		"has not been calibrated",
		"not calibrated",
		"no enabled queries",
		"no enabled checks",
		"at least one query must be enabled",
		"stop deadline has expired",
		"auto-stop has expired",
		"automatic stop has expired",
		"stop time has passed",
		"watcher is calibrating",
		"finish or cancel calibration",
		"watcher is archived",
		"agent is archived",
		"missing datasource",
		"requires a datasource",
		"definition changed",
		"definition version",
		"SENTINEL",
	} {
		t.Run(message, func(t *testing.T) {
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"message": message + " SENTINEL", "details": "SENTINEL"}))
			}))
			_, err := client.Start(t.Context(), "id")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "Watcher readiness or request validation failed; inspect calibration, enabled checks, and the automatic-stop deadline")
			assert.NotContains(t, err.Error(), message)
			assert.NotContains(t, err.Error(), "SENTINEL")
			encoded, encodeErr := json.Marshal(err)
			require.NoError(t, encodeErr)
			assert.NotContains(t, string(encoded), "SENTINEL")
		})
	}
}
