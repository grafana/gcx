package incidents

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

// ErrNotFound is returned when a requested incident does not exist.
var ErrNotFound = errors.New("incident not found")

const basePath = "/api/plugins/grafana-irm-app/resources/api"

// Client is an HTTP client for the Grafana IRM Incidents API.
type Client struct {
	httpClient *http.Client
	host       string
}

// NewClient creates a new incidents client from the given REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: cfg.Host}, nil
}

// List queries incidents with the given parameters and handles pagination.
func (c *Client) List(ctx context.Context, query IncidentQuery) ([]Incident, error) {
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.OrderDirection == "" {
		query.OrderDirection = "DESC"
	}
	if query.OrderField == "" {
		query.OrderField = "createdTime"
	}

	limit := query.Limit
	var all []Incident
	for {
		resp, err := c.queryIncidents(ctx, query)
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Incidents...)
		if len(all) >= limit || !resp.Cursor.HasMore {
			break
		}
		query.ContextPayload = resp.Cursor.NextValue
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// Get returns a single incident by ID.
func (c *Client) Get(ctx context.Context, id string) (*Incident, error) {
	body, err := json.Marshal(map[string]string{"incidentID": id})
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal get request: %w", err)
	}

	resp, err := c.doRequest(ctx, basePath+"/IncidentsService.GetIncident", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: get %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("incidents: get %s: %w", id, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result struct {
		Incident Incident `json:"incident"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode get response: %w", err)
	}

	if result.Incident.IncidentID == "" {
		return nil, fmt.Errorf("incidents: get %s: %w", id, ErrNotFound)
	}

	return &result.Incident, nil
}

// Create creates a new incident and returns the created incident.
func (c *Client) Create(ctx context.Context, inc *Incident) (*Incident, error) {
	req := createIncidentRequest{
		Title:          inc.Title,
		Status:         inc.Status,
		IsDrill:        inc.IsDrill,
		Labels:         inc.Labels,
		IncidentType:   inc.IncidentType,
		FieldGroupUUID: inc.FieldGroupUUID,
		SeverityID:     inc.SeverityID,
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Labels == nil {
		req.Labels = []IncidentLabel{}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal create request: %w", err)
	}

	resp, err := c.doRequest(ctx, basePath+"/IncidentsService.CreateIncident", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result createIncidentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode create response: %w", err)
	}

	return &result.Incident, nil
}

// UpdateStatus updates an incident's status and returns the updated incident.
func (c *Client) UpdateStatus(ctx context.Context, id, status string) (*Incident, error) {
	req := updateStatusRequest{
		IncidentID: id,
		Status:     status,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal update request: %w", err)
	}

	resp, err := c.doRequest(ctx, basePath+"/IncidentsService.UpdateStatus", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: update status %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result updateStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode update response: %w", err)
	}

	return &result.Incident, nil
}

// QueryActivity retrieves the activity timeline for an incident.
func (c *Client) QueryActivity(ctx context.Context, incidentID string, limit int) ([]ActivityItem, error) {
	if limit <= 0 {
		limit = 50
	}

	body, err := json.Marshal(map[string]any{
		"query": map[string]any{
			"incidentID":     incidentID,
			"limit":          limit,
			"orderDirection": "ASC",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal activity request: %w", err)
	}

	resp, err := c.doRequest(ctx, basePath+"/ActivityService.QueryActivity", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: query activity for %s: %w", incidentID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result struct {
		ActivityItems []ActivityItem `json:"activityItems"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode activity response: %w", err)
	}

	return result.ActivityItems, nil
}

// AddActivity adds an activity note to an incident.
func (c *Client) AddActivity(ctx context.Context, incidentID, body string) error {
	reqBody, err := json.Marshal(map[string]string{
		"incidentID":   incidentID,
		"activityKind": "userNote",
		"body":         body,
	})
	if err != nil {
		return fmt.Errorf("incidents: marshal add activity request: %w", err)
	}

	resp, err := c.doRequest(ctx, basePath+"/ActivityService.AddActivity", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("incidents: add activity to %s: %w", incidentID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return handleErrorResponse(resp)
	}

	return nil
}

// GetSeverities retrieves the organization's severity levels.
func (c *Client) GetSeverities(ctx context.Context) ([]Severity, error) {
	body, err := json.Marshal(map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal severities request: %w", err)
	}

	resp, err := c.doRequest(ctx, basePath+"/SeveritiesService.GetOrgSeverities", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: get severities: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result struct {
		Severities []Severity `json:"severities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode severities response: %w", err)
	}

	return result.Severities, nil
}

// queryIncidents performs a single paginated query.
func (c *Client) queryIncidents(ctx context.Context, query IncidentQuery) (*queryIncidentsResponse, error) {
	body, err := json.Marshal(queryIncidentsRequest{Query: query})
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal query request: %w", err)
	}

	resp, err := c.doRequest(ctx, basePath+"/IncidentsService.QueryIncidents", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result queryIncidentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode query response: %w", err)
	}

	return &result, nil
}

// doRequest builds and executes a POST request against the IRM API.
// The IRM API uses POST for all operations (gRPC-style).
func (c *Client) doRequest(ctx context.Context, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}

// handleErrorResponse reads an error response body and returns a formatted error.
func handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, errResp.Error)
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}
