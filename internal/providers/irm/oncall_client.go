package irm

import (
	"context"
	"encoding/json"
	"fmt"
	"bytes"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// Internal API paths (relative to basePath, which is the plugin resources root).
// The IRM plugin proxy prepends "api/internal/v1/" before forwarding to the backend.
const (
	basePath = "/api/plugins/grafana-irm-app/resources"

	integrationsPath      = "alert_receive_channels/"
	escalationChainsPath  = "escalation_chains/"
	escalationPoliciesPath = "escalation_policies/"
	schedulesPath         = "schedules/"
	shiftsPath            = "oncall_shifts/"
	routesPath            = "channel_filters/"
	webhooksPath          = "webhooks/"
	alertGroupsPath       = "alertgroups/"
	usersPath             = "users/"
	currentUserPath       = "user/"
	teamsPath             = "teams/"
	userGroupsPath        = "user_groups/"
	slackChannelsPath     = "slack_channels/"
	alertsPath            = "alerts/"
	organizationPath      = "organization/"
	resolutionNotesPath   = "resolution_notes/"
	shiftSwapsPath        = "shift_swaps/"
	directPagingPath      = "direct_paging"
)

// OnCallClient is an HTTP client for the OnCall internal API via the IRM plugin proxy.
type OnCallClient struct {
	httpClient *http.Client
	host       string
}

// NewOnCallClient creates a new OnCall client from the given REST config.
// It uses rest.HTTPClientFor to get a client with Bearer token auth via the k8s transport.
func NewOnCallClient(cfg config.NamespacedRESTConfig) (*OnCallClient, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("irm oncall: create http client: %w", err)
	}
	return &OnCallClient{httpClient: httpClient, host: cfg.Host}, nil
}

func (c *OnCallClient) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	reqURL := c.host + basePath + "/" + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
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

func handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}
	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}
	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}

// iterResources yields items one at a time across paginated API pages.
func iterResources[T any](c *OnCallClient, ctx context.Context, path, resourceType string) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		next := path
		for next != "" {
			if ctx.Err() != nil {
				var z T
				yield(z, ctx.Err())
				return
			}

			resp, err := c.doRequest(ctx, http.MethodGet, next, nil)
			if err != nil {
				var z T
				yield(z, fmt.Errorf("irm: list %s: %w", resourceType, err))
				return
			}

			if resp.StatusCode != http.StatusOK {
				err := handleErrorResponse(resp)
				resp.Body.Close()
				var z T
				yield(z, err)
				return
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				var z T
				yield(z, fmt.Errorf("irm: read %s response: %w", resourceType, err))
				return
			}

			// The internal API returns either a paginated object {"results": [...], "next": "..."}
			// or a raw array [...] for non-paginated endpoints.
			var items []T
			var nextURL *string

			trimmed := bytes.TrimSpace(body)
			if len(trimmed) > 0 && trimmed[0] == '[' {
				if err := json.Unmarshal(body, &items); err != nil {
					var z T
					yield(z, fmt.Errorf("irm: decode %s: %w", resourceType, err))
					return
				}
			} else {
				var result paginatedResponse[T]
				if err := json.Unmarshal(body, &result); err != nil {
					var z T
					yield(z, fmt.Errorf("irm: decode %s: %w", resourceType, err))
					return
				}
				items = result.Results
				nextURL = result.Next
			}

			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}

			if nextURL == nil || *nextURL == "" {
				break
			}

			next, err = extractNextPath(*nextURL)
			if err != nil {
				var z T
				yield(z, fmt.Errorf("irm: pagination %s: %w", resourceType, err))
				return
			}
		}
	}
}

// extractNextPath extracts the relative API path from a pagination URL.
// The backend returns absolute URLs pointing to the real OnCall host
// (e.g., "https://oncall-prod/oncall/api/internal/v1/alertgroups/?page=2").
// We extract the path after "api/internal/v1/" and re-request through the proxy.
func extractNextPath(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid pagination URL %q: %w", rawURL, err)
	}

	const marker = "/api/internal/v1/"
	if idx := strings.Index(parsed.Path, marker); idx >= 0 {
		path := parsed.Path[idx+len(marker):]
		if parsed.RawQuery != "" {
			path += "?" + parsed.RawQuery
		}
		return path, nil
	}

	// Fallback: use the full path (shouldn't happen in practice).
	path := strings.TrimPrefix(parsed.Path, "/")
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	return path, nil
}

func collectAll[T any](it iter.Seq2[T, error]) ([]T, error) {
	var items []T
	for item, err := range it {
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func getResource[T any](c *OnCallClient, ctx context.Context, basePath, id, resourceType string) (*T, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("%s%s/", basePath, url.PathEscape(id)), nil)
	if err != nil {
		return nil, fmt.Errorf("irm: get %s: %w", resourceType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("irm: %s %q not found", resourceType, id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("irm: decode %s: %w", resourceType, err)
	}
	return &result, nil
}

func createResource[In any, Out any](c *OnCallClient, ctx context.Context, path string, body In, resourceType string) (*Out, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("irm: marshal %s: %w", resourceType, err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("irm: create %s: %w", resourceType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, handleErrorResponse(resp)
	}

	var result Out
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("irm: decode created %s: %w", resourceType, err)
	}
	return &result, nil
}

func updateResource[In any, Out any](c *OnCallClient, ctx context.Context, basePath, id string, body In, resourceType string) (*Out, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("irm: marshal %s: %w", resourceType, err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("%s%s/", basePath, url.PathEscape(id)), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("irm: update %s: %w", resourceType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result Out
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("irm: decode updated %s: %w", resourceType, err)
	}
	return &result, nil
}

func deleteResource(c *OnCallClient, ctx context.Context, basePath, id, resourceType string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("%s%s/", basePath, url.PathEscape(id)), nil)
	if err != nil {
		return fmt.Errorf("irm: delete %s: %w", resourceType, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return handleErrorResponse(resp)
	}
	return nil
}

func pathWithParams(base string, params url.Values) string {
	if len(params) > 0 {
		return base + "?" + params.Encode()
	}
	return base
}

// --- Integrations ---

func (c *OnCallClient) ListIntegrations(ctx context.Context) ([]Integration, error) {
	return collectAll(iterResources[Integration](c, ctx, integrationsPath, "integration"))
}

func (c *OnCallClient) GetIntegration(ctx context.Context, id string) (*Integration, error) {
	return getResource[Integration](c, ctx, integrationsPath, id, "integration")
}

func (c *OnCallClient) CreateIntegration(ctx context.Context, i Integration) (*Integration, error) {
	return createResource[Integration, Integration](c, ctx, integrationsPath, i, "integration")
}

func (c *OnCallClient) UpdateIntegration(ctx context.Context, id string, i Integration) (*Integration, error) {
	return updateResource[Integration, Integration](c, ctx, integrationsPath, id, i, "integration")
}

func (c *OnCallClient) DeleteIntegration(ctx context.Context, id string) error {
	return deleteResource(c, ctx, integrationsPath, id, "integration")
}

// --- Escalation Chains ---

func (c *OnCallClient) ListEscalationChains(ctx context.Context) ([]EscalationChain, error) {
	return collectAll(iterResources[EscalationChain](c, ctx, escalationChainsPath, "escalation chain"))
}

func (c *OnCallClient) GetEscalationChain(ctx context.Context, id string) (*EscalationChain, error) {
	return getResource[EscalationChain](c, ctx, escalationChainsPath, id, "escalation chain")
}

func (c *OnCallClient) CreateEscalationChain(ctx context.Context, ec EscalationChain) (*EscalationChain, error) {
	return createResource[EscalationChain, EscalationChain](c, ctx, escalationChainsPath, ec, "escalation chain")
}

func (c *OnCallClient) UpdateEscalationChain(ctx context.Context, id string, ec EscalationChain) (*EscalationChain, error) {
	return updateResource[EscalationChain, EscalationChain](c, ctx, escalationChainsPath, id, ec, "escalation chain")
}

func (c *OnCallClient) DeleteEscalationChain(ctx context.Context, id string) error {
	return deleteResource(c, ctx, escalationChainsPath, id, "escalation chain")
}

// --- Escalation Policies ---

func (c *OnCallClient) ListEscalationPolicies(ctx context.Context, chainID string) ([]EscalationPolicy, error) {
	params := url.Values{}
	if chainID != "" {
		params.Set("escalation_chain_id", chainID)
	}
	return collectAll(iterResources[EscalationPolicy](c, ctx, pathWithParams(escalationPoliciesPath, params), "escalation policy"))
}

func (c *OnCallClient) GetEscalationPolicy(ctx context.Context, id string) (*EscalationPolicy, error) {
	return getResource[EscalationPolicy](c, ctx, escalationPoliciesPath, id, "escalation policy")
}

func (c *OnCallClient) CreateEscalationPolicy(ctx context.Context, p EscalationPolicy) (*EscalationPolicy, error) {
	return createResource[EscalationPolicy, EscalationPolicy](c, ctx, escalationPoliciesPath, p, "escalation policy")
}

func (c *OnCallClient) UpdateEscalationPolicy(ctx context.Context, id string, p EscalationPolicy) (*EscalationPolicy, error) {
	return updateResource[EscalationPolicy, EscalationPolicy](c, ctx, escalationPoliciesPath, id, p, "escalation policy")
}

func (c *OnCallClient) DeleteEscalationPolicy(ctx context.Context, id string) error {
	return deleteResource(c, ctx, escalationPoliciesPath, id, "escalation policy")
}

// --- Schedules ---

func (c *OnCallClient) ListSchedules(ctx context.Context) ([]Schedule, error) {
	return collectAll(iterResources[Schedule](c, ctx, schedulesPath, "schedule"))
}

func (c *OnCallClient) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	return getResource[Schedule](c, ctx, schedulesPath, id, "schedule")
}

func (c *OnCallClient) CreateSchedule(ctx context.Context, s Schedule) (*Schedule, error) {
	return createResource[Schedule, Schedule](c, ctx, schedulesPath, s, "schedule")
}

func (c *OnCallClient) UpdateSchedule(ctx context.Context, id string, s Schedule) (*Schedule, error) {
	return updateResource[Schedule, Schedule](c, ctx, schedulesPath, id, s, "schedule")
}

func (c *OnCallClient) DeleteSchedule(ctx context.Context, id string) error {
	return deleteResource(c, ctx, schedulesPath, id, "schedule")
}

// ListFilterEvents returns resolved on-call events for a schedule.
func (c *OnCallClient) ListFilterEvents(ctx context.Context, scheduleID, userTZ, startingDate string, days int) (*FilterEventsResponse, error) {
	params := url.Values{}
	params.Set("type", "final")
	params.Set("user_tz", userTZ)
	params.Set("starting_date", startingDate)
	params.Set("days", fmt.Sprintf("%d", days))
	path := fmt.Sprintf("%s%s/filter_events/?%s", schedulesPath, url.PathEscape(scheduleID), params.Encode())

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("irm: list final shifts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result FilterEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("irm: decode final shifts: %w", err)
	}
	return &result, nil
}

// --- Shifts ---

func (c *OnCallClient) ListShifts(ctx context.Context) ([]Shift, error) {
	return collectAll(iterResources[Shift](c, ctx, shiftsPath, "shift"))
}

func (c *OnCallClient) GetShift(ctx context.Context, id string) (*Shift, error) {
	return getResource[Shift](c, ctx, shiftsPath, id, "shift")
}

func (c *OnCallClient) CreateShift(ctx context.Context, s ShiftRequest) (*Shift, error) {
	return createResource[ShiftRequest, Shift](c, ctx, shiftsPath, s, "shift")
}

func (c *OnCallClient) UpdateShift(ctx context.Context, id string, s ShiftRequest) (*Shift, error) {
	return updateResource[ShiftRequest, Shift](c, ctx, shiftsPath, id, s, "shift")
}

func (c *OnCallClient) DeleteShift(ctx context.Context, id string) error {
	return deleteResource(c, ctx, shiftsPath, id, "shift")
}

// --- Routes ---

func (c *OnCallClient) ListRoutes(ctx context.Context, integrationID string) ([]Route, error) {
	params := url.Values{}
	if integrationID != "" {
		params.Set("alert_receive_channel", integrationID)
	}
	return collectAll(iterResources[Route](c, ctx, pathWithParams(routesPath, params), "route"))
}

func (c *OnCallClient) GetRoute(ctx context.Context, id string) (*Route, error) {
	return getResource[Route](c, ctx, routesPath, id, "route")
}

func (c *OnCallClient) CreateRoute(ctx context.Context, r Route) (*Route, error) {
	return createResource[Route, Route](c, ctx, routesPath, r, "route")
}

func (c *OnCallClient) UpdateRoute(ctx context.Context, id string, r Route) (*Route, error) {
	return updateResource[Route, Route](c, ctx, routesPath, id, r, "route")
}

func (c *OnCallClient) DeleteRoute(ctx context.Context, id string) error {
	return deleteResource(c, ctx, routesPath, id, "route")
}

// --- Webhooks ---

func (c *OnCallClient) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	return collectAll(iterResources[Webhook](c, ctx, webhooksPath, "webhook"))
}

func (c *OnCallClient) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	return getResource[Webhook](c, ctx, webhooksPath, id, "webhook")
}

func (c *OnCallClient) CreateWebhook(ctx context.Context, w Webhook) (*Webhook, error) {
	return createResource[Webhook, Webhook](c, ctx, webhooksPath, w, "webhook")
}

func (c *OnCallClient) UpdateWebhook(ctx context.Context, id string, w Webhook) (*Webhook, error) {
	return updateResource[Webhook, Webhook](c, ctx, webhooksPath, id, w, "webhook")
}

func (c *OnCallClient) DeleteWebhook(ctx context.Context, id string) error {
	return deleteResource(c, ctx, webhooksPath, id, "webhook")
}

// --- Alert Groups ---

func (c *OnCallClient) ListAlertGroups(ctx context.Context) ([]AlertGroup, error) {
	return collectAll(iterResources[AlertGroup](c, ctx, alertGroupsPath, "alert group"))
}

func (c *OnCallClient) GetAlertGroup(ctx context.Context, id string) (*AlertGroup, error) {
	return getResource[AlertGroup](c, ctx, alertGroupsPath, id, "alert group")
}

func (c *OnCallClient) DeleteAlertGroup(ctx context.Context, id string) error {
	return deleteResource(c, ctx, alertGroupsPath, id, "alert group")
}

func (c *OnCallClient) AcknowledgeAlertGroup(ctx context.Context, id string) error {
	return c.alertGroupAction(ctx, id, "acknowledge")
}

func (c *OnCallClient) ResolveAlertGroup(ctx context.Context, id string) error {
	return c.alertGroupAction(ctx, id, "resolve")
}

func (c *OnCallClient) SilenceAlertGroup(ctx context.Context, id string, delaySecs int) error {
	data, err := json.Marshal(map[string]int{"delay": delaySecs})
	if err != nil {
		return fmt.Errorf("irm: marshal silence request: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/silence/", alertGroupsPath, url.PathEscape(id)), bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("irm: silence alert group: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return handleErrorResponse(resp)
	}
	return nil
}

func (c *OnCallClient) UnacknowledgeAlertGroup(ctx context.Context, id string) error {
	return c.alertGroupAction(ctx, id, "unacknowledge")
}

func (c *OnCallClient) UnresolveAlertGroup(ctx context.Context, id string) error {
	return c.alertGroupAction(ctx, id, "unresolve")
}

func (c *OnCallClient) UnsilenceAlertGroup(ctx context.Context, id string) error {
	return c.alertGroupAction(ctx, id, "unsilence")
}

func (c *OnCallClient) alertGroupAction(ctx context.Context, id, action string) error {
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/%s/", alertGroupsPath, url.PathEscape(id), action), nil)
	if err != nil {
		return fmt.Errorf("irm: %s alert group: %w", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return handleErrorResponse(resp)
	}
	return nil
}

// --- Users ---

func (c *OnCallClient) ListUsers(ctx context.Context) ([]User, error) {
	return collectAll(iterResources[User](c, ctx, usersPath, "user"))
}

func (c *OnCallClient) GetUser(ctx context.Context, id string) (*User, error) {
	return getResource[User](c, ctx, usersPath, id, "user")
}

func (c *OnCallClient) GetCurrentUser(ctx context.Context) (*User, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, currentUserPath, nil)
	if err != nil {
		return nil, fmt.Errorf("irm: get current user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}
	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("irm: decode current user: %w", err)
	}
	return &user, nil
}

// --- Teams ---

func (c *OnCallClient) ListTeams(ctx context.Context) ([]Team, error) {
	return collectAll(iterResources[Team](c, ctx, teamsPath, "team"))
}

func (c *OnCallClient) GetTeam(ctx context.Context, id string) (*Team, error) {
	return getResource[Team](c, ctx, teamsPath, id, "team")
}

// --- User Groups ---

func (c *OnCallClient) ListUserGroups(ctx context.Context) ([]UserGroup, error) {
	return collectAll(iterResources[UserGroup](c, ctx, userGroupsPath, "user group"))
}

// --- Slack Channels ---

func (c *OnCallClient) ListSlackChannels(ctx context.Context) ([]SlackChannel, error) {
	return collectAll(iterResources[SlackChannel](c, ctx, slackChannelsPath, "slack channel"))
}

// --- Alerts ---

func (c *OnCallClient) ListAlerts(ctx context.Context, alertGroupID string) ([]Alert, error) {
	params := url.Values{}
	if alertGroupID != "" {
		params.Set("alert_group_id", alertGroupID)
	}
	return collectAll(iterResources[Alert](c, ctx, pathWithParams(alertsPath, params), "alert"))
}

func (c *OnCallClient) GetAlert(ctx context.Context, id string) (*Alert, error) {
	return getResource[Alert](c, ctx, alertsPath, id, "alert")
}

// --- Organization ---

func (c *OnCallClient) GetOrganization(ctx context.Context) (*Organization, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, organizationPath, nil)
	if err != nil {
		return nil, fmt.Errorf("irm: get organization: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}
	var org Organization
	if err := json.NewDecoder(resp.Body).Decode(&org); err != nil {
		return nil, fmt.Errorf("irm: decode organization: %w", err)
	}
	return &org, nil
}

// --- Resolution Notes ---

func (c *OnCallClient) ListResolutionNotes(ctx context.Context, alertGroupID string) ([]ResolutionNote, error) {
	params := url.Values{}
	if alertGroupID != "" {
		params.Set("alert_group_id", alertGroupID)
	}
	return collectAll(iterResources[ResolutionNote](c, ctx, pathWithParams(resolutionNotesPath, params), "resolution note"))
}

func (c *OnCallClient) GetResolutionNote(ctx context.Context, id string) (*ResolutionNote, error) {
	return getResource[ResolutionNote](c, ctx, resolutionNotesPath, id, "resolution note")
}

func (c *OnCallClient) CreateResolutionNote(ctx context.Context, input CreateResolutionNoteInput) (*ResolutionNote, error) {
	return createResource[CreateResolutionNoteInput, ResolutionNote](c, ctx, resolutionNotesPath, input, "resolution note")
}

func (c *OnCallClient) UpdateResolutionNote(ctx context.Context, id string, input UpdateResolutionNoteInput) (*ResolutionNote, error) {
	return updateResource[UpdateResolutionNoteInput, ResolutionNote](c, ctx, resolutionNotesPath, id, input, "resolution note")
}

func (c *OnCallClient) DeleteResolutionNote(ctx context.Context, id string) error {
	return deleteResource(c, ctx, resolutionNotesPath, id, "resolution note")
}

// --- Shift Swaps ---

func (c *OnCallClient) ListShiftSwaps(ctx context.Context) ([]ShiftSwap, error) {
	return collectAll(iterResources[ShiftSwap](c, ctx, shiftSwapsPath, "shift swap"))
}

func (c *OnCallClient) GetShiftSwap(ctx context.Context, id string) (*ShiftSwap, error) {
	return getResource[ShiftSwap](c, ctx, shiftSwapsPath, id, "shift swap")
}

func (c *OnCallClient) CreateShiftSwap(ctx context.Context, input CreateShiftSwapInput) (*ShiftSwap, error) {
	return createResource[CreateShiftSwapInput, ShiftSwap](c, ctx, shiftSwapsPath, input, "shift swap")
}

func (c *OnCallClient) UpdateShiftSwap(ctx context.Context, id string, input UpdateShiftSwapInput) (*ShiftSwap, error) {
	return updateResource[UpdateShiftSwapInput, ShiftSwap](c, ctx, shiftSwapsPath, id, input, "shift swap")
}

func (c *OnCallClient) DeleteShiftSwap(ctx context.Context, id string) error {
	return deleteResource(c, ctx, shiftSwapsPath, id, "shift swap")
}

func (c *OnCallClient) TakeShiftSwap(ctx context.Context, id string, input TakeShiftSwapInput) (*ShiftSwap, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("irm: marshal take shift swap: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/take/", shiftSwapsPath, url.PathEscape(id)), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("irm: take shift swap: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}
	var result ShiftSwap
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("irm: decode shift swap: %w", err)
	}
	return &result, nil
}

// --- Direct Paging ---

func (c *OnCallClient) CreateDirectPaging(ctx context.Context, input DirectPagingInput) (*DirectPagingResult, error) {
	return createResource[DirectPagingInput, DirectPagingResult](c, ctx, directPagingPath, input, "direct paging")
}
