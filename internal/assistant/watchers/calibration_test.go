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

func TestRequestInitialCalibrationTransportAndProjection(t *testing.T) {
	var calls atomic.Int32
	client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, basePath+"/id%2Fwith%3Fcharacters/initial-calibration", r.URL.EscapedPath())
		assert.Empty(t, r.URL.RawQuery)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.JSONEq(t, `{"startWhenReady":false}`, string(body))
		writeData(t, w, map[string]any{
			"status": "queued", "startWhenReady": false, "message": "Queued", "startedAt": "2030-01-01T00:00:00Z",
			"activity": map[string]any{"chatId": "chat", "taskId": "task"}, "unmodeled": "ignored",
		})
	}))
	result, err := client.RequestInitialCalibration(t.Context(), "id/with?characters")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "queued", result.Status)
	assert.Equal(t, "Queued", result.Message)
	require.NotNil(t, result.StartedAt)
	assert.Equal(t, "2030-01-01T00:00:00Z", result.StartedAt.Format(time.RFC3339))
	require.NotNil(t, result.Activity)
	assert.Equal(t, "chat", result.Activity.ChatID)
	assert.Equal(t, "task", result.Activity.TaskID)
	assert.Equal(t, int32(1), calls.Load())
}

func TestRequestInitialCalibrationAcceptsImmediateStates(t *testing.T) {
	for _, state := range []string{"queued", "running", "completed", "failed", "cancelled", "needs_input"} {
		t.Run(state, func(t *testing.T) {
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeData(t, w, map[string]any{"status": state, "startWhenReady": false})
			}))
			result, err := client.RequestInitialCalibration(t.Context(), "id")
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, state, result.Status)
		})
	}
}

func TestRequestInitialCalibrationRejectsUncertainResponses(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"invalid JSON", `SENTINEL`},
		{"missing data", `{}`},
		{"null data", `{"data":null}`},
		{"empty data", `{"data":{}}`},
		{"unknown state", `{"data":{"status":"SENTINEL","startWhenReady":false}}`},
		{"idle state", `{"data":{"status":"idle","startWhenReady":false}}`},
		{"missing state", `{"data":{"startWhenReady":false,"message":"SENTINEL"}}`},
		{"empty state", `{"data":{"status":"","startWhenReady":false}}`},
		{"invalid state type", `{"data":{"status":123,"startWhenReady":false,"message":"SENTINEL"}}`},
		{"activation enabled", `{"data":{"status":"queued","startWhenReady":true,"message":"SENTINEL"}}`},
		{"activation missing", `{"data":{"status":"queued","message":"SENTINEL"}}`},
		{"activation null", `{"data":{"status":"queued","startWhenReady":null,"message":"SENTINEL"}}`},
		{"activation invalid type", `{"data":{"status":"queued","startWhenReady":"SENTINEL"}}`},
		{"invalid timestamp", `{"data":{"status":"queued","startWhenReady":false,"startedAt":"SENTINEL"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			result, err := client.RequestInitialCalibration(t.Context(), "id")
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "invalid response")
			assert.NotContains(t, err.Error(), "SENTINEL")
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestRequestInitialCalibrationClassifiesErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		expected error
	}{
		{"not implemented", http.StatusNotImplemented, `{"message":"SENTINEL"}`, watchers.ErrCapabilityUnavailable},
		{"structured unavailable", http.StatusInternalServerError, `{"name":"NOT_IMPLEMENTED","message":"SENTINEL"}`, watchers.ErrCapabilityUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			result, err := client.RequestInitialCalibration(t.Context(), "id")
			require.Error(t, err)
			assert.Nil(t, result)
			if tc.expected != nil {
				require.ErrorIs(t, err, tc.expected)
			}
			var apiErr *watchers.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, tc.status, apiErr.HTTPStatusCode())
			assert.NotContains(t, err.Error(), "SENTINEL")
			encoded, encodeErr := json.Marshal(err)
			require.NoError(t, encodeErr)
			assert.NotContains(t, string(encoded), "SENTINEL")
		})
	}
}

func TestRequestInitialCalibration404ConfirmsSavedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name        string
		readStatus  int
		readBody    string
		cancelRead  bool
		unavailable bool
	}{
		{name: "missing route", readBody: `{"data":{"id":"id","name":"Example"}}`, unavailable: true},
		{name: "missing Watcher", readStatus: http.StatusNotFound},
		{name: "access denied", readStatus: http.StatusForbidden},
		{name: "read failure", readStatus: http.StatusInternalServerError},
		{name: "invalid identity", readBody: `{"data":{"name":"Example"}}`},
		{name: "different identity", readBody: `{"data":{"id":"different","name":"Example"}}`},
		{name: "cancelled read", cancelRead: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var posts, reads atomic.Int32
			client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
					assert.Equal(t, basePath+"/id/initial-calibration", r.URL.Path)
					body, err := io.ReadAll(r.Body)
					assert.NoError(t, err)
					assert.JSONEq(t, `{"startWhenReady":false}`, string(body))
					http.Error(w, "SENTINEL", http.StatusNotFound)
					return
				}
				reads.Add(1)
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, basePath+"/id", r.URL.Path)
				if tc.cancelRead {
					cancel()
					return
				}
				if tc.readStatus != 0 {
					http.Error(w, "SENTINEL", tc.readStatus)
					return
				}
				_, _ = fmt.Fprint(w, tc.readBody)
			}))
			result, err := client.RequestInitialCalibration(ctx, "id")
			require.Error(t, err)
			require.Nil(t, result)
			if tc.unavailable {
				require.ErrorIs(t, err, watchers.ErrCapabilityUnavailable)
				require.NotErrorIs(t, err, watchers.ErrNotFound)
			} else {
				require.ErrorIs(t, err, watchers.ErrNotFound)
				require.NotErrorIs(t, err, watchers.ErrCapabilityUnavailable)
			}
			var apiErr *watchers.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusNotFound, apiErr.HTTPStatusCode())
			assert.Equal(t, "request Watcher initial calibration", apiErr.Operation)
			assert.NotContains(t, err.Error(), "SENTINEL")
			assert.Equal(t, int32(1), posts.Load())
			assert.Equal(t, int32(1), reads.Load())
		})
	}
}

func TestRequestInitialCalibrationInputAndCancellationSendNothing(t *testing.T) {
	var calls atomic.Int32
	client := mutationClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeData(t, w, map[string]any{"status": "queued", "startWhenReady": false})
	}))
	for _, id := range []string{"", " "} {
		result, err := client.RequestInitialCalibration(t.Context(), id)
		require.Error(t, err)
		assert.Nil(t, result)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := client.RequestInitialCalibration(ctx, "id")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, result)
	assert.Zero(t, calls.Load())
}

type calibrationRoundTripper func(*http.Request) (*http.Response, error)

func (f calibrationRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRequestInitialCalibrationTransportFailureRetainsCauseWithoutRetry(t *testing.T) {
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
	result, err := watchers.NewClient(base).RequestInitialCalibration(t.Context(), "id")
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	assert.Nil(t, result)
	assert.Equal(t, int32(1), calls.Load())
}

func TestRequestInitialCalibrationCancellationRetainsCause(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	var calls atomic.Int32
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{
		Host: "https://example.invalid",
		WrapTransport: func(http.RoundTripper) http.RoundTripper {
			return calibrationRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				cancel()
				return nil, r.Context().Err()
			})
		},
	}})
	require.NoError(t, err)
	result, err := watchers.NewClient(base).RequestInitialCalibration(ctx, "id")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, result)
	assert.Equal(t, int32(1), calls.Load())
}

func TestRequestInitialCalibrationNeverRetriesHTTPFailure(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			t.Cleanup(server.Close)
			base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{
				Host: server.URL,
				WrapTransport: func(base http.RoundTripper) http.RoundTripper {
					return &retry.Transport{Base: base, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
				},
			}})
			require.NoError(t, err)
			result, err := watchers.NewClient(base).RequestInitialCalibration(t.Context(), "id")
			require.Error(t, err)
			assert.Nil(t, result)
			var apiErr *watchers.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, status, apiErr.HTTPStatusCode())
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}
