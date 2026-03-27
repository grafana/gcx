package httputils

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/grafana/gcx/internal/config"
)

func NewTransport(gCtx *config.Context) *http.Transport {
	tlsConfig := &tls.Config{InsecureSkipVerify: false}
	if gCtx.Grafana != nil && gCtx.Grafana.TLS != nil {
		tlsConfig = gCtx.Grafana.TLS.ToStdTLSConfig()
	}

	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConfig,
	}
}

func NewHTTPClient(gCtx *config.Context) (*http.Client, error) {
	return &http.Client{
		Timeout: 10 * time.Second, // TODO: make this configurable maybe?
		Transport: &LoggedHTTPRoundTripper{
			DecoratedTransport: NewTransport(gCtx),
		},
	}, nil
}
