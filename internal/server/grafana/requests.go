package grafana

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

func AuthenticateAndProxyHandler(restCfg config.NamespacedRESTConfig) http.HandlerFunc {
	// Build the client once. rest.HTTPClientFor re-runs the REST config's
	// WrapTransport, and in OAuth mode that closure assigns Base on the single
	// shared RefreshTransport, so constructing per request races concurrent
	// proxy requests on a field RefreshTransport reads without its mutex.
	httpClient := sync.OnceValues(func() (*http.Client, error) {
		client, err := rest.HTTPClientFor(&restCfg.Config)
		if err != nil {
			return nil, err
		}
		client.Timeout = 10 * time.Second
		client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
			// Being redirected to the login page means authentication is misconfigured.
			// We interrupt the redirect and let the rest of AuthenticateAndProxyHandler
			// handle that case.
			if strings.HasSuffix(req.URL.Path, "/login") {
				return http.ErrUseLastResponse
			}

			return nil
		}

		return client, nil
	})

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html")

		target := strings.TrimSuffix(restCfg.Host, "/") + r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
		if err != nil {
			httputils.Error(r, w, http.StatusText(http.StatusInternalServerError), err, http.StatusInternalServerError)
			return
		}

		client, err := httpClient()
		if err != nil {
			httputils.Error(r, w, "Grafana transport configuration error", err, http.StatusInternalServerError)
			return
		}

		resp, err := client.Do(req)
		if err != nil {
			httputils.Error(r, w, http.StatusText(http.StatusInternalServerError), err, http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusFound {
			w.WriteHeader(http.StatusUnauthorized)
			httputils.Write(r, w, []byte(`<html>
<body style="margin-top: 3rem; color: hsla(225deg, 15%, 90%, 0.82);">
	<h1>Authentication error</h1>
	<p>It appears that the Grafana credentials in your configuration are missing or incorrect.</p>
</body>
</html>`))
			return
		}

		body, _ := io.ReadAll(resp.Body)
		w.WriteHeader(resp.StatusCode)
		httputils.Write(r, w, body)
	}
}
