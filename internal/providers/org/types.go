// Package org provides commands for managing Grafana organization resources.
package org

import "strconv"

// OrgUser represents a user membership in the current Grafana organization.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type OrgUser struct {
	OrgID         int    `json:"orgId,omitempty"`
	UserID        int    `json:"userId,omitempty"`
	Login         string `json:"login,omitempty"`
	Name          string `json:"name,omitempty"`
	Email         string `json:"email,omitempty"`
	LoginOrEmail  string `json:"loginOrEmail,omitempty"`
	Role          string `json:"role"`
	AvatarURL     string `json:"avatarUrl,omitempty"`
	LastSeenAt    string `json:"lastSeenAt,omitempty"`
	LastSeenAtAge string `json:"lastSeenAtAge,omitempty"`
}

// GetResourceName returns the numeric UserID as a string, acting as the
// stable identity for this org user.
func (u OrgUser) GetResourceName() string {
	return strconv.Itoa(u.UserID)
}

// SetResourceName restores the numeric UserID from a string (e.g., after a
// Kubernetes-style round-trip via metadata.name). Parse errors are silently
// ignored, per the ResourceIdentity contract for numeric identifiers.
func (u *OrgUser) SetResourceName(name string) {
	if id, err := strconv.Atoi(name); err == nil {
		u.UserID = id
	}
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
