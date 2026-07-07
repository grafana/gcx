package org

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/coreapi"
)

// ErrNotFound is returned when an org resource does not exist.
var ErrNotFound = errors.New("org resource not found")

// Client is an HTTP client for the /api/org endpoints of the Grafana API.
type Client struct {
	base *coreapi.Client
}

// NewClient creates a new org API client from a namespaced REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	base, err := coreapi.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{base: base}, nil
}

// List returns all users in the current organization.
func (c *Client) List(ctx context.Context) ([]OrgUser, error) {
	return coreapi.DoJSON[any, []OrgUser](ctx, c.base, http.MethodGet, "/api/org/users", nil, http.StatusOK)
}

// Get returns a single user in the current organization by numeric user ID.
// The Grafana /api/org/users endpoint has no single-user sub-resource, so this
// lists all users and filters client-side.
func (c *Client) Get(ctx context.Context, userID int) (*OrgUser, error) {
	users, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range users {
		if users[i].UserID == userID {
			return &users[i], nil
		}
	}
	return nil, fmt.Errorf("user id %d: %w", userID, ErrNotFound)
}

// GetByLoginOrEmail returns a single user matching the given login or email.
func (c *Client) GetByLoginOrEmail(ctx context.Context, loginOrEmail string) (*OrgUser, error) {
	users, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range users {
		if strings.EqualFold(users[i].Login, loginOrEmail) || strings.EqualFold(users[i].Email, loginOrEmail) {
			return &users[i], nil
		}
	}
	return nil, fmt.Errorf("user %q: %w", loginOrEmail, ErrNotFound)
}

// Add adds a user (by login or email) to the current organization.
func (c *Client) Add(ctx context.Context, req AddUserRequest) error {
	return coreapi.DoStatus[AddUserRequest](ctx, c.base, http.MethodPost, "/api/org/users", &req, http.StatusOK)
}

// UpdateUserRole changes the role of a user in the current organization.
func (c *Client) UpdateUserRole(ctx context.Context, userID int, role string) error {
	body := map[string]string{"role": role}
	return coreapi.DoStatusNotFound[map[string]string](ctx, c.base, http.MethodPatch,
		fmt.Sprintf("/api/org/users/%d", userID), &body,
		fmt.Errorf("user id %d: %w", userID, ErrNotFound), http.StatusOK)
}

// RemoveUser removes a user from the current organization.
func (c *Client) RemoveUser(ctx context.Context, userID int) error {
	return coreapi.DoStatusNotFound[any](ctx, c.base, http.MethodDelete,
		fmt.Sprintf("/api/org/users/%d", userID), nil,
		fmt.Errorf("user id %d: %w", userID, ErrNotFound), http.StatusOK)
}
