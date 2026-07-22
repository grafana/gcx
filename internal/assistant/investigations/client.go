package investigations

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

// v1 investigation endpoint paths. All v1 routes live under the plugin's
// /api/v1 surface; the assistanthttp base only contributes the plugin prefix.
const (
	v1InvestigationsBasePath = "/api/v1/investigations"
	v1ListPath               = v1InvestigationsBasePath + "/summary"
	v1CreatePath             = v1InvestigationsBasePath
)

// Client is an HTTP client for Assistant investigation endpoints. v1 methods
// use /api/v1 paths; v2 methods use /api/v2 paths. Callers gate v2 calls on
// DetectAPIMode().SupportsV2() at the command layer.
type Client struct {
	base *assistanthttp.Client
}

// NewClient creates a new investigation client.
func NewClient(base *assistanthttp.Client) *Client {
	return &Client{base: base}
}

// ListOptions holds optional parameters for listing investigations.
type ListOptions struct {
	State  string
	Limit  int
	Offset int
}

// List returns investigation summaries.
func (c *Client) List(ctx context.Context, opts ListOptions) ([]InvestigationSummary, error) {
	params := url.Values{}
	if opts.State != "" {
		params.Set("state", opts.State)
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		params.Set("offset", strconv.Itoa(opts.Offset))
	}
	path := v1ListPath
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list investigations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data struct {
			Investigations []InvestigationSummary `json:"investigations"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode investigations: %w", err)
	}
	if envelope.Data.Investigations == nil {
		return []InvestigationSummary{}, nil
	}
	return envelope.Data.Investigations, nil
}

// Get returns full investigation detail by ID.
func (c *Client) Get(ctx context.Context, id string) (*Investigation, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, v1InvestigationsBasePath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get investigation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data Investigation `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode investigation: %w", err)
	}
	return &envelope.Data, nil
}

// Create creates a new investigation.
func (c *Client) Create(ctx context.Context, req CreateRequest) (*CreateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, v1CreatePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create investigation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data CreateResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode create response: %w", err)
	}
	return &envelope.Data, nil
}

// Cancel cancels a running investigation.
func (c *Client) Cancel(ctx context.Context, id string) (*CancelResponse, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodPost, v1InvestigationsBasePath+"/"+url.PathEscape(id)+"/cancel", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel investigation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data CancelResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode cancel response: %w", err)
	}
	return &envelope.Data, nil
}
