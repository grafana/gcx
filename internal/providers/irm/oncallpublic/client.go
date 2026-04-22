// Package oncallpublic provides a client for the public API of the OnCall
// backend. We only need this right now because the IRM plugin proxy
// rejects SA tokens for all requests, but work is in progress to change that.
// Once the IRM plugin proxy supports SA tokens, we can rip out this package.
package oncallpublic

import (
	"bytes"
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
	"time"

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
		return "", errors.New("OnCall API URL not found in plugin settings")
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

func iterResources[T any](ctx context.Context, c *Client, path, resourceType string) iter.Seq2[T, error] {
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

			var result paginatedResponse[T]
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				var z T
				yield(z, fmt.Errorf("irm: decode %s: %w", resourceType, err))
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
				yield(z, fmt.Errorf("irm: invalid pagination URL %q: %w", *result.Next, parseErr))
				return
			}
			baseURL, _ := url.Parse(c.oncallURL)
			if nextURL.Host != "" && nextURL.Host != baseURL.Host {
				var z T
				yield(z, fmt.Errorf("irm: pagination URL host %q does not match base URL host %q", nextURL.Host, baseURL.Host))
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
	return collectN(it, 0)
}

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

func getResource[T any](ctx context.Context, c *Client, basePath, id, resourceType string) (*T, error) {
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

func createResource[In any, Out any](ctx context.Context, c *Client, path string, body In, resourceType string) (*Out, error) {
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

func updateResource[In any, Out any](ctx context.Context, c *Client, basePath, id string, body In, resourceType string) (*Out, error) {
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

func deleteResource(ctx context.Context, c *Client, basePath, id, resourceType string) error {
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

func alertGroupAction(ctx context.Context, c *Client, id, action string) error {
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

func pathWithParams(base string, params url.Values) string {
	if len(params) > 0 {
		return base + "?" + params.Encode()
	}
	return base
}

// --- OnCallAPI implementation ---

func (c *Client) ListIntegrations(ctx context.Context) ([]oncalltypes.Integration, error) {
	items, err := collectAll(iterResources[integration](ctx, c, integrationsPath, "integration"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptIntegration), nil
}

func (c *Client) GetIntegration(ctx context.Context, id string) (*oncalltypes.Integration, error) {
	p, err := getResource[integration](ctx, c, integrationsPath, id, "integration")
	if err != nil {
		return nil, err
	}
	r := adaptIntegration(*p)
	return &r, nil
}

func (c *Client) CreateIntegration(ctx context.Context, i oncalltypes.Integration) (*oncalltypes.Integration, error) {
	p, err := createResource[oncalltypes.Integration, integration](ctx, c, integrationsPath, i, "integration")
	if err != nil {
		return nil, err
	}
	r := adaptIntegration(*p)
	return &r, nil
}

func (c *Client) UpdateIntegration(ctx context.Context, id string, i oncalltypes.Integration) (*oncalltypes.Integration, error) {
	p, err := updateResource[oncalltypes.Integration, integration](ctx, c, integrationsPath, id, i, "integration")
	if err != nil {
		return nil, err
	}
	r := adaptIntegration(*p)
	return &r, nil
}

func (c *Client) DeleteIntegration(ctx context.Context, id string) error {
	return deleteResource(ctx, c, integrationsPath, id, "integration")
}

func (c *Client) ListEscalationChains(ctx context.Context) ([]oncalltypes.EscalationChain, error) {
	items, err := collectAll(iterResources[escalationChain](ctx, c, escalationChainsPath, "escalation chain"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptEscalationChain), nil
}

func (c *Client) GetEscalationChain(ctx context.Context, id string) (*oncalltypes.EscalationChain, error) {
	p, err := getResource[escalationChain](ctx, c, escalationChainsPath, id, "escalation chain")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationChain(*p)
	return &r, nil
}

func (c *Client) CreateEscalationChain(ctx context.Context, ec oncalltypes.EscalationChain) (*oncalltypes.EscalationChain, error) {
	p, err := createResource[oncalltypes.EscalationChain, escalationChain](ctx, c, escalationChainsPath, ec, "escalation chain")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationChain(*p)
	return &r, nil
}

func (c *Client) UpdateEscalationChain(ctx context.Context, id string, ec oncalltypes.EscalationChain) (*oncalltypes.EscalationChain, error) {
	p, err := updateResource[oncalltypes.EscalationChain, escalationChain](ctx, c, escalationChainsPath, id, ec, "escalation chain")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationChain(*p)
	return &r, nil
}

func (c *Client) DeleteEscalationChain(ctx context.Context, id string) error {
	return deleteResource(ctx, c, escalationChainsPath, id, "escalation chain")
}

func (c *Client) ListEscalationPolicies(ctx context.Context, chainID string) ([]oncalltypes.EscalationPolicy, error) {
	params := url.Values{}
	if chainID != "" {
		params.Set("escalation_chain_id", chainID)
	}
	items, err := collectAll(iterResources[escalationPolicy](ctx, c, pathWithParams(escalationPoliciesPath, params), "escalation policy"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptEscalationPolicy), nil
}

func (c *Client) GetEscalationPolicy(ctx context.Context, id string) (*oncalltypes.EscalationPolicy, error) {
	p, err := getResource[escalationPolicy](ctx, c, escalationPoliciesPath, id, "escalation policy")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationPolicy(*p)
	return &r, nil
}

func (c *Client) CreateEscalationPolicy(ctx context.Context, ep oncalltypes.EscalationPolicy) (*oncalltypes.EscalationPolicy, error) {
	p, err := createResource[oncalltypes.EscalationPolicy, escalationPolicy](ctx, c, escalationPoliciesPath, ep, "escalation policy")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationPolicy(*p)
	return &r, nil
}

func (c *Client) UpdateEscalationPolicy(ctx context.Context, id string, ep oncalltypes.EscalationPolicy) (*oncalltypes.EscalationPolicy, error) {
	p, err := updateResource[oncalltypes.EscalationPolicy, escalationPolicy](ctx, c, escalationPoliciesPath, id, ep, "escalation policy")
	if err != nil {
		return nil, err
	}
	r := adaptEscalationPolicy(*p)
	return &r, nil
}

func (c *Client) DeleteEscalationPolicy(ctx context.Context, id string) error {
	return deleteResource(ctx, c, escalationPoliciesPath, id, "escalation policy")
}

func (c *Client) ListSchedules(ctx context.Context) ([]oncalltypes.Schedule, error) {
	items, err := collectAll(iterResources[schedule](ctx, c, schedulesPath, "schedule"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptSchedule), nil
}

func (c *Client) GetSchedule(ctx context.Context, id string) (*oncalltypes.Schedule, error) {
	p, err := getResource[schedule](ctx, c, schedulesPath, id, "schedule")
	if err != nil {
		return nil, err
	}
	r := adaptSchedule(*p)
	return &r, nil
}

func (c *Client) CreateSchedule(ctx context.Context, s oncalltypes.Schedule) (*oncalltypes.Schedule, error) {
	p, err := createResource[oncalltypes.Schedule, schedule](ctx, c, schedulesPath, s, "schedule")
	if err != nil {
		return nil, err
	}
	r := adaptSchedule(*p)
	return &r, nil
}

func (c *Client) UpdateSchedule(ctx context.Context, id string, s oncalltypes.Schedule) (*oncalltypes.Schedule, error) {
	p, err := updateResource[oncalltypes.Schedule, schedule](ctx, c, schedulesPath, id, s, "schedule")
	if err != nil {
		return nil, err
	}
	r := adaptSchedule(*p)
	return &r, nil
}

func (c *Client) DeleteSchedule(ctx context.Context, id string) error {
	return deleteResource(ctx, c, schedulesPath, id, "schedule")
}

func (c *Client) ListFilterEvents(ctx context.Context, scheduleID, userTZ, startingDate string, days int) (*oncalltypes.FilterEventsResponse, error) {
	// The public API uses final_shifts, not filter_events.
	// Fetch and convert to the FilterEventsResponse shape.
	start, err := time.Parse("2006-01-02", startingDate)
	if err != nil {
		return nil, fmt.Errorf("irm: parse start date: %w", err)
	}
	endDate := start.AddDate(0, 0, days-1).Format("2006-01-02")

	params := url.Values{}
	params.Set("start_date", startingDate)
	params.Set("end_date", endDate)
	path := fmt.Sprintf("%s%s/final_shifts/?%s", schedulesPath, url.PathEscape(scheduleID), params.Encode())
	items, err := collectAll(iterResources[finalShift](ctx, c, path, "final shift"))
	if err != nil {
		return nil, err
	}
	events := make([]oncalltypes.FinalShiftEvent, 0, len(items))
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
	items, err := collectAll(iterResources[shift](ctx, c, shiftsPath, "shift"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptShift), nil
}

func (c *Client) GetShift(ctx context.Context, id string) (*oncalltypes.Shift, error) {
	p, err := getResource[shift](ctx, c, shiftsPath, id, "shift")
	if err != nil {
		return nil, err
	}
	r := adaptShift(*p)
	return &r, nil
}

func (c *Client) CreateShift(ctx context.Context, s oncalltypes.ShiftRequest) (*oncalltypes.Shift, error) {
	p, err := createResource[oncalltypes.ShiftRequest, shift](ctx, c, shiftsPath, s, "shift")
	if err != nil {
		return nil, err
	}
	r := adaptShift(*p)
	return &r, nil
}

func (c *Client) UpdateShift(ctx context.Context, id string, s oncalltypes.ShiftRequest) (*oncalltypes.Shift, error) {
	p, err := updateResource[oncalltypes.ShiftRequest, shift](ctx, c, shiftsPath, id, s, "shift")
	if err != nil {
		return nil, err
	}
	r := adaptShift(*p)
	return &r, nil
}

func (c *Client) DeleteShift(ctx context.Context, id string) error {
	return deleteResource(ctx, c, shiftsPath, id, "shift")
}

func (c *Client) ListRoutes(ctx context.Context, integrationID string) ([]oncalltypes.Route, error) {
	params := url.Values{}
	if integrationID != "" {
		params.Set("integration_id", integrationID)
	}
	items, err := collectAll(iterResources[route](ctx, c, pathWithParams(routesPath, params), "route"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptRoute), nil
}

func (c *Client) GetRoute(ctx context.Context, id string) (*oncalltypes.Route, error) {
	p, err := getResource[route](ctx, c, routesPath, id, "route")
	if err != nil {
		return nil, err
	}
	r := adaptRoute(*p)
	return &r, nil
}

func (c *Client) CreateRoute(ctx context.Context, rt oncalltypes.Route) (*oncalltypes.Route, error) {
	p, err := createResource[oncalltypes.Route, route](ctx, c, routesPath, rt, "route")
	if err != nil {
		return nil, err
	}
	r := adaptRoute(*p)
	return &r, nil
}

func (c *Client) UpdateRoute(ctx context.Context, id string, rt oncalltypes.Route) (*oncalltypes.Route, error) {
	p, err := updateResource[oncalltypes.Route, route](ctx, c, routesPath, id, rt, "route")
	if err != nil {
		return nil, err
	}
	r := adaptRoute(*p)
	return &r, nil
}

func (c *Client) DeleteRoute(ctx context.Context, id string) error {
	return deleteResource(ctx, c, routesPath, id, "route")
}

func (c *Client) ListWebhooks(ctx context.Context) ([]oncalltypes.Webhook, error) {
	items, err := collectAll(iterResources[webhook](ctx, c, webhooksPath, "webhook"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptWebhook), nil
}

func (c *Client) GetWebhook(ctx context.Context, id string) (*oncalltypes.Webhook, error) {
	p, err := getResource[webhook](ctx, c, webhooksPath, id, "webhook")
	if err != nil {
		return nil, err
	}
	r := adaptWebhook(*p)
	return &r, nil
}

func (c *Client) CreateWebhook(ctx context.Context, w oncalltypes.Webhook) (*oncalltypes.Webhook, error) {
	p, err := createResource[oncalltypes.Webhook, webhook](ctx, c, webhooksPath, w, "webhook")
	if err != nil {
		return nil, err
	}
	r := adaptWebhook(*p)
	return &r, nil
}

func (c *Client) UpdateWebhook(ctx context.Context, id string, w oncalltypes.Webhook) (*oncalltypes.Webhook, error) {
	p, err := updateResource[oncalltypes.Webhook, webhook](ctx, c, webhooksPath, id, w, "webhook")
	if err != nil {
		return nil, err
	}
	r := adaptWebhook(*p)
	return &r, nil
}

func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	return deleteResource(ctx, c, webhooksPath, id, "webhook")
}

func (c *Client) ListAlertGroups(ctx context.Context, opts ...oncalltypes.ListOption) ([]oncalltypes.AlertGroup, error) {
	cfg := oncalltypes.ApplyListOpts(opts)
	params := url.Values{}
	if cfg.StartedAfter != nil {
		const layout = "2006-01-02T15:04:05"
		start := cfg.StartedAfter.UTC().Format(layout)
		end := time.Now().UTC().Format(layout)
		params.Set("started_at", start+"_"+end)
	}
	items, err := collectN(iterResources[alertGroup](ctx, c, pathWithParams(alertGroupsPath, params), "alert group"), cfg.Limit)
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptAlertGroup), nil
}

func (c *Client) GetAlertGroup(ctx context.Context, id string) (*oncalltypes.AlertGroup, error) {
	p, err := getResource[alertGroup](ctx, c, alertGroupsPath, id, "alert group")
	if err != nil {
		return nil, err
	}
	r := adaptAlertGroup(*p)
	return &r, nil
}

func (c *Client) DeleteAlertGroup(ctx context.Context, id string) error {
	return deleteResource(ctx, c, alertGroupsPath, id, "alert group")
}

func (c *Client) AcknowledgeAlertGroup(ctx context.Context, id string) error {
	return alertGroupAction(ctx, c, id, "acknowledge")
}

func (c *Client) ResolveAlertGroup(ctx context.Context, id string) error {
	return alertGroupAction(ctx, c, id, "resolve")
}

func (c *Client) SilenceAlertGroup(ctx context.Context, id string, delaySecs int) error {
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

func (c *Client) UnacknowledgeAlertGroup(ctx context.Context, id string) error {
	return alertGroupAction(ctx, c, id, "unacknowledge")
}

func (c *Client) UnresolveAlertGroup(ctx context.Context, id string) error {
	return alertGroupAction(ctx, c, id, "unresolve")
}

func (c *Client) UnsilenceAlertGroup(ctx context.Context, id string) error {
	return alertGroupAction(ctx, c, id, "unsilence")
}

func (c *Client) ListUsers(ctx context.Context) ([]oncalltypes.User, error) {
	items, err := collectAll(iterResources[user](ctx, c, usersPath, "user"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptUser), nil
}

func (c *Client) GetUser(ctx context.Context, id string) (*oncalltypes.User, error) {
	p, err := getResource[user](ctx, c, usersPath, id, "user")
	if err != nil {
		return nil, err
	}
	r := adaptUser(*p)
	return &r, nil
}

func (c *Client) GetCurrentUser(ctx context.Context) (*oncalltypes.User, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, usersPath+"current/", nil)
	if err != nil {
		return nil, fmt.Errorf("irm: get current user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}
	var u user
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("irm: decode current user: %w", err)
	}
	r := adaptUser(u)
	return &r, nil
}

func (c *Client) ListTeams(ctx context.Context) ([]oncalltypes.Team, error) {
	items, err := collectAll(iterResources[team](ctx, c, teamsPath, "team"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptTeam), nil
}

func (c *Client) GetTeam(ctx context.Context, id string) (*oncalltypes.Team, error) {
	p, err := getResource[team](ctx, c, teamsPath, id, "team")
	if err != nil {
		return nil, err
	}
	r := adaptTeam(*p)
	return &r, nil
}

func (c *Client) ListUserGroups(ctx context.Context) ([]oncalltypes.UserGroup, error) {
	items, err := collectAll(iterResources[userGroup](ctx, c, userGroupsPath, "user group"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptUserGroup), nil
}

func (c *Client) ListSlackChannels(ctx context.Context) ([]oncalltypes.SlackChannel, error) {
	items, err := collectAll(iterResources[slackChannel](ctx, c, slackChannelsPath, "slack channel"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptSlackChannel), nil
}

func (c *Client) ListAlerts(ctx context.Context, alertGroupID string, opts ...oncalltypes.ListOption) ([]oncalltypes.Alert, error) {
	params := url.Values{}
	if alertGroupID != "" {
		params.Set("alert_group_id", alertGroupID)
	}
	cfg := oncalltypes.ApplyListOpts(opts)
	items, err := collectN(iterResources[alert](ctx, c, pathWithParams(alertsPath, params), "alert"), cfg.Limit)
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptAlert), nil
}

func (c *Client) GetAlert(ctx context.Context, id string) (*oncalltypes.Alert, error) {
	p, err := getResource[alert](ctx, c, alertsPath, id, "alert")
	if err != nil {
		return nil, err
	}
	r := adaptAlert(*p)
	return &r, nil
}

func (c *Client) GetOrganization(ctx context.Context) (*oncalltypes.Organization, error) {
	// The public API has a list endpoint; get the first org.
	items, err := collectAll(iterResources[organization](ctx, c, organizationsPath, "organization"))
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("irm: no organizations found")
	}
	r := adaptOrganization(items[0])
	return &r, nil
}

func (c *Client) ListResolutionNotes(ctx context.Context, alertGroupID string) ([]oncalltypes.ResolutionNote, error) {
	params := url.Values{}
	if alertGroupID != "" {
		params.Set("alert_group_id", alertGroupID)
	}
	items, err := collectAll(iterResources[resolutionNote](ctx, c, pathWithParams(resolutionNotesPath, params), "resolution note"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptResolutionNote), nil
}

func (c *Client) GetResolutionNote(ctx context.Context, id string) (*oncalltypes.ResolutionNote, error) {
	p, err := getResource[resolutionNote](ctx, c, resolutionNotesPath, id, "resolution note")
	if err != nil {
		return nil, err
	}
	r := adaptResolutionNote(*p)
	return &r, nil
}

func (c *Client) CreateResolutionNote(ctx context.Context, input oncalltypes.CreateResolutionNoteInput) (*oncalltypes.ResolutionNote, error) {
	p, err := createResource[oncalltypes.CreateResolutionNoteInput, resolutionNote](ctx, c, resolutionNotesPath, input, "resolution note")
	if err != nil {
		return nil, err
	}
	r := adaptResolutionNote(*p)
	return &r, nil
}

func (c *Client) UpdateResolutionNote(ctx context.Context, id string, input oncalltypes.UpdateResolutionNoteInput) (*oncalltypes.ResolutionNote, error) {
	p, err := updateResource[oncalltypes.UpdateResolutionNoteInput, resolutionNote](ctx, c, resolutionNotesPath, id, input, "resolution note")
	if err != nil {
		return nil, err
	}
	r := adaptResolutionNote(*p)
	return &r, nil
}

func (c *Client) DeleteResolutionNote(ctx context.Context, id string) error {
	return deleteResource(ctx, c, resolutionNotesPath, id, "resolution note")
}

func (c *Client) ListShiftSwaps(ctx context.Context) ([]oncalltypes.ShiftSwap, error) {
	items, err := collectAll(iterResources[shiftSwap](ctx, c, shiftSwapsPath, "shift swap"))
	if err != nil {
		return nil, err
	}
	return adaptSlice(items, adaptShiftSwap), nil
}

func (c *Client) GetShiftSwap(ctx context.Context, id string) (*oncalltypes.ShiftSwap, error) {
	p, err := getResource[shiftSwap](ctx, c, shiftSwapsPath, id, "shift swap")
	if err != nil {
		return nil, err
	}
	r := adaptShiftSwap(*p)
	return &r, nil
}

func (c *Client) CreateShiftSwap(ctx context.Context, input oncalltypes.CreateShiftSwapInput) (*oncalltypes.ShiftSwap, error) {
	p, err := createResource[oncalltypes.CreateShiftSwapInput, shiftSwap](ctx, c, shiftSwapsPath, input, "shift swap")
	if err != nil {
		return nil, err
	}
	r := adaptShiftSwap(*p)
	return &r, nil
}

func (c *Client) UpdateShiftSwap(ctx context.Context, id string, input oncalltypes.UpdateShiftSwapInput) (*oncalltypes.ShiftSwap, error) {
	p, err := updateResource[oncalltypes.UpdateShiftSwapInput, shiftSwap](ctx, c, shiftSwapsPath, id, input, "shift swap")
	if err != nil {
		return nil, err
	}
	r := adaptShiftSwap(*p)
	return &r, nil
}

func (c *Client) DeleteShiftSwap(ctx context.Context, id string) error {
	return deleteResource(ctx, c, shiftSwapsPath, id, "shift swap")
}

func (c *Client) TakeShiftSwap(ctx context.Context, id string, input oncalltypes.TakeShiftSwapInput) (*oncalltypes.ShiftSwap, error) {
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
	var p shiftSwap
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("irm: decode shift swap: %w", err)
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
		important := false
		for i, u := range input.Users {
			ids[i] = u.ID
			if u.Important {
				important = true
			}
		}
		pubInput["user_ids"] = ids
		if important {
			pubInput["important"] = true
		}
	}

	p, err := createResource[map[string]any, directEscalationResult](ctx, c, escalationsPath, pubInput, "direct escalation")
	if err != nil {
		return nil, err
	}
	return &oncalltypes.DirectPagingResult{AlertGroupID: p.AlertGroupID}, nil
}

// Compile-time check.
var _ oncalltypes.OnCallAPI = (*Client)(nil)
