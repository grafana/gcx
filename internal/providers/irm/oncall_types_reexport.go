package irm

import "github.com/grafana/gcx/internal/providers/irm/oncalltypes"

// Re-export types from oncalltypes so existing code in this package
// and oncallpublic can both reference the same types without import cycles.

type (
	OnCallAPI              = oncalltypes.OnCallAPI
	Integration            = oncalltypes.Integration
	EscalationChain        = oncalltypes.EscalationChain
	EscalationPolicy       = oncalltypes.EscalationPolicy
	Schedule               = oncalltypes.Schedule
	Shift                  = oncalltypes.Shift
	Route                  = oncalltypes.Route
	Webhook                = oncalltypes.Webhook
	AlertGroup             = oncalltypes.AlertGroup
	User                   = oncalltypes.User
	Team                   = oncalltypes.Team
	UserGroup              = oncalltypes.UserGroup
	SlackChannel           = oncalltypes.SlackChannel
	Alert                  = oncalltypes.Alert
	Organization           = oncalltypes.Organization
	ResolutionNote         = oncalltypes.ResolutionNote
	ShiftSwap              = oncalltypes.ShiftSwap
	FlatShift              = oncalltypes.FlatShift
	FinalShiftEvent        = oncalltypes.FinalShiftEvent
	FilterEventsResponse   = oncalltypes.FilterEventsResponse
	CreateResolutionNoteInput = oncalltypes.CreateResolutionNoteInput
	UpdateResolutionNoteInput = oncalltypes.UpdateResolutionNoteInput
	CreateShiftSwapInput   = oncalltypes.CreateShiftSwapInput
	UpdateShiftSwapInput   = oncalltypes.UpdateShiftSwapInput
	TakeShiftSwapInput     = oncalltypes.TakeShiftSwapInput
	UserReference          = oncalltypes.UserReference
	DirectPagingInput      = oncalltypes.DirectPagingInput
	DirectPagingResult     = oncalltypes.DirectPagingResult
	ShiftRequest           = oncalltypes.ShiftRequest
)

const (
	APIGroup   = oncalltypes.APIGroup
	APIVersion = oncalltypes.APIVersion
	Version    = oncalltypes.Version
)

var DefaultStripFields = oncalltypes.DefaultStripFields
