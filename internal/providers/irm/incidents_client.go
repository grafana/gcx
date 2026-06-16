package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/client-go/rest"
)

// ErrNotFound wraps adapter.ErrNotFound so the adapter layer can detect
// not-found and fall through to Create during push upsert.
var ErrNotFound = fmt.Errorf("incident: %w", adapter.ErrNotFound)

const (
	// incidentBasePath is the documented versioned base path of the IRM
	// Incident API (IncidentsService, ActivityService).
	incidentBasePath = "/api/plugins/grafana-irm-app/resources/api/v1"
	// incidentLegacyBasePath is the unversioned base path. SeveritiesService
	// and IncidentContextService are not part of the documented v1 API and
	// 404 under the /v1 prefix — they only respond here.
	incidentLegacyBasePath = "/api/plugins/grafana-irm-app/resources/api"

	incGetPath        = incidentBasePath + "/IncidentsService.GetIncident"
	incCreatePath     = incidentBasePath + "/IncidentsService.CreateIncident"
	incUpdateStatPath = incidentBasePath + "/IncidentsService.UpdateStatus"
	incQueryPath      = incidentBasePath + "/IncidentsService.QueryIncidentPreviews"
	actQueryPath      = incidentBasePath + "/ActivityService.QueryActivity"
	actAddPath        = incidentBasePath + "/ActivityService.AddActivity"
	sevGetPath        = incidentLegacyBasePath + "/SeveritiesService.GetOrgSeverities"
	ctxQueryPath      = incidentLegacyBasePath + "/IncidentContextService.QueryIncidentContext"
)

// Client is an HTTP client for the Grafana IRM Incidents API.
type IncidentClient struct {
	httpClient *http.Client
	host       string
}

// NewClient creates a new incidents client from the given REST config.
func NewIncidentClient(cfg config.NamespacedRESTConfig) (*IncidentClient, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &IncidentClient{httpClient: httpClient, host: cfg.Host}, nil
}

// incidentsMaxPageSize is the documented maximum for IncidentPreviewsQuery.limit.
const incidentsMaxPageSize = 100

// quoteIncidentQueryValue wraps a value for the incident query-string
// language, which requires quoting for values containing spaces or colons.
// Double-quoted values match both Tags-key label text and keyed key:value
// composites and may themselves contain single quotes. The reverse does not
// hold — the server rejects double quotes inside single-quoted values — so
// values containing a double quote cannot be expressed in the language and
// are rejected by List and by the list command's validation.
func quoteIncidentQueryValue(v string) string {
	return `"` + v + `"`
}

// buildLabelsQueryString translates label filters into query-string terms.
// Juxtaposed label terms AND together, which matches the behaviour of the
// structured incidentLabels filter on the deprecated QueryIncidents endpoint
// (verified live: identical result sets).
func buildLabelsQueryString(labels []string) string {
	terms := make([]string, len(labels))
	for i, l := range labels {
		terms[i] = "label:" + quoteIncidentQueryValue(l)
	}
	return strings.Join(terms, " ")
}

// List queries incident previews with the given parameters, following the
// response cursor until query.Limit incidents are collected or the server
// reports no more pages. A non-positive query.Limit defaults to 100.
//
// QueryIncidentPreviews has no structured filter fields: label filters are
// translated into the query-string language, and date bounds — which even
// the deprecated QueryIncidents endpoint ignored server-side — are enforced
// here on createdTime, dateFrom inclusive and dateTo exclusive.
func (c *IncidentClient) List(ctx context.Context, query IncidentQuery) ([]Incident, error) {
	for _, l := range query.IncidentLabels {
		if strings.Contains(l, `"`) {
			return nil, fmt.Errorf("incidents: invalid label %q: the incident query-string language cannot express values containing double quotes", l)
		}
	}

	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.OrderDirection == "" {
		query.OrderDirection = "DESC"
	}
	if query.OrderField == "" {
		query.OrderField = "createdTime"
	}

	wire := incidentPreviewsQuery{
		OrderDirection: query.OrderDirection,
		OrderField:     query.OrderField,
		QueryString:    buildLabelsQueryString(query.IncidentLabels),
	}

	var from, to time.Time
	if query.DateFrom != nil {
		from = time.Time(*query.DateFrom)
	}
	if query.DateTo != nil {
		to = time.Time(*query.DateTo)
	}
	dateBounded := !from.IsZero() || !to.IsZero()
	// With the default createdTime-descending order, every incident after the
	// first one older than `from` is older too, so paging can stop early.
	newestFirst := query.OrderDirection == "DESC" && query.OrderField == "createdTime"

	limit := query.Limit
	var (
		all      []Incident
		cursor   *IncidentCursor
		pastFrom bool
	)
	for {
		if dateBounded {
			// Date filtering happens client-side, so a page can contribute
			// anywhere from zero to all of its previews. Fetch full pages to
			// keep the crawl towards the bounds short; the result is
			// truncated to limit below.
			wire.Limit = incidentsMaxPageSize
		} else {
			wire.Limit = min(limit-len(all), incidentsMaxPageSize)
		}
		resp, err := c.queryIncidentPreviews(ctx, wire, cursor)
		if err != nil {
			return nil, err
		}

		for _, preview := range resp.IncidentPreviews {
			created := time.Time(preview.CreatedTime)
			if created.IsZero() {
				// A preview without a createdTime cannot be placed in the
				// requested window, so date-bounded queries exclude it.
				if from.IsZero() && to.IsZero() {
					all = append(all, preview.ToIncident())
				}
				continue
			}
			if !from.IsZero() && created.Before(from) {
				if newestFirst {
					pastFrom = true
					break
				}
				continue
			}
			if !to.IsZero() && !created.Before(to) {
				continue
			}
			all = append(all, preview.ToIncident())
		}

		if len(all) >= limit {
			return all[:limit], nil
		}
		// Stop when no further page can add results: the from-bound was
		// crossed, the server reports no more pages, or it returns an empty
		// page or cursor value (looping on those would re-fetch forever).
		if pastFrom || !resp.Cursor.HasMore || resp.Cursor.NextValue == "" || len(resp.IncidentPreviews) == 0 {
			return all, nil
		}
		// The API contract is to pass previously returned cursor values back
		// as-is.
		cursor = &resp.Cursor
	}
}

// Get returns a single incident by ID.
func (c *IncidentClient) Get(ctx context.Context, id string) (*Incident, error) {
	body, err := json.Marshal(map[string]string{"incidentID": id})
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal get request: %w", err)
	}

	resp, err := c.doRequest(ctx, incGetPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: get %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("incidents: get %s: %w", id, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, handleIncidentErrorResponse(resp)
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
func (c *IncidentClient) Create(ctx context.Context, inc *Incident) (*Incident, error) {
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

	resp, err := c.doRequest(ctx, incCreatePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: create: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleIncidentErrorResponse(resp)
	}

	var result createIncidentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode create response: %w", err)
	}

	return &result.Incident, nil
}

// UpdateStatus updates an incident's status and returns the updated incident.
func (c *IncidentClient) UpdateStatus(ctx context.Context, id, status string) (*Incident, error) {
	req := updateStatusRequest{
		IncidentID: id,
		Status:     status,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal update request: %w", err)
	}

	resp, err := c.doRequest(ctx, incUpdateStatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: update status %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleIncidentErrorResponse(resp)
	}

	var result updateStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode update response: %w", err)
	}

	return &result.Incident, nil
}

// QueryActivity retrieves the activity timeline for an incident.
func (c *IncidentClient) QueryActivity(ctx context.Context, incidentID string, limit int) ([]ActivityItem, error) {
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

	resp, err := c.doRequest(ctx, actQueryPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: query activity for %s: %w", incidentID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleIncidentErrorResponse(resp)
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
func (c *IncidentClient) AddActivity(ctx context.Context, incidentID, body string) error {
	reqBody, err := json.Marshal(map[string]string{
		"incidentID":   incidentID,
		"activityKind": "userNote",
		"body":         body,
	})
	if err != nil {
		return fmt.Errorf("incidents: marshal add activity request: %w", err)
	}

	resp, err := c.doRequest(ctx, actAddPath, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("incidents: add activity to %s: %w", incidentID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return handleIncidentErrorResponse(resp)
	}

	return nil
}

// QueryIncidentContext returns the contexts (alert groups, dashboards, …)
// attached to an incident. Additional fields on query — Type, Status,
// AlertGroupID, etc. — narrow the result; only IncidentID is required.
func (c *IncidentClient) QueryIncidentContext(ctx context.Context, query IncidentContextQuery) ([]IncidentContext, error) {
	if query.IncidentID == "" {
		return nil, errors.New("incidents: QueryIncidentContext: incidentID is required")
	}

	body, err := json.Marshal(queryIncidentContextRequest{Query: query})
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal context query: %w", err)
	}

	resp, err := c.doRequest(ctx, ctxQueryPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: query context for %s: %w", query.IncidentID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleIncidentErrorResponse(resp)
	}

	var result queryIncidentContextResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode context response: %w", err)
	}

	return result.IncidentContexts, nil
}

// GetSeverities retrieves the organization's severity levels.
func (c *IncidentClient) GetSeverities(ctx context.Context) ([]Severity, error) {
	body, err := json.Marshal(map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal severities request: %w", err)
	}

	resp, err := c.doRequest(ctx, sevGetPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: get severities: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleIncidentErrorResponse(resp)
	}

	var result struct {
		Severities []Severity `json:"severities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode severities response: %w", err)
	}

	return result.Severities, nil
}

// queryIncidentPreviews fetches a single page. cursor is nil for the first
// page and the previously returned cursor for subsequent pages. Custom field
// values and incident channels are always requested so previews carry the
// same optional data the full incidents used to.
func (c *IncidentClient) queryIncidentPreviews(ctx context.Context, query incidentPreviewsQuery, cursor *IncidentCursor) (*queryIncidentPreviewsResponse, error) {
	body, err := json.Marshal(queryIncidentPreviewsRequest{
		Query:                    query,
		Cursor:                   cursor,
		IncludeCustomFieldValues: true,
		IncludeIncidentChannels:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal query request: %w", err)
	}

	resp, err := c.doRequest(ctx, incQueryPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleIncidentErrorResponse(resp)
	}

	var result queryIncidentPreviewsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode query response: %w", err)
	}
	// The API can report failure in-band on a 200 response.
	if result.Error != "" {
		return nil, fmt.Errorf("incidents: query: %s", result.Error)
	}

	return &result, nil
}

// doRequest builds and executes a POST request against the IRM API.
// The IRM API uses POST for all operations (gRPC-style).
func (c *IncidentClient) doRequest(ctx context.Context, path string, body io.Reader) (*http.Response, error) {
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

// handleIncidentErrorResponse reads an error response body and returns a formatted error.
func handleIncidentErrorResponse(resp *http.Response) error {
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
