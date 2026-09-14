package irm

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	irmclient "github.com/grafana/gcx/client/irm"
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

	// The IRM API has one operation per mutable field: CreateIncident ignores
	// the severity of the request body, and there is no combined update
	// method. The method names are held apart from the base path, because
	// updateIncidentField tries the versioned base path first and the
	// unversioned one second.
	incUpdateStatusMethod   = "IncidentsService.UpdateStatus"
	incUpdateSeverityMethod = "IncidentsService.UpdateSeverity"
	incUpdateTitleMethod    = "IncidentsService.UpdateTitle"

	actQueryPath = incidentBasePath + "/ActivityService.QueryActivity"
	sevGetPath   = incidentLegacyBasePath + "/SeveritiesService.GetOrgSeverities"
	ctxQueryPath = incidentLegacyBasePath + "/IncidentContextService.QueryIncidentContext"
	// IntegrationService is likewise not part of the documented v1 API and
	// is served from the unversioned base path.
	hookRunsPath = incidentLegacyBasePath + "/IntegrationService.GetHookRuns"
)

// pirFileURLField is the hook-run metadata field holding the document URL.
const pirFileURLField = "fileURL"

// pirHookRank ranks the hooks that copy a post-incident review template into a
// document, and reports 0 for every other hook. PIRs are optional and only the
// Google Workspace integration creates them, so these are the only hook runs
// that can carry a PIR link. copyTemplate outranks copyFile: it is the
// PIR-specific hook, where copyFile is the older general file copy.
func pirHookRank(hookID string) int {
	switch hookID {
	case "grate.googleworkspace.copyTemplate":
		return 2
	case "grate.google.copyFile":
		return 1
	default:
		return 0
	}
}

// pirURL returns the PIR document URL a hook run recorded, or "" if it is not
// a PIR-creating hook or recorded no link. The fileURL field is what IRM's PIR
// bookmarks read; metadata.url is the same value on a full run and covers a
// partial one that recorded only the URL. Restricting this to PIR hooks matters
// for that fallback: other integrations put their Slack, Meet and GitHub links
// in the very same field.
func pirURL(run HookRun) string {
	if pirHookRank(run.HookID) == 0 || run.Metadata == nil {
		return ""
	}
	for _, f := range run.Metadata.Fields {
		if f.Key == pirFileURLField && f.Value != "" {
			return f.Value
		}
	}
	return run.Metadata.URL
}

// pirURLFromHookRuns returns the PIR document URL for an incident, or "" when
// it has no PIR document. An incident can carry several PIR links — both hooks
// ran, or one was re-run — and the API returns hook runs unordered, so the most
// recent one wins and equal timestamps fall back to hook rank and then to the
// URL itself. That keeps repeated calls for one incident in agreement.
func pirURLFromHookRuns(runs []HookRun) string {
	candidates := make([]HookRun, 0, len(runs))
	for _, run := range runs {
		if pirURL(run) != "" {
			candidates = append(candidates, run)
		}
	}
	if len(candidates) == 0 {
		return ""
	}

	slices.SortFunc(candidates, func(a, b HookRun) int {
		if c := time.Time(b.LastRun).Compare(time.Time(a.LastRun)); c != 0 {
			return c
		}
		if c := cmp.Compare(pirHookRank(b.HookID), pirHookRank(a.HookID)); c != 0 {
			return c
		}
		return cmp.Compare(pirURL(a), pirURL(b))
	})
	return pirURL(candidates[0])
}

// IncidentClient is a CLI-facing wrapper around the public
// client/irm.IncidentClient: it builds the *http.Client from a
// NamespacedRESTConfig (via client-go's rest.HTTPClientFor), converts between
// the CLI's Incident type — which carries resource-adapter methods the public
// API has no business exposing — and the public client's wire type, and keeps
// the operations that are not part of the public surface.
type IncidentClient struct {
	httpClient *http.Client
	host       string
	inner      *irmclient.IncidentClient
}

// NewClient creates a new incidents client from the given REST config.
func NewIncidentClient(cfg config.NamespacedRESTConfig) (*IncidentClient, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &IncidentClient{
		httpClient: httpClient,
		host:       cfg.Host,
		inner:      irmclient.NewIncidentClient(httpClient, cfg.Host),
	}, nil
}

func isIncidentStatusFilter(s string) bool {
	return s == "active" || s == "resolved"
}

// toWire converts the CLI's Incident (which also carries resource-adapter
// methods irrelevant to the wire format) to the public client's wire type.
// Both types share identical JSON tags, so a marshal round-trip is a correct
// and simple field-for-field copy without hand-mapping every nested struct.
func toWire(inc *Incident) (*irmclient.Incident, error) {
	b, err := json.Marshal(inc)
	if err != nil {
		return nil, err
	}
	var wire irmclient.Incident
	if err := json.Unmarshal(b, &wire); err != nil {
		return nil, err
	}
	return &wire, nil
}

// fromWire converts the public client's wire type back to the CLI's Incident.
func fromWire(wire *irmclient.Incident) (*Incident, error) {
	b, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	var inc Incident
	if err := json.Unmarshal(b, &inc); err != nil {
		return nil, err
	}
	return &inc, nil
}

// toWireQuery converts the CLI's IncidentQuery to the public client's. The
// type has no JSON tags, so the fields are copied directly.
func toWireQuery(query IncidentQuery) irmclient.IncidentQuery {
	wire := irmclient.IncidentQuery{
		Limit:          query.Limit,
		OrderDirection: query.OrderDirection,
		OrderField:     query.OrderField,
		IncidentLabels: query.IncidentLabels,
		Statuses:       query.Statuses,
		Severity:       query.Severity,
		QueryString:    query.QueryString,
	}
	if query.DateFrom != nil {
		from := irmclient.FlexTime(*query.DateFrom)
		wire.DateFrom = &from
	}
	if query.DateTo != nil {
		to := irmclient.FlexTime(*query.DateTo)
		wire.DateTo = &to
	}
	return wire
}

// List queries incident previews with the given parameters.
func (c *IncidentClient) List(ctx context.Context, query IncidentQuery) ([]Incident, error) {
	items, err := c.inner.List(ctx, toWireQuery(query))
	if err != nil {
		return nil, err
	}

	incidents := make([]Incident, len(items))
	for i, item := range items {
		inc, err := fromWire(&item)
		if err != nil {
			return nil, fmt.Errorf("incidents: convert %s: %w", item.IncidentID, err)
		}
		incidents[i] = *inc
	}

	return incidents, nil
}

// Get returns a single incident by ID.
func (c *IncidentClient) Get(ctx context.Context, id string) (*Incident, error) {
	item, err := c.inner.Get(ctx, id)
	if err != nil {
		// The public client has no knowledge of the resource adapter, so its
		// not-found sentinel is remapped onto the one the push upsert probes.
		if errors.Is(err, irmclient.ErrNotFound) {
			return nil, fmt.Errorf("incidents: get %s: %w", id, ErrNotFound)
		}
		return nil, err
	}

	return fromWire(item)
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

	wire, err := toWire(inc)
	if err != nil {
		return nil, fmt.Errorf("incidents: convert create request: %w", err)
	}

	item, err := c.inner.Create(ctx, wire)
	if err != nil {
		return nil, err
	}

	created, err := fromWire(item)
	if err != nil {
		return nil, fmt.Errorf("incidents: convert %s: %w", item.IncidentID, err)
	}

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

// resolveSeverityLabel returns the canonical severity label that the caller
// asked for. It resolves both accepted inputs against the organization list
// before a write. This prevents an unknown label from becoming a successful
// no-op on a backend that accepts it with status 200.
//
// A hand-written spec.severityID has precedence when it is not empty. A pulled
// manifest carries spec.severity alone because the read removes severityID.
// An empty result means that the caller asked for no severity.
func (c *IncidentClient) resolveSeverityLabel(ctx context.Context, inc *Incident) (string, error) {
	if inc.SeverityID == "" && inc.Severity == "" {
		return "", nil
	}

	severities, err := c.GetSeverities(ctx)
	if err != nil {
		return "", fmt.Errorf("incidents: resolve severity: %w", err)
	}
	if inc.SeverityID != "" {
		for _, s := range severities {
			if s.SeverityID == inc.SeverityID {
				return s.DisplayLabel, nil
			}
		}
		return "", fmt.Errorf("incidents: unknown severityID %q, run `gcx irm incidents severities list` for the valid values", inc.SeverityID)
	}

	for _, s := range severities {
		if strings.EqualFold(s.DisplayLabel, inc.Severity) {
			return s.DisplayLabel, nil
		}
	}
	return "", fmt.Errorf("incidents: unknown severity %q, run `gcx irm incidents severities list` for the valid values", inc.Severity)
}

// UpdateSeverity sets the severity of an incident and returns the updated
// incident. severity is the display label, not the identifier.
func (c *IncidentClient) UpdateSeverity(ctx context.Context, id, severity string) (*Incident, error) {
	req := updateSeverityRequest{IncidentID: id, Severity: severity}
	return c.updateIncidentField(ctx, incUpdateSeverityMethod, id, req, "update severity")
}

// UpdateTitle sets the title of an incident and returns the updated incident.
func (c *IncidentClient) UpdateTitle(ctx context.Context, id, title string) (*Incident, error) {
	req := updateTitleRequest{IncidentID: id, Title: title}
	return c.updateIncidentField(ctx, incUpdateTitleMethod, id, req, "update title")
}

// Update applies the three fields that the IRM API exposes as their own
// operation: the status, the title, and the severity. The API has no single
// update method, so each field costs one call. gcx reads the incident first,
// then skips each field that the caller left empty or that already matches
// the server. Nothing enforces the required fields of the schema on push, so
// a manifest without a status must not send an empty status.
//
// The resources push pipeline reads the incident before it calls Update to
// select create or update. Update cannot reuse that value through the
// TypedCRUD contract, so a push reads the incident a second time here.
//
// Update drops every other field of inc, because the IRM API has no update
// operation for it: a pushed manifest that changes labels, incidentType,
// isDrill, description or fieldGroupUUID leaves those values on the server.
//
// The second result names the fields that reached the server. An empty list
// means the incident already matched the request.
//
// A call that fails after an earlier call succeeded leaves the incident
// between the two states. Update then returns no incident, and an error that
// names the fields it did apply.
func (c *IncidentClient) Update(ctx context.Context, id string, inc *Incident) (*Incident, []string, error) {
	current, err := c.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	// The resolve reads the severity list of the organization, and writes
	// nothing. It runs before the first write, because the IRM API cannot undo
	// a write that a later step abandons: a manifest with an unknown
	// spec.severityID then costs no half-applied incident.
	label, err := c.resolveSeverityLabel(ctx, inc)
	if err != nil {
		return nil, nil, err
	}

	var applied []string
	// fail builds the error of a half-applied update: it names the fields that
	// reached the server before the failure.
	fail := func(field string, err error) error {
		if len(applied) == 0 {
			return err
		}
		return fmt.Errorf("incidents: update %s: gcx applied the %s, but the %s update failed: %w",
			id, strings.Join(applied, " and the "), field, err)
	}

	if inc.Status != "" && !strings.EqualFold(current.Status, inc.Status) {
		updated, err := c.UpdateStatus(ctx, id, inc.Status)
		if err != nil {
			return nil, nil, fail("status", err)
		}
		current = updated
		applied = append(applied, "status")
	}

	if inc.Title != "" && inc.Title != current.Title {
		updated, err := c.UpdateTitle(ctx, id, inc.Title)
		if err != nil {
			return nil, nil, fail("title", err)
		}
		current = updated
		applied = append(applied, "title")
	}

	if label != "" && !strings.EqualFold(current.Severity, label) {
		updated, err := c.UpdateSeverity(ctx, id, label)
		if err != nil {
			return nil, nil, fail("severity", err)
		}
		current = updated
		applied = append(applied, "severity")
	}

	current.updatedFields = applied
	return current, applied, nil
}

// updateIncidentField posts a single-field update to IncidentsService and
// returns the updated incident.
//
// IncidentsService answers on the versioned base path. A Grafana build that
// predates that path answers on the unversioned one, so a 404 on the first
// path retries on the second. If both update paths return 404, Get determines
// whether the incident is missing or the update operation is unavailable.
func (c *IncidentClient) updateIncidentField(ctx context.Context, method, id string, req any, description string) (*Incident, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal %s request: %w", description, err)
	}

	resp, err := c.doIncidentMethod(ctx, method, data)
	if err != nil {
		return nil, fmt.Errorf("incidents: %s: %w", description, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		if _, getErr := c.Get(ctx, id); getErr != nil {
			if errors.Is(getErr, ErrNotFound) {
				return nil, fmt.Errorf("incidents: %s %s: %w", description, id, ErrNotFound)
			}
			return nil, fmt.Errorf("incidents: %s %s: verify incident after both API paths returned 404: %w", description, id, getErr)
		}
		return nil, fmt.Errorf("incidents: %s %s: operation is unavailable on the versioned and unversioned API paths", description, id)
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

// doIncidentMethod posts to an IncidentsService method. A 404 retries the
// unversioned base path so the field-update methods support the same Grafana
// builds Get does. The caller owns and must close the response body.
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

// UpdateStatus updates an incident's status and returns the updated incident.
func (c *IncidentClient) UpdateStatus(ctx context.Context, id, status string) (*Incident, error) {
	req := updateStatusRequest{IncidentID: id, Status: status}
	return c.updateIncidentField(ctx, incUpdateStatusMethod, id, req, "update status")
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
	return c.inner.AddActivity(ctx, incidentID, body)
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

// GetHookRuns returns the integration hook runs recorded against an incident.
func (c *IncidentClient) GetHookRuns(ctx context.Context, incidentID string) ([]HookRun, error) {
	if incidentID == "" {
		return nil, errors.New("incidents: GetHookRuns: incidentID is required")
	}

	body, err := json.Marshal(getHookRunsRequest{IncidentID: incidentID})
	if err != nil {
		return nil, fmt.Errorf("incidents: marshal hook runs request: %w", err)
	}

	resp, err := c.doRequest(ctx, hookRunsPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("incidents: get hook runs for %s: %w", incidentID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, providers.HandleErrorResponse(resp)
	}

	var result getHookRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("incidents: decode hook runs response: %w", err)
	}
	// The API can report failure in-band on a 200 response.
	if result.Error != "" {
		return nil, fmt.Errorf("incidents: get hook runs for %s: %s", incidentID, result.Error)
	}

	return result.HookRuns, nil
}

// GetPIRURL resolves the post-incident review document URL for an incident.
// The URL is not part of the incident payload: it is recorded on the hook run
// of the integration that created the PIR. Returns "" when there is no PIR.
func (c *IncidentClient) GetPIRURL(ctx context.Context, incidentID string) (string, error) {
	runs, err := c.GetHookRuns(ctx, incidentID)
	if err != nil {
		return "", err
	}
	return pirURLFromHookRuns(runs), nil
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
