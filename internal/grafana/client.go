package grafana

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-openapi/strfmt"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/version"
	"github.com/grafana/grafana-app-sdk/logging"
	goapi "github.com/grafana/grafana-openapi-client-go/client"
)

// VersionIncompatibleError is returned when a Grafana instance is too old for gcx.
type VersionIncompatibleError struct {
	Version *semver.Version
}

func (e *VersionIncompatibleError) Error() string {
	return fmt.Sprintf("grafana version %s is not supported; gcx requires Grafana 12.0.0 or later", e.Version)
}

// clientResult bundles the goapi client with the resolved *tls.Config so
// callers like GetVersion can reuse it without calling ToStdTLSConfig twice.
type clientResult struct {
	api       *goapi.GrafanaHTTPAPI
	tlsConfig *tls.Config // nil when no TLS is configured
}

func clientFromContextWithTLS(ctx *config.Context) (clientResult, error) {
	if ctx == nil {
		return clientResult{}, errors.New("no context provided")
	}
	if ctx.Grafana == nil {
		return clientResult{}, errors.New("grafana not configured")
	}

	grafanaURL, err := url.Parse(ctx.Grafana.Server)
	if err != nil {
		return clientResult{}, err
	}

	cfg := &goapi.TransportConfig{
		Host:     grafanaURL.Host,
		BasePath: strings.TrimLeft(grafanaURL.Path+"/api", "/"),
		Schemes:  []string{grafanaURL.Scheme},
		HTTPHeaders: map[string]string{
			"User-Agent": version.UserAgent(),
		},
	}

	var stdTLS *tls.Config
	if ctx.Grafana.TLS != nil {
		stdTLS, err = ctx.Grafana.TLS.ToStdTLSConfig()
		if err != nil {
			return clientResult{}, fmt.Errorf("TLS configuration: %w", err)
		}
		cfg.TLSConfig = stdTLS
	}

	// Authentication
	if ctx.Grafana.User != "" && ctx.Grafana.Password != "" {
		cfg.BasicAuth = url.UserPassword(ctx.Grafana.User, ctx.Grafana.Password)
	}
	if ctx.Grafana.APIToken != "" {
		cfg.APIKey = ctx.Grafana.APIToken
	}
	if ctx.Grafana.OrgID != 0 {
		cfg.OrgID = ctx.Grafana.OrgID
	}

	return clientResult{
		api:       goapi.NewHTTPClientWithConfig(strfmt.Default, cfg),
		tlsConfig: stdTLS,
	}, nil
}

// ClientFromContext returns a goapi client configured from the given context.
// The returned client's default transport does NOT include TLS configuration;
// callers that need to wrap the client with middleware via WithHTTPClient
// should use ClientFromContextWithTLS instead to avoid silently losing mTLS.
func ClientFromContext(ctx *config.Context) (*goapi.GrafanaHTTPAPI, error) {
	res, err := clientFromContextWithTLS(ctx)
	if err != nil {
		return nil, err
	}
	return res.api, nil
}

// ClientFromContextWithTLS returns both the goapi client and the resolved
// *tls.Config. Use this when wrapping the client with WithHTTPClient to
// ensure TLS settings are preserved (see GetVersion for an example).
func ClientFromContextWithTLS(ctx *config.Context) (*goapi.GrafanaHTTPAPI, *tls.Config, error) {
	res, err := clientFromContextWithTLS(ctx)
	if err != nil {
		return nil, nil, err
	}
	return res.api, res.tlsConfig, nil
}

// GetVersion returns the Grafana version reported by /api/health.
//
// Return contract:
//   - err != nil: the health request itself failed (unreachable, auth rejected,
//     malformed config). The other return values are empty.
//   - err == nil, parsed == nil, raw == "": the server answered but did not
//     include a version. Grafana Cloud hides the version from anonymous
//     callers as a fingerprinting defense.
//   - err == nil, parsed == nil, raw != "": the server returned a version
//     string that the semver parser rejected (e.g. build-metadata-only
//     strings from some dev deployments). Callers should display raw but
//     cannot range-compare.
//   - err == nil, parsed != nil: fully parseable semver; raw is the
//     original string.
func GetVersion(ctx context.Context, cfgCtx *config.Context) (*semver.Version, string, error) {
	res, err := clientFromContextWithTLS(cfgCtx)
	if err != nil {
		return nil, "", err
	}
	// Wire a CLI HTTP client that carries the --log-http-payload logging
	// transport when enabled, reusing the TLS config already resolved by
	// clientFromContextWithTLS to avoid redundant file reads.
	// WithHTTPClient only replaces the underlying transport, not the
	// goapi auth settings (API key / basic / OrgID).
	gClient := res.api
	gClient.WithHTTPClient(httputils.NewDefaultClientWithTLS(ctx, res.tlsConfig))

	healthResponse, err := gClient.Health.GetHealth()
	if err != nil {
		return nil, "", err
	}

	raw := healthResponse.Payload.Version
	commit := healthResponse.Payload.Commit
	db := healthResponse.Payload.Database
	logging.FromContext(ctx).Debug("grafana health response",
		"server", cfgCtx.Grafana.Server,
		"raw_version", raw,
		"commit", commit,
		"database", db,
		"has_api_token", cfgCtx.Grafana.APIToken != "",
		"has_oauth_token", cfgCtx.Grafana.OAuthToken != "",
	)
	if raw == "" {
		return nil, "", nil
	}
	parsed, parseErr := semver.NewVersion(raw)
	if parseErr != nil {
		// Intentionally discarding parseErr: the health probe succeeded and
		// callers handle a nil parsed version + non-empty raw string as
		// "reachable but version not in a parseable form" (see function doc).
		return nil, raw, nil //nolint:nilerr
	}
	return parsed, raw, nil
}
