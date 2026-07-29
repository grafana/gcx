package grafana

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

func AuthenticateAndProxyHandler(cfg *config.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html")

		if err := ValidateDevProxyAuth(cfg); err != nil {
			httputils.Error(r, w, err.Error(), err, http.StatusBadRequest)
			return
		}
		restCfg, err := cfg.ToRESTConfig(r.Context())
		if err != nil {
			httputils.Error(r, w, "Grafana authentication configuration error", err, http.StatusBadRequest)
			return
		}

		target := strings.TrimSuffix(restCfg.Host, "/") + r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
		if err != nil {
			httputils.Error(r, w, http.StatusText(http.StatusInternalServerError), err, http.StatusInternalServerError)
			return
		}

		client, err := rest.HTTPClientFor(&restCfg.Config)
		if err != nil {
			httputils.Error(r, w, "Grafana transport configuration error", err, http.StatusInternalServerError)
			return
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

// ValidateDevProxyAuth rejects configurations that the local development
// proxy cannot use without risking credential loss. OAuth refresh-token
// rotation must be wired to the owning config source; this proxy has only a
// resolved Context, so it must fail before constructing or sending a request.
func ValidateDevProxyAuth(cfg *config.Context) error {
	if cfg == nil || cfg.Grafana == nil || cfg.Grafana.Server == "" {
		return errors.New("no Grafana URL configured")
	}
	authMethod, err := cfg.EffectiveGrafanaAuthMethod()
	if err != nil {
		return err
	}
	if authMethod == "oauth" {
		return errors.New("OAuth authentication is not supported by `gcx dev serve` because refreshed credentials cannot be persisted safely; run `gcx login --token <token>` for this context or select a token, Basic, or mTLS context")
	}
	return nil
}
