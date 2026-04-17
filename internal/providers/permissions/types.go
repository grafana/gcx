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
