package synth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/gcx/internal/providers/synth/checks"
)

// ListChecks returns all SM checks visible through the given datasource.
func (c *Client) ListChecks(ctx context.Context, datasourceUID string) ([]checks.Check, error) {
	body, err := c.proxyGet(ctx, datasourceUID, "sm/check/list", "list checks")
	if err != nil {
		return nil, err
	}
	var result []checks.Check
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode checks: %w", err)
	}
	return result, nil
}
