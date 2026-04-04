package investigations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
)

// Client is an HTTP client for Assistant investigation endpoints.
type Client struct {
	base *assistanthttp.Client
}

// NewClient creates a new investigation client.
func NewClient(base *assistanthttp.Client) *Client {
	return &Client{base: base}
}

// List returns investigation summaries.
func (c *Client) List(ctx context.Context, state string) ([]InvestigationSummary, error) {
	path := "/investigations/summary"
	if state != "" {
		path += "?" + url.Values{"state": {state}}.Encode()
	}

	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list investigations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var summaries []InvestigationSummary
	if err := json.NewDecoder(resp.Body).Decode(&summaries); err != nil {
		return nil, fmt.Errorf("failed to decode investigations: %w", err)
	}
	if summaries == nil {
		return []InvestigationSummary{}, nil
	}
	return summaries, nil
}

// Get returns full investigation detail by ID.
func (c *Client) Get(ctx context.Context, id string) (*Investigation, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/investigations/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get investigation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var inv Investigation
	if err := json.NewDecoder(resp.Body).Decode(&inv); err != nil {
		return nil, fmt.Errorf("failed to decode investigation: %w", err)
	}
	return &inv, nil
}

// Create creates a new investigation.
func (c *Client) Create(ctx context.Context, req CreateRequest) (*CreateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, "/investigations", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create investigation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var result CreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode create response: %w", err)
	}
	return &result, nil
}

// Cancel cancels a running investigation.
func (c *Client) Cancel(ctx context.Context, id string) (*CancelResponse, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodPost, "/investigations/"+url.PathEscape(id)+"/cancel", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel investigation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var result CancelResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode cancel response: %w", err)
	}
	return &result, nil
}

// Todos returns agent tasks for an investigation.
func (c *Client) Todos(ctx context.Context, id string) ([]Todo, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/investigations/"+url.PathEscape(id)+"/todos", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get todos for investigation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var todos []Todo
	if err := json.NewDecoder(resp.Body).Decode(&todos); err != nil {
		return nil, fmt.Errorf("failed to decode todos: %w", err)
	}
	if todos == nil {
		return []Todo{}, nil
	}
	return todos, nil
}

// Timeline returns the activity timeline for an investigation.
func (c *Client) Timeline(ctx context.Context, id string) ([]TimelineEntry, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/investigations/"+url.PathEscape(id)+"/timeline-snapshot", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get timeline for investigation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var entries []TimelineEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("failed to decode timeline: %w", err)
	}
	if entries == nil {
		return []TimelineEntry{}, nil
	}
	return entries, nil
}

// Report returns the condensed report summary for an investigation.
func (c *Client) Report(ctx context.Context, id string) (*ReportSummary, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/investigations/"+url.PathEscape(id)+"/report-summary", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get report for investigation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var report ReportSummary
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, fmt.Errorf("failed to decode report: %w", err)
	}
	return &report, nil
}

// Document returns a specific document from an investigation.
func (c *Client) Document(ctx context.Context, id, docID string) (*Document, error) {
	path := "/investigations/" + url.PathEscape(id) + "/documents/" + url.PathEscape(docID)
	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get document %s for investigation %s: %w", docID, id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var doc Document
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to decode document: %w", err)
	}
	return &doc, nil
}

// Approvals returns approval requests for an investigation.
func (c *Client) Approvals(ctx context.Context, id string) ([]Approval, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/investigations/"+url.PathEscape(id)+"/approvals", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get approvals for investigation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var approvals []Approval
	if err := json.NewDecoder(resp.Body).Decode(&approvals); err != nil {
		return nil, fmt.Errorf("failed to decode approvals: %w", err)
	}
	if approvals == nil {
		return []Approval{}, nil
	}
	return approvals, nil
}
