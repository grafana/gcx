package oncall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"k8s.io/client-go/rest"
)

// API path constants for the OnCall API.
const (
	IntegrationsPath              = "/api/v1/integrations/"
	EscalationChainsPath          = "/api/v1/escalation_chains/"
	EscalationPoliciesPath        = "/api/v1/escalation_policies/"
	SchedulesPath                 = "/api/v1/schedules/"
	ShiftsPath                    = "/api/v1/on_call_shifts/"
	RoutesPath                    = "/api/v1/routes/"
	WebhooksPath                  = "/api/v1/webhooks/"
	AlertGroupsPath               = "/api/v1/alert_groups/"
	PersonalNotificationRulesPath = "/api/v1/personal_notification_rules/"
	UsersPath                     = "/api/v1/users/"
	UserGroupsPath                = "/api/v1/user_groups/"
	SlackChannelsPath             = "/api/v1/slack_channels/"
	TeamsPath                     = "/api/v1/teams/"
)

// Client is an HTTP client for the Grafana OnCall API.
type Client struct {
	oncallURL  string
	stackURL   string
	token      string
	httpClient *http.Client
}

// NewClient creates a new OnCall client from the given REST config and OnCall API URL.
// oncallURL is the OnCall API base URL (e.g., https://oncall-prod-us-central-0.grafana.net/oncall).
// cfg is the namespaced REST config providing auth, TLS, and the stack URL.
func NewClient(oncallURL string, cfg config.NamespacedRESTConfig) (*Client, error) {
	// OnCall API uses its own auth (raw token in Authorization header), not the
	// Grafana bearer token. Using rest.HTTPClientFor() would inject the Grafana
	// bearer token via the k8s transport round-tripper, causing 404/auth errors.
	httpClient := providers.ExternalHTTPClient()

	token := cfg.BearerToken
	if strings.HasPrefix(token, "Bearer ") {
		slog.Warn("OnCall token already contains 'Bearer ' prefix — this may be a misconfiguration; the token is used as-is without an additional prefix")
	}

	return &Client{
		oncallURL:  strings.TrimRight(oncallURL, "/"),
		stackURL:   cfg.Host,
		token:      token,
		httpClient: httpClient,
	}, nil
}

// doRequest builds and executes an HTTP request against the OnCall API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.oncallURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	// OnCall API uses raw token auth (no "Bearer" prefix), matching the cloud CLI's WithRawTokenAuth().
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Content-Type", "application/json")
	if c.stackURL != "" {
		req.Header.Set("X-Grafana-Url", c.stackURL)
	}

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

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}

// iterResources yields items one at a time across paginated API pages.
func iterResources[T any](c *Client, ctx context.Context, path, resourceType string) iter.Seq2[T, error] {
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
				yield(z, fmt.Errorf("oncall: list %s: %w", resourceType, err))
				return
			}

			if resp.StatusCode != http.StatusOK {
				err := handleErrorResponse(resp)
				resp.Body.Close()
				var z T
				yield(z, err)
				return
			}

			var result paginatedResponse[T]
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				var z T
				yield(z, fmt.Errorf("oncall: decode %s: %w", resourceType, err))
				return
			}
			resp.Body.Close()

			for _, item := range result.Results {
				if !yield(item, nil) {
					return
				}
			}

			if result.Next == nil || *result.Next == "" {
				break
			}
			// The API returns an absolute URL; validate and extract the path+query.
			nextURL, parseErr := url.Parse(*result.Next)
			if parseErr != nil {
				var z T
				yield(z, fmt.Errorf("oncall: invalid pagination URL %q: %w", *result.Next, parseErr))
				return
			}
			baseURL, _ := url.Parse(c.oncallURL)
			if nextURL.Host != "" && nextURL.Host != baseURL.Host {
				var z T
				yield(z, fmt.Errorf("oncall: pagination URL host %q does not match base URL host %q", nextURL.Host, baseURL.Host))
				return
			}
			// The API returns an absolute path that may include the oncallURL
			// path prefix (e.g. "/oncall/api/v1/..."). Strip the base path so
			// doRequest (which prepends oncallURL) doesn't double it.
			next = strings.TrimPrefix(nextURL.Path, baseURL.Path)
			if nextURL.RawQuery != "" {
				next += "?" + nextURL.RawQuery
			}
		}
	}
}

// collectAll collects all items from an iterator into a slice.
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

// getResource fetches a single resource by ID.
func getResource[T any](c *Client, ctx context.Context, basePath, id, resourceType string) (*T, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("%s%s/", basePath, url.PathEscape(id)), nil)
	if err != nil {
		return nil, fmt.Errorf("oncall: get %s: %w", resourceType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("oncall: %s %q not found", resourceType, id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("oncall: decode %s: %w", resourceType, err)
	}

	return &result, nil
}

// createResource creates a resource via POST. Input and output types may differ.
func createResource[In any, Out any](c *Client, ctx context.Context, path string, body In, resourceType string) (*Out, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("oncall: marshal %s: %w", resourceType, err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, path, strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("oncall: create %s: %w", resourceType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, handleErrorResponse(resp)
	}

	var result Out
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("oncall: decode created %s: %w", resourceType, err)
	}

	return &result, nil
}

// updateResource updates a resource via PUT. Input and output types may differ.
func updateResource[In any, Out any](c *Client, ctx context.Context, basePath, id string, body In, resourceType string) (*Out, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("oncall: marshal %s: %w", resourceType, err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("%s%s/", basePath, url.PathEscape(id)), strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("oncall: update %s: %w", resourceType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var result Out
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("oncall: decode updated %s: %w", resourceType, err)
	}

	return &result, nil
}

// deleteResource deletes a resource by ID.
func deleteResource(c *Client, ctx context.Context, basePath, id, resourceType string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("%s%s/", basePath, url.PathEscape(id)), nil)
	if err != nil {
		return fmt.Errorf("oncall: delete %s: %w", resourceType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return handleErrorResponse(resp)
	}

	return nil
}

// pathWithParams appends query parameters to a base path.
func pathWithParams(base string, params url.Values) string {
	if len(params) > 0 {
		return base + "?" + params.Encode()
	}
	return base
}

// --- Integrations ---

// ListIntegrations returns all integrations.
func (c *Client) ListIntegrations(ctx context.Context) ([]Integration, error) {
	return collectAll(iterResources[Integration](c, ctx, IntegrationsPath, "integration"))
}

// GetIntegration retrieves an integration by ID.
func (c *Client) GetIntegration(ctx context.Context, id string) (*Integration, error) {
	return getResource[Integration](c, ctx, IntegrationsPath, id, "integration")
}

// CreateIntegration creates a new integration.
func (c *Client) CreateIntegration(ctx context.Context, i Integration) (*Integration, error) {
	return createResource[Integration, Integration](c, ctx, IntegrationsPath, i, "integration")
}

// UpdateIntegration updates an existing integration.
func (c *Client) UpdateIntegration(ctx context.Context, id string, i Integration) (*Integration, error) {
	return updateResource[Integration, Integration](c, ctx, IntegrationsPath, id, i, "integration")
}

// DeleteIntegration deletes an integration.
func (c *Client) DeleteIntegration(ctx context.Context, id string) error {
	return deleteResource(c, ctx, IntegrationsPath, id, "integration")
}

// --- Escalation Chains ---

// ListEscalationChains returns all escalation chains.
func (c *Client) ListEscalationChains(ctx context.Context) ([]EscalationChain, error) {
	return collectAll(iterResources[EscalationChain](c, ctx, EscalationChainsPath, "escalation chain"))
}

// GetEscalationChain retrieves an escalation chain by ID.
func (c *Client) GetEscalationChain(ctx context.Context, id string) (*EscalationChain, error) {
	return getResource[EscalationChain](c, ctx, EscalationChainsPath, id, "escalation chain")
}

// CreateEscalationChain creates a new escalation chain.
func (c *Client) CreateEscalationChain(ctx context.Context, ec EscalationChain) (*EscalationChain, error) {
	return createResource[EscalationChain, EscalationChain](c, ctx, EscalationChainsPath, ec, "escalation chain")
}

// UpdateEscalationChain updates an existing escalation chain.
func (c *Client) UpdateEscalationChain(ctx context.Context, id string, ec EscalationChain) (*EscalationChain, error) {
	return updateResource[EscalationChain, EscalationChain](c, ctx, EscalationChainsPath, id, ec, "escalation chain")
}

// DeleteEscalationChain deletes an escalation chain.
func (c *Client) DeleteEscalationChain(ctx context.Context, id string) error {
	return deleteResource(c, ctx, EscalationChainsPath, id, "escalation chain")
}

// --- Escalation Policies ---

// ListEscalationPolicies returns all escalation policies, optionally filtered by chain ID.
func (c *Client) ListEscalationPolicies(ctx context.Context, chainID string) ([]EscalationPolicy, error) {
	params := url.Values{}
	if chainID != "" {
		params.Set("escalation_chain_id", chainID)
	}
	return collectAll(iterResources[EscalationPolicy](c, ctx, pathWithParams(EscalationPoliciesPath, params), "escalation policy"))
}

// GetEscalationPolicy retrieves an escalation policy by ID.
func (c *Client) GetEscalationPolicy(ctx context.Context, id string) (*EscalationPolicy, error) {
	return getResource[EscalationPolicy](c, ctx, EscalationPoliciesPath, id, "escalation policy")
}

// --- Schedules ---

// ListSchedules returns all schedules.
func (c *Client) ListSchedules(ctx context.Context) ([]Schedule, error) {
	return collectAll(iterResources[Schedule](c, ctx, SchedulesPath, "schedule"))
}

// GetSchedule retrieves a schedule by ID.
func (c *Client) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	return getResource[Schedule](c, ctx, SchedulesPath, id, "schedule")
}

// CreateSchedule creates a new schedule.
func (c *Client) CreateSchedule(ctx context.Context, s Schedule) (*Schedule, error) {
	return createResource[Schedule, Schedule](c, ctx, SchedulesPath, s, "schedule")
}

// UpdateSchedule updates an existing schedule.
func (c *Client) UpdateSchedule(ctx context.Context, id string, s Schedule) (*Schedule, error) {
	return updateResource[Schedule, Schedule](c, ctx, SchedulesPath, id, s, "schedule")
}

// DeleteSchedule deletes a schedule.
func (c *Client) DeleteSchedule(ctx context.Context, id string) error {
	return deleteResource(c, ctx, SchedulesPath, id, "schedule")
}

// --- Shifts ---

// ListShifts returns all shifts.
func (c *Client) ListShifts(ctx context.Context) ([]Shift, error) {
	return collectAll(iterResources[Shift](c, ctx, ShiftsPath, "shift"))
}

// GetShift retrieves a shift by ID.
func (c *Client) GetShift(ctx context.Context, id string) (*Shift, error) {
	return getResource[Shift](c, ctx, ShiftsPath, id, "shift")
}

// --- Routes ---

// ListRoutes returns all routes, optionally filtered by integration ID.
func (c *Client) ListRoutes(ctx context.Context, integrationID string) ([]IntegrationRoute, error) {
	params := url.Values{}
	if integrationID != "" {
		params.Set("integration_id", integrationID)
	}
	return collectAll(iterResources[IntegrationRoute](c, ctx, pathWithParams(RoutesPath, params), "route"))
}

// GetRoute retrieves a route by ID.
func (c *Client) GetRoute(ctx context.Context, id string) (*IntegrationRoute, error) {
	return getResource[IntegrationRoute](c, ctx, RoutesPath, id, "route")
}

// --- Outgoing Webhooks ---

// ListOutgoingWebhooks returns all outgoing webhooks.
func (c *Client) ListOutgoingWebhooks(ctx context.Context) ([]OutgoingWebhook, error) {
	return collectAll(iterResources[OutgoingWebhook](c, ctx, WebhooksPath, "outgoing webhook"))
}

// GetOutgoingWebhook retrieves an outgoing webhook by ID.
func (c *Client) GetOutgoingWebhook(ctx context.Context, id string) (*OutgoingWebhook, error) {
	return getResource[OutgoingWebhook](c, ctx, WebhooksPath, id, "outgoing webhook")
}

// CreateOutgoingWebhook creates a new outgoing webhook.
func (c *Client) CreateOutgoingWebhook(ctx context.Context, w OutgoingWebhook) (*OutgoingWebhook, error) {
	return createResource[OutgoingWebhook, OutgoingWebhook](c, ctx, WebhooksPath, w, "outgoing webhook")
}

// UpdateOutgoingWebhook updates an existing outgoing webhook.
func (c *Client) UpdateOutgoingWebhook(ctx context.Context, id string, w OutgoingWebhook) (*OutgoingWebhook, error) {
	return updateResource[OutgoingWebhook, OutgoingWebhook](c, ctx, WebhooksPath, id, w, "outgoing webhook")
}

// DeleteOutgoingWebhook deletes an outgoing webhook.
func (c *Client) DeleteOutgoingWebhook(ctx context.Context, id string) error {
	return deleteResource(c, ctx, WebhooksPath, id, "outgoing webhook")
}

// --- Alert Groups ---

// AlertGroupFilter holds optional filters for listing alert groups.
type AlertGroupFilter struct {
	// StartedAt filters by time range (format: "2006-01-02T15:04:05_2006-01-02T15:04:05").
	// The API treats this as created_at range.
	StartedAt string
}

// ListAlertGroups returns all alert groups, optionally filtered.
func (c *Client) ListAlertGroups(ctx context.Context, filters ...AlertGroupFilter) ([]AlertGroup, error) {
	params := url.Values{}
	if len(filters) > 0 && filters[0].StartedAt != "" {
		params.Set("started_at", filters[0].StartedAt)
	}
	return collectAll(iterResources[AlertGroup](c, ctx, pathWithParams(AlertGroupsPath, params), "alert group"))
}

// GetAlertGroup retrieves an alert group by ID.
func (c *Client) GetAlertGroup(ctx context.Context, id string) (*AlertGroup, error) {
	return getResource[AlertGroup](c, ctx, AlertGroupsPath, id, "alert group")
}

// AcknowledgeAlertGroup acknowledges an alert group.
func (c *Client) AcknowledgeAlertGroup(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/acknowledge/", AlertGroupsPath, url.PathEscape(id)), nil)
	if err != nil {
		return fmt.Errorf("oncall: acknowledge alert group: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return handleErrorResponse(resp)
	}
	return nil
}

// ResolveAlertGroup resolves an alert group.
func (c *Client) ResolveAlertGroup(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/resolve/", AlertGroupsPath, url.PathEscape(id)), nil)
	if err != nil {
		return fmt.Errorf("oncall: resolve alert group: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return handleErrorResponse(resp)
	}
	return nil
}

// SilenceAlertGroup silences an alert group for the given duration in seconds.
func (c *Client) SilenceAlertGroup(ctx context.Context, id string, delaySecs int) error {
	data, err := json.Marshal(map[string]int{"delay": delaySecs})
	if err != nil {
		return fmt.Errorf("oncall: marshal silence request: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/silence/", AlertGroupsPath, url.PathEscape(id)), strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("oncall: silence alert group: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return handleErrorResponse(resp)
	}
	return nil
}

// --- Alert Group Actions ---

// UnacknowledgeAlertGroup unacknowledges an alert group.
func (c *Client) UnacknowledgeAlertGroup(ctx context.Context, id string) error {
	return c.alertGroupAction(ctx, id, "unacknowledge")
}

// UnresolveAlertGroup unresolves an alert group.
func (c *Client) UnresolveAlertGroup(ctx context.Context, id string) error {
	return c.alertGroupAction(ctx, id, "unresolve")
}

// UnsilenceAlertGroup unsilences an alert group.
func (c *Client) UnsilenceAlertGroup(ctx context.Context, id string) error {
	return c.alertGroupAction(ctx, id, "unsilence")
}

// DeleteAlertGroup deletes an alert group.
func (c *Client) DeleteAlertGroup(ctx context.Context, id string) error {
	return deleteResource(c, ctx, AlertGroupsPath, id, "alert group")
}

func (c *Client) alertGroupAction(ctx context.Context, id, action string) error {
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/%s/", AlertGroupsPath, url.PathEscape(id), action), nil)
	if err != nil {
		return fmt.Errorf("oncall: %s alert group: %w", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return handleErrorResponse(resp)
	}
	return nil
}

// --- Users ---

// ListUsers returns all users.
func (c *Client) ListUsers(ctx context.Context) ([]User, error) {
	return collectAll(iterResources[User](c, ctx, UsersPath, "user"))
}

// GetUser retrieves a user by ID.
func (c *Client) GetUser(ctx context.Context, id string) (*User, error) {
	return getResource[User](c, ctx, UsersPath, id, "user")
}

// GetCurrentUser retrieves the currently authenticated user.
func (c *Client) GetCurrentUser(ctx context.Context) (*User, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, UsersPath+"current/", nil)
	if err != nil {
		return nil, fmt.Errorf("oncall: get current user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}
	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("oncall: decode current user: %w", err)
	}
	return &user, nil
}

// --- Personal Notification Rules ---

// ListPersonalNotificationRules returns all personal notification rules.
func (c *Client) ListPersonalNotificationRules(ctx context.Context) ([]PersonalNotificationRule, error) {
	return collectAll(iterResources[PersonalNotificationRule](c, ctx, PersonalNotificationRulesPath, "personal notification rule"))
}

// GetPersonalNotificationRule retrieves a personal notification rule by ID.
func (c *Client) GetPersonalNotificationRule(ctx context.Context, id string) (*PersonalNotificationRule, error) {
	return getResource[PersonalNotificationRule](c, ctx, PersonalNotificationRulesPath, id, "personal notification rule")
}

// CreatePersonalNotificationRule creates a new personal notification rule.
func (c *Client) CreatePersonalNotificationRule(ctx context.Context, r PersonalNotificationRule) (*PersonalNotificationRule, error) {
	return createResource[PersonalNotificationRule, PersonalNotificationRule](c, ctx, PersonalNotificationRulesPath, r, "personal notification rule")
}

// UpdatePersonalNotificationRule updates a personal notification rule.
func (c *Client) UpdatePersonalNotificationRule(ctx context.Context, id string, r PersonalNotificationRule) (*PersonalNotificationRule, error) {
	return updateResource[PersonalNotificationRule, PersonalNotificationRule](c, ctx, PersonalNotificationRulesPath, id, r, "personal notification rule")
}

// DeletePersonalNotificationRule deletes a personal notification rule.
func (c *Client) DeletePersonalNotificationRule(ctx context.Context, id string) error {
	return deleteResource(c, ctx, PersonalNotificationRulesPath, id, "personal notification rule")
}

// --- Alerts ---

// ListAlerts returns all alerts, optionally filtered by alert group.
func (c *Client) ListAlerts(ctx context.Context, alertGroupID string) ([]Alert, error) {
	params := url.Values{}
	if alertGroupID != "" {
		params.Set("alert_group_id", alertGroupID)
	}
	return collectAll(iterResources[Alert](c, ctx, pathWithParams("/api/v1/alerts/", params), "alert"))
}

// GetAlert retrieves an alert by ID.
func (c *Client) GetAlert(ctx context.Context, id string) (*Alert, error) {
	return getResource[Alert](c, ctx, "/api/v1/alerts/", id, "alert")
}

// --- Resolution Notes ---

// ListResolutionNotes returns all resolution notes, optionally filtered by alert group.
func (c *Client) ListResolutionNotes(ctx context.Context, alertGroupID string) ([]ResolutionNote, error) {
	params := url.Values{}
	if alertGroupID != "" {
		params.Set("alert_group_id", alertGroupID)
	}
	return collectAll(iterResources[ResolutionNote](c, ctx, pathWithParams("/api/v1/resolution_notes/", params), "resolution note"))
}

// GetResolutionNote retrieves a resolution note by ID.
func (c *Client) GetResolutionNote(ctx context.Context, id string) (*ResolutionNote, error) {
	return getResource[ResolutionNote](c, ctx, "/api/v1/resolution_notes/", id, "resolution note")
}

// CreateResolutionNote creates a new resolution note.
func (c *Client) CreateResolutionNote(ctx context.Context, input CreateResolutionNoteInput) (*ResolutionNote, error) {
	return createResource[CreateResolutionNoteInput, ResolutionNote](c, ctx, "/api/v1/resolution_notes/", input, "resolution note")
}

// UpdateResolutionNote updates a resolution note.
func (c *Client) UpdateResolutionNote(ctx context.Context, id string, input UpdateResolutionNoteInput) (*ResolutionNote, error) {
	return updateResource[UpdateResolutionNoteInput, ResolutionNote](c, ctx, "/api/v1/resolution_notes/", id, input, "resolution note")
}

// DeleteResolutionNote deletes a resolution note.
func (c *Client) DeleteResolutionNote(ctx context.Context, id string) error {
	return deleteResource(c, ctx, "/api/v1/resolution_notes/", id, "resolution note")
}

// --- Shift Swaps ---

// ListShiftSwaps returns all shift swaps.
func (c *Client) ListShiftSwaps(ctx context.Context) ([]ShiftSwap, error) {
	return collectAll(iterResources[ShiftSwap](c, ctx, "/api/v1/shift_swaps/", "shift swap"))
}

// GetShiftSwap retrieves a shift swap by ID.
func (c *Client) GetShiftSwap(ctx context.Context, id string) (*ShiftSwap, error) {
	return getResource[ShiftSwap](c, ctx, "/api/v1/shift_swaps/", id, "shift swap")
}

// CreateShiftSwap creates a new shift swap.
func (c *Client) CreateShiftSwap(ctx context.Context, input CreateShiftSwapInput) (*ShiftSwap, error) {
	return createResource[CreateShiftSwapInput, ShiftSwap](c, ctx, "/api/v1/shift_swaps/", input, "shift swap")
}

// UpdateShiftSwap updates a shift swap.
func (c *Client) UpdateShiftSwap(ctx context.Context, id string, input UpdateShiftSwapInput) (*ShiftSwap, error) {
	return updateResource[UpdateShiftSwapInput, ShiftSwap](c, ctx, "/api/v1/shift_swaps/", id, input, "shift swap")
}

// DeleteShiftSwap deletes a shift swap.
func (c *Client) DeleteShiftSwap(ctx context.Context, id string) error {
	return deleteResource(c, ctx, "/api/v1/shift_swaps/", id, "shift swap")
}

// TakeShiftSwap takes a shift swap (assigns a benefactor).
func (c *Client) TakeShiftSwap(ctx context.Context, id string, input TakeShiftSwapInput) (*ShiftSwap, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("oncall: marshal take shift swap: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/shift_swaps/%s/take/", url.PathEscape(id)), strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("oncall: take shift swap: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}
	var result ShiftSwap
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("oncall: decode shift swap: %w", err)
	}
	return &result, nil
}

// --- Direct Escalation ---

// CreateDirectEscalation creates a direct escalation (pages a user or team).
func (c *Client) CreateDirectEscalation(ctx context.Context, input DirectEscalationInput) (*DirectEscalationResult, error) {
	return createResource[DirectEscalationInput, DirectEscalationResult](c, ctx, "/api/v1/escalations/", input, "direct escalation")
}

// --- Organizations ---

// ListOrganizations returns all organizations.
func (c *Client) ListOrganizations(ctx context.Context) ([]Organization, error) {
	return collectAll(iterResources[Organization](c, ctx, "/api/v1/organizations/", "organization"))
}

// GetOrganization retrieves an organization by ID.
func (c *Client) GetOrganization(ctx context.Context, id string) (*Organization, error) {
	return getResource[Organization](c, ctx, "/api/v1/organizations/", id, "organization")
}

// --- Shifts (CRUD) ---

// CreateShift creates a new shift.
func (c *Client) CreateShift(ctx context.Context, s ShiftRequest) (*Shift, error) {
	return createResource[ShiftRequest, Shift](c, ctx, ShiftsPath, s, "shift")
}

// UpdateShift updates an existing shift.
func (c *Client) UpdateShift(ctx context.Context, id string, s ShiftRequest) (*Shift, error) {
	return updateResource[ShiftRequest, Shift](c, ctx, ShiftsPath, id, s, "shift")
}

// DeleteShift deletes a shift.
func (c *Client) DeleteShift(ctx context.Context, id string) error {
	return deleteResource(c, ctx, ShiftsPath, id, "shift")
}

// --- Routes (CRUD) ---

// CreateRoute creates a new route.
func (c *Client) CreateRoute(ctx context.Context, r IntegrationRoute) (*IntegrationRoute, error) {
	return createResource[IntegrationRoute, IntegrationRoute](c, ctx, RoutesPath, r, "route")
}

// UpdateRoute updates an existing route.
func (c *Client) UpdateRoute(ctx context.Context, id string, r IntegrationRoute) (*IntegrationRoute, error) {
	return updateResource[IntegrationRoute, IntegrationRoute](c, ctx, RoutesPath, id, r, "route")
}

// DeleteRoute deletes a route.
func (c *Client) DeleteRoute(ctx context.Context, id string) error {
	return deleteResource(c, ctx, RoutesPath, id, "route")
}

// --- Escalation Policies (CRUD) ---

// CreateEscalationPolicy creates a new escalation policy.
func (c *Client) CreateEscalationPolicy(ctx context.Context, p EscalationPolicy) (*EscalationPolicy, error) {
	return createResource[EscalationPolicy, EscalationPolicy](c, ctx, EscalationPoliciesPath, p, "escalation policy")
}

// UpdateEscalationPolicy updates an existing escalation policy.
func (c *Client) UpdateEscalationPolicy(ctx context.Context, id string, p EscalationPolicy) (*EscalationPolicy, error) {
	return updateResource[EscalationPolicy, EscalationPolicy](c, ctx, EscalationPoliciesPath, id, p, "escalation policy")
}

// DeleteEscalationPolicy deletes an escalation policy.
func (c *Client) DeleteEscalationPolicy(ctx context.Context, id string) error {
	return deleteResource(c, ctx, EscalationPoliciesPath, id, "escalation policy")
}

// --- Schedules (Final Shifts) ---

// ListFinalShifts returns resolved on-call shifts for a schedule within a date range.
func (c *Client) ListFinalShifts(ctx context.Context, scheduleID, startDate, endDate string) ([]FinalShift, error) {
	params := url.Values{}
	params.Set("start_date", startDate)
	params.Set("end_date", endDate)
	path := fmt.Sprintf("%s%s/final_shifts/?%s", SchedulesPath, url.PathEscape(scheduleID), params.Encode())
	return collectAll(iterResources[FinalShift](c, ctx, path, "final shift"))
}

// --- User Groups ---

// ListUserGroups returns all user groups.
func (c *Client) ListUserGroups(ctx context.Context) ([]UserGroup, error) {
	return collectAll(iterResources[UserGroup](c, ctx, UserGroupsPath, "user group"))
}

// --- Slack Channels ---

// ListSlackChannels returns all Slack channels.
func (c *Client) ListSlackChannels(ctx context.Context) ([]SlackChannel, error) {
	return collectAll(iterResources[SlackChannel](c, ctx, SlackChannelsPath, "slack channel"))
}

// --- Teams ---

// ListTeams returns all teams.
func (c *Client) ListTeams(ctx context.Context) ([]Team, error) {
	return collectAll(iterResources[Team](c, ctx, TeamsPath, "team"))
}

// GetTeam retrieves a team by ID.
func (c *Client) GetTeam(ctx context.Context, id string) (*Team, error) {
	return getResource[Team](c, ctx, TeamsPath, id, "team")
}

// --- OnCall API URL Discovery ---

// PluginSettings represents the OnCall plugin settings.
type PluginSettings struct {
	JSONData struct {
		OnCallAPIURL string `json:"onCallApiUrl"`
	} `json:"jsonData"`
}

// DiscoverOnCallURL fetches the OnCall API URL from the Grafana IRM plugin settings.
func DiscoverOnCallURL(ctx context.Context, cfg config.NamespacedRESTConfig) (string, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Host+"/api/plugins/grafana-irm-app/settings", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get OnCall plugin settings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", handleErrorResponse(resp)
	}

	var settings PluginSettings
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		return "", fmt.Errorf("failed to decode plugin settings: %w", err)
	}

	if settings.JSONData.OnCallAPIURL == "" {
		return "", errors.New("OnCall API URL not configured in plugin settings")
	}

	return settings.JSONData.OnCallAPIURL, nil
}
