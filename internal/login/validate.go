package login

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	intgrafana "github.com/grafana/gcx/internal/grafana"
	"github.com/grafana/gcx/internal/resources/discovery"
)

// grafanaClient is satisfied by any type that can return the Grafana version.
// The real implementation calls internal/grafana.GetVersion (which hits /api/health).
// Tests inject a stub.
//
// Return contract mirrors grafana.GetVersion: err != nil means the health
// probe itself failed; (nil, "", nil) means the server hid its version;
// (nil, raw, nil) means the version string wasn't parseable; otherwise
// parsed is populated.
type grafanaClient interface {
	GetVersion(ctx context.Context) (*semver.Version, string, error)
}

// gcomClient is satisfied by *cloud.GCOMClient and test stubs.
type gcomClient interface {
	GetStack(ctx context.Context, slug string) (cloud.StackInfo, error)
}

// validator holds injectable clients so tests can run without network access.
type validator struct {
	grafana   grafanaClient
	discovery func(ctx context.Context, cfg config.NamespacedRESTConfig) error
	gcom      gcomClient // nil when Cloud+CAP not required
}

// validate runs all connectivity checks in order and returns the first failure.
// On success it returns the Grafana version string and nil. It never writes to config.
//
// If the server's /api/health response hides the version (Grafana Cloud
// fingerprinting defense) or returns a non-semver string, the version check
// is skipped: the server is reachable, and blocking login on an unparseable
// build string would surprise users on dev stacks. The returned version
// string is "unknown" in that case so callers can surface it.
func (v *validator) validate(ctx context.Context, opts Options, restCfg config.NamespacedRESTConfig) (string, error) {
	// Step 1: Health reachability
	// grafana.GetVersion hits /api/health, so an unreachable server surfaces
	// here as a "health check failed" error.
	version, rawVersion, err := v.grafana.GetVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("connectivity validation: health check failed: %w", err)
	}

	// Step 2: K8s API availability via discovery /apis
	if err := v.discovery(ctx, restCfg); err != nil {
		return "", fmt.Errorf("connectivity validation: kubernetes API unavailable: %w", err)
	}

	// Step 3: Grafana version must be >= 12 when we can parse it. Empty or
	// unparseable versions bypass the check — see function comment.
	if version != nil && version.Major() < 12 {
		return "", fmt.Errorf("connectivity validation: version check failed: %w",
			&intgrafana.VersionIncompatibleError{Version: version})
	}

	// Step 4: GCOM stack check when Cloud target has a CAP token and a derivable slug
	if opts.Target == TargetCloud && opts.CloudToken != "" && v.gcom != nil {
		slug := resolveStackSlug(opts.Server)
		if slug != "" {
			if _, err := v.gcom.GetStack(ctx, slug); err != nil {
				return "", fmt.Errorf("connectivity validation: GCOM check failed: %w", err)
			}
		}
	}

	switch {
	case version != nil:
		return version.String(), nil
	case rawVersion != "":
		return rawVersion, nil
	default:
		return "unknown", nil
	}
}

// resolveStackSlug derives the Grafana Cloud stack slug from a server URL.
// Returns "" for non-Grafana-Cloud or custom-domain URLs.
func resolveStackSlug(server string) string {
	ctx := &config.Context{
		Grafana: &config.GrafanaConfig{Server: server},
	}
	return ctx.ResolveStackSlug()
}

// realGrafanaClient wraps internal/grafana.GetVersion using a config.Context
// built from login Options (server URL + API token).
type realGrafanaClient struct {
	cfgCtx *config.Context
}

func (c *realGrafanaClient) GetVersion(ctx context.Context) (*semver.Version, string, error) {
	return intgrafana.GetVersion(ctx, c.cfgCtx)
}

// Validate performs the full connectivity validation pipeline for the login flow
// (FR-013, FR-014, D11, D12). On success it returns the Grafana version string and
// nil; on failure it returns an error naming the failed step without touching config.
//
// The restCfg parameter is the namespaced REST config to probe K8s discovery (/apis).
func Validate(ctx context.Context, opts Options, restCfg config.NamespacedRESTConfig) (string, error) {
	grafanaCtx := &config.Context{
		Grafana: &config.GrafanaConfig{
			Server:   opts.Server,
			APIToken: opts.GrafanaToken,
		},
	}

	v := &validator{
		grafana: &realGrafanaClient{cfgCtx: grafanaCtx},
		discovery: func(ctx context.Context, cfg config.NamespacedRESTConfig) error {
			_, err := discovery.NewDefaultRegistry(ctx, cfg)
			return err
		},
	}

	if opts.Target == TargetCloud && opts.CloudToken != "" {
		gcomBaseURL := opts.CloudAPIURL
		if gcomBaseURL == "" {
			gcomBaseURL = "https://grafana.com"
		}
		gcomC, err := cloud.NewGCOMClient(gcomBaseURL, opts.CloudToken)
		if err != nil {
			return "", fmt.Errorf("connectivity validation: invalid GCOM URL: %w", err)
		}
		v.gcom = gcomC
	}

	return v.validate(ctx, opts, restCfg) //nolint:wrapcheck
}
