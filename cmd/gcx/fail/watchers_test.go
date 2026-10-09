package fail_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/watcher"
	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestWatcherUnavailableConvertedAtCommandBoundary(t *testing.T) {
	for _, operation := range []string{"collection", "calibration", "enrollment", "detail"} {
		t.Run(operation, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotImplemented)
			}))
			t.Cleanup(server.Close)
			base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
			require.NoError(t, err)
			client := watchers.NewClient(base)
			switch operation {
			case "collection":
				_, err = client.ListAll(context.Background(), false)
			case "calibration":
				_, err = client.Calibration(context.Background(), "test-id")
			case "enrollment":
				_, err = client.Enrollment(context.Background(), "test-id")
			case "detail":
				_, err = client.Get(context.Background(), "test-id")
			}
			_, plain := err.(*watchers.APIError) //nolint:errorlint // This contract requires a direct APIError, without wrappers.
			require.True(t, plain, "client must return the API error directly")
			assert.NotContains(t, err.Error(), "\n")
			converted := fail.ErrorToDetailedError(fmt.Errorf("read selected Watcher: %w", err))
			require.NotNil(t, converted)
			assert.Equal(t, "Endpoint not available", converted.Summary)
			assert.Contains(t, converted.Details, "read selected Watcher")
			var apiErr *watchers.APIError
			require.ErrorAs(t, err, &apiErr)
			require.Len(t, converted.Suggestions, 1)
			assert.Contains(t, converted.Suggestions[0], apiErr.Operation)
		})
	}
}

func TestWatcherNotFoundAndPermissionClassification(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		collection bool
		summary    string
	}{
		{"missing collection", http.StatusNotFound, true, "Endpoint not available"},
		{"missing detail", http.StatusNotFound, false, "Assistant Watchers API resource not found"},
		{"denied detail", http.StatusForbidden, false, "Authentication failed querying Assistant Watchers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
			t.Cleanup(server.Close)
			base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
			require.NoError(t, err)
			client := watchers.NewClient(base)
			if tc.collection {
				_, err = client.ListAll(context.Background(), false)
			} else {
				_, err = client.Get(context.Background(), "test-id")
			}
			converted := fail.ErrorToDetailedError(err)
			require.NotNil(t, converted)
			assert.Equal(t, tc.summary, converted.Summary)
			if tc.status == http.StatusForbidden {
				require.NotNil(t, converted.ExitCode)
				assert.Equal(t, 3, *converted.ExitCode)
			}
		})
	}
}

func TestWatcherMixedReadFailuresPreserveSelectedErrorClassification(t *testing.T) {
	failures := make([]watcher.ListFailure, 0, 2)
	for _, status := range []int{http.StatusForbidden, http.StatusNotImplemented} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		t.Cleanup(server.Close)
		base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
		require.NoError(t, err)
		_, err = watchers.NewClient(base).Get(context.Background(), "test-id")
		require.Error(t, err)
		failures = append(failures, watcher.ListFailure{Err: err})
	}

	converted := fail.ErrorToDetailedError(&watcher.ListReadError{Failures: failures})
	require.NotNil(t, converted)
	assert.Equal(t, "Authentication failed querying Assistant Watchers", converted.Summary)
	require.NotNil(t, converted.ExitCode)
	assert.Equal(t, 3, *converted.ExitCode)
}
