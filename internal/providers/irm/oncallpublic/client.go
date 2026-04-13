// Package oncallpublic provides a client for the public API of the OnCall
// backend. We only need this right now because the IRM plugin proxy
// rejects SA tokens for all requests, but work is in progress to change that.
// Once the IRM plugin proxy supports SA tokens, we can rip out this package.
package oncallpublic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/providers/irm/oncalltypes"
	"k8s.io/client-go/rest"
)

const (
	integrationsPath       = "/api/v1/integrations/"
	escalationChainsPath   = "/api/v1/escalation_chains/"
	escalationPoliciesPath = "/api/v1/escalation_policies/"
	schedulesPath          = "/api/v1/schedules/"
	shiftsPath             = "/api/v1/on_call_shifts/"
	routesPath             = "/api/v1/routes/"
	webhooksPath           = "/api/v1/webhooks/"
	alertGroupsPath        = "/api/v1/alert_groups/"
	usersPath              = "/api/v1/users/"
	teamsPath              = "/api/v1/teams/"
	userGroupsPath         = "/api/v1/user_groups/"
	slackChannelsPath      = "/api/v1/slack_channels/"
	alertsPath             = "/api/v1/alerts/"
	organizationsPath      = "/api/v1/organizations/"
	resolutionNotesPath    = "/api/v1/resolution_notes/"
	shiftSwapsPath         = "/api/v1/shift_swaps/"
	escalationsPath        = "/api/v1/escalations/"
)

// Client calls the OnCall public API directly with an SA token.
// It adapts all responses to the internal API type shape (oncalltypes.Integration, etc.)
// so commands and codecs see a consistent interface regardless of auth mode.
type Client struct {
	oncallURL  string
	stackURL   string
	token      string
	httpClient *http.Client
}

// NewClient creates a public API OnCall client.
func NewClient(ctx context.Context, oncallURL string, cfg config.NamespacedRESTConfig) (*Client, error) {
	token := cfg.BearerToken
	if strings.HasPrefix(token, "Bearer ") {
		slog.Warn("OnCall token already contains 'Bearer ' prefix — used as-is")
	}
	return &Client{
		oncallURL:  strings.TrimRight(oncallURL, "/"),
		stackURL:   cfg.Host,
		token:      token,
		httpClient: httputils.NewDefaultClient(ctx),
	}, nil
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
		return "", fmt.Errorf("failed to fetch plugin settings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch plugin settings: status %d", resp.StatusCode)
	}

	var settings struct {
		JSONData struct {
			OnCallAPIURL string `json:"onCallApiUrl"`
		} `json:"jsonData"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&settings); err != nil {
		return "", fmt.Errorf("failed to decode plugin settings: %w", err)
	}

	if settings.JSONData.OnCallAPIURL == "" {
		return "", fmt.Errorf("OnCall API URL not found in plugin settings")
	}

	return settings.JSONData.OnCallAPIURL, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.oncallURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
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
			next = strings.TrimPrefix(nextURL.Path, baseURL.Path)
			if nextURL.RawQuery != "" {
				next += "?" + nextURL.RawQuery
			}
		}
	}
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

func getResource[T any](c *Client, ctx context.Context, basePath, id, resourceType string) (*T, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("%s%s/", basePath, url.PathEscape(id)), nil)
	if err != nil {
		return nil, fmt.Errorf("oncall: get %s: %w", resourceType, err)
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
		return nil, fmt.Errorf("oncall: decode %s: %w", resourceType, err)
	}
	return &result, nil
}

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

func alertGroupAction(c *Client, ctx context.Context, id, action string) error {
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/%s/", alertGroupsPath, url.PathEscape(id), action), nil)
	if err != nil {
		return fmt.Errorf("oncall: %s alert group: %w", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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

// --- OnCallAPI implementation ---

func (c *Client) ListIntegrations(ctx context.Context) ([]oncalltypes.Integration, error) {
	items, err := collectAll(iterResources[integration](c, ctx, integrationsPath, "integration"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptIntegration), nil
}

func (c *Client) GetIntegration(ctx context.Context, id string) (*oncalltypes.Integration, error) {
	p, err := getResource[integration](c, ctx, integrationsPath, id, "integration")
	if err != nil {
		return nil, err
	}
	r := adaptIntegration(*p)
	return &r, nil
}

func (c *Client) CreateIntegration(ctx context.Context, i oncalltypes.Integration) (*oncalltypes.Integration, error) {
	p, err := createResource[oncalltypes.Integration, integration](c, ctx, integrationsPath, i, "integration")
	if err != nil {
		return nil, err
	}
	r := adaptIntegration(*p)
	return &r, nil
}

func (c *Client) UpdateIntegration(ctx context.Context, id string, i oncalltypes.Integration) (*oncalltypes.Integration, error) {
	p, err := updateResource[oncalltypes.Integration, integration](c, ctx, integrationsPath, id, i, "integration")
	if err != nil {
		return nil, err
	}
	r := adaptIntegration(*p)
	return &r, nil
}

func (c *Client) DeleteIntegration(ctx context.Context, id string) error {
	return deleteResource(c, ctx, integrationsPath, id, "integration")
}

func (c *Client) ListEscalationChains(ctx context.Context) ([]oncalltypes.EscalationChain, error) {
	items, err := collectAll(iterResources[escalationChain](c, ctx, escalationChainsPath, "escalation chain"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptEscalationChain), nil
}

func (c *Client) GetEscalationChain(ctx context.Context, id string) (*oncalltypes.EscalationChain, error) {
	p, err := getResource[escalationChain](c, ctx, escalationChainsPath, id, "escalation chain")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationChain(*p)
	return &r, nil
}

func (c *Client) CreateEscalationChain(ctx context.Context, ec oncalltypes.EscalationChain) (*oncalltypes.EscalationChain, error) {
	p, err := createResource[oncalltypes.EscalationChain, escalationChain](c, ctx, escalationChainsPath, ec, "escalation chain")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationChain(*p)
	return &r, nil
}

func (c *Client) UpdateEscalationChain(ctx context.Context, id string, ec oncalltypes.EscalationChain) (*oncalltypes.EscalationChain, error) {
	p, err := updateResource[oncalltypes.EscalationChain, escalationChain](c, ctx, escalationChainsPath, id, ec, "escalation chain")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationChain(*p)
	return &r, nil
}

func (c *Client) DeleteEscalationChain(ctx context.Context, id string) error {
	return deleteResource(c, ctx, escalationChainsPath, id, "escalation chain")
}

func (c *Client) ListEscalationPolicies(ctx context.Context, chainID string) ([]oncalltypes.EscalationPolicy, error) {
	params := url.Values{}
	if chainID != "" {
		params.Set("escalation_chain_id", chainID)
	}
	items, err := collectAll(iterResources[escalationPolicy](c, ctx, pathWithParams(escalationPoliciesPath, params), "escalation policy"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptEscalationPolicy), nil
}

func (c *Client) GetEscalationPolicy(ctx context.Context, id string) (*oncalltypes.EscalationPolicy, error) {
	p, err := getResource[escalationPolicy](c, ctx, escalationPoliciesPath, id, "escalation policy")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationPolicy(*p)
	return &r, nil
}

func (c *Client) CreateEscalationPolicy(ctx context.Context, ep oncalltypes.EscalationPolicy) (*oncalltypes.EscalationPolicy, error) {
	p, err := createResource[oncalltypes.EscalationPolicy, escalationPolicy](c, ctx, escalationPoliciesPath, ep, "escalation policy")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationPolicy(*p)
	return &r, nil
}

func (c *Client) UpdateEscalationPolicy(ctx context.Context, id string, ep oncalltypes.EscalationPolicy) (*oncalltypes.EscalationPolicy, error) {
	p, err := updateResource[oncalltypes.EscalationPolicy, escalationPolicy](c, ctx, escalationPoliciesPath, id, ep, "escalation policy")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationPolicy(*p)
	return &r, nil
}

func (c *Client) DeleteEscalationPolicy(ctx context.Context, id string) error {
	return deleteResource(c, ctx, escalationPoliciesPath, id, "escalation policy")
}

func (c *Client) ListSchedules(ctx context.Context) ([]oncalltypes.Schedule, error) {
	items, err := collectAll(iterResources[schedule](c, ctx, schedulesPath, "schedule"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptSchedule), nil
}

func (c *Client) GetSchedule(ctx context.Context, id string) (*oncalltypes.Schedule, error) {
	p, err := getResource[schedule](c, ctx, schedulesPath, id, "schedule")
	if err != nil {
		return nil, err
	}
	r := adaptSchedule(*p)
	return &r, nil
}

func (c *Client) CreateSchedule(ctx context.Context, s oncalltypes.Schedule) (*oncalltypes.Schedule, error) {
	p, err := createResource[oncalltypes.Schedule, schedule](c, ctx, schedulesPath, s, "schedule")
	if err != nil {
		return nil, err
	}
	r := adaptSchedule(*p)
	return &r, nil
}

func (c *Client) UpdateSchedule(ctx context.Context, id string, s oncalltypes.Schedule) (*oncalltypes.Schedule, error) {
	p, err := updateResource[oncalltypes.Schedule, schedule](c, ctx, schedulesPath, id, s, "schedule")
	if err != nil {
		return nil, err
	}
	r := adaptSchedule(*p)
	return &r, nil
}

func (c *Client) DeleteSchedule(ctx context.Context, id string) error {
	return deleteResource(c, ctx, schedulesPath, id, "schedule")
}

func (c *Client) ListFilterEvents(ctx context.Context, scheduleID, userTZ, startingDate string, days int) (*oncalltypes.FilterEventsResponse, error) {
	// The public API uses final_shifts, not filter_events.
	// Fetch and convert to the FilterEventsResponse shape.
	params := url.Values{}
	params.Set("start_date", startingDate)
	params.Set("end_date", startingDate) // will be computed properly by the command
	path := fmt.Sprintf("%s%s/final_shifts/?%s", schedulesPath, url.PathEscape(scheduleID), params.Encode())
	items, err := collectAll(iterResources[finalShift](c, ctx, path, "final shift"))
	if err != nil {
		return nil, err
	}
	var events []oncalltypes.FinalShiftEvent
	for _, fs := range items {
		events = append(events, oncalltypes.FinalShiftEvent{
			Start: fs.ShiftStart,
			End:   fs.ShiftEnd,
			Users: []struct {
				DisplayName string `json:"display_name"`
				PK          string `json:"pk"`
				Email       string `json:"email"`
			}{
				{DisplayName: fs.UserUsername, PK: fs.UserPK, Email: fs.UserEmail},
			},
		})
	}
	return &oncalltypes.FilterEventsResponse{Events: events}, nil
}

func (c *Client) ListShifts(ctx context.Context) ([]oncalltypes.Shift, error) {
	items, err := collectAll(iterResources[shift](c, ctx, shiftsPath, "shift"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptShift), nil
}

func (c *Client) GetShift(ctx context.Context, id string) (*oncalltypes.Shift, error) {
	p, err := getResource[shift](c, ctx, shiftsPath, id, "shift")
	if err != nil {
		return nil, err
	}
	r := adaptShift(*p)
	return &r, nil
}

func (c *Client) CreateShift(ctx context.Context, s oncalltypes.ShiftRequest) (*oncalltypes.Shift, error) {
	p, err := createResource[oncalltypes.ShiftRequest, shift](c, ctx, shiftsPath, s, "shift")
	if err != nil {
		return nil, err
	}
	r := adaptShift(*p)
	return &r, nil
}

func (c *Client) UpdateShift(ctx context.Context, id string, s oncalltypes.ShiftRequest) (*oncalltypes.Shift, error) {
	p, err := updateResource[oncalltypes.ShiftRequest, shift](c, ctx, shiftsPath, id, s, "shift")
	if err != nil {
		return nil, err
	}
	r := adaptShift(*p)
	return &r, nil
}

func (c *Client) DeleteShift(ctx context.Context, id string) error {
	return deleteResource(c, ctx, shiftsPath, id, "shift")
}

func (c *Client) ListRoutes(ctx context.Context, integrationID string) ([]oncalltypes.Route, error) {
	params := url.Values{}
	if integrationID != "" {
		params.Set("integration_id", integrationID)
	}
	items, err := collectAll(iterResources[route](c, ctx, pathWithParams(routesPath, params), "route"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptRoute), nil
}

func (c *Client) GetRoute(ctx context.Context, id string) (*oncalltypes.Route, error) {
	p, err := getResource[route](c, ctx, routesPath, id, "route")
	if err != nil {
		return nil, err
	}
	r := adaptRoute(*p)
	return &r, nil
}

func (c *Client) CreateRoute(ctx context.Context, rt oncalltypes.Route) (*oncalltypes.Route, error) {
	p, err := createResource[oncalltypes.Route, route](c, ctx, routesPath, rt, "route")
	if err != nil {
		return nil, err
	}
	r := adaptRoute(*p)
	return &r, nil
}

func (c *Client) UpdateRoute(ctx context.Context, id string, rt oncalltypes.Route) (*oncalltypes.Route, error) {
	p, err := updateResource[oncalltypes.Route, route](c, ctx, routesPath, id, rt, "route")
	if err != nil {
		return nil, err
	}
	r := adaptRoute(*p)
	return &r, nil
}

func (c *Client) DeleteRoute(ctx context.Context, id string) error {
	return deleteResource(c, ctx, routesPath, id, "route")
}

func (c *Client) ListWebhooks(ctx context.Context) ([]oncalltypes.Webhook, error) {
	items, err := collectAll(iterResources[webhook](c, ctx, webhooksPath, "webhook"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptWebhook), nil
}

func (c *Client) GetWebhook(ctx context.Context, id string) (*oncalltypes.Webhook, error) {
	p, err := getResource[webhook](c, ctx, webhooksPath, id, "webhook")
	if err != nil {
		return nil, err
	}
	r := adaptWebhook(*p)
	return &r, nil
}

func (c *Client) CreateWebhook(ctx context.Context, w oncalltypes.Webhook) (*oncalltypes.Webhook, error) {
	p, err := createResource[oncalltypes.Webhook, webhook](c, ctx, webhooksPath, w, "webhook")
	if err != nil {
		return nil, err
	}
	r := adaptWebhook(*p)
	return &r, nil
}

func (c *Client) UpdateWebhook(ctx context.Context, id string, w oncalltypes.Webhook) (*oncalltypes.Webhook, error) {
	p, err := updateResource[oncalltypes.Webhook, webhook](c, ctx, webhooksPath, id, w, "webhook")
	if err != nil {
		return nil, err
	}
	r := adaptWebhook(*p)
	return &r, nil
}

func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	return deleteResource(c, ctx, webhooksPath, id, "webhook")
}

func (c *Client) ListAlertGroups(ctx context.Context) ([]oncalltypes.AlertGroup, error) {
	items, err := collectAll(iterResources[alertGroup](c, ctx, alertGroupsPath, "alert group"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptAlertGroup), nil
}

func (c *Client) GetAlertGroup(ctx context.Context, id string) (*oncalltypes.AlertGroup, error) {
	p, err := getResource[alertGroup](c, ctx, alertGroupsPath, id, "alert group")
	if err != nil {
		return nil, err
	}
	r := adaptAlertGroup(*p)
	return &r, nil
}

func (c *Client) DeleteAlertGroup(ctx context.Context, id string) error {
	return deleteResource(c, ctx, alertGroupsPath, id, "alert group")
}

func (c *Client) AcknowledgeAlertGroup(ctx context.Context, id string) error {
	return alertGroupAction(c, ctx, id, "acknowledge")
}

func (c *Client) ResolveAlertGroup(ctx context.Context, id string) error {
	return alertGroupAction(c, ctx, id, "resolve")
}

func (c *Client) SilenceAlertGroup(ctx context.Context, id string, delaySecs int) error {
	data, err := json.Marshal(map[string]int{"delay": delaySecs})
	if err != nil {
		return fmt.Errorf("oncall: marshal silence request: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/silence/", alertGroupsPath, url.PathEscape(id)), strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("oncall: silence alert group: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return handleErrorResponse(resp)
	}
	return nil
}

func (c *Client) UnacknowledgeAlertGroup(ctx context.Context, id string) error {
	return alertGroupAction(c, ctx, id, "unacknowledge")
}

func (c *Client) UnresolveAlertGroup(ctx context.Context, id string) error {
	return alertGroupAction(c, ctx, id, "unresolve")
}

func (c *Client) UnsilenceAlertGroup(ctx context.Context, id string) error {
	return alertGroupAction(c, ctx, id, "unsilence")
}

func (c *Client) ListUsers(ctx context.Context) ([]oncalltypes.User, error) {
	items, err := collectAll(iterResources[user](c, ctx, usersPath, "user"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptUser), nil
}

func (c *Client) GetUser(ctx context.Context, id string) (*oncalltypes.User, error) {
	p, err := getResource[user](c, ctx, usersPath, id, "user")
	if err != nil {
		return nil, err
	}
	r := adaptUser(*p)
	return &r, nil
}

func (c *Client) GetCurrentUser(ctx context.Context) (*oncalltypes.User, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, usersPath+"current/", nil)
	if err != nil {
		return nil, fmt.Errorf("oncall: get current user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}
	var u user
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("oncall: decode current user: %w", err)
	}
	r := adaptUser(u)
	return &r, nil
}

func (c *Client) ListTeams(ctx context.Context) ([]oncalltypes.Team, error) {
	items, err := collectAll(iterResources[team](c, ctx, teamsPath, "team"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptTeam), nil
}

func (c *Client) GetTeam(ctx context.Context, id string) (*oncalltypes.Team, error) {
	p, err := getResource[team](c, ctx, teamsPath, id, "team")
	if err != nil {
		return nil, err
	}
	r := adaptTeam(*p)
	return &r, nil
}

func (c *Client) ListUserGroups(ctx context.Context) ([]oncalltypes.UserGroup, error) {
	items, err := collectAll(iterResources[userGroup](c, ctx, userGroupsPath, "user group"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptUserGroup), nil
}

func (c *Client) ListSlackChannels(ctx context.Context) ([]oncalltypes.SlackChannel, error) {
	items, err := collectAll(iterResources[slackChannel](c, ctx, slackChannelsPath, "slack channel"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptSlackChannel), nil
}

func (c *Client) ListAlerts(ctx context.Context, alertGroupID string) ([]oncalltypes.Alert, error) {
	params := url.Values{}
	if alertGroupID != "" {
		params.Set("alert_group_id", alertGroupID)
	}
	items, err := collectAll(iterResources[alert](c, ctx, pathWithParams(alertsPath, params), "alert"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptAlert), nil
}

func (c *Client) GetAlert(ctx context.Context, id string) (*oncalltypes.Alert, error) {
	p, err := getResource[alert](c, ctx, alertsPath, id, "alert")
	if err != nil {
		return nil, err
	}
	r := adaptAlert(*p)
	return &r, nil
}

func (c *Client) GetOrganization(ctx context.Context) (*oncalltypes.Organization, error) {
	// The public API has a list endpoint; get the first org.
	items, err := collectAll(iterResources[organization](c, ctx, organizationsPath, "organization"))
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("irm: no organizations found")
	}
	r := adaptOrganization(items[0])
	return &r, nil
}

func (c *Client) ListResolutionNotes(ctx context.Context, alertGroupID string) ([]oncalltypes.ResolutionNote, error) {
	params := url.Values{}
	if alertGroupID != "" {
		params.Set("alert_group_id", alertGroupID)
	}
	items, err := collectAll(iterResources[resolutionNote](c, ctx, pathWithParams(resolutionNotesPath, params), "resolution note"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptResolutionNote), nil
}

func (c *Client) GetResolutionNote(ctx context.Context, id string) (*oncalltypes.ResolutionNote, error) {
	p, err := getResource[resolutionNote](c, ctx, resolutionNotesPath, id, "resolution note")
	if err != nil {
		return nil, err
	}
	r := adaptResolutionNote(*p)
	return &r, nil
}

func (c *Client) CreateResolutionNote(ctx context.Context, input oncalltypes.CreateResolutionNoteInput) (*oncalltypes.ResolutionNote, error) {
	p, err := createResource[oncalltypes.CreateResolutionNoteInput, resolutionNote](c, ctx, resolutionNotesPath, input, "resolution note")
	if err != nil {
		return nil, err
	}
	r := adaptResolutionNote(*p)
	return &r, nil
}

func (c *Client) UpdateResolutionNote(ctx context.Context, id string, input oncalltypes.UpdateResolutionNoteInput) (*oncalltypes.ResolutionNote, error) {
	p, err := updateResource[oncalltypes.UpdateResolutionNoteInput, resolutionNote](c, ctx, resolutionNotesPath, id, input, "resolution note")
	if err != nil {
		return nil, err
	}
	r := adaptResolutionNote(*p)
	return &r, nil
}

func (c *Client) DeleteResolutionNote(ctx context.Context, id string) error {
	return deleteResource(c, ctx, resolutionNotesPath, id, "resolution note")
}

func (c *Client) ListShiftSwaps(ctx context.Context) ([]oncalltypes.ShiftSwap, error) {
	items, err := collectAll(iterResources[shiftSwap](c, ctx, shiftSwapsPath, "shift swap"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptShiftSwap), nil
}

func (c *Client) GetShiftSwap(ctx context.Context, id string) (*oncalltypes.ShiftSwap, error) {
	p, err := getResource[shiftSwap](c, ctx, shiftSwapsPath, id, "shift swap")
	if err != nil {
		return nil, err
	}
	r := adaptShiftSwap(*p)
	return &r, nil
}

func (c *Client) CreateShiftSwap(ctx context.Context, input oncalltypes.CreateShiftSwapInput) (*oncalltypes.ShiftSwap, error) {
	p, err := createResource[oncalltypes.CreateShiftSwapInput, shiftSwap](c, ctx, shiftSwapsPath, input, "shift swap")
	if err != nil {
		return nil, err
	}
	r := adaptShiftSwap(*p)
	return &r, nil
}

func (c *Client) UpdateShiftSwap(ctx context.Context, id string, input oncalltypes.UpdateShiftSwapInput) (*oncalltypes.ShiftSwap, error) {
	p, err := updateResource[oncalltypes.UpdateShiftSwapInput, shiftSwap](c, ctx, shiftSwapsPath, id, input, "shift swap")
	if err != nil {
		return nil, err
	}
	r := adaptShiftSwap(*p)
	return &r, nil
}

func (c *Client) DeleteShiftSwap(ctx context.Context, id string) error {
	return deleteResource(c, ctx, shiftSwapsPath, id, "shift swap")
}

func (c *Client) TakeShiftSwap(ctx context.Context, id string, input oncalltypes.TakeShiftSwapInput) (*oncalltypes.ShiftSwap, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("oncall: marshal take shift swap: %w", err)
	}
	resp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("%s%s/take/", shiftSwapsPath, url.PathEscape(id)), strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("oncall: take shift swap: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}
	var p shiftSwap
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("oncall: decode shift swap: %w", err)
	}
	r := adaptShiftSwap(p)
	return &r, nil
}

func (c *Client) CreateDirectPaging(ctx context.Context, input oncalltypes.DirectPagingInput) (*oncalltypes.DirectPagingResult, error) {
	// Adapt internal API input to public API format.
	pubInput := map[string]any{
		"title": input.Title,
	}
	if input.Message != "" {
		pubInput["message"] = input.Message
	}
	if input.Team != "" {
		pubInput["team_id"] = input.Team
	}
	if input.AlertGroupID != "" {
		pubInput["alert_group_id"] = input.AlertGroupID
	}
	if len(input.Users) > 0 {
		ids := make([]string, len(input.Users))
		for i, u := range input.Users {
			ids[i] = u.ID
		}
		pubInput["user_ids"] = ids
		if input.Users[0].Important {
			pubInput["important"] = true
		}
	}

	p, err := createResource[map[string]any, directEscalationResult](c, ctx, escalationsPath, pubInput, "direct escalation")
	if err != nil {
		return nil, err
	}
	return &oncalltypes.DirectPagingResult{AlertGroupID: p.AlertGroupID}, nil
}

// Compile-time check.
var _ oncalltypes.OnCallAPI = (*Client)(nil)
