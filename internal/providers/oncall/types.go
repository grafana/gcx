// Package oncall provides the OnCall provider for grafanactl.
package oncall

// ---------- ResourceIdentity implementations ----------
// All OnCall domain types use ID (string) as their resource identity.

func (x Integration) GetResourceName() string                   { return x.ID }
func (x *Integration) SetResourceName(name string)              { x.ID = name }
func (x EscalationChain) GetResourceName() string               { return x.ID }
func (x *EscalationChain) SetResourceName(name string)          { x.ID = name }
func (x EscalationPolicy) GetResourceName() string              { return x.ID }
func (x *EscalationPolicy) SetResourceName(name string)         { x.ID = name }
func (x Schedule) GetResourceName() string                      { return x.ID }
func (x *Schedule) SetResourceName(name string)                 { x.ID = name }
func (x Shift) GetResourceName() string                         { return x.ID }
func (x *Shift) SetResourceName(name string)                    { x.ID = name }
func (x Team) GetResourceName() string                          { return x.ID }
func (x *Team) SetResourceName(name string)                     { x.ID = name }
func (x IntegrationRoute) GetResourceName() string              { return x.ID }
func (x *IntegrationRoute) SetResourceName(name string)         { x.ID = name }
func (x OutgoingWebhook) GetResourceName() string               { return x.ID }
func (x *OutgoingWebhook) SetResourceName(name string)          { x.ID = name }
func (x AlertGroup) GetResourceName() string                    { return x.ID }
func (x *AlertGroup) SetResourceName(name string)               { x.ID = name }
func (x User) GetResourceName() string                          { return x.ID }
func (x *User) SetResourceName(name string)                     { x.ID = name }
func (x PersonalNotificationRule) GetResourceName() string      { return x.ID }
func (x *PersonalNotificationRule) SetResourceName(name string) { x.ID = name }
func (x UserGroup) GetResourceName() string                     { return x.ID }
func (x *UserGroup) SetResourceName(name string)                { x.ID = name }
func (x SlackChannel) GetResourceName() string                  { return x.ID }
func (x *SlackChannel) SetResourceName(name string)             { x.ID = name }
func (x Alert) GetResourceName() string                         { return x.ID }
func (x *Alert) SetResourceName(name string)                    { x.ID = name }
func (x ResolutionNote) GetResourceName() string                { return x.ID }
func (x *ResolutionNote) SetResourceName(name string)           { x.ID = name }
func (x ShiftSwap) GetResourceName() string                     { return x.ID }
func (x *ShiftSwap) SetResourceName(name string)                { x.ID = name }
func (x Organization) GetResourceName() string                  { return x.ID }
func (x *Organization) SetResourceName(name string)             { x.ID = name }

// Integration represents an OnCall integration.
//
//nolint:recvcheck
type Integration struct {
	ID                   string         `json:"id,omitempty"`
	Name                 string         `json:"name"`
	DescriptionShort     string         `json:"description_short,omitempty"`
	Link                 string         `json:"link,omitempty"`
	InboundEmail         string         `json:"inbound_email,omitempty"`
	Type                 string         `json:"type"`
	TeamID               string         `json:"team_id,omitempty"`
	MaintenanceMode      string         `json:"maintenance_mode,omitempty"`
	MaintenanceStartedAt string         `json:"maintenance_started_at,omitempty"`
	MaintenanceEndAt     string         `json:"maintenance_end_at,omitempty"`
	DefaultRoute         *Route         `json:"default_route,omitempty"`
	Templates            *Templates     `json:"templates,omitempty"`
	Heartbeat            *Heartbeat     `json:"heartbeat,omitempty"`
	Labels               []DynamicLabel `json:"labels,omitempty"`
	DynamicLabels        []DynamicLabel `json:"dynamic_labels,omitempty"`
}

// DynamicLabel represents a dynamic label for OnCall integrations.
type DynamicLabel struct {
	Key   LabelKey   `json:"key"`
	Value LabelValue `json:"value"`
}

// LabelKey represents the key object for a dynamic label.
type LabelKey struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// LabelValue represents the value object for a dynamic label.
type LabelValue struct {
	Name string `json:"name"`
}

// Heartbeat holds the heartbeat link for an integration.
type Heartbeat struct {
	Link string `json:"link,omitempty"`
}

// RouteSlack holds the Slack channel config for a route.
type RouteSlack struct {
	ChannelID string `json:"channel_id,omitempty"`
	Enabled   bool   `json:"enabled"`
}

// RouteChannel holds a generic channel config for a route.
type RouteChannel struct {
	ID      string `json:"id,omitempty"`
	Enabled bool   `json:"enabled"`
}

// Route represents a routing configuration.
type Route struct {
	ID                string        `json:"id,omitempty"`
	EscalationChainID string        `json:"escalation_chain_id,omitempty"`
	Slack             *RouteSlack   `json:"slack,omitempty"`
	Telegram          *RouteChannel `json:"telegram,omitempty"`
	MobileApp         *RouteChannel `json:"mobile_app,omitempty"`
	MobileAppCritical *RouteChannel `json:"mobile_app_critical,omitempty"`
	MSTeams           *RouteChannel `json:"msteams,omitempty"`
	Webhook           *RouteChannel `json:"webhook,omitempty"`
	Email             *RouteChannel `json:"email,omitempty"`
}

// TemplateChannel holds title, message, and image_url templates for a channel.
type TemplateChannel struct {
	Title    string `json:"title,omitempty"`
	Message  string `json:"message,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// TemplateTitleMessage holds title and message templates for a channel.
type TemplateTitleMessage struct {
	Title   string `json:"title,omitempty"`
	Message string `json:"message,omitempty"`
}

// TemplateTitleOnly holds only a title template for a channel.
type TemplateTitleOnly struct {
	Title string `json:"title,omitempty"`
}

// Templates represents message templates for OnCall integrations.
type Templates struct {
	GroupingKey       string                `json:"grouping_key,omitempty"`
	ResolveSignal     string                `json:"resolve_signal,omitempty"`
	AcknowledgeSignal string                `json:"acknowledge_signal,omitempty"`
	SourceLink        string                `json:"source_link,omitempty"`
	Slack             *TemplateChannel      `json:"slack,omitempty"`
	Web               *TemplateChannel      `json:"web,omitempty"`
	SMS               *TemplateTitleOnly    `json:"sms,omitempty"`
	PhoneCall         *TemplateTitleOnly    `json:"phone_call,omitempty"`
	Telegram          *TemplateChannel      `json:"telegram,omitempty"`
	MobileApp         *TemplateTitleMessage `json:"mobile_app,omitempty"`
	MSTeams           *TemplateChannel      `json:"msteams,omitempty"`
	WebhookTmpl       *TemplateChannel      `json:"webhook,omitempty"`
	Email             *TemplateTitleMessage `json:"email,omitempty"`
}

// EscalationChain represents an escalation chain.
//
//nolint:recvcheck
type EscalationChain struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	TeamID string `json:"team_id,omitempty"`
}

// EscalationPolicy represents an escalation policy step.
//
//nolint:recvcheck
type EscalationPolicy struct {
	ID                string   `json:"id,omitempty"`
	EscalationChainID string   `json:"escalation_chain_id"`
	Position          int      `json:"position"`
	Type              string   `json:"type"`
	Duration          int      `json:"duration,omitempty"`
	PersonsToNotify   []string `json:"persons_to_notify,omitempty"`
	NotifyOnCallFrom  string   `json:"notify_on_call_from_schedule,omitempty"`
	GroupsToNotify    []string `json:"groups_to_notify,omitempty"`
	ActionToTrigger   string   `json:"action_to_trigger,omitempty"`
	Important         bool     `json:"important,omitempty"`
}

// ScheduleSlack holds the Slack channel and user group linked to a schedule.
type ScheduleSlack struct {
	ChannelID   string `json:"channel_id,omitempty"`
	UserGroupID string `json:"user_group_id,omitempty"`
}

// Schedule represents an on-call schedule.
//
//nolint:recvcheck
type Schedule struct {
	ID                 string         `json:"id,omitempty"`
	Name               string         `json:"name"`
	Type               string         `json:"type"`
	TeamID             string         `json:"team_id,omitempty"`
	TimeZone           string         `json:"time_zone,omitempty"`
	Shifts             []string       `json:"shifts,omitempty"`
	OnCallNow          []string       `json:"on_call_now,omitempty"`
	ICalURL            string         `json:"ical_url,omitempty"`
	ICalURLOverrides   *string        `json:"ical_url_overrides,omitempty"`
	EnableWebOverrides bool           `json:"enable_web_overrides,omitempty"`
	Slack              *ScheduleSlack `json:"slack,omitempty"`
}

// Shift represents a shift in a schedule.
//
//nolint:recvcheck
type Shift struct {
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
}

// Team represents an OnCall team.
//
//nolint:recvcheck
type Team struct {
	ID        string `json:"id,omitempty"`
	GrafanaID int    `json:"grafana_id,omitempty"`
	Name      string `json:"name"`
	Email     string `json:"email,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// IntegrationRoute represents a routing rule for an integration.
//
//nolint:recvcheck
type IntegrationRoute struct {
	ID                string         `json:"id,omitempty"`
	IntegrationID     string         `json:"integration_id"`
	EscalationChainID string         `json:"escalation_chain_id,omitempty"`
	RoutingRegex      string         `json:"routing_regex,omitempty"`
	RoutingType       string         `json:"routing_type,omitempty"`
	Position          int            `json:"position"`
	IsTheLastRoute    bool           `json:"is_the_last_route,omitempty"`
	SlackChannel      map[string]any `json:"slack,omitempty"`
	TelegramChannel   map[string]any `json:"telegram,omitempty"`
	MSTeamsChannel    map[string]any `json:"msteams,omitempty"`
}

// OutgoingWebhook represents an OnCall outgoing webhook.
//
//nolint:recvcheck
type OutgoingWebhook struct {
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

// AlertGroup represents an OnCall alert group.
//
//nolint:recvcheck
type AlertGroup struct {
	ID             string `json:"id,omitempty"`
	Title          string `json:"title,omitempty"`
	WebTitle       string `json:"web_title,omitempty"`
	State          string `json:"state,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	ResolvedAt     string `json:"resolved_at,omitempty"`
	AcknowledgedAt string `json:"acknowledged_at,omitempty"`
	AlertsCount    int    `json:"alerts_count,omitempty"`
	IntegrationID  string `json:"integration_id,omitempty"`
	TeamID         string `json:"team_id,omitempty"`
	RelatedUsers   []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"related_users,omitempty"`
	Labels []struct {
		Key struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"key"`
		Value struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"value"`
	} `json:"labels,omitempty"`
}

// SlackInfo holds Slack identifiers for an OnCall user.
type SlackInfo struct {
	UserID string `json:"user_id,omitempty"`
	TeamID string `json:"team_id,omitempty"`
}

// User represents an OnCall user.
//
//nolint:recvcheck
type User struct {
	ID                    string     `json:"id"`
	GrafanaID             int        `json:"grafana_id,omitempty"`
	Username              string     `json:"username"`
	Email                 string     `json:"email,omitempty"`
	Name                  string     `json:"name,omitempty"`
	Role                  string     `json:"role,omitempty"`
	AvatarURL             string     `json:"avatar_url,omitempty"`
	Slack                 *SlackInfo `json:"slack,omitempty"`
	IsPhoneNumberVerified bool       `json:"is_phone_number_verified,omitempty"`
	Timezone              string     `json:"timezone,omitempty"`
	Teams                 []string   `json:"teams,omitempty"`
}

// PersonalNotificationRule represents a personal notification rule.
//
//nolint:recvcheck
type PersonalNotificationRule struct {
	ID                    string `json:"id,omitempty"`
	UserID                string `json:"user_id,omitempty"`
	Step                  int    `json:"step,omitempty"`
	Type                  string `json:"type,omitempty"`
	Duration              int    `json:"duration,omitempty"`
	NotificationChannelID string `json:"notification_channel_id,omitempty"`
}

// UserGroup represents an OnCall user group (e.g. Slack user group).
//
//nolint:recvcheck
type UserGroup struct {
	ID     string `json:"id,omitempty"`
	Type   string `json:"type,omitempty"`
	Name   string `json:"name,omitempty"`
	Handle string `json:"handle,omitempty"`
}

// SlackChannel represents a Slack channel.
//
//nolint:recvcheck
type SlackChannel struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	SlackID string `json:"slack_id,omitempty"`
}

// Alert represents an individual alert within an alert group.
//
//nolint:recvcheck
type Alert struct {
	ID           string `json:"id,omitempty"`
	AlertGroupID string `json:"alert_group_id,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	Link         string `json:"link,omitempty"`
	Title        string `json:"title,omitempty"`
}

// ResolutionNote represents a resolution note for an alert group.
//
//nolint:recvcheck
type ResolutionNote struct {
	ID           string  `json:"id,omitempty"`
	AlertGroupID string  `json:"alert_group_id,omitempty"`
	Author       *string `json:"author,omitempty"`
	Source       string  `json:"source,omitempty"`
	CreatedAt    string  `json:"created_at,omitempty"`
	Text         string  `json:"text,omitempty"`
}

// CreateResolutionNoteInput is the input for creating a resolution note.
type CreateResolutionNoteInput struct {
	AlertGroupID string `json:"alert_group_id"`
	Text         string `json:"text"`
}

// UpdateResolutionNoteInput is the input for updating a resolution note.
type UpdateResolutionNoteInput struct {
	Text string `json:"text"`
}

// ShiftSwap represents a shift swap request.
//
//nolint:recvcheck
type ShiftSwap struct {
	ID          string  `json:"id,omitempty"`
	Schedule    string  `json:"schedule,omitempty"`
	SwapStart   string  `json:"swap_start,omitempty"`
	SwapEnd     string  `json:"swap_end,omitempty"`
	Beneficiary string  `json:"beneficiary,omitempty"`
	Benefactor  *string `json:"benefactor,omitempty"`
	Status      string  `json:"status,omitempty"`
	CreatedAt   string  `json:"created_at,omitempty"`
}

// CreateShiftSwapInput is the input for creating a shift swap.
type CreateShiftSwapInput struct {
	Schedule    string `json:"schedule"`
	SwapStart   string `json:"swap_start"`
	SwapEnd     string `json:"swap_end"`
	Beneficiary string `json:"beneficiary"`
}

// UpdateShiftSwapInput is the input for updating a shift swap.
type UpdateShiftSwapInput struct {
	SwapStart string `json:"swap_start,omitempty"`
	SwapEnd   string `json:"swap_end,omitempty"`
}

// TakeShiftSwapInput is the input for taking a shift swap.
type TakeShiftSwapInput struct {
	Benefactor string `json:"benefactor"`
}

// DirectEscalationInput is the input for creating a direct escalation (paging).
type DirectEscalationInput struct {
	Title        string   `json:"title"`
	Message      string   `json:"message,omitempty"`
	TeamID       string   `json:"team_id,omitempty"`
	UserIDs      []string `json:"user_ids,omitempty"`
	AlertGroupID string   `json:"alert_group_id,omitempty"`
	Important    bool     `json:"important,omitempty"`
}

// DirectEscalationResult is the result of a direct escalation.
type DirectEscalationResult struct {
	ID           string `json:"id,omitempty"`
	AlertGroupID string `json:"alert_group_id,omitempty"`
}

// Organization represents a Grafana OnCall organization.
//
//nolint:recvcheck
type Organization struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Slug         string `json:"slug,omitempty"`
	GlobalSlug   string `json:"global_slug,omitempty"`
	ContactEmail string `json:"contact_email,omitempty"`
}

// FinalShift represents a resolved on-call shift period for a specific user.
type FinalShift struct {
	UserPK       string `json:"user_pk"`
	UserEmail    string `json:"user_email"`
	UserUsername string `json:"user_username"`
	ShiftStart   string `json:"shift_start"`
	ShiftEnd     string `json:"shift_end"`
}

// ShiftRequest represents a request to create/update a shift.
type ShiftRequest struct {
	Name                       string     `json:"name"`
	Type                       string     `json:"type"`
	TeamID                     string     `json:"team_id,omitempty"`
	TimeZone                   string     `json:"time_zone,omitempty"`
	Start                      string     `json:"start,omitempty"`
	Duration                   int        `json:"duration,omitempty"`
	Users                      []string   `json:"users,omitempty"`
	RollingUsers               [][]string `json:"rolling_users,omitempty"`
	Frequency                  string     `json:"frequency,omitempty"`
	Interval                   int        `json:"interval,omitempty"`
	ByDay                      []string   `json:"by_day,omitempty"`
	WeekStart                  string     `json:"week_start,omitempty"`
	StartRotationFromUserIndex *int       `json:"start_rotation_from_user_index,omitempty"`
}

// paginatedResponse is the generic paginated response from the OnCall API.
type paginatedResponse[T any] struct {
	Results []T     `json:"results"`
	Next    *string `json:"next"`
}
