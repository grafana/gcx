package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() { //nolint:gochecknoinits // Natural key registration for cross-stack push identity matching.
	adapter.RegisterNaturalKey(
		schema.GroupVersionKind{Group: APIGroup, Version: Version, Kind: "Integration"},
		adapter.SpecFieldKey("verbal_name"),
	)
	adapter.RegisterNaturalKey(
		schema.GroupVersionKind{Group: APIGroup, Version: Version, Kind: "Schedule"},
		adapter.SpecFieldKey("name"),
	)
	adapter.RegisterNaturalKey(
		schema.GroupVersionKind{Group: APIGroup, Version: Version, Kind: "EscalationChain"},
		adapter.SpecFieldKey("name"),
	)
}

// ---------------------------------------------------------------------------
// Per-resource CRUD wiring, shared between the resource adapters (push/pull)
// and the noun-command verbs (create/update/delete). A single source of truth
// keeps both access paths identical (dual-CRUD invariant in CONSTITUTION.md).
// ---------------------------------------------------------------------------

func integrationCRUDOpts() []crudOption[Integration] {
	return []crudOption[Integration]{
		adapter.WithCreate(func(ctx context.Context, c OnCallAPI, item *Integration) (*Integration, error) {
			return c.CreateIntegration(ctx, *item)
		}),
		adapter.WithUpdate(func(ctx context.Context, c OnCallAPI, name string, item *Integration) (*Integration, error) {
			return c.UpdateIntegration(ctx, name, *item)
		}),
		adapter.WithDelete[Integration](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteIntegration(ctx, name)
		}),
	}
}

func escalationChainCRUDOpts() []crudOption[EscalationChain] {
	return []crudOption[EscalationChain]{
		adapter.WithCreate(func(ctx context.Context, c OnCallAPI, item *EscalationChain) (*EscalationChain, error) {
			return c.CreateEscalationChain(ctx, *item)
		}),
		adapter.WithUpdate(func(ctx context.Context, c OnCallAPI, name string, item *EscalationChain) (*EscalationChain, error) {
			return c.UpdateEscalationChain(ctx, name, *item)
		}),
		adapter.WithDelete[EscalationChain](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteEscalationChain(ctx, name)
		}),
	}
}

func escalationPolicyCRUDOpts() []crudOption[EscalationPolicy] {
	return []crudOption[EscalationPolicy]{
		adapter.WithCreate(func(ctx context.Context, c OnCallAPI, item *EscalationPolicy) (*EscalationPolicy, error) {
			return c.CreateEscalationPolicy(ctx, *item)
		}),
		adapter.WithUpdate(func(ctx context.Context, c OnCallAPI, name string, item *EscalationPolicy) (*EscalationPolicy, error) {
			return c.UpdateEscalationPolicy(ctx, name, *item)
		}),
		adapter.WithDelete[EscalationPolicy](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteEscalationPolicy(ctx, name)
		}),
	}
}

//nolint:dupl // Each resource keeps the same create, update, and delete wiring.
func scheduleCRUDOpts() []crudOption[Schedule] {
	return []crudOption[Schedule]{
		adapter.WithCreate(func(ctx context.Context, c OnCallAPI, item *Schedule) (*Schedule, error) {
			if err := validateScheduleType(item.Type); err != nil {
				return nil, err
			}
			return c.CreateSchedule(ctx, *item)
		}),
		adapter.WithUpdate(func(ctx context.Context, c OnCallAPI, name string, item *Schedule) (*Schedule, error) {
			if err := validateScheduleType(item.Type); err != nil {
				return nil, err
			}
			return c.UpdateSchedule(ctx, name, *item)
		}),
		adapter.WithDelete[Schedule](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteSchedule(ctx, name)
		}),
	}
}

func shiftCRUDOpts() []crudOption[Shift] {
	return []crudOption[Shift]{
		adapter.WithCreate(func(ctx context.Context, c OnCallAPI, item *Shift) (*Shift, error) {
			if err := validateShiftValues(item); err != nil {
				return nil, err
			}
			return c.CreateShift(ctx, shiftToRequest(item))
		}),
		adapter.WithUpdate(func(ctx context.Context, c OnCallAPI, name string, item *Shift) (*Shift, error) {
			if err := validateShiftValues(item); err != nil {
				return nil, err
			}
			return c.UpdateShift(ctx, name, shiftToRequest(item))
		}),
		adapter.WithDelete[Shift](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteShift(ctx, name)
		}),
	}
}

func routeCRUDOpts() []crudOption[Route] {
	return []crudOption[Route]{
		adapter.WithCreate(func(ctx context.Context, c OnCallAPI, item *Route) (*Route, error) {
			return c.CreateRoute(ctx, *item)
		}),
		adapter.WithUpdate(func(ctx context.Context, c OnCallAPI, name string, item *Route) (*Route, error) {
			return c.UpdateRoute(ctx, name, *item)
		}),
		adapter.WithDelete[Route](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteRoute(ctx, name)
		}),
	}
}

//nolint:dupl // Each resource keeps the same create, update, and delete wiring.
func webhookCRUDOpts() []crudOption[Webhook] {
	return []crudOption[Webhook]{
		adapter.WithCreate(func(ctx context.Context, c OnCallAPI, item *Webhook) (*Webhook, error) {
			if err := validateWebhookTrigger(item.TriggerType); err != nil {
				return nil, err
			}
			return c.CreateWebhook(ctx, *item)
		}),
		adapter.WithUpdate(func(ctx context.Context, c OnCallAPI, name string, item *Webhook) (*Webhook, error) {
			if err := validateWebhookTrigger(item.TriggerType); err != nil {
				return nil, err
			}
			return c.UpdateWebhook(ctx, name, *item)
		}),
		adapter.WithDelete[Webhook](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteWebhook(ctx, name)
		}),
	}
}

func validateScheduleType(value any) error {
	if value == nil || value == "" {
		return errors.New("schedule type is required; use the numeric type from an existing schedule")
	}
	return nil
}

func validateWebhookTrigger(value any) error {
	if value == nil || value == "" {
		return nil // A preset can control the trigger.
	}
	text := fmt.Sprint(value)
	trigger, err := strconv.Atoi(text)
	if err != nil || strconv.Itoa(trigger) != text || trigger < 0 || trigger > 20 {
		return fmt.Errorf("webhook trigger_type %q must be a catalog value from 0 to 20; see webhooks list-triggers", text)
	}
	return nil
}

func validateShiftValues(shift *Shift) error {
	for _, field := range []struct {
		name     string
		value    any
		optional bool
	}{
		{"type", shift.Type, false},
		{"frequency", shift.Frequency, true},
	} {
		if field.value == nil || field.value == "" {
			if field.optional {
				continue
			}
			return errors.New("shift type is required; use a numeric value from the shift type catalog")
		}
		value := fmt.Sprint(field.value)
		number, err := strconv.Atoi(value)
		if err != nil || strconv.Itoa(number) != value || number < 0 || number > 3 {
			return fmt.Errorf("shift %s %q must be a numeric value from 0 to 3", field.name, value)
		}
	}
	return nil
}

func resolutionNoteCRUDOpts() []crudOption[ResolutionNote] {
	return []crudOption[ResolutionNote]{
		adapter.WithCreate(func(ctx context.Context, c OnCallAPI, item *ResolutionNote) (*ResolutionNote, error) {
			return c.CreateResolutionNote(ctx, CreateResolutionNoteInput{
				AlertGroup: item.AlertGroup,
				Text:       item.Text,
			})
		}),
		adapter.WithUpdate(func(ctx context.Context, c OnCallAPI, name string, item *ResolutionNote) (*ResolutionNote, error) {
			return c.UpdateResolutionNote(ctx, name, UpdateResolutionNoteInput{
				Text: item.Text,
			})
		}),
		adapter.WithDelete[ResolutionNote](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteResolutionNote(ctx, name)
		}),
	}
}

func shiftSwapCRUDOpts() []crudOption[ShiftSwap] {
	return []crudOption[ShiftSwap]{
		adapter.WithCreate(func(ctx context.Context, c OnCallAPI, item *ShiftSwap) (*ShiftSwap, error) {
			return c.CreateShiftSwap(ctx, CreateShiftSwapInput{
				Schedule:    item.Schedule,
				SwapStart:   item.SwapStart,
				SwapEnd:     item.SwapEnd,
				Beneficiary: item.Beneficiary,
			})
		}),
		adapter.WithUpdate(func(ctx context.Context, c OnCallAPI, name string, item *ShiftSwap) (*ShiftSwap, error) {
			return c.UpdateShiftSwap(ctx, name, UpdateShiftSwapInput{
				Schedule:  item.Schedule,
				SwapStart: item.SwapStart,
				SwapEnd:   item.SwapEnd,
			})
		}),
		adapter.WithDelete[ShiftSwap](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteShiftSwap(ctx, name)
		}),
	}
}

// oncallMeta builds an adapter.RegistrationMeta for an OnCall resource type,
// wiring OnCall's GroupVersion and default strip fields. Schema and GVK are
// auto-derived by adapter.BuildRegistration — callers only set Example and
// URLTemplate on the returned value.
func oncallMeta(kind, singular, plural string) adapter.RegistrationMeta {
	meta := adapter.NewRegistrationMeta(schema.GroupVersion{Group: APIGroup, Version: Version}, kind, singular, plural)
	meta.StripFields = DefaultStripFields
	return meta
}

func buildOnCallRegistrations(loader OnCallConfigLoader) []adapter.Registration {
	var regs []adapter.Registration

	// 1. Integration — full CRUD
	meta := oncallMeta("Integration", "integration", "integrations")
	meta.Example = integrationExample()
	meta.URLTemplate = "/a/grafana-oncall-app/integrations/{name}"
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]Integration, error) { return c.ListIntegrations(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*Integration, error) {
			return c.GetIntegration(ctx, name)
		},
		integrationCRUDOpts()...,
	))

	// 2. EscalationChain — full CRUD
	meta = oncallMeta("EscalationChain", "escalationchain", "escalationchains")
	meta.Example = escalationChainExample()
	meta.URLTemplate = "/a/grafana-oncall-app/escalation-chains/{name}"
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]EscalationChain, error) {
			return c.ListEscalationChains(ctx)
		},
		func(ctx context.Context, c OnCallAPI, name string) (*EscalationChain, error) {
			return c.GetEscalationChain(ctx, name)
		},
		escalationChainCRUDOpts()...,
	))

	// 3. EscalationPolicy — full CRUD
	meta = oncallMeta("EscalationPolicy", "escalationpolicy", "escalationpolicies")
	meta.Example = escalationPolicyExample()
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]EscalationPolicy, error) {
			return c.ListEscalationPolicies(ctx, "")
		},
		func(ctx context.Context, c OnCallAPI, name string) (*EscalationPolicy, error) {
			return c.GetEscalationPolicy(ctx, name)
		},
		escalationPolicyCRUDOpts()...,
	))

	// 4. Schedule — full CRUD
	meta = oncallMeta("Schedule", "schedule", "schedules")
	meta.Example = scheduleExample()
	meta.URLTemplate = "/a/grafana-oncall-app/schedules/{name}"
	scheduleRegistration := adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]Schedule, error) { return c.ListSchedules(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*Schedule, error) {
			return c.GetSchedule(ctx, name)
		},
		scheduleCRUDOpts()...,
	)
	scheduleRegistration.Schema = onCallScheduleSchema(scheduleRegistration.Schema)
	regs = append(regs, scheduleRegistration)

	// 5. Shift — CRUD with ShiftRequest conversion
	meta = oncallMeta("Shift", "shift", "shifts")
	meta.Example = shiftExample()
	shiftRegistration := adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]Shift, error) { return c.ListShifts(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*Shift, error) { return c.GetShift(ctx, name) },
		shiftCRUDOpts()...,
	)
	shiftRegistration.Schema = onCallNumericChoiceSchema(shiftRegistration.Schema, map[string][]int{
		"type":      {0, 1, 2, 3},
		"frequency": {0, 1, 2, 3},
	}, map[string]bool{"frequency": true}, map[string]string{
		"type":      "Internal shift type: 0 single event, 1 recurrent event, 2 rolling users, 3 override. The current create endpoint advertises only 2 and 3.",
		"frequency": "Internal recurrence: 0 daily, 1 weekly, 2 monthly, 3 hourly.",
	})
	regs = append(regs, shiftRegistration)

	// 6. Route — full CRUD
	meta = oncallMeta("Route", "route", "routes")
	meta.Example = routeExample()
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]Route, error) { return c.ListRoutes(ctx, "") },
		func(ctx context.Context, c OnCallAPI, name string) (*Route, error) { return c.GetRoute(ctx, name) },
		routeCRUDOpts()...,
	))

	// 7. Webhook — full CRUD
	meta = oncallMeta("Webhook", "webhook", "webhooks")
	meta.Example = webhookExample()
	meta.URLTemplate = "/a/grafana-oncall-app/outgoing-webhooks/{name}"
	webhookRegistration := adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]Webhook, error) { return c.ListWebhooks(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*Webhook, error) {
			return c.GetWebhook(ctx, name)
		},
		webhookCRUDOpts()...,
	)
	webhookRegistration.Schema = onCallNumericChoiceSchema(webhookRegistration.Schema, map[string][]int{
		"trigger_type": {0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
	}, map[string]bool{"trigger_type": true}, map[string]string{
		"trigger_type": "OnCall webhook trigger value. Run webhooks list-triggers to see current names and values.",
	})
	regs = append(regs, webhookRegistration)

	// 8. AlertGroup — read-only + delete
	meta = oncallMeta("AlertGroup", "alertgroup", "alertgroups")
	meta.URLTemplate = "/a/grafana-oncall-app/alert-groups/{name}"
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]AlertGroup, error) { return c.ListAlertGroups(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*AlertGroup, error) {
			return c.GetAlertGroup(ctx, name)
		},
		adapter.WithDelete[AlertGroup](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteAlertGroup(ctx, name)
		}),
	))

	// 9. User — read-only
	meta = oncallMeta("User", "oncalluser", "oncallusers")
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]User, error) { return c.ListUsers(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*User, error) { return c.GetUser(ctx, name) },
	))

	// 10. Team — read-only
	meta = oncallMeta("Team", "oncallteam", "oncallteams")
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]Team, error) { return c.ListTeams(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*Team, error) { return c.GetTeam(ctx, name) },
	))

	// 11. UserGroup — list-only
	meta = oncallMeta("UserGroup", "usergroup", "usergroups")
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]UserGroup, error) { return c.ListUserGroups(ctx) },
		nil,
	))

	// 12. SlackChannel — list-only
	meta = oncallMeta("SlackChannel", "slackchannel", "slackchannels")
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]SlackChannel, error) { return c.ListSlackChannels(ctx) },
		nil,
	))

	// 13. Alert — get-only
	meta = oncallMeta("Alert", "alert", "alerts")
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		nil,
		func(ctx context.Context, c OnCallAPI, name string) (*Alert, error) { return c.GetAlert(ctx, name) },
	))

	// 14. Organization — read-only (singular endpoint, no list)
	meta = oncallMeta("Organization", "organization", "organizations")
	regs = append(regs, adapter.BuildRegistration[Organization](loader.LoadOnCallClient, meta,
		nil,
		func(ctx context.Context, c OnCallAPI, _ string) (*Organization, error) {
			return c.GetOrganization(ctx)
		},
	))

	// 15. ResolutionNote — CRUD with input conversion
	meta = oncallMeta("ResolutionNote", "resolutionnote", "resolutionnotes")
	meta.Example = resolutionNoteExample()
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]ResolutionNote, error) {
			return c.ListResolutionNotes(ctx, "")
		},
		func(ctx context.Context, c OnCallAPI, name string) (*ResolutionNote, error) {
			return c.GetResolutionNote(ctx, name)
		},
		resolutionNoteCRUDOpts()...,
	))

	// 16. ShiftSwap — CRUD with input conversion
	meta = oncallMeta("ShiftSwap", "shiftswap", "shiftswaps")
	meta.Example = shiftSwapExample()
	regs = append(regs, adapter.BuildRegistration(loader.LoadOnCallClient, meta,
		func(ctx context.Context, c OnCallAPI) ([]ShiftSwap, error) { return c.ListShiftSwaps(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*ShiftSwap, error) {
			return c.GetShiftSwap(ctx, name)
		},
		shiftSwapCRUDOpts()...,
	))

	return regs
}

func onCallNumericChoiceSchema(base func() json.RawMessage, choices map[string][]int, allowUnset map[string]bool, descriptions map[string]string) func() json.RawMessage {
	return func() json.RawMessage {
		document, fields := onCallSchemaFields(base)
		for field, values := range choices {
			numbers := make([]any, 0, len(values))
			strings := make([]any, 0, len(values)+1)
			for _, value := range values {
				numbers = append(numbers, value)
				strings = append(strings, strconv.Itoa(value))
			}
			if allowUnset[field] {
				strings = append(strings, "")
			}
			options := []any{
				map[string]any{"type": "integer", "enum": numbers},
				map[string]any{"type": "string", "enum": strings},
			}
			if allowUnset[field] {
				options = append(options, map[string]any{"type": "null"})
			}
			fields[field] = map[string]any{"oneOf": options, "description": descriptions[field]}
		}
		result, err := json.Marshal(document)
		if err != nil {
			panic(fmt.Sprintf("encode OnCall schema: %v", err))
		}
		return result
	}
}

func onCallScheduleSchema(base func() json.RawMessage) func() json.RawMessage {
	return func() json.RawMessage {
		document, fields := onCallSchemaFields(base)
		fields["type"] = map[string]any{"not": map[string]any{"enum": []any{nil, ""}}}
		result, err := json.Marshal(document)
		if err != nil {
			panic(fmt.Sprintf("encode Schedule schema: %v", err))
		}
		return result
	}
}

func onCallSchemaFields(base func() json.RawMessage) (map[string]any, map[string]any) {
	var document map[string]any
	if err := json.Unmarshal(base(), &document); err != nil {
		panic(fmt.Sprintf("decode OnCall schema: %v", err))
	}
	properties, ok := document["properties"].(map[string]any)
	if !ok {
		panic("OnCall schema has no properties")
	}
	spec, ok := properties["spec"].(map[string]any)
	if !ok {
		panic("OnCall schema has no spec")
	}
	fields, ok := spec["properties"].(map[string]any)
	if !ok {
		panic("OnCall spec schema has no properties")
	}
	return document, fields
}

func shiftToRequest(s *Shift) ShiftRequest {
	return ShiftRequest{
		Name:          s.Name,
		Type:          s.Type,
		Schedule:      s.Schedule,
		PriorityLevel: s.PriorityLevel,
		ShiftStart:    s.ShiftStart,
		ShiftEnd:      s.ShiftEnd,
		RotationStart: s.RotationStart,
		Until:         s.Until,
		Frequency:     s.Frequency,
		Interval:      s.Interval,
		ByDay:         s.ByDay,
		WeekStart:     s.WeekStart,
		RollingUsers:  s.RollingUsers,
	}
}

// --- Example helpers (adapted for internal API field names) ---

func integrationExample() json.RawMessage {
	return mustMarshal(map[string]any{
		"apiVersion": APIVersion,
		"kind":       "Integration",
		"metadata":   map[string]any{"name": "my-alertmanager"},
		"spec": map[string]any{
			"verbal_name":       "my-alertmanager",
			"description_short": "Receives alerts from Alertmanager",
			"integration":       "alertmanager",
		},
	})
}

func escalationChainExample() json.RawMessage {
	return mustMarshal(map[string]any{
		"apiVersion": APIVersion,
		"kind":       "EscalationChain",
		"metadata":   map[string]any{"name": "my-chain"},
		"spec":       map[string]any{"name": "my-chain"},
	})
}

func escalationPolicyExample() json.RawMessage {
	return mustMarshal(map[string]any{
		"apiVersion": APIVersion,
		"kind":       "EscalationPolicy",
		"metadata":   map[string]any{"name": "my-policy"},
		"spec": map[string]any{
			"escalation_chain":      "ABCD1234",
			"step":                  0,
			"notify_to_users_queue": []string{"U1234"},
		},
	})
}

func scheduleExample() json.RawMessage {
	return mustMarshal(map[string]any{
		"apiVersion": APIVersion,
		"kind":       "Schedule",
		"metadata":   map[string]any{"name": "my-schedule"},
		"spec": map[string]any{
			"name":      "my-schedule",
			"type":      2,
			"time_zone": "UTC",
		},
	})
}

func shiftExample() json.RawMessage {
	return mustMarshal(map[string]any{
		"apiVersion": APIVersion,
		"kind":       "Shift",
		"metadata":   map[string]any{"name": "my-shift"},
		"spec": map[string]any{
			"name":        "my-shift",
			"type":        2,
			"shift_start": "2026-01-01T00:00:00",
			"frequency":   1,
			"interval":    1,
			"by_day":      []string{"MO", "TU", "WE", "TH", "FR"},
		},
	})
}

func routeExample() json.RawMessage {
	return mustMarshal(map[string]any{
		"apiVersion": APIVersion,
		"kind":       "Route",
		"metadata":   map[string]any{"name": "my-route"},
		"spec": map[string]any{
			"alert_receive_channel": "INT1234",
			"filtering_term":        "severity=critical",
		},
	})
}

func webhookExample() json.RawMessage {
	return mustMarshal(map[string]any{
		"apiVersion": APIVersion,
		"kind":       "Webhook",
		"metadata":   map[string]any{"name": "my-webhook"},
		"spec": map[string]any{
			"name":               "my-webhook",
			"url":                "https://example.com/webhook",
			"trigger_type":       "1",
			"is_webhook_enabled": true,
			"http_method":        "POST",
		},
	})
}

func resolutionNoteExample() json.RawMessage {
	return mustMarshal(map[string]any{
		"apiVersion": APIVersion,
		"kind":       "ResolutionNote",
		"metadata":   map[string]any{"name": "my-note"},
		"spec": map[string]any{
			"alert_group": "AG1234",
			"text":        "Root cause identified: memory leak in auth service.",
		},
	})
}

func shiftSwapExample() json.RawMessage {
	return mustMarshal(map[string]any{
		"apiVersion": APIVersion,
		"kind":       "ShiftSwap",
		"metadata":   map[string]any{"name": "my-swap"},
		"spec": map[string]any{
			"schedule":    "SCHED1234",
			"swap_start":  "2026-04-01T00:00:00Z",
			"swap_end":    "2026-04-02T00:00:00Z",
			"beneficiary": "U1234",
		},
	})
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("irm: failed to marshal example: %v", err))
	}
	return b
}
