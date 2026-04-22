package grafana

import (
	"context"
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

func ClientFromContext(ctx *config.Context) (*goapi.GrafanaHTTPAPI, error) {
	if ctx == nil {
		return nil, errors.New("no context provided")
	}
	if ctx.Grafana == nil {
		return nil, errors.New("grafana not configured")
	}

	grafanaURL, err := url.Parse(ctx.Grafana.Server)
	if err != nil {
		return nil, err
	}

	cfg := &goapi.TransportConfig{
		Host:     grafanaURL.Host,
		BasePath: strings.TrimLeft(grafanaURL.Path+"/api", "/"),
		Schemes:  []string{grafanaURL.Scheme},
		HTTPHeaders: map[string]string{
			"User-Agent": version.UserAgent(),
		},
	}

	if ctx.Grafana.TLS != nil {
		cfg.TLSConfig = ctx.Grafana.TLS.ToStdTLSConfig()
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

	return goapi.NewHTTPClientWithConfig(strfmt.Default, cfg), nil
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
	gClient, err := ClientFromContext(cfgCtx)
	if err != nil {
		return nil, "", err
	}
	// Wire the CLI's HTTP client (which carries the --log-http-payload
	// logging transport when enabled) so `gcx ... --log-http-payload` dumps
	// the /api/health request/response alongside every other call.
	gClient.WithHTTPClient(httputils.NewDefaultClient(ctx))

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
