package probes

import (
	"context"
	"encoding/json"
	"fmt"

	querysynth "github.com/grafana/gcx/internal/query/synth"
)

// SM API helpers. These wrap query/synth byte primitives with typed Probe
// marshaling. Stateless — they take the proxy client as a parameter.
//
// Long-term the SM types should come from the synthetic-monitoring-agent
// protobuf definitions; until then these helpers do the JSON dance.

// ListProbes returns all SM probes visible through the given datasource.
func ListProbes(ctx context.Context, c *querysynth.Client, datasourceUID string) ([]Probe, error) {
	body, err := c.ProxyGet(ctx, datasourceUID, "sm/probe/list", "list probes")
	if err != nil {
		return nil, err
	}
	var result []Probe
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode probes: %w", err)
	}
	return result, nil
}

// CreateProbe creates a new probe and returns the created probe with its auth
// token. The token is returned only on creation and cannot be retrieved later.
func CreateProbe(ctx context.Context, c *querysynth.Client, datasourceUID string, p Probe) (*CreateResponse, error) {
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode probe: %w", err)
	}
	body, err := c.ProxyPost(ctx, datasourceUID, "sm/probe/add", payload, "create probe")
	if err != nil {
		return nil, err
	}
	var created CreateResponse
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("decode created probe: %w", err)
	}
	return &created, nil
}

// ResetProbeToken posts the probe with resetToken=true, causing the SM API to
// issue a new auth token. The new token is NOT returned in the response — the
// probe must be re-created if the token value is needed.
func ResetProbeToken(ctx context.Context, c *querysynth.Client, datasourceUID string, p Probe) (*Probe, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode probe: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("rewrite probe payload: %w", err)
	}
	m["resetToken"] = true
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode reset request: %w", err)
	}
	body, err := c.ProxyPost(ctx, datasourceUID, "sm/probe/update", payload, "reset probe token")
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Probe Probe `json:"probe"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("decode reset response: %w", err)
	}
	return &wrapper.Probe, nil
}

// DeleteProbe deletes a probe by ID.
func DeleteProbe(ctx context.Context, c *querysynth.Client, datasourceUID string, id int64) error {
	_, err := c.ProxyDelete(ctx, datasourceUID, fmt.Sprintf("sm/probe/delete/%d", id), "delete probe")
	return err
}
