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
	"github.com/grafana/gcx/internal/providers"
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

	incGetPath    = incidentBasePath + "/IncidentsService.GetIncident"
	incCreatePath = incidentBasePath + "/IncidentsService.CreateIncident"

	// The IRM API has one operation per mutable field: CreateIncident ignores
	// the severity of the request body, and there is no combined update
	// method. The method names are held apart from the base path, because
	// updateIncidentField tries the versioned base path first and the
	// unversioned one second.
	incUpdateStatusMethod   = "IncidentsService.UpdateStatus"
	incUpdateSeverityMethod = "IncidentsService.UpdateSeverity"
	incUpdateTitleMethod    = "IncidentsService.UpdateTitle"

	incQueryPath = incidentBasePath + "/IncidentsService.QueryIncidentPreviews"
	actQueryPath = incidentBasePath + "/ActivityService.QueryActivity"
	actAddPath   = incidentBasePath + "/ActivityService.AddActivity"
	sevGetPath   = incidentLegacyBasePath + "/SeveritiesService.GetOrgSeverities"
	ctxQueryPath = incidentLegacyBasePath + "/IncidentContextService.QueryIncidentContext"
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
func (f incidentPreviewFilter) classify(p IncidentPreview) (bool, bool) {
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

func (f incidentPreviewFilter) matchesLabels(p IncidentPreview) bool {
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
		cursor   *IncidentCursor
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
				all = append(all, preview.ToIncident())
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
		return nil, providers.HandleErrorResponse(resp)
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
// every incident starts at the default severity. Severity is the first column
// of any incident report, so a caller that cannot set it cannot provision
// reporting by severity. UpdateSeverity is the only route, and it takes the
// label, not the identifier. The label resolves before the create call,
// because the IRM API cannot delete an incident that a later step abandons.
func (c *IncidentClient) Create(ctx context.Context, inc *Incident) (*Incident, error) {
	label, err := c.resolveSeverityLabel(ctx, inc)
	if err != nil {
		return nil, err
	}

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
		return nil, providers.HandleErrorResponse(resp)
	}

	var result createIncidentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode create response: %w", err)
	}

	created := &result.Incident
	if label == "" || strings.EqualFold(created.Severity, label) {
		return created, nil
	}

	updated, err := c.UpdateSeverity(ctx, created.IncidentID, label)
	if err != nil {
		// The incident exists at the default severity, and the IRM API has no
		// delete method. The caller needs the identifier to repair it.
		return created, fmt.Errorf(
			"incidents: create: incident %s exists, but the severity update failed: %w: run `gcx irm incidents update %s --severity %q` to set the severity",
			created.IncidentID, err, created.IncidentID, label)
	}
	return updated, nil
}

// resolveSeverityLabel returns the severity label the caller asked for.
// A hand-written spec.severityID has precedence: when it is not empty, the
// organization severity list resolves it to a label, and gcx ignores
// spec.severity. A pulled manifest carries spec.severity alone, because the
// pull removes spec.severityID. An empty result means the caller asked for no
// severity.
func (c *IncidentClient) resolveSeverityLabel(ctx context.Context, inc *Incident) (string, error) {
	if inc.SeverityID == "" {
		return inc.Severity, nil
	}

	severities, err := c.GetSeverities(ctx)
	if err != nil {
		return "", fmt.Errorf("incidents: resolve severityID %q: %w", inc.SeverityID, err)
	}
	for _, s := range severities {
		if s.SeverityID == inc.SeverityID {
			return s.DisplayLabel, nil
		}
	}
	return "", fmt.Errorf("incidents: unknown severityID %q, run `gcx irm incidents severities list` for the valid values", inc.SeverityID)
}

// UpdateSeverity sets the severity of an incident and returns the updated
// incident. severity is the display label, not the identifier.
func (c *IncidentClient) UpdateSeverity(ctx context.Context, id, severity string) (*Incident, error) {
	req := updateSeverityRequest{IncidentID: id, Severity: severity}
	return c.updateIncidentField(ctx, incUpdateSeverityMethod, req, "update severity")
}

// UpdateTitle sets the title of an incident and returns the updated incident.
func (c *IncidentClient) UpdateTitle(ctx context.Context, id, title string) (*Incident, error) {
	req := updateTitleRequest{IncidentID: id, Title: title}
	return c.updateIncidentField(ctx, incUpdateTitleMethod, req, "update title")
}

// Update applies every field of inc that the IRM API exposes as its own
// operation: status, title, and severity. The API has no single update
// method, so each field costs one call. gcx reads the incident first, then
// skips each field that the caller left empty or that already matches the
// server. Nothing enforces the required fields of the schema on push, so a
// manifest without a status must not send an empty status.
func (c *IncidentClient) Update(ctx context.Context, id string, inc *Incident) (*Incident, error) {
	current, err := c.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if inc.Status != "" && !strings.EqualFold(current.Status, inc.Status) {
		current, err = c.UpdateStatus(ctx, id, inc.Status)
		if err != nil {
			return nil, err
		}
	}

	if inc.Title != "" && inc.Title != current.Title {
		current, err = c.UpdateTitle(ctx, id, inc.Title)
		if err != nil {
			return nil, err
		}
	}

	label, err := c.resolveSeverityLabel(ctx, inc)
	if err != nil {
		return nil, err
	}
	if label != "" && !strings.EqualFold(current.Severity, label) {
		current, err = c.UpdateSeverity(ctx, id, label)
		if err != nil {
			return nil, err
		}
	}

	return current, nil
}

// updateIncidentField posts a single-field update to IncidentsService and
// returns the updated incident.
//
// IncidentsService answers on the versioned base path. A Grafana build that
// predates that path answers on the unversioned one, so a 404 on the first
// path retries on the second.
func (c *IncidentClient) updateIncidentField(ctx context.Context, method string, req any, description string) (*Incident, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal %s request: %w", description, err)
	}

	resp, err := c.doRequest(ctx, incidentBasePath+"/"+method, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("incidents: %s: %w", description, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		resp, err = c.doRequest(ctx, incidentLegacyBasePath+"/"+method, bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("incidents: %s: %w", description, err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, providers.HandleErrorResponse(resp)
	}

	var result updateStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode %s response: %w", description, err)
	}
	return &result.Incident, nil
}

// UpdateStatus updates an incident's status and returns the updated incident.
func (c *IncidentClient) UpdateStatus(ctx context.Context, id, status string) (*Incident, error) {
	req := updateStatusRequest{IncidentID: id, Status: status}
	return c.updateIncidentField(ctx, incUpdateStatusMethod, req, "update status")
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
		return nil, providers.HandleErrorResponse(resp)
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
		return providers.HandleErrorResponse(resp)
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
		return nil, providers.HandleErrorResponse(resp)
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
		return nil, providers.HandleErrorResponse(resp)
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
		return nil, providers.HandleErrorResponse(resp)
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
