// Package permissions provides Grafana folder and dashboard permission management.
package permissions

// Permission levels as used by the Grafana permissions API.
const (
	PermissionView  = 1
	PermissionEdit  = 2
	PermissionAdmin = 4
)

// Item is a single permission entry on a folder or dashboard.
type Item struct {
	Role       string `json:"role,omitempty"`
	Permission int    `json:"permission"`
	UserLogin  string `json:"userLogin,omitempty"`
	TeamID     int    `json:"teamId,omitempty"`
}

// setBody is the POST body format for updating permissions.
type setBody struct {
	Items []Item `json:"items"`
}

// ErrorResponse is the error body returned by the Grafana HTTP API.
type ErrorResponse struct {
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// FolderPermissions is the per-folder permissions document. It is a singleton
// per folder UID — the ResourceName IS the folder UID.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type FolderPermissions struct {
	UID   string `json:"-"`
	Items []Item `json:"items"`
}

// GetResourceName returns the folder UID.
func (fp FolderPermissions) GetResourceName() string { return fp.UID }

// SetResourceName sets the folder UID from the resource name.
func (fp *FolderPermissions) SetResourceName(name string) { fp.UID = name }

// DashboardPermissions is the per-dashboard permissions document. It is a
// singleton per dashboard UID — the ResourceName IS the dashboard UID.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type DashboardPermissions struct {
	UID   string `json:"-"`
	Items []Item `json:"items"`
}

// GetResourceName returns the dashboard UID.
func (dp DashboardPermissions) GetResourceName() string { return dp.UID }

// SetResourceName sets the dashboard UID from the resource name.
func (dp *DashboardPermissions) SetResourceName(name string) { dp.UID = name }
