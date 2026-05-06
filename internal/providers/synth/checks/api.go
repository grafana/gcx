package checks

import (
	"context"
	"encoding/json"
	"fmt"

	querysynth "github.com/grafana/gcx/internal/query/synth"
)

// SM API helpers. These wrap query/synth byte primitives with typed Check /
// Tenant marshaling. Stateless — they take the proxy client as a parameter.
//
// Long-term the SM types should come from the synthetic-monitoring-agent
// protobuf definitions; until then these helpers do the JSON dance.

// ListChecks returns all SM checks visible through the given datasource.
func ListChecks(ctx context.Context, c *querysynth.Client, datasourceUID string) ([]Check, error) {
	return listChecks(ctx, c, datasourceUID, false)
}

// ListChecksWithAlerts returns all SM checks with their alert rules embedded
// in each Check.Alerts field. Backed by the SM datasource's
// /sm/check/list?includeAlerts=true server-side composition — the same
// endpoint the SM app uses to render the check-list page with alert state.
func ListChecksWithAlerts(ctx context.Context, c *querysynth.Client, datasourceUID string) ([]Check, error) {
	return listChecks(ctx, c, datasourceUID, true)
}

func listChecks(ctx context.Context, c *querysynth.Client, datasourceUID string, includeAlerts bool) ([]Check, error) {
	path := "sm/check/list"
	if includeAlerts {
		path += "?includeAlerts=true"
	}
	body, err := c.ProxyGet(ctx, datasourceUID, path, "list checks")
	if err != nil {
		return nil, err
	}
	var result []Check
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode checks: %w", err)
	}
	return result, nil
}

// GetCheck fetches a single check by ID.
func GetCheck(ctx context.Context, c *querysynth.Client, datasourceUID string, id int64) (*Check, error) {
	body, err := c.ProxyGet(ctx, datasourceUID, fmt.Sprintf("sm/check/%d", id), "get check")
	if err != nil {
		return nil, err
	}
	var check Check
	if err := json.Unmarshal(body, &check); err != nil {
		return nil, fmt.Errorf("decode check: %w", err)
	}
	return &check, nil
}

// CreateCheck creates a new check. The body must not have ID or TenantID set —
// the SM API populates TenantID from the proxy's auth context.
func CreateCheck(ctx context.Context, c *querysynth.Client, datasourceUID string, ch Check) (*Check, error) {
	payload, err := json.Marshal(ch)
	if err != nil {
		return nil, fmt.Errorf("encode check: %w", err)
	}
	body, err := c.ProxyPost(ctx, datasourceUID, "sm/check/add", payload, "create check")
	if err != nil {
		return nil, err
	}
	var created Check
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("decode created check: %w", err)
	}
	return &created, nil
}

// UpdateCheck updates an existing check. The body must have ID and TenantID
// set; the SM API rejects update bodies whose TenantID does not match the
// auth user's tenant. Callers should populate TenantID via GetTenant.
func UpdateCheck(ctx context.Context, c *querysynth.Client, datasourceUID string, ch Check) (*Check, error) {
	payload, err := json.Marshal(ch)
	if err != nil {
		return nil, fmt.Errorf("encode check: %w", err)
	}
	body, err := c.ProxyPost(ctx, datasourceUID, "sm/check/update", payload, "update check")
	if err != nil {
		return nil, err
	}
	var updated Check
	if err := json.Unmarshal(body, &updated); err != nil {
		return nil, fmt.Errorf("decode updated check: %w", err)
	}
	return &updated, nil
}

// DeleteCheck deletes a check by ID.
func DeleteCheck(ctx context.Context, c *querysynth.Client, datasourceUID string, id int64) error {
	_, err := c.ProxyDelete(ctx, datasourceUID, fmt.Sprintf("sm/check/delete/%d", id), "delete check")
	return err
}

// GetTenant returns the SM tenant info needed to populate TenantID on update operations.
func GetTenant(ctx context.Context, c *querysynth.Client, datasourceUID string) (*Tenant, error) {
	body, err := c.ProxyGet(ctx, datasourceUID, "sm/tenant", "get tenant")
	if err != nil {
		return nil, err
	}
	var tenant Tenant
	if err := json.Unmarshal(body, &tenant); err != nil {
		return nil, fmt.Errorf("decode tenant: %w", err)
	}
	return &tenant, nil
}
