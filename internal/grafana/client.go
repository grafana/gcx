package grafana

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/version"
	"github.com/grafana/grafana-app-sdk/logging"
)

const (
	// orgIDHeader selects the organization for a request authenticated as a user.
	orgIDHeader = "X-Grafana-Org-Id"
	// maxResponseBytes caps the JSON getJSON decodes; /api/health and /api/user
	// answer with a few hundred bytes.
	maxResponseBytes = 1 << 20
)

// VersionIncompatibleError is returned when a Grafana instance is too old for gcx.
type VersionIncompatibleError struct {
	Version *semver.Version
}

func (e *VersionIncompatibleError) Error() string {
	return fmt.Sprintf("grafana version %s is not supported; gcx requires Grafana 12.0.0 or later", e.Version)
}

// StatusError reports a Grafana API response with a non-2xx status. It carries
// no response body: error bodies can echo credentials.
type StatusError struct {
	Path       string
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GET %s returned HTTP %d", e.Path, e.StatusCode)
}

// HTTPStatusCode lets error classification read the status.
func (e *StatusError) HTTPStatusCode() int { return e.StatusCode }

// getJSON requests GET /api<path> from the context's Grafana and decodes the
// JSON response into out.
//
// EffectiveGrafanaAuthMethod is the single selection authority. Only the
// selected HTTP credential is attached, so stale fields from a previous auth
// method cannot leak. OAuth requests are intentionally anonymous against the
// direct Grafana endpoint; mTLS authenticates at the transport layer and
// carries no Authorization header.
func getJSON(ctx context.Context, cfgCtx *config.Context, path string, out any) error {
	if cfgCtx == nil {
		return errors.New("no context provided")
	}
	if cfgCtx.Grafana == nil {
		return errors.New("grafana not configured")
	}
	authMethod, err := cfgCtx.EffectiveGrafanaAuthMethod()
	if err != nil {
		return fmt.Errorf("grafana authentication: %w", err)
	}
	selectedTLS, err := cfgCtx.EffectiveGrafanaTLS()
	if err != nil {
		return fmt.Errorf("grafana TLS configuration: %w", err)
	}
	var stdTLS *tls.Config // nil when no TLS is configured
	if selectedTLS != nil {
		stdTLS, err = selectedTLS.ToStdTLSConfig()
		if err != nil {
			return fmt.Errorf("TLS configuration: %w", err)
		}
	}

	apiPath := "/api" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(cfgCtx.Grafana.Server, "/")+apiPath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	switch authMethod {
	case "basic":
		req.SetBasicAuth(cfgCtx.Grafana.User, cfgCtx.Grafana.Password)
	case "token":
		req.Header.Set("Authorization", "Bearer "+cfgCtx.Grafana.APIToken)
	}
	if cfgCtx.Grafana.OrgID != 0 {
		req.Header.Set(orgIDHeader, strconv.FormatInt(cfgCtx.Grafana.OrgID, 10))
	}

	// The CLI client carries the --insecure-log-http-payload logging transport
	// when enabled.
	resp, err := httputils.NewDefaultClientWithTLS(ctx, stdTLS).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &StatusError{Path: apiPath, StatusCode: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("decoding GET %s: %w", apiPath, err)
	}
	return nil
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
	var health struct {
		Version  string `json:"version"`
		Commit   string `json:"commit"`
		Database string `json:"database"`
	}
	if err := getJSON(ctx, cfgCtx, "/health", &health); err != nil {
		return nil, "", err
	}

	raw := health.Version
	logging.FromContext(ctx).Debug("grafana health response",
		"server", cfgCtx.Grafana.Server,
		"raw_version", raw,
		"commit", health.Commit,
		"database", health.Database,
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

// SignedInUserID returns the ID of the user the context's credentials sign in
// as, from /api/user. A non-2xx response is a *StatusError.
func SignedInUserID(ctx context.Context, cfgCtx *config.Context) (int64, error) {
	var user struct {
		ID int64 `json:"id"`
	}
	if err := getJSON(ctx, cfgCtx, "/user", &user); err != nil {
		return 0, err
	}
	return user.ID, nil
}
