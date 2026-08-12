package assistant

import (
	"github.com/grafana/gcx/internal/config"
)

// RequireGrafanaCloud returns an error if the given Grafana context is not
// a Grafana Cloud stack. Callers should check this before building an
// Assistant client to produce a clearer error than the raw auth failure.
func RequireGrafanaCloud(ctx *config.Context) error {
	return requireGrafanaCloud(ctx)
}
