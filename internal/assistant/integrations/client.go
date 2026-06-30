package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
)

type Client struct {
	base *assistanthttp.Client
}

// NewClient creates a new integrations client.
func NewClient(base *assistanthttp.Client) *Client {
	return &Client{base: base}
}

// ListOptions holds optional parameters for listing integrations.
type ListOptions struct {
	Scope       string
	EnabledOnly bool
	Limit       int
	Offset      int
}

// List returns integrations with optional filtering.
func (c *Client) List(ctx context.Context, opts ListOptions) ([]Integration, *Pagination, error) {
	params := url.Values{}
	if opts.Scope != "" {
		params.Set("scope", opts.Scope)
	}
	if opts.EnabledOnly {
		params.Set("enabled_only", "true")
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		params.Set("offset", strconv.Itoa(opts.Offset))
	}

	path := "/integrations"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list integrations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data struct {
			Integrations []Integration `json:"integrations"`
			Pagination   Pagination    `json:"pagination"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, nil, fmt.Errorf("failed to decode integrations: %w", err)
	}
	if envelope.Data.Integrations == nil {
		return []Integration{}, &envelope.Data.Pagination, nil
	}
	return envelope.Data.Integrations, &envelope.Data.Pagination, nil
}

// Get returns a single integration by ID.
func (c *Client) Get(ctx context.Context, id string) (*Integration, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/integrations/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get integration %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data Integration `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode integration: %w", err)
	}
	return &envelope.Data, nil
}

// Create creates a new integration.
func (c *Client) Create(ctx context.Context, req CreateRequest) (*Integration, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, "/integrations", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create integration: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data Integration `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode create response: %w", err)
	}
	return &envelope.Data, nil
}

// Update updates an existing integration.
func (c *Client) Update(ctx context.Context, id string, req UpdateRequest) (*Integration, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal update request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPut, "/integrations/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update integration %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data Integration `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode update response: %w", err)
	}
	return &envelope.Data, nil
}

// Delete fetches the integration to discover its scope (required by the
// API's X-Resource-Scope header), then issues the DELETE.
func (c *Client) Delete(ctx context.Context, id string) error {
	integration, err := c.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to fetch integration before delete: %w", err)
	}

	headers := http.Header{}
	headers.Set("X-Resource-Scope", integration.Scope)

	resp, err := c.base.DoRequestWithHeaders(ctx, http.MethodDelete, "/integrations/"+url.PathEscape(id), nil, headers)
	if err != nil {
		return fmt.Errorf("failed to delete integration %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return assistanthttp.HandleErrorResponse(resp)
	}
	return nil
}

// Validate tests connectivity for an integration and returns discovered tools.
func (c *Client) Validate(ctx context.Context, id string) (*ValidationResult, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/integrations/"+url.PathEscape(id)+"/validate", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to validate integration %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data struct {
			Result ValidationResult `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode validation response: %w", err)
	}
	return &envelope.Data.Result, nil
}
