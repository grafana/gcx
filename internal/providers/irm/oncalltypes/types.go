// package oncalltypes defines types for the oncall provider. It is only separate
// from the irm package to allow us to put the temporary oncallpublic package in
// a separate package: we needed to avoid an import cycle. When we rip out the
// oncallpublic package, we can put these types back in the irm package.
package oncalltypes

func (x Integration) GetResourceName() string           { return x.ID }
func (x *Integration) SetResourceName(name string)      { x.ID = name }
func (x EscalationChain) GetResourceName() string       { return x.ID }
func (x *EscalationChain) SetResourceName(name string)  { x.ID = name }
func (x EscalationPolicy) GetResourceName() string      { return x.ID }
func (x *EscalationPolicy) SetResourceName(name string) { x.ID = name }
func (x Schedule) GetResourceName() string              { return x.ID }
func (x *Schedule) SetResourceName(name string)         { x.ID = name }
func (x Shift) GetResourceName() string                 { return x.ID }
func (x *Shift) SetResourceName(name string)            { x.ID = name }
func (x Team) GetResourceName() string                  { return x.ID }
func (x *Team) SetResourceName(name string)             { x.ID = name }
func (x Route) GetResourceName() string                 { return x.ID }
func (x *Route) SetResourceName(name string)            { x.ID = name }
func (x Webhook) GetResourceName() string               { return x.ID }
func (x *Webhook) SetResourceName(name string)          { x.ID = name }
func (x AlertGroup) GetResourceName() string            { return x.PK }
func (x *AlertGroup) SetResourceName(name string)       { x.PK = name }
func (x User) GetResourceName() string                  { return x.PK }
func (x *User) SetResourceName(name string)             { x.PK = name }
func (x UserGroup) GetResourceName() string             { return x.ID }
func (x *UserGroup) SetResourceName(name string)        { x.ID = name }
func (x SlackChannel) GetResourceName() string          { return x.ID }
func (x *SlackChannel) SetResourceName(name string)     { x.ID = name }
func (x Alert) GetResourceName() string                 { return x.ID }
func (x *Alert) SetResourceName(name string)            { x.ID = name }
func (x ResolutionNote) GetResourceName() string        { return x.ID }
func (x *ResolutionNote) SetResourceName(name string)   { x.ID = name }
func (x ShiftSwap) GetResourceName() string             { return x.ID }
func (x *ShiftSwap) SetResourceName(name string)        { x.ID = name }
func (x Organization) GetResourceName() string          { return x.PK }
func (x *Organization) SetResourceName(name string)     { x.PK = name }

// Field names match the OnCall internal API (api/internal/v1/).

//nolint:recvcheck
type Integration struct {
	ID               string `json:"id,omitempty"`
	Description      string `json:"description,omitempty"`
	DescriptionShort string `json:"description_short,omitempty"`
	Integration      string `json:"integration,omitempty"`
	VerbalName       string `json:"verbal_name"`
	IntegrationURL   string `json:"integration_url,omitempty"`
	InboundEmail     string `json:"inbound_email,omitempty"`
	Team             any    `json:"team,omitempty"`
	MaintenanceMode  any    `json:"maintenance_mode,omitempty"`
	MaintenanceTill  any    `json:"maintenance_till,omitempty"`
	Labels           any    `json:"labels,omitempty"`
	AlertGroupLabels any    `json:"alert_group_labels,omitempty"`
}

//nolint:recvcheck
type EscalationChain struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	Team any    `json:"team,omitempty"`
}

//nolint:recvcheck
type EscalationPolicy struct {
	ID                  string `json:"id,omitempty"`
	Step                any    `json:"step"`
	WaitDelay           any    `json:"wait_delay,omitempty"`
	EscalationChain     string `json:"escalation_chain"`
	NotifyToUsersQueue  any    `json:"notify_to_users_queue,omitempty"`
	NotifySchedule      any    `json:"notify_schedule,omitempty"`
	NotifyToTeamMembers any    `json:"notify_to_team_members,omitempty"`
	NotifyToGroup       any    `json:"notify_to_group,omitempty"`
	CustomWebhook       any    `json:"custom_webhook,omitempty"`
	Important           bool   `json:"important,omitempty"`
	Severity            string `json:"severity,omitempty"`
}

//nolint:recvcheck
type Schedule struct {
	ID                 string `json:"id,omitempty"`
	Name               string `json:"name"`
	Type               any    `json:"type,omitempty"`
	Team               any    `json:"team,omitempty"`
	TimeZone           string `json:"time_zone,omitempty"`
	OnCallNow          any    `json:"on_call_now,omitempty"`
	SlackChannel       any    `json:"slack_channel,omitempty"`
	UserGroup          any    `json:"user_group,omitempty"`
	EnableWebOverrides any    `json:"enable_web_overrides,omitempty"`
}

//nolint:recvcheck
type Shift struct {
	ID            string   `json:"id,omitempty"`
	Name          string   `json:"name"`
	Type          any      `json:"type"`
	Schedule      string   `json:"schedule,omitempty"`
	PriorityLevel int      `json:"priority_level,omitempty"`
	ShiftStart    string   `json:"shift_start,omitempty"`
	ShiftEnd      any      `json:"shift_end,omitempty"`
	RotationStart string   `json:"rotation_start,omitempty"`
	Until         string   `json:"until,omitempty"`
	Frequency     any      `json:"frequency,omitempty"`
	Interval      int      `json:"interval,omitempty"`
	ByDay         []string `json:"by_day,omitempty"`
	WeekStart     string   `json:"week_start,omitempty"`
	RollingUsers  any      `json:"rolling_users,omitempty"`
}

//nolint:recvcheck
type Route struct {
	ID                  string `json:"id,omitempty"`
	AlertReceiveChannel string `json:"alert_receive_channel"`
	EscalationChain     any    `json:"escalation_chain,omitempty"`
	FilteringTerm       string `json:"filtering_term,omitempty"`
	FilteringTermType   any    `json:"filtering_term_type,omitempty"`
	IsDefault           bool   `json:"is_default,omitempty"`
	FilteringLabels     any    `json:"filtering_labels,omitempty"`
}

//nolint:recvcheck
type Webhook struct {
	ID                  string   `json:"id,omitempty"`
	Name                string   `json:"name"`
	URL                 string   `json:"url,omitempty"`
	HTTPMethod          string   `json:"http_method,omitempty"`
	TriggerType         any      `json:"trigger_type,omitempty"`
	IsWebhookEnabled    bool     `json:"is_webhook_enabled"`
	IsLegacy            bool     `json:"is_legacy,omitempty"`
	Team                any      `json:"team,omitempty"`
	Data                string   `json:"data,omitempty"`
	Username            string   `json:"username,omitempty"`
	Password            string   `json:"password,omitempty"`
	AuthorizationHeader string   `json:"authorization_header,omitempty"`
	Headers             string   `json:"headers,omitempty"`
	TriggerTemplate     string   `json:"trigger_template,omitempty"`
	IntegrationFilter   []string `json:"integration_filter,omitempty"`
	ForwardAll          bool     `json:"forward_all,omitempty"`
	Preset              string   `json:"preset,omitempty"`
	Labels              any      `json:"labels,omitempty"`
}

//nolint:recvcheck
type AlertGroup struct {
	PK                  string `json:"pk,omitempty"`
	AlertsCount         int    `json:"alerts_count,omitempty"`
	Status              any    `json:"status,omitempty"`
	StartedAt           string `json:"started_at,omitempty"`
	ResolvedAt          string `json:"resolved_at,omitempty"`
	AcknowledgedAt      string `json:"acknowledged_at,omitempty"`
	SilencedAt          string `json:"silenced_at,omitempty"`
	AlertReceiveChannel any    `json:"alert_receive_channel,omitempty"`
	Team                any    `json:"team,omitempty"`
	Labels              any    `json:"labels,omitempty"`
	RenderForWeb        any    `json:"render_for_web,omitempty"`
	Permalinks          any    `json:"permalinks,omitempty"`
}

//nolint:recvcheck
type User struct {
	PK                string `json:"pk,omitempty"`
	Email             string `json:"email,omitempty"`
	Username          string `json:"username"`
	Name              string `json:"name,omitempty"`
	Role              any    `json:"role,omitempty"`
	Avatar            string `json:"avatar,omitempty"`
	Timezone          string `json:"timezone,omitempty"`
	CurrentTeam       any    `json:"current_team,omitempty"`
	SlackUserIdentity any    `json:"slack_user_identity,omitempty"`
}

//nolint:recvcheck
type Team struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Email     string `json:"email,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

//nolint:recvcheck
type UserGroup struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Handle string `json:"handle,omitempty"`
}

//nolint:recvcheck
type SlackChannel struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	SlackID     string `json:"slack_id,omitempty"`
}

//nolint:recvcheck
type Alert struct {
	ID                    string `json:"id,omitempty"`
	LinkToUpstreamDetails string `json:"link_to_upstream_details,omitempty"`
	CreatedAt             string `json:"created_at,omitempty"`
	RenderForWeb          any    `json:"render_for_web,omitempty"`
}

//nolint:recvcheck
type ResolutionNote struct {
	ID         string `json:"id,omitempty"`
	AlertGroup string `json:"alert_group,omitempty"`
	Author     any    `json:"author,omitempty"`
	Source     any    `json:"source,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
	Text       string `json:"text,omitempty"`
}

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
	Description string  `json:"description,omitempty"`
}

//nolint:recvcheck
type Organization struct {
	PK        string `json:"pk,omitempty"`
	OrgID     int    `json:"org_id,omitempty"`
	Name      string `json:"name,omitempty"`
	StackSlug string `json:"stack_slug,omitempty"`
}

type CreateResolutionNoteInput struct {
	AlertGroup string `json:"alert_group"`
	Text       string `json:"text"`
}

type UpdateResolutionNoteInput struct {
	Text string `json:"text"`
}

type CreateShiftSwapInput struct {
	Schedule    string `json:"schedule"`
	SwapStart   string `json:"swap_start"`
	SwapEnd     string `json:"swap_end"`
	Beneficiary string `json:"beneficiary"`
}

type UpdateShiftSwapInput struct {
	SwapStart string `json:"swap_start,omitempty"`
	SwapEnd   string `json:"swap_end,omitempty"`
}

type TakeShiftSwapInput struct {
	Benefactor string `json:"benefactor"`
}

type UserReference struct {
	ID        string `json:"id,omitempty"`
	Username  string `json:"username,omitempty"`
	Important bool   `json:"important"`
}

type DirectPagingInput struct {
	Title                   string          `json:"title,omitempty"`
	Message                 string          `json:"message,omitempty"`
	Team                    string          `json:"team,omitempty"`
	Users                   []UserReference `json:"users,omitempty"`
	ImportantTeamEscalation bool            `json:"important_team_escalation,omitempty"`
	AlertGroupID            string          `json:"alert_group_id,omitempty"`
}

type DirectPagingResult struct {
	AlertGroupID string `json:"alert_group_id,omitempty"`
}

type ShiftRequest struct {
	Name                       string   `json:"name"`
	Type                       any      `json:"type"`
	Schedule                   string   `json:"schedule,omitempty"`
	PriorityLevel              int      `json:"priority_level,omitempty"`
	ShiftStart                 string   `json:"shift_start,omitempty"`
	RotationStart              string   `json:"rotation_start,omitempty"`
	Until                      string   `json:"until,omitempty"`
	Frequency                  any      `json:"frequency,omitempty"`
	Interval                   int      `json:"interval,omitempty"`
	ByDay                      []string `json:"by_day,omitempty"`
	WeekStart                  string   `json:"week_start,omitempty"`
	RollingUsers               any      `json:"rolling_users,omitempty"`
	StartRotationFromUserIndex *int     `json:"start_rotation_from_user_index,omitempty"`
}

// FlatShift is a flattened per-user shift row derived from filter_events response.
type FlatShift struct {
	UserPK       string `json:"user_pk"`
	UserEmail    string `json:"user_email"`
	UserUsername string `json:"user_username"`
	ShiftStart   string `json:"shift_start"`
	ShiftEnd     string `json:"shift_end"`
}

// FinalShiftEvent represents an event from the schedule filter_events endpoint.
type FinalShiftEvent struct {
	Start      string `json:"start"`
	End        string `json:"end"`
	AllDay     bool   `json:"all_day,omitempty"`
	IsGap      bool   `json:"is_gap,omitempty"`
	IsOverride bool   `json:"is_override,omitempty"`
	Users      []struct {
		DisplayName string `json:"display_name"`
		PK          string `json:"pk"`
		Email       string `json:"email"`
	} `json:"users,omitempty"`
}

// FilterEventsResponse is the response from schedules/{id}/filter_events/.
type FilterEventsResponse struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Type   any               `json:"type"`
	Events []FinalShiftEvent `json:"events"`
}
