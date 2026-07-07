// Package permissions provides commands for managing Grafana resource
// permissions via the granular access-control (RBAC) API,
// /api/access-control/{resource}/{resourceID}.
package permissions

import (
	"fmt"
	"slices"
)

// ValidResources are the resource kinds the granular RBAC permissions API
// exposes. A management CLI targets these uniformly.
//
//nolint:gochecknoglobals // immutable lookup table.
var ValidResources = []string{"folders", "dashboards", "datasources", "teams", "serviceaccounts"}

// validateResource returns an error if resource is not a supported RBAC
// permission resource kind.
func validateResource(resource string) error {
	if !slices.Contains(ValidResources, resource) {
		return fmt.Errorf("invalid resource %q: must be one of %v", resource, ValidResources)
	}
	return nil
}

// ResourcePermission is one entry in a resource's permission list, as returned
// by GET /api/access-control/{resource}/{resourceID}.
type ResourcePermission struct {
	ID               int64    `json:"id"`
	RoleName         string   `json:"roleName,omitempty"`
	IsManaged        bool     `json:"isManaged"`
	IsInherited      bool     `json:"isInherited"`
	IsServiceAccount bool     `json:"isServiceAccount"`
	UserID           int64    `json:"userId,omitempty"`
	UserUID          string   `json:"userUid,omitempty"`
	UserLogin        string   `json:"userLogin,omitempty"`
	Team             string   `json:"team,omitempty"`
	TeamID           int64    `json:"teamId,omitempty"`
	TeamUID          string   `json:"teamUid,omitempty"`
	BuiltInRole      string   `json:"builtInRole,omitempty"`
	Actions          []string `json:"actions,omitempty"`
	Permission       string   `json:"permission"`
}

// Description reports the assignable permission levels and assignment types for
// a resource kind, as returned by GET /api/access-control/{resource}/description.
type Description struct {
	Assignments Assignments `json:"assignments"`
	Permissions []string    `json:"permissions"`
}

// Assignments reports which principal types can be assigned permissions on a
// resource kind.
type Assignments struct {
	Users           bool `json:"users"`
	ServiceAccounts bool `json:"serviceAccounts"`
	Teams           bool `json:"teams"`
	BuiltInRoles    bool `json:"builtInRoles"`
}

// SetResourcePermissionCommand is one assignment in a full permission-set
// request. Provide exactly one of UserID, TeamID, or BuiltInRole.
type SetResourcePermissionCommand struct {
	UserID      int64  `json:"userId,omitempty"`
	TeamID      int64  `json:"teamId,omitempty"`
	BuiltInRole string `json:"builtInRole,omitempty"`
	Permission  string `json:"permission"`
}

// setPermissionsBody is the request body for POST /api/access-control/{resource}/{resourceID}.
type setPermissionsBody struct {
	Permissions []SetResourcePermissionCommand `json:"permissions"`
}

// setPermissionBody is the request body for the per-principal grant endpoints
// (.../users/{id}, .../teams/{id}, .../builtInRoles/{role}). An empty
// Permission removes the assignment.
type setPermissionBody struct {
	Permission string `json:"permission"`
}
