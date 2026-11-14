package changereview_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/changereview"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newClient(t *testing.T, status int, body string) *changereview.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "stacks-12345"})
	require.NoError(t, err)
	return changereview.NewClient(base)
}

func TestListChecksClassifiesForbidden(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantNotEnabled bool
	}{
		{name: "feature off", body: `{"message":"Change review checks are not enabled for this stack"}`, wantNotEnabled: true},
		{name: "permission", body: `{"message":"Forbidden"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newClient(t, http.StatusForbidden, tc.body).ListChecks(t.Context())
			require.Error(t, err)
			detailed, isDetailed := err.(*gcxerrors.DetailedError) //nolint:errorlint // classify returns it unwrapped
			assert.Equal(t, tc.wantNotEnabled, isDetailed)
			if tc.wantNotEnabled {
				assert.Contains(t, detailed.Summary, "not enabled")
				return
			}
			forbidden, ok := err.(*assistanthttp.ForbiddenError) //nolint:errorlint // classify returns it unwrapped
			require.True(t, ok)
			assert.Equal(t, "grafana-assistant-app.github:read", forbidden.RequiredPermission())
		})
	}
}
