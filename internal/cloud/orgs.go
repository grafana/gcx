package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// OrgMembership is a Cloud organisation membership of the authenticated user.
// Membership does not imply permission to create stacks.
type OrgMembership struct {
	Slug string `json:"slug" yaml:"slug"`
	Role string `json:"role" yaml:"role"`
}

// ListOrgs enumerates the current user's memberships. The OAuth endpoint returns
// the complete array without pagination; null denotes no authenticated user.
func (c *GCOMClient) ListOrgs(ctx context.Context) ([]OrgMembership, error) {
	endpoint, err := c.buildURL("/api/oauth2/user/orgs")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("gcom client: create request: %w", err)
	}
	c.setHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gcom client: do request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gcom client: read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newGCOMHTTPError(resp.StatusCode, body)
	}
	var rows []struct {
		Login string `json:"login"`
		Role  string `json:"role"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("gcom client: decode organisations: %w", err)
	}
	if rows == nil {
		return nil, errors.New("organisation listing requires a user OAuth login; run gcx cloud login using the same config/context (default scopes include profile); unset GRAFANA_CLOUD_TOKEN or cloud.<entry>.token if set, because access-policy tokens take precedence over OAuth")
	}
	result := make([]OrgMembership, 0, len(rows))
	for i, row := range rows {
		if row.Login == "" {
			return nil, fmt.Errorf("gcom client: organisation membership at index %d has no slug", i)
		}
		result = append(result, OrgMembership{Slug: row.Login, Role: row.Role})
	}
	return result, nil
}
