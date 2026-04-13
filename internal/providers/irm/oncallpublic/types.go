// Package oncallpublic provides a client for the OnCall public API (api/v1/).
// It adapts responses to the internal API type shape used by the irm package.
// This client is used for SA token auth where the IRM plugin proxy rejects
// service account requests.
//
// This package is intended to be temporary — it should be removed when the
// OnCall backend allows SA tokens through PluginAuthentication.
package oncallpublic

type integration struct {
	ID               string `json:"id,omitempty"`
	Name             string `json:"name"`
	DescriptionShort string `json:"description_short,omitempty"`
	Link             string `json:"link,omitempty"`
	InboundEmail     string `json:"inbound_email,omitempty"`
	Type             string `json:"type"`
	TeamID           string `json:"team_id,omitempty"`
	MaintenanceMode  any    `json:"maintenance_mode,omitempty"`
	Labels           any    `json:"labels,omitempty"`
}

type escalationChain struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	TeamID string `json:"team_id,omitempty"`
}

type escalationPolicy struct {
	ID                      string   `json:"id,omitempty"`
	EscalationChainID       string   `json:"escalation_chain_id"`
	Position                int      `json:"position"`
	Type                    string   `json:"type"`
	Duration                any      `json:"duration,omitempty"`
	PersonsToNotify         []string `json:"persons_to_notify,omitempty"`
	NotifyOnCallFromSchedule string  `json:"notify_on_call_from_schedule,omitempty"`
	GroupsToNotify          []string `json:"groups_to_notify,omitempty"`
	ActionToTrigger         string   `json:"action_to_trigger,omitempty"`
	Important               bool     `json:"important,omitempty"`
	Severity                string   `json:"severity,omitempty"`
}

type schedule struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	TeamID   string `json:"team_id,omitempty"`
	TimeZone string `json:"time_zone,omitempty"`
	OnCallNow any   `json:"on_call_now,omitempty"`
	Slack     any    `json:"slack,omitempty"`
}

type shift struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	TeamID    string   `json:"team_id,omitempty"`
	Start     string   `json:"start,omitempty"`
	Duration  int      `json:"duration,omitempty"`
	Users     []string `json:"users,omitempty"`
	Frequency string   `json:"frequency,omitempty"`
	Interval  int      `json:"interval,omitempty"`
	ByDay     []string `json:"by_day,omitempty"`
	Level     int      `json:"level,omitempty"`
}

type route struct {
	ID                string `json:"id,omitempty"`
	IntegrationID     string `json:"integration_id"`
	EscalationChainID string `json:"escalation_chain_id,omitempty"`
	RoutingRegex      string `json:"routing_regex,omitempty"`
	RoutingType       string `json:"routing_type,omitempty"`
	Position          int    `json:"position"`
	IsTheLastRoute    bool   `json:"is_the_last_route,omitempty"`
}

type webhook struct {
	ID                  string   `json:"id,omitempty"`
	Name                string   `json:"name"`
	URL                 string   `json:"url,omitempty"`
	HTTPMethod          string   `json:"http_method,omitempty"`
	TriggerType         string   `json:"trigger_type"`
	IsWebhookEnabled    bool     `json:"is_webhook_enabled"`
	TeamID              string   `json:"team_id,omitempty"`
	Data                string   `json:"data,omitempty"`
	Username            string   `json:"username,omitempty"`
	Password            string   `json:"password,omitempty"`
	AuthorizationHeader string   `json:"authorization_header,omitempty"`
	Headers             string   `json:"headers,omitempty"`
	TriggerTemplate     string   `json:"trigger_template,omitempty"`
	IntegrationFilter   []string `json:"integration_filter,omitempty"`
	ForwardAll          bool     `json:"forward_all,omitempty"`
	Preset              string   `json:"preset,omitempty"`
}

type alertGroup struct {
	ID             string `json:"id,omitempty"`
	Title          string `json:"title,omitempty"`
	State          string `json:"state,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	ResolvedAt     string `json:"resolved_at,omitempty"`
	AcknowledgedAt string `json:"acknowledged_at,omitempty"`
	SilencedAt     string `json:"silenced_at,omitempty"`
	AlertsCount    int    `json:"alerts_count,omitempty"`
	IntegrationID  string `json:"integration_id,omitempty"`
	TeamID         string `json:"team_id,omitempty"`
	Labels         any    `json:"labels,omitempty"`
	Permalinks     any    `json:"permalinks,omitempty"`
}

type user struct {
	ID        string `json:"id"`
	GrafanaID int    `json:"grafana_id,omitempty"`
	Username  string `json:"username"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name,omitempty"`
	Role      string `json:"role,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Slack     any    `json:"slack,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
	Teams     any    `json:"teams,omitempty"`
}

type team struct {
	ID        string `json:"id,omitempty"`
	GrafanaID int    `json:"grafana_id,omitempty"`
	Name      string `json:"name"`
	Email     string `json:"email,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type userGroup struct {
	ID     string `json:"id,omitempty"`
	Type   string `json:"type,omitempty"`
	Name   string `json:"name,omitempty"`
	Handle string `json:"handle,omitempty"`
}

type slackChannel struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	SlackID string `json:"slack_id,omitempty"`
}

type alert struct {
	ID           string `json:"id,omitempty"`
	AlertGroupID string `json:"alert_group_id,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	Link         string `json:"link,omitempty"`
	Title        string `json:"title,omitempty"`
}

type organization struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Slug         string `json:"slug,omitempty"`
	ContactEmail string `json:"contact_email,omitempty"`
}

type resolutionNote struct {
	ID           string  `json:"id,omitempty"`
	AlertGroupID string  `json:"alert_group_id,omitempty"`
	Author       *string `json:"author,omitempty"`
	Source       string  `json:"source,omitempty"`
	CreatedAt    string  `json:"created_at,omitempty"`
	Text         string  `json:"text,omitempty"`
}

type shiftSwap struct {
	ID          string  `json:"id,omitempty"`
	Schedule    string  `json:"schedule,omitempty"`
	SwapStart   string  `json:"swap_start,omitempty"`
	SwapEnd     string  `json:"swap_end,omitempty"`
	Beneficiary string  `json:"beneficiary,omitempty"`
	Benefactor  *string `json:"benefactor,omitempty"`
	Status      string  `json:"status,omitempty"`
	CreatedAt   string  `json:"created_at,omitempty"`
}

type finalShift struct {
	UserPK       string `json:"user_pk"`
	UserEmail    string `json:"user_email"`
	UserUsername string `json:"user_username"`
	ShiftStart   string `json:"shift_start"`
	ShiftEnd     string `json:"shift_end"`
}

type directEscalationResult struct {
	ID           string `json:"id,omitempty"`
	AlertGroupID string `json:"alert_group_id,omitempty"`
}

type paginatedResponse[T any] struct {
	Results []T     `json:"results"`
	Next    *string `json:"next"`
}
