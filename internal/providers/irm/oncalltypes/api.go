package oncalltypes

import "context"

// OnCallAPI defines the operations available on the OnCall backend.
// Both the plugin proxy client (OAuth) and the public API client (SA token)
// implement this interface, returning data in the internal API type shape.
type OnCallAPI interface {
	ListIntegrations(ctx context.Context) ([]Integration, error)
	GetIntegration(ctx context.Context, id string) (*Integration, error)
	CreateIntegration(ctx context.Context, i Integration) (*Integration, error)
	UpdateIntegration(ctx context.Context, id string, i Integration) (*Integration, error)
	DeleteIntegration(ctx context.Context, id string) error

	ListEscalationChains(ctx context.Context) ([]EscalationChain, error)
	GetEscalationChain(ctx context.Context, id string) (*EscalationChain, error)
	CreateEscalationChain(ctx context.Context, ec EscalationChain) (*EscalationChain, error)
	UpdateEscalationChain(ctx context.Context, id string, ec EscalationChain) (*EscalationChain, error)
	DeleteEscalationChain(ctx context.Context, id string) error

	ListEscalationPolicies(ctx context.Context, chainID string) ([]EscalationPolicy, error)
	GetEscalationPolicy(ctx context.Context, id string) (*EscalationPolicy, error)
	CreateEscalationPolicy(ctx context.Context, p EscalationPolicy) (*EscalationPolicy, error)
	UpdateEscalationPolicy(ctx context.Context, id string, p EscalationPolicy) (*EscalationPolicy, error)
	DeleteEscalationPolicy(ctx context.Context, id string) error

	ListSchedules(ctx context.Context) ([]Schedule, error)
	GetSchedule(ctx context.Context, id string) (*Schedule, error)
	CreateSchedule(ctx context.Context, s Schedule) (*Schedule, error)
	UpdateSchedule(ctx context.Context, id string, s Schedule) (*Schedule, error)
	DeleteSchedule(ctx context.Context, id string) error
	ListFilterEvents(ctx context.Context, scheduleID, userTZ, startingDate string, days int) (*FilterEventsResponse, error)

	ListShifts(ctx context.Context) ([]Shift, error)
	GetShift(ctx context.Context, id string) (*Shift, error)
	CreateShift(ctx context.Context, s ShiftRequest) (*Shift, error)
	UpdateShift(ctx context.Context, id string, s ShiftRequest) (*Shift, error)
	DeleteShift(ctx context.Context, id string) error

	ListRoutes(ctx context.Context, integrationID string) ([]Route, error)
	GetRoute(ctx context.Context, id string) (*Route, error)
	CreateRoute(ctx context.Context, r Route) (*Route, error)
	UpdateRoute(ctx context.Context, id string, r Route) (*Route, error)
	DeleteRoute(ctx context.Context, id string) error

	ListWebhooks(ctx context.Context) ([]Webhook, error)
	GetWebhook(ctx context.Context, id string) (*Webhook, error)
	CreateWebhook(ctx context.Context, w Webhook) (*Webhook, error)
	UpdateWebhook(ctx context.Context, id string, w Webhook) (*Webhook, error)
	DeleteWebhook(ctx context.Context, id string) error

	ListAlertGroups(ctx context.Context) ([]AlertGroup, error)
	GetAlertGroup(ctx context.Context, id string) (*AlertGroup, error)
	DeleteAlertGroup(ctx context.Context, id string) error
	AcknowledgeAlertGroup(ctx context.Context, id string) error
	ResolveAlertGroup(ctx context.Context, id string) error
	SilenceAlertGroup(ctx context.Context, id string, delaySecs int) error
	UnacknowledgeAlertGroup(ctx context.Context, id string) error
	UnresolveAlertGroup(ctx context.Context, id string) error
	UnsilenceAlertGroup(ctx context.Context, id string) error

	ListUsers(ctx context.Context) ([]User, error)
	GetUser(ctx context.Context, id string) (*User, error)
	GetCurrentUser(ctx context.Context) (*User, error)

	ListTeams(ctx context.Context) ([]Team, error)
	GetTeam(ctx context.Context, id string) (*Team, error)

	ListUserGroups(ctx context.Context) ([]UserGroup, error)
	ListSlackChannels(ctx context.Context) ([]SlackChannel, error)

	ListAlerts(ctx context.Context, alertGroupID string) ([]Alert, error)
	GetAlert(ctx context.Context, id string) (*Alert, error)

	GetOrganization(ctx context.Context) (*Organization, error)

	ListResolutionNotes(ctx context.Context, alertGroupID string) ([]ResolutionNote, error)
	GetResolutionNote(ctx context.Context, id string) (*ResolutionNote, error)
	CreateResolutionNote(ctx context.Context, input CreateResolutionNoteInput) (*ResolutionNote, error)
	UpdateResolutionNote(ctx context.Context, id string, input UpdateResolutionNoteInput) (*ResolutionNote, error)
	DeleteResolutionNote(ctx context.Context, id string) error

	ListShiftSwaps(ctx context.Context) ([]ShiftSwap, error)
	GetShiftSwap(ctx context.Context, id string) (*ShiftSwap, error)
	CreateShiftSwap(ctx context.Context, input CreateShiftSwapInput) (*ShiftSwap, error)
	UpdateShiftSwap(ctx context.Context, id string, input UpdateShiftSwapInput) (*ShiftSwap, error)
	DeleteShiftSwap(ctx context.Context, id string) error
	TakeShiftSwap(ctx context.Context, id string, input TakeShiftSwapInput) (*ShiftSwap, error)

	CreateDirectPaging(ctx context.Context, input DirectPagingInput) (*DirectPagingResult, error)
}
