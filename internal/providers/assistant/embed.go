package assistant

import (
	"github.com/grafana/gcx/internal/config"
)

// RequireGrafanaCloud returns an error if the given context has a Grafana
// server configured that is NOT a Grafana Cloud stack (i.e. self-hosted).
// A context with no Grafana configuration returns nil — callers still
// need to validate the config before building an Assistant client. The
// intent is to fail fast with a clear "self-hosted not supported" message
// rather than let the raw auth failure surface first.
func RequireGrafanaCloud(ctx *config.Context) error {
	return requireGrafanaCloud(ctx)
}
