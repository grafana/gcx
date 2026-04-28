package gcxlib_test

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/gcxlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecute_HelpTree(t *testing.T) {
	result, err := gcxlib.Execute(context.Background(), []string{"help-tree"}, gcxlib.Config{
		GrafanaURL: "https://unused.grafana.net",
		Namespace:  "stacks-1",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Stdout)
	assert.Contains(t, string(result.Stdout), "alert")
	assert.Contains(t, string(result.Stdout), "slo")
}

func TestExecute_InjectsAuthTransport(t *testing.T) {
	var sawAuth atomic.Value
	srv := startTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			sawAuth.Store(auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})

	// Use alert rules list — it's simpler than resources and goes through
	// LoadGrafanaConfig → injected config → HTTP client with our transport.
	_, _ = gcxlib.Execute(context.Background(), []string{"alert", "rules", "list"}, gcxlib.Config{
		GrafanaURL: srv,
		Namespace:  "stacks-42",
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &authTransport{base: rt, token: "test-bearer-token"}
		},
	})

	got, ok := sawAuth.Load().(string)
	require.True(t, ok, "expected at least one HTTP request with Authorization header")
	assert.Equal(t, "Bearer test-bearer-token", got)
}

func TestExecute_UnknownCommand(t *testing.T) {
	_, err := gcxlib.Execute(context.Background(), []string{"nonexistent-command"}, gcxlib.Config{
		GrafanaURL: "https://unused.grafana.net",
		Namespace:  "stacks-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

type authTransport struct {
	base  http.RoundTripper
	token string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

func startTestServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	srv := &http.Server{Handler: mux}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String()
}
