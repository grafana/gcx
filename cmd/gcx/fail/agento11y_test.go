package fail_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgento11yPluginUnavailableAtCommandBoundary(t *testing.T) {
	for _, message := range []string{"Plugin not found", "Plugin not found with id: grafana-agento11y-app"} {
		t.Run(message, func(t *testing.T) {
			resp := &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(message))}
			err := agento11yhttp.HandleErrorResponse(resp)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "\n")
			assert.NotContains(t, err.Error(), "\x1b")
			detailed := fail.ErrorToDetailedError(fmt.Errorf("read experiment: %w", err))
			require.NotNil(t, detailed)
			assert.Equal(t, gcxerrors.SummaryEndpointNotAvailable, detailed.Summary)
			assert.Contains(t, detailed.Details, "read experiment")
			assert.Contains(t, detailed.Details, "Agent Observability")
			assert.Contains(t, detailed.Details, "grafana-agento11y-app")
			assert.Contains(t, strings.Join(detailed.Suggestions, " "), "gcx config current-context")
			assert.Contains(t, detailed.Suggestions, "Check that Agent Observability is installed and enabled: gcx api /api/plugins/grafana-agento11y-app/settings")
			var statusErr *gcxerrors.HTTPStatusError
			require.ErrorAs(t, detailed, &statusErr)
			assert.Equal(t, http.StatusNotFound, statusErr.Status)
		})
	}
}
