// Package org provides commands for managing Grafana organization resources.
package org

// OrgUser represents a user membership in the current Grafana organization.
type OrgUser struct {
	OrgID         int    `json:"orgId,omitempty"`
	UserID        int    `json:"userId,omitempty"`
	Login         string `json:"login,omitempty"`
	Name          string `json:"name,omitempty"`
	Email         string `json:"email,omitempty"`
	Role          string `json:"role"`
	AvatarURL     string `json:"avatarUrl,omitempty"`
	LastSeenAt    string `json:"lastSeenAt,omitempty"`
	LastSeenAtAge string `json:"lastSeenAtAge,omitempty"`
}

// AddUserRequest is the payload for POST /api/org/users.
type AddUserRequest struct {
	LoginOrEmail string `json:"loginOrEmail"`
	Role         string `json:"role"`
}

// errorResponse models the shape of Grafana API error responses.
type errorResponse struct {
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}
