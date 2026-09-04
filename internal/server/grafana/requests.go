package grafana

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/httputils"
)

// AuthenticateAndProxyHandler proxies GET requests to the real Grafana
// instance at target, using transport for auth (Basic, service-account bearer,
// or OAuth with refresh) and TLS — see config.NewNamespacedRESTConfig, which
// builds a transport carrying whichever auth mode the context is configured
// for plus logging and retry.
//
// target is config.NamespacedRESTConfig.ProxyTarget(): its Path is the prefix
// to prepend to each incoming request path (empty in direct mode so the
// Grafana subpath already carried by the request is not doubled; the proxy
// prefix in OAuth mode).
func AuthenticateAndProxyHandler(target *url.URL, transport http.RoundTripper) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html")

		if target == nil || target.Host == "" {
			httputils.Error(r, w, "Error: No Grafana URL configured", errors.New("no Grafana URL configured"), http.StatusBadRequest)
			return
		}

		out := *target
		out.Path = joinProxyPath(target.Path, r.URL.Path)

		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, out.String(), nil)
		if err != nil {
			httputils.Error(r, w, http.StatusText(http.StatusInternalServerError), err, http.StatusInternalServerError)
			return
		}

		client := &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
			CheckRedirect: func(redirReq *http.Request, _ []*http.Request) error {
				// Being redirected to the login page means authentication is misconfigured.
				// We interrupt the redirect and let the rest of AuthenticateAndProxyHandler
				// handle that case.
				if strings.HasSuffix(redirReq.URL.Path, "/login") {
					return http.ErrUseLastResponse
				}

				// A relative redirect from Grafana resolves against the
				// proxy-prefixed request URL and can drop the proxy prefix
				// (OAuth proxy mode). Re-apply it so the followed request still
				// routes through the proxy rather than hitting the backend root.
				if target.Path != "" && redirReq.URL.Host == target.Host && !strings.HasPrefix(redirReq.URL.Path, target.Path) {
					redirReq.URL.Path = joinProxyPath(target.Path, redirReq.URL.Path)
				}

				return nil
			},
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

// joinProxyPath joins a proxy base-path prefix with an incoming request path,
// matching net/http/httputil's single-slash join semantics. An empty base
// returns the request path unchanged, so direct mode (no prefix) never doubles
// the Grafana subpath already carried by reqPath.
func joinProxyPath(base, reqPath string) string {
	switch {
	case base == "":
		return reqPath
	case strings.HasSuffix(base, "/") && strings.HasPrefix(reqPath, "/"):
		return base + reqPath[1:]
	case !strings.HasSuffix(base, "/") && !strings.HasPrefix(reqPath, "/"):
		return base + "/" + reqPath
	default:
		return base + reqPath
	}
}
