package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/irm/oncalltypes"
	"k8s.io/client-go/rest"
)

// Internal API paths (relative to basePath, which is the plugin resources root).
// The IRM plugin proxy prepends "api/internal/v1/" before forwarding to the backend.
const (
	// BasePath is the plugin resources root for the IRM app.
	BasePath = "/api/plugins/grafana-irm-app/resources"

	integrationsPath       = "alert_receive_channels/"
	escalationChainsPath   = "escalation_chains/"
	escalationPoliciesPath = "escalation_policies/"
	schedulesPath          = "schedules/"
	shiftsPath             = "oncall_shifts/"
	routesPath             = "channel_filters/"
	webhooksPath           = "webhooks/"
	alertGroupsPath        = "alertgroups/"
	usersPath              = "users/"
	currentUserPath        = "user/"
	teamsPath              = "teams/"
	userGroupsPath         = "user_groups/"
	slackChannelsPath      = "slack_channels/"
	alertsPath             = "alerts/"
	organizationPath       = "organization/"
	resolutionNotesPath    = "resolution_notes/"
	shiftSwapsPath         = "shift_swaps/"
	directPagingPath       = "direct_paging"
)

// OnCallClient is an HTTP client for the OnCall internal API via the IRM plugin proxy.
type OnCallClient struct {
	HTTPClient *http.Client
	Host       string
}

// NewOnCallClient creates a new OnCall client from the given REST config.
// It uses rest.HTTPClientFor to get a client with Bearer token auth via the k8s transport.
func NewOnCallClient(cfg config.NamespacedRESTConfig) (*OnCallClient, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("irm oncall: create http client: %w", err)
	}
	return &OnCallClient{HTTPClient: httpClient, Host: cfg.Host}, nil
}

// DoRequest builds and executes an HTTP request against the OnCall internal API via the plugin proxy.
func (c *OnCallClient) DoRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	reqURL := c.Host + BasePath + "/" + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
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

type paginatedResponse[T any] struct {
	Results []T     `json:"results"`
	Next    *string `json:"next"`
}

// iterResources yields items one at a time across paginated API pages.
func iterResources[T any](ctx context.Context, c *OnCallClient, path, resourceType string) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		next := path
		for next != "" {
			if ctx.Err() != nil {
				var z T
				yield(z, ctx.Err())
				return
			}

			resp, err := c.DoRequest(ctx, http.MethodGet, next, nil)
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

			next, err = ExtractNextPath(*nextURL)
			if err != nil {
				var z T
				yield(z, fmt.Errorf("irm: pagination %s: %w", resourceType, err))
				return
			}
		}
	}
}

// ExtractNextPath extracts the relative API path from a pagination URL.
// The backend returns absolute URLs pointing to the real OnCall host
// (e.g., "https://oncall-prod/oncall/api/internal/v1/alertgroups/?page=2").
// We extract the path after "api/internal/v1/" and re-request through the proxy.
func ExtractNextPath(rawURL string) (string, error) {
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
	return collectN(it, 0)
}

// collectN collects up to n items from an iterator. If n <= 0, all items are collected.
func collectN[T any](it iter.Seq2[T, error], n int) ([]T, error) {
	items := make([]T, 0, max(0, n))
	for item, err := range it {
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if n > 0 && len(items) >= n {
			break
		}
	}
	return items, nil
}

func getResource[T any](ctx context.Context, c *OnCallClient, basePath, id, resourceType string) (*T, error) {
	resp, err := c.DoRequest(ctx, http.MethodGet, fmt.Sprintf("%s%s/", basePath, url.PathEscape(id)), nil)
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

func createResource[In any, Out any](ctx context.Context, c *OnCallClient, path string, body In, resourceType string) (*Out, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("irm: marshal %s: %w", resourceType, err)
	}

	resp, err := c.DoRequest(ctx, http.MethodPost, path, bytes.NewReader(data))
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

func updateResource[In any, Out any](ctx context.Context, c *OnCallClient, basePath, id string, body In, resourceType string) (*Out, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("irm: marshal %s: %w", resourceType, err)
	}

	resp, err := c.DoRequest(ctx, http.MethodPut, fmt.Sprintf("%s%s/", basePath, url.PathEscape(id)), bytes.NewReader(data))
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

func deleteResource(ctx context.Context, c *OnCallClient, basePath, id, resourceType string) error {
	resp, err := c.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("%s%s/", basePath, url.PathEscape(id)), nil)
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
	return collectAll(iterResources[Integration](ctx, c, integrationsPath, "integration"))
}

func (c *OnCallClient) GetIntegration(ctx context.Context, id string) (*Integration, error) {
	return getResource[Integration](ctx, c, integrationsPath, id, "integration")
}

func (c *OnCallClient) CreateIntegration(ctx context.Context, i Integration) (*Integration, error) {
	return createResource[Integration, Integration](ctx, c, integrationsPath, i, "integration")
}

func (c *OnCallClient) UpdateIntegration(ctx context.Context, id string, i Integration) (*Integration, error) {
	return updateResource[Integration, Integration](ctx, c, integrationsPath, id, i, "integration")
}

func (c *OnCallClient) DeleteIntegration(ctx context.Context, id string) error {
	return deleteResource(ctx, c, integrationsPath, id, "integration")
}

// --- Escalation Chains ---

func (c *OnCallClient) ListEscalationChains(ctx context.Context) ([]EscalationChain, error) {
	return collectAll(iterResources[EscalationChain](ctx, c, escalationChainsPath, "escalation chain"))
}

func (c *OnCallClient) GetEscalationChain(ctx context.Context, id string) (*EscalationChain, error) {
	return getResource[EscalationChain](ctx, c, escalationChainsPath, id, "escalation chain")
}

func (c *OnCallClient) CreateEscalationChain(ctx context.Context, ec EscalationChain) (*EscalationChain, error) {
	return createResource[EscalationChain, EscalationChain](ctx, c, escalationChainsPath, ec, "escalation chain")
}

func (c *OnCallClient) UpdateEscalationChain(ctx context.Context, id string, ec EscalationChain) (*EscalationChain, error) {
	return updateResource[EscalationChain, EscalationChain](ctx, c, escalationChainsPath, id, ec, "escalation chain")
}

func (c *OnCallClient) DeleteEscalationChain(ctx context.Context, id string) error {
	return deleteResource(ctx, c, escalationChainsPath, id, "escalation chain")
}

// --- Escalation Policies ---

func (c *OnCallClient) ListEscalationPolicies(ctx context.Context, chainID string) ([]EscalationPolicy, error) {
	params := url.Values{}
	if chainID != "" {
		params.Set("escalation_chain_id", chainID)
	}
	return collectAll(iterResources[EscalationPolicy](ctx, c, pathWithParams(escalationPoliciesPath, params), "escalation policy"))
}

func (c *OnCallClient) GetEscalationPolicy(ctx context.Context, id string) (*EscalationPolicy, error) {
	return getResource[EscalationPolicy](ctx, c, escalationPoliciesPath, id, "escalation policy")
}

func (c *OnCallClient) CreateEscalationPolicy(ctx context.Context, p EscalationPolicy) (*EscalationPolicy, error) {
	return createResource[EscalationPolicy, EscalationPolicy](ctx, c, escalationPoliciesPath, p, "escalation policy")
}

func (c *OnCallClient) UpdateEscalationPolicy(ctx context.Context, id string, p EscalationPolicy) (*EscalationPolicy, error) {
	return updateResource[EscalationPolicy, EscalationPolicy](ctx, c, escalationPoliciesPath, id, p, "escalation policy")
}

func (c *OnCallClient) DeleteEscalationPolicy(ctx context.Context, id string) error {
	return deleteResource(ctx, c, escalationPoliciesPath, id, "escalation policy")
}

// --- Schedules ---

func (c *OnCallClient) ListSchedules(ctx context.Context) ([]Schedule, error) {
	return collectAll(iterResources[Schedule](ctx, c, schedulesPath, "schedule"))
}

func (c *OnCallClient) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	return getResource[Schedule](ctx, c, schedulesPath, id, "schedule")
}

func (c *OnCallClient) CreateSchedule(ctx context.Context, s Schedule) (*Schedule, error) {
	return createResource[Schedule, Schedule](ctx, c, schedulesPath, s, "schedule")
}

func (c *OnCallClient) UpdateSchedule(ctx context.Context, id string, s Schedule) (*Schedule, error) {
	return updateResource[Schedule, Schedule](ctx, c, schedulesPath, id, s, "schedule")
}

func (c *OnCallClient) DeleteSchedule(ctx context.Context, id string) error {
	return deleteResource(ctx, c, schedulesPath, id, "schedule")
}

// ListFilterEvents returns resolved on-call events for a schedule.
func (c *OnCallClient) ListFilterEvents(ctx context.Context, scheduleID, userTZ, startingDate string, days int) (*FilterEventsResponse, error) {
	params := url.Values{}
	params.Set("type", "final")
	params.Set("user_tz", userTZ)
	params.Set("starting_date", startingDate)
	params.Set("days", strconv.Itoa(days))
	path := fmt.Sprintf("%s%s/filter_events/?%s", schedulesPath, url.PathEscape(scheduleID), params.Encode())

	resp, err := c.DoRequest(ctx, http.MethodGet, path, nil)
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
	return collectAll(iterResources[Shift](ctx, c, shiftsPath, "shift"))
}

func (c *OnCallClient) GetShift(ctx context.Context, id string) (*Shift, error) {
	return getResource[Shift](ctx, c, shiftsPath, id, "shift")
}

func (c *OnCallClient) CreateShift(ctx context.Context, s ShiftRequest) (*Shift, error) {
	return createResource[ShiftRequest, Shift](ctx, c, shiftsPath, s, "shift")
}

func (c *OnCallClient) UpdateShift(ctx context.Context, id string, s ShiftRequest) (*Shift, error) {
	return updateResource[ShiftRequest, Shift](ctx, c, shiftsPath, id, s, "shift")
}

func (c *OnCallClient) DeleteShift(ctx context.Context, id string) error {
	return deleteResource(ctx, c, shiftsPath, id, "shift")
}

// --- Routes ---

func (c *OnCallClient) ListRoutes(ctx context.Context, integrationID string) ([]Route, error) {
	params := url.Values{}
	if integrationID != "" {
		params.Set("alert_receive_channel", integrationID)
	}
	return collectAll(iterResources[Route](ctx, c, pathWithParams(routesPath, params), "route"))
}

func (c *OnCallClient) GetRoute(ctx context.Context, id string) (*Route, error) {
	return getResource[Route](ctx, c, routesPath, id, "route")
}

func (c *OnCallClient) CreateRoute(ctx context.Context, r Route) (*Route, error) {
	return createResource[Route, Route](ctx, c, routesPath, r, "route")
}

func (c *OnCallClient) UpdateRoute(ctx context.Context, id string, r Route) (*Route, error) {
	return updateResource[Route, Route](ctx, c, routesPath, id, r, "route")
}

func (c *OnCallClient) DeleteRoute(ctx context.Context, id string) error {
	return deleteResource(ctx, c, routesPath, id, "route")
}

// --- Webhooks ---

func (c *OnCallClient) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	return collectAll(iterResources[Webhook](ctx, c, webhooksPath, "webhook"))
}

func (c *OnCallClient) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	return getResource[Webhook](ctx, c, webhooksPath, id, "webhook")
}

func (c *OnCallClient) CreateWebhook(ctx context.Context, w Webhook) (*Webhook, error) {
	return createResource[Webhook, Webhook](ctx, c, webhooksPath, w, "webhook")
}

func (c *OnCallClient) UpdateWebhook(ctx context.Context, id string, w Webhook) (*Webhook, error) {
	return updateResource[Webhook, Webhook](ctx, c, webhooksPath, id, w, "webhook")
}

func (c *OnCallClient) DeleteWebhook(ctx context.Context, id string) error {
	return deleteResource(ctx, c, webhooksPath, id, "webhook")
}

// --- Alert Groups ---

func (c *OnCallClient) ListAlertGroups(ctx context.Context, opts ...oncalltypes.ListOption) ([]AlertGroup, error) {
	cfg := oncalltypes.ApplyListOpts(opts)
	return collectN(iterResources[AlertGroup](ctx, c, alertGroupsPath, "alert group"), cfg.Limit)
}

func (c *OnCallClient) GetAlertGroup(ctx context.Context, id string) (*AlertGroup, error) {
	return getResource[AlertGroup](ctx, c, alertGroupsPath, id, "alert group")
}

func (c *OnCallClient) DeleteAlertGroup(ctx context.Context, id string) error {
	return deleteResource(ctx, c, alertGroupsPath, id, "alert group")
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
	resp, err := c.DoRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/silence/", alertGroupsPath, url.PathEscape(id)), bytes.NewReader(data))
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
	resp, err := c.DoRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/%s/", alertGroupsPath, url.PathEscape(id), action), nil)
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
	return collectAll(iterResources[User](ctx, c, usersPath, "user"))
}

func (c *OnCallClient) GetUser(ctx context.Context, id string) (*User, error) {
	return getResource[User](ctx, c, usersPath, id, "user")
}

func (c *OnCallClient) GetCurrentUser(ctx context.Context) (*User, error) {
	resp, err := c.DoRequest(ctx, http.MethodGet, currentUserPath, nil)
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
	return collectAll(iterResources[Team](ctx, c, teamsPath, "team"))
}

func (c *OnCallClient) GetTeam(ctx context.Context, id string) (*Team, error) {
	return getResource[Team](ctx, c, teamsPath, id, "team")
}

// --- User Groups ---

func (c *OnCallClient) ListUserGroups(ctx context.Context) ([]UserGroup, error) {
	return collectAll(iterResources[UserGroup](ctx, c, userGroupsPath, "user group"))
}

// --- Slack Channels ---

func (c *OnCallClient) ListSlackChannels(ctx context.Context) ([]SlackChannel, error) {
	return collectAll(iterResources[SlackChannel](ctx, c, slackChannelsPath, "slack channel"))
}

// --- Alerts ---

func (c *OnCallClient) ListAlerts(ctx context.Context, alertGroupID string, opts ...oncalltypes.ListOption) ([]Alert, error) {
	params := url.Values{}
	if alertGroupID != "" {
		params.Set("alert_group_id", alertGroupID)
	}
	cfg := oncalltypes.ApplyListOpts(opts)
	return collectN(iterResources[Alert](ctx, c, pathWithParams(alertsPath, params), "alert"), cfg.Limit)
}

func (c *OnCallClient) GetAlert(ctx context.Context, id string) (*Alert, error) {
	return getResource[Alert](ctx, c, alertsPath, id, "alert")
}

// --- Organization ---

func (c *OnCallClient) GetOrganization(ctx context.Context) (*Organization, error) {
	resp, err := c.DoRequest(ctx, http.MethodGet, organizationPath, nil)
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
	return collectAll(iterResources[ResolutionNote](ctx, c, pathWithParams(resolutionNotesPath, params), "resolution note"))
}

func (c *OnCallClient) GetResolutionNote(ctx context.Context, id string) (*ResolutionNote, error) {
	return getResource[ResolutionNote](ctx, c, resolutionNotesPath, id, "resolution note")
}

func (c *OnCallClient) CreateResolutionNote(ctx context.Context, input CreateResolutionNoteInput) (*ResolutionNote, error) {
	return createResource[CreateResolutionNoteInput, ResolutionNote](ctx, c, resolutionNotesPath, input, "resolution note")
}

func (c *OnCallClient) UpdateResolutionNote(ctx context.Context, id string, input UpdateResolutionNoteInput) (*ResolutionNote, error) {
	return updateResource[UpdateResolutionNoteInput, ResolutionNote](ctx, c, resolutionNotesPath, id, input, "resolution note")
}

func (c *OnCallClient) DeleteResolutionNote(ctx context.Context, id string) error {
	return deleteResource(ctx, c, resolutionNotesPath, id, "resolution note")
}

// --- Shift Swaps ---

func (c *OnCallClient) ListShiftSwaps(ctx context.Context) ([]ShiftSwap, error) {
	return collectAll(iterResources[ShiftSwap](ctx, c, shiftSwapsPath, "shift swap"))
}

func (c *OnCallClient) GetShiftSwap(ctx context.Context, id string) (*ShiftSwap, error) {
	return getResource[ShiftSwap](ctx, c, shiftSwapsPath, id, "shift swap")
}

func (c *OnCallClient) CreateShiftSwap(ctx context.Context, input CreateShiftSwapInput) (*ShiftSwap, error) {
	return createResource[CreateShiftSwapInput, ShiftSwap](ctx, c, shiftSwapsPath, input, "shift swap")
}

func (c *OnCallClient) UpdateShiftSwap(ctx context.Context, id string, input UpdateShiftSwapInput) (*ShiftSwap, error) {
	return updateResource[UpdateShiftSwapInput, ShiftSwap](ctx, c, shiftSwapsPath, id, input, "shift swap")
}

func (c *OnCallClient) DeleteShiftSwap(ctx context.Context, id string) error {
	return deleteResource(ctx, c, shiftSwapsPath, id, "shift swap")
}

func (c *OnCallClient) TakeShiftSwap(ctx context.Context, id string, input TakeShiftSwapInput) (*ShiftSwap, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("irm: marshal take shift swap: %w", err)
	}
	resp, err := c.DoRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/take/", shiftSwapsPath, url.PathEscape(id)), bytes.NewReader(data))
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
	return createResource[DirectPagingInput, DirectPagingResult](ctx, c, directPagingPath, input, "direct paging")
}
