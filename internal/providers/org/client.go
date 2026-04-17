package org

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// ErrNotFound is returned when an org resource does not exist.
var ErrNotFound = errors.New("org resource not found")

// Client is an HTTP client for the /api/org endpoints of the Grafana API.
type Client struct {
	httpClient *http.Client
	host       string
}

// NewClient creates a new org API client from a namespaced REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: cfg.Host}, nil
}

// ListUsers returns all users in the current organization.
func (c *Client) ListUsers(ctx context.Context) ([]OrgUser, error) {
	var users []OrgUser
	if err := c.do(ctx, http.MethodGet, "/api/org/users", nil, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// AddUser adds a user (by login or email) to the current organization.
func (c *Client) AddUser(ctx context.Context, req AddUserRequest) error {
	return c.do(ctx, http.MethodPost, "/api/org/users", req, nil)
}

// UpdateUserRole changes the role of a user in the current organization.
func (c *Client) UpdateUserRole(ctx context.Context, userID int, role string) error {
	path := fmt.Sprintf("/api/org/users/%d", userID)
	body := map[string]string{"role": role}
	return c.do(ctx, http.MethodPatch, path, body, nil)
}

// RemoveUser removes a user from the current organization.
func (c *Client) RemoveUser(ctx context.Context, userID int) error {
	path := fmt.Sprintf("/api/org/users/%d", userID)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// do executes an HTTP request, encoding body as JSON when non-nil and decoding
// the response into out when non-nil.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to encode request body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.host+path, reader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return handleErrorResponse(resp)
	}

	if out == nil {
		// Drain body so the connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// handleErrorResponse reads an error response body and returns a formatted error.
func handleErrorResponse(resp *http.Response) error {
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, readErr)
	}

	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		msg := errResp.Message
		if msg == "" {
			msg = errResp.Error
		}
		if msg != "" {
			if resp.StatusCode == http.StatusNotFound {
				return fmt.Errorf("%w: %s (status %d)", ErrNotFound, msg, resp.StatusCode)
			}
			return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, msg)
		}
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w (status %d)", ErrNotFound, resp.StatusCode)
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}
	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}
