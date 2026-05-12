package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	contactPointsPath = "/api/v1/provisioning/contact-points"
	muteTimingsPath   = "/api/v1/provisioning/mute-timings"
	policiesPath      = "/api/v1/provisioning/policies"
	templatesPath     = "/api/v1/provisioning/templates"
)

// ErrProvisioningNotFound is returned when a provisioning resource does not exist.
var ErrProvisioningNotFound = errors.New("provisioning resource not found")

// doJSON performs an HTTP request with an optional JSON body and optionally
// decodes a JSON response. Returns ErrProvisioningNotFound for 404 responses.
// A 202 Accepted with an empty body is treated as success.
func (c *Client) doJSON(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.host+path, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s %s: %w", method, path, ErrProvisioningNotFound)
	case resp.StatusCode >= 400:
		return handleErrorResponse(resp)
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// doRaw performs an HTTP GET and returns the raw response body bytes.
func (c *Client) doRaw(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("GET %s: %w", path, ErrProvisioningNotFound)
	}
	if resp.StatusCode >= 400 {
		return nil, handleErrorResponse(resp)
	}

	return io.ReadAll(resp.Body)
}

// ---------------------------------------------------------------------------
// Contact points
// ---------------------------------------------------------------------------

// ListContactPoints returns all contact points.
func (c *Client) ListContactPoints(ctx context.Context) ([]ContactPoint, error) {
	var out []ContactPoint
	if err := c.doJSON(ctx, http.MethodGet, contactPointsPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetContactPoint returns a contact point by UID. The API has no get-by-UID
// endpoint so this filters the list.
func (c *Client) GetContactPoint(ctx context.Context, uid string) (*ContactPoint, error) {
	list, err := c.ListContactPoints(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].UID == uid {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("contact point %q: %w", uid, ErrProvisioningNotFound)
}

// CreateContactPoint creates a new contact point.
func (c *Client) CreateContactPoint(ctx context.Context, cp ContactPoint) (*ContactPoint, error) {
	var created ContactPoint
	if err := c.doJSON(ctx, http.MethodPost, contactPointsPath, cp, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateContactPoint updates an existing contact point. The PUT endpoint
// returns 202 Accepted with no body; the caller gets back the input with
// its UID preserved.
func (c *Client) UpdateContactPoint(ctx context.Context, uid string, cp ContactPoint) (*ContactPoint, error) {
	path := contactPointsPath + "/" + url.PathEscape(uid)
	if err := c.doJSON(ctx, http.MethodPut, path, cp, nil); err != nil {
		return nil, err
	}
	cp.UID = uid
	return &cp, nil
}

// DeleteContactPoint deletes a contact point by UID.
func (c *Client) DeleteContactPoint(ctx context.Context, uid string) error {
	path := contactPointsPath + "/" + url.PathEscape(uid)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

// ExportContactPoints returns all contact points in the provisioning export
// format (yaml, json, or hcl).
func (c *Client) ExportContactPoints(ctx context.Context, format string) ([]byte, error) {
	return c.doRaw(ctx, contactPointsPath+"/export?format="+url.QueryEscape(format))
}

// ---------------------------------------------------------------------------
// Mute timings
// ---------------------------------------------------------------------------

// ListMuteTimings returns all mute timings.
func (c *Client) ListMuteTimings(ctx context.Context) ([]MuteTiming, error) {
	var out []MuteTiming
	if err := c.doJSON(ctx, http.MethodGet, muteTimingsPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetMuteTiming returns a mute timing by name.
func (c *Client) GetMuteTiming(ctx context.Context, name string) (*MuteTiming, error) {
	var out MuteTiming
	path := muteTimingsPath + "/" + url.PathEscape(name)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateMuteTiming creates a new mute timing.
func (c *Client) CreateMuteTiming(ctx context.Context, mt MuteTiming) (*MuteTiming, error) {
	var created MuteTiming
	if err := c.doJSON(ctx, http.MethodPost, muteTimingsPath, mt, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// UpdateMuteTiming updates a mute timing. The PUT endpoint returns 202 with
// no body; the caller gets back the input with its name preserved.
func (c *Client) UpdateMuteTiming(ctx context.Context, name string, mt MuteTiming) (*MuteTiming, error) {
	path := muteTimingsPath + "/" + url.PathEscape(name)
	if err := c.doJSON(ctx, http.MethodPut, path, mt, nil); err != nil {
		return nil, err
	}
	mt.Name = name
	return &mt, nil
}

// DeleteMuteTiming deletes a mute timing by name.
func (c *Client) DeleteMuteTiming(ctx context.Context, name string) error {
	path := muteTimingsPath + "/" + url.PathEscape(name)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

// ExportMuteTimings exports all mute timings in the given provisioning format.
func (c *Client) ExportMuteTimings(ctx context.Context, format string) ([]byte, error) {
	return c.doRaw(ctx, muteTimingsPath+"/export?format="+url.QueryEscape(format))
}

// ExportMuteTiming exports a single mute timing by name in the given format.
func (c *Client) ExportMuteTiming(ctx context.Context, name, format string) ([]byte, error) {
	path := fmt.Sprintf("%s/%s/export?format=%s", muteTimingsPath, url.PathEscape(name), url.QueryEscape(format))
	return c.doRaw(ctx, path)
}

// ---------------------------------------------------------------------------
// Notification policies
// ---------------------------------------------------------------------------

// GetNotificationPolicy returns the (singleton) notification policy tree.
func (c *Client) GetNotificationPolicy(ctx context.Context) (*NotificationPolicy, error) {
	var out NotificationPolicy
	if err := c.doJSON(ctx, http.MethodGet, policiesPath, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetNotificationPolicy replaces the entire notification policy tree.
func (c *Client) SetNotificationPolicy(ctx context.Context, p NotificationPolicy) error {
	return c.doJSON(ctx, http.MethodPut, policiesPath, p, nil)
}

// ResetNotificationPolicy restores the default notification policy.
func (c *Client) ResetNotificationPolicy(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodDelete, policiesPath, nil, nil)
}

// ExportNotificationPolicy exports the notification policy tree.
func (c *Client) ExportNotificationPolicy(ctx context.Context, format string) ([]byte, error) {
	return c.doRaw(ctx, policiesPath+"/export?format="+url.QueryEscape(format))
}

// ---------------------------------------------------------------------------
// Notification templates
// ---------------------------------------------------------------------------

// ListTemplates returns all notification templates.
func (c *Client) ListTemplates(ctx context.Context) ([]NotificationTemplate, error) {
	var out []NotificationTemplate
	if err := c.doJSON(ctx, http.MethodGet, templatesPath, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetTemplate returns a template by name.
func (c *Client) GetTemplate(ctx context.Context, name string) (*NotificationTemplate, error) {
	var out NotificationTemplate
	path := templatesPath + "/" + url.PathEscape(name)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpsertTemplate creates or updates a template by name. The provisioning API
// uses the same PUT endpoint for both create and update.
func (c *Client) UpsertTemplate(ctx context.Context, t NotificationTemplate) (*NotificationTemplate, error) {
	var out NotificationTemplate
	path := templatesPath + "/" + url.PathEscape(t.Name)
	if err := c.doJSON(ctx, http.MethodPut, path, t, &out); err != nil {
		return nil, err
	}
	if out.Name == "" {
		// Some versions return 202 with empty body; echo the input back.
		return &t, nil
	}
	return &out, nil
}

// DeleteTemplate deletes a template by name.
func (c *Client) DeleteTemplate(ctx context.Context, name string) error {
	path := templatesPath + "/" + url.PathEscape(name)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}
