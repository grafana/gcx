package agentping_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agentping"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestSendResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"json accepted", 200, `{}`, ""},
		{"queued", 202, `{"status":"accepted"}`, ""},
		{"no content", 204, "", ""},
		{"field validation", 400, `{"inbox":["Specify the Agents inbox when sending agent metadata."]}`, "Specify the Agents inbox"},
		{"non JSON validation", 400, "<html>private detail</html>", "check the notification fields"},
		{"oversized validation", 400, strings.Repeat("x", 4097), "check the notification fields"},
		{"unauthorized", 401, `{"message":"private detail"}`, "gcx login"},
		{"forbidden", 403, "", "user identity"},
		{"not found", 404, "", "supports mobile notifications"},
		{"rate limited", 429, "", "wait before trying again"},
		{"server failure", 500, "", "HTTP 500"},
		{"redirect", 302, "", "HTTP 302"},
		{"html frontend", 200, "<html>login</html>", "non-JSON"},
		{"too large", 200, strings.Repeat(" ", (1<<20)+1), "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, "/api/cli/v1/proxy/api/plugins/grafana-irm-app/resources/mobile_notifications/self", r.URL.Path)
				w.Header().Set("Location", "/login")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			cfg := config.NamespacedRESTConfig{Config: rest.Config{
				Host:          server.URL + "/api/cli/v1/proxy",
				WrapTransport: httputils.LoggingMiddleware,
			}}
			err := agentping.Send(t.Context(), cfg, agentping.Message{Host: "host", Agent: "agent", Title: "title", Text: "text"})
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
				assert.NotContains(t, err.Error(), "private detail")
				if tc.status >= 300 {
					var statusErr *gcxerrors.HTTPStatusError
					require.ErrorAs(t, err, &statusErr)
					assert.Equal(t, tc.status, statusErr.Status)
				}
			}
			assert.Equal(t, 1, calls)
		})
	}
}
