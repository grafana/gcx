package permissions

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/coreapi"
)

// Client talks to Grafana's granular access-control (RBAC) permissions API.
type Client struct {
	base *coreapi.Client
}

// NewClient creates a new permissions client bound to the provided REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	base, err := coreapi.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{base: base}, nil
}

func basePath(resource string) string {
	return "/api/access-control/" + resource
}

// Describe returns the assignable permission levels and assignment types for a
// resource kind.
func (c *Client) Describe(ctx context.Context, resource string) (*Description, error) {
	d, err := coreapi.DoJSON[Description](ctx, c.base, http.MethodGet, basePath(resource)+"/description", nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Get returns the permission list for a resource instance.
func (c *Client) Get(ctx context.Context, resource, resourceID string) ([]ResourcePermission, error) {
	return coreapi.DoJSON[[]ResourcePermission](ctx, c.base, http.MethodGet,
		fmt.Sprintf("%s/%s", basePath(resource), url.PathEscape(resourceID)), nil, http.StatusOK)
}

// Set replaces the full permission set for a resource instance.
func (c *Client) Set(ctx context.Context, resource, resourceID string, perms []SetResourcePermissionCommand) error {
	body := setPermissionsBody{Permissions: perms}
	err := coreapi.DoStatus(ctx, c.base, http.MethodPost,
		fmt.Sprintf("%s/%s", basePath(resource), url.PathEscape(resourceID)), &body, http.StatusOK)
	return annotateForbidden(err)
}

// SetUserPermission sets (or, with an empty level, removes) one user's
// permission on a resource instance. userRef may be a numeric user ID or a UID.
func (c *Client) SetUserPermission(ctx context.Context, resource, resourceID, userRef, level string) error {
	return c.setPrincipal(ctx, resource, resourceID, "users", userRef, level)
}

// SetTeamPermission sets (or removes) one team's permission on a resource
// instance. teamRef may be a numeric team ID or a UID.
func (c *Client) SetTeamPermission(ctx context.Context, resource, resourceID, teamRef, level string) error {
	return c.setPrincipal(ctx, resource, resourceID, "teams", teamRef, level)
}

// SetBuiltInRolePermission sets (or removes) a built-in role's permission on a
// resource instance (e.g. role "Viewer", "Editor", "Admin").
func (c *Client) SetBuiltInRolePermission(ctx context.Context, resource, resourceID, role, level string) error {
	return c.setPrincipal(ctx, resource, resourceID, "builtInRoles", role, level)
}

func (c *Client) setPrincipal(ctx context.Context, resource, resourceID, principalKind, principalRef, level string) error {
	body := setPermissionBody{Permission: level}
	path := fmt.Sprintf("%s/%s/%s/%s", basePath(resource), url.PathEscape(resourceID), principalKind, url.PathEscape(principalRef))
	err := coreapi.DoStatus(ctx, c.base, http.MethodPost, path, &body, http.StatusOK)
	return annotateForbidden(err)
}

// annotateForbidden adds an actionable hint when a write is rejected with 403,
// which on Grafana OSS without Enterprise/RBAC means the granular permission
// API is unavailable.
func annotateForbidden(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "status 403") {
		return fmt.Errorf("%w\nwriting granular permissions requires RBAC (Grafana Enterprise or Cloud); reads work on all editions", err)
	}
	return err
}
