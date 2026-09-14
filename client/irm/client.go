// Package irm is a typed Go client for Grafana IRM's incidents API. It takes
// a caller-supplied *http.Client and base URL: it builds no transport,
// discovers no tokens, and reads no config file. Auth is entirely the
// caller's responsibility.
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

	"github.com/grafana/gcx/client/internal/httperr"
)

// ErrNotFound is returned when a requested incident does not exist.
var ErrNotFound = errors.New("incident not found")

const (
	// incidentBasePath is the documented versioned base path of the IRM
	// Incident API (IncidentsService, ActivityService).
	incidentBasePath = "/api/plugins/grafana-irm-app/resources/api/v1"
	// incidentLegacyBasePath is the unversioned base path, which older
	// Grafana builds answer on.
	incidentLegacyBasePath = "/api/plugins/grafana-irm-app/resources/api"

	incCreatePath = incidentBasePath + "/IncidentsService.CreateIncident"

	// The method name is held apart from the base path, because
	// doIncidentMethod tries the versioned base path first and the
	// unversioned one second.
	incGetMethod = "IncidentsService.GetIncident"

	incQueryPath = incidentBasePath + "/IncidentsService.QueryIncidentPreviews"
	actAddPath   = incidentBasePath + "/ActivityService.AddActivity"
)

// IncidentClient is a typed HTTP client for the Grafana IRM Incidents API.
type IncidentClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewIncidentClient creates a new incidents client using the given HTTP
// client and base URL. The caller owns auth: supply an *http.Client whose
// transport already attaches whatever credentials the target Grafana
// instance requires.
func NewIncidentClient(httpClient *http.Client, baseURL string) *IncidentClient {
	return &IncidentClient{httpClient: httpClient, baseURL: baseURL}
}

// incidentsMaxPageSize is the documented maximum for IncidentPreviewsQuery.limit.
const incidentsMaxPageSize = 100

// quoteIncidentQueryValue wraps a value for the incident query-string
// language, which requires quoting for values containing spaces.
func quoteIncidentQueryValue(v string) string {
	return `"` + v + `"`
}

func isIncidentStatusFilter(s string) bool {
	return s == "active" || s == "resolved"
}

func incidentLabelValue(l IncidentLabel) string {
	if l.Label != "" {
		return l.Label
	}
	return l.Value
}

func incidentMatchesLabel(labels []IncidentLabel, filter string) bool {
	key, value, keyed := strings.Cut(filter, ":")
	for _, l := range labels {
		labelValue := incidentLabelValue(l)
		if labelValue == filter {
			return true
		}
		if keyed && key != "" && value != "" && l.Key == key && labelValue == value {
			return true
		}
	}
	return false
}

func incidentMatchesLabels(labels []IncidentLabel, filters []string) bool {
	for _, f := range filters {
		if !incidentMatchesLabel(labels, f) {
			return false
		}
	}
	return true
}

// buildIncidentQueryString compiles the structured filters into a single
// incident query-string-language expression. A non-empty query.QueryString is
// used verbatim (raw escape hatch) and the structured filters are ignored.
//
// Terms are juxtaposed, which the language treats as AND; values within the
// multi-valued statuses filter are ORed with or(...) so that two statuses
// match either, not both at once. Verified live against QueryIncidentPreviews.
func buildIncidentQueryString(query IncidentQuery) string {
	if query.QueryString != "" {
		return query.QueryString
	}

	var terms []string
	if len(query.Statuses) > 0 {
		statusTerms := make([]string, len(query.Statuses))
		for i, s := range query.Statuses {
			statusTerms[i] = "status:" + s
		}
		if len(statusTerms) == 1 {
			terms = append(terms, statusTerms[0])
		} else {
			// or(...) — match any of the statuses; juxtaposition would AND
			// them and match nothing.
			terms = append(terms, "or("+strings.Join(statusTerms, " ")+")")
		}
	}
	if query.Severity != "" {
		terms = append(terms, "severity:"+quoteIncidentQueryValue(query.Severity))
	}
	return strings.Join(terms, " ")
}

// validateIncidentQuery rejects filter values the incident query-string
// language cannot express: a status outside the supported enum, or a severity
// containing a double quote. A raw query string is the complete server-side
// expression, so structured fields are ignored and not validated.
func validateIncidentQuery(query IncidentQuery) error {
	if query.QueryString != "" {
		return nil
	}
	for _, s := range query.Statuses {
		if !isIncidentStatusFilter(s) {
			return fmt.Errorf("incidents: invalid status %q: must be active or resolved", s)
		}
	}
	if strings.Contains(query.Severity, `"`) {
		return fmt.Errorf("incidents: invalid severity %q: the incident query-string language cannot express values containing double quotes", query.Severity)
	}
	return nil
}

// incidentPreviewFilter enforces the bounds QueryIncidentPreviews has no fields
// for: the createdTime window (dateFrom inclusive, dateTo exclusive) and label
// matching. A zero from/to disables that side of the window; empty labels
// disables label matching.
type incidentPreviewFilter struct {
	from, to    time.Time
	newestFirst bool
	labels      []string
}

// classify reports whether a preview should be kept (first result) and whether
// paging can stop early because the newest-first crawl has passed the
// from-bound (second result); when stop is true, keep is always false.
func (f incidentPreviewFilter) classify(p incidentPreview) (bool, bool) {
	created := time.Time(p.CreatedTime)
	if created.IsZero() {
		// A preview without a createdTime cannot be placed in the requested
		// window, so date-bounded queries exclude it.
		if !f.from.IsZero() || !f.to.IsZero() {
			return false, false
		}
		return f.matchesLabels(p), false
	}
	if !f.from.IsZero() && created.Before(f.from) {
		return false, f.newestFirst
	}
	if !f.to.IsZero() && !created.Before(f.to) {
		return false, false
	}
	return f.matchesLabels(p), false
}

func (f incidentPreviewFilter) matchesLabels(p incidentPreview) bool {
	if len(f.labels) == 0 {
		return true
	}
	return incidentMatchesLabels(p.Labels, f.labels)
}

// List queries incident previews with the given parameters, following the
// response cursor until query.Limit incidents are collected or the server
// reports no more pages. A non-positive query.Limit defaults to 100.
//
// QueryIncidentPreviews has no structured filter fields: statuses and severity
// are compiled into the query-string language. Label and date bounds are
// enforced here against returned previews: labels are matched as either plain
// label text or key:value pairs, and createdTime uses dateFrom inclusive and
// dateTo exclusive. Because that matching is client-side, a highly selective
// label or date filter can page through the full history before collecting
// query.Limit results.
func (c *IncidentClient) List(ctx context.Context, query IncidentQuery) ([]Incident, error) {
	if err := validateIncidentQuery(query); err != nil {
		return nil, err
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
		QueryString:    buildIncidentQueryString(query),
	}

	var from, to time.Time
	if query.DateFrom != nil {
		from = time.Time(*query.DateFrom)
	}
	if query.DateTo != nil {
		to = time.Time(*query.DateTo)
	}
	labelFiltered := query.QueryString == "" && len(query.IncidentLabels) > 0
	filter := incidentPreviewFilter{
		from: from,
		to:   to,
		// With the default createdTime-descending order, every incident after
		// the first one older than `from` is older too, so paging can stop
		// early.
		newestFirst: query.OrderDirection == "DESC" && query.OrderField == "createdTime",
	}
	if labelFiltered {
		filter.labels = query.IncidentLabels
	}
	clientFiltered := !from.IsZero() || !to.IsZero() || labelFiltered

	limit := query.Limit
	var (
		all      []Incident
		cursor   *incidentCursor
		pastFrom bool
	)
	for {
		if clientFiltered {
			// Client-side filters can discard any number of previews, so a
			// page can contribute anywhere from zero to all of its previews.
			// Fetch full pages to keep the crawl towards the bounds short;
			// the result is truncated to limit below.
			wire.Limit = incidentsMaxPageSize
		} else {
			wire.Limit = min(limit-len(all), incidentsMaxPageSize)
		}
		resp, err := c.queryIncidentPreviews(ctx, wire, cursor)
		if err != nil {
			return nil, err
		}

		for _, preview := range resp.IncidentPreviews {
			keep, stop := filter.classify(preview)
			if stop {
				// Newest-first crawl passed the from-bound; no later preview
				// can fall in range.
				pastFrom = true
				break
			}
			if keep {
				all = append(all, preview.toIncident())
			}
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

// Get returns a single incident by ID. It returns ErrNotFound when no
// incident carries that ID.
func (c *IncidentClient) Get(ctx context.Context, id string) (*Incident, error) {
	body, err := json.Marshal(map[string]string{"incidentID": id})
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal get request: %w", err)
	}

	resp, err := c.doIncidentMethod(ctx, incGetMethod, body)
	if err != nil {
		return nil, fmt.Errorf("incidents: get %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("incidents: get %s: %w", id, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httperr.FromResponse(resp)
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
//
// CreateIncident ignores both severity and severityID of the request body:
// every incident starts at the organization's default severity, and
// IncidentsService.UpdateSeverity is the only route to another one.
func (c *IncidentClient) Create(ctx context.Context, inc *Incident) (*Incident, error) {
	req := createIncidentRequest{
		Title:          inc.Title,
		Status:         inc.Status,
		IsDrill:        inc.IsDrill,
		Labels:         inc.Labels,
		IncidentType:   inc.IncidentType,
		FieldGroupUUID: inc.FieldGroupUUID,
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
		return nil, httperr.FromResponse(resp)
	}

	var result createIncidentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode create response: %w", err)
	}

	return &result.Incident, nil
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
		return httperr.FromResponse(resp)
	}

	return nil
}

// queryIncidentPreviews fetches a single page. cursor is nil for the first
// page and the previously returned cursor for subsequent pages. Custom field
// values and incident channels are always requested so previews carry the
// same optional data the full incidents used to.
func (c *IncidentClient) queryIncidentPreviews(ctx context.Context, query incidentPreviewsQuery, cursor *incidentCursor) (*queryIncidentPreviewsResponse, error) {
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
		return nil, httperr.FromResponse(resp)
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

// doIncidentMethod posts to an IncidentsService method. A 404 retries the
// unversioned base path so Get supports the same Grafana builds the
// versioned path does. The caller owns and must close the response body.
func (c *IncidentClient) doIncidentMethod(ctx context.Context, method string, data []byte) (*http.Response, error) {
	resp, err := c.doRequest(ctx, incidentBasePath+"/"+method, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusNotFound {
		return resp, nil
	}

	resp.Body.Close()
	return c.doRequest(ctx, incidentLegacyBasePath+"/"+method, bytes.NewReader(data))
}

// doRequest builds and executes a POST request against the IRM API.
// The IRM API uses POST for all operations (gRPC-style).
func (c *IncidentClient) doRequest(ctx context.Context, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
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
