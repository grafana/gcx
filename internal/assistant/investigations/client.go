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
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/investigations/"+url.PathEscape(id), nil)
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

	resp, err := c.base.DoRequest(ctx, http.MethodPost, "/investigations", bytes.NewReader(body))
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
	resp, err := c.base.DoRequest(ctx, http.MethodPost, "/investigations/"+url.PathEscape(id)+"/cancel", nil)
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

// Todos returns agent tasks for an investigation by extracting them from the
// timeline-snapshot endpoint, which lists all agents and their statuses.
func (c *Client) Todos(ctx context.Context, id string) ([]Todo, error) {
	agents, err := c.Timeline(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get todos for investigation %s: %w", id, err)
	}

	todos := make([]Todo, 0, len(agents))
	for _, a := range agents {
		todos = append(todos, Todo{
			ID:     a.AgentID,
			Title:  a.AgentName,
			Status: a.Status,
		})
	}
	return todos, nil
}

// Timeline returns the activity timeline for an investigation.
func (c *Client) Timeline(ctx context.Context, id string) ([]TimelineAgent, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/investigations/"+url.PathEscape(id)+"/timeline-snapshot", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get timeline for investigation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, assistanthttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Data struct {
			Agents []TimelineAgent `json:"agents"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode timeline: %w", err)
	}
	if envelope.Data.Agents == nil {
		return []TimelineAgent{}, nil
	}
	return envelope.Data.Agents, nil
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

	var envelope struct {
		Data ReportSummary `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode report: %w", err)
	}
	return &envelope.Data, nil
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

	var envelope struct {
		Data Document `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode document: %w", err)
	}
	return &envelope.Data, nil
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

	var envelope struct {
		Data struct {
			Approvals []Approval `json:"approvals"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode approvals: %w", err)
	}
	if envelope.Data.Approvals == nil {
		return []Approval{}, nil
	}
	return envelope.Data.Approvals, nil
}
