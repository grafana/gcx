package grafana

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-openapi/strfmt"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/version"
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

func GetVersion(ctx *config.Context) (*semver.Version, error) {
	gClient, err := ClientFromContext(ctx)
	if err != nil {
		return nil, err
	}

	healthResponse, err := gClient.Health.GetHealth()
	if err != nil {
		return nil, err
	}

	return semver.NewVersion(healthResponse.Payload.Version)
}
