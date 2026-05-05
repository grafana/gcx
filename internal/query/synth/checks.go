package synth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/gcx/internal/providers/synth/checks"
)

// ListChecks returns all SM checks visible through the given datasource.
func (c *Client) ListChecks(ctx context.Context, datasourceUID string) ([]checks.Check, error) {
	return c.listChecks(ctx, datasourceUID, false)
}

// ListChecksWithAlerts returns all SM checks with their alert rules embedded
// in each Check.Alerts field. Backed by the SM datasource's
// /sm/check/list?includeAlerts=true server-side composition — the same
// endpoint the SM app uses to render the check-list page with alert state.
func (c *Client) ListChecksWithAlerts(ctx context.Context, datasourceUID string) ([]checks.Check, error) {
	return c.listChecks(ctx, datasourceUID, true)
}

func (c *Client) listChecks(ctx context.Context, datasourceUID string, includeAlerts bool) ([]checks.Check, error) {
	path := "sm/check/list"
	if includeAlerts {
		path += "?includeAlerts=true"
	}
	body, err := c.proxyGet(ctx, datasourceUID, path, "list checks")
	if err != nil {
		return nil, err
	}
	var result []checks.Check
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode checks: %w", err)
	}
	return result, nil
}
