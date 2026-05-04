package synth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/gcx/internal/providers/synth/probes"
)

// ListProbes returns all SM probes visible through the given datasource.
func (c *Client) ListProbes(ctx context.Context, datasourceUID string) ([]probes.Probe, error) {
	body, err := c.proxyGet(ctx, datasourceUID, "sm/probe/list", "list probes")
	if err != nil {
		return nil, err
	}
	var result []probes.Probe
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode probes: %w", err)
	}
	return result, nil
}
