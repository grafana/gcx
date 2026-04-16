package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
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

type resourceMeta struct {
	Descriptor  resources.Descriptor
	Schema      json.RawMessage
	Example     json.RawMessage
	URLTemplate string
}

func withCreate[T adapter.ResourceNamer](fn func(ctx context.Context, c OnCallAPI, item *T) (*T, error)) crudOption[T] {
	return func(client OnCallAPI, crud *adapter.TypedCRUD[T]) {
		crud.CreateFn = func(ctx context.Context, item *T) (*T, error) {
			return fn(ctx, client, item)
		}
	}
}

func withUpdate[T adapter.ResourceNamer](fn func(ctx context.Context, c OnCallAPI, name string, item *T) (*T, error)) crudOption[T] {
	return func(client OnCallAPI, crud *adapter.TypedCRUD[T]) {
		crud.UpdateFn = func(ctx context.Context, name string, item *T) (*T, error) {
			return fn(ctx, client, name, item)
		}
	}
}

func withDelete[T adapter.ResourceNamer](fn func(ctx context.Context, c OnCallAPI, name string) error) crudOption[T] {
	return func(client OnCallAPI, crud *adapter.TypedCRUD[T]) {
		crud.DeleteFn = func(ctx context.Context, name string) error {
			return fn(ctx, client, name)
		}
	}
}

func buildRegistration[T adapter.ResourceNamer](
	loader OnCallConfigLoader,
	meta resourceMeta,
	listFn func(ctx context.Context, client OnCallAPI) ([]T, error),
	getFn func(ctx context.Context, client OnCallAPI, name string) (*T, error),
	opts ...crudOption[T],
) adapter.Registration {
	desc := meta.Descriptor
	return adapter.Registration{
		Factory: func(ctx context.Context) (adapter.ResourceAdapter, error) {
			client, namespace, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to load IRM OnCall config for %s adapter: %w", desc.Kind, err)
			}

			crud := &adapter.TypedCRUD[T]{
				StripFields: DefaultStripFields,
				Namespace:   namespace,
				Descriptor:  desc,
			}

			if listFn != nil {
				crud.ListFn = adapter.LimitedListFn(func(ctx context.Context) ([]T, error) { return listFn(ctx, client) })
			}

			if getFn != nil {
				crud.GetFn = func(ctx context.Context, name string) (*T, error) { return getFn(ctx, client, name) }
			} else {
				crud.GetFn = func(_ context.Context, _ string) (*T, error) { return nil, errors.ErrUnsupported }
			}

			for _, opt := range opts {
				opt(client, crud)
			}

			return crud.AsAdapter(), nil
		},
		Descriptor:  desc,
		GVK:         desc.GroupVersionKind(),
		Schema:      meta.Schema,
		Example:     meta.Example,
		URLTemplate: meta.URLTemplate,
	}
}

func oncallMeta(kind, singular, plural string) resourceMeta {
	return resourceMeta{
		Descriptor: resources.Descriptor{
			GroupVersion: schema.GroupVersion{
				Group:   APIGroup,
				Version: Version,
			},
			Kind:     kind,
			Singular: singular,
			Plural:   plural,
		},
	}
}

//nolint:dupl,maintidx // Table-driven registration: each block configures a different type with identical structure.
func buildOnCallRegistrations(loader OnCallConfigLoader) []adapter.Registration {
	var regs []adapter.Registration

	// 1. Integration — full CRUD
	meta := oncallMeta("Integration", "integration", "integrations")
	meta.Schema = adapter.SchemaFromType[Integration](meta.Descriptor)
	meta.Example = integrationExample()
	meta.URLTemplate = "/a/grafana-oncall-app/integrations/{name}"
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]Integration, error) { return c.ListIntegrations(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*Integration, error) {
			return c.GetIntegration(ctx, name)
		},
		withCreate(func(ctx context.Context, c OnCallAPI, item *Integration) (*Integration, error) {
			return c.CreateIntegration(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c OnCallAPI, name string, item *Integration) (*Integration, error) {
			return c.UpdateIntegration(ctx, name, *item)
		}),
		withDelete[Integration](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteIntegration(ctx, name)
		}),
	))

	// 2. EscalationChain — full CRUD
	meta = oncallMeta("EscalationChain", "escalationchain", "escalationchains")
	meta.Schema = adapter.SchemaFromType[EscalationChain](meta.Descriptor)
	meta.Example = escalationChainExample()
	meta.URLTemplate = "/a/grafana-oncall-app/escalation-chains/{name}"
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]EscalationChain, error) {
			return c.ListEscalationChains(ctx)
		},
		func(ctx context.Context, c OnCallAPI, name string) (*EscalationChain, error) {
			return c.GetEscalationChain(ctx, name)
		},
		withCreate(func(ctx context.Context, c OnCallAPI, item *EscalationChain) (*EscalationChain, error) {
			return c.CreateEscalationChain(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c OnCallAPI, name string, item *EscalationChain) (*EscalationChain, error) {
			return c.UpdateEscalationChain(ctx, name, *item)
		}),
		withDelete[EscalationChain](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteEscalationChain(ctx, name)
		}),
	))

	// 3. EscalationPolicy — full CRUD
	meta = oncallMeta("EscalationPolicy", "escalationpolicy", "escalationpolicies")
	meta.Schema = adapter.SchemaFromType[EscalationPolicy](meta.Descriptor)
	meta.Example = escalationPolicyExample()
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]EscalationPolicy, error) {
			return c.ListEscalationPolicies(ctx, "")
		},
		func(ctx context.Context, c OnCallAPI, name string) (*EscalationPolicy, error) {
			return c.GetEscalationPolicy(ctx, name)
		},
		withCreate(func(ctx context.Context, c OnCallAPI, item *EscalationPolicy) (*EscalationPolicy, error) {
			return c.CreateEscalationPolicy(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c OnCallAPI, name string, item *EscalationPolicy) (*EscalationPolicy, error) {
			return c.UpdateEscalationPolicy(ctx, name, *item)
		}),
		withDelete[EscalationPolicy](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteEscalationPolicy(ctx, name)
		}),
	))

	// 4. Schedule — full CRUD
	meta = oncallMeta("Schedule", "schedule", "schedules")
	meta.Schema = adapter.SchemaFromType[Schedule](meta.Descriptor)
	meta.Example = scheduleExample()
	meta.URLTemplate = "/a/grafana-oncall-app/schedules/{name}"
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]Schedule, error) { return c.ListSchedules(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*Schedule, error) {
			return c.GetSchedule(ctx, name)
		},
		withCreate(func(ctx context.Context, c OnCallAPI, item *Schedule) (*Schedule, error) {
			return c.CreateSchedule(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c OnCallAPI, name string, item *Schedule) (*Schedule, error) {
			return c.UpdateSchedule(ctx, name, *item)
		}),
		withDelete[Schedule](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteSchedule(ctx, name)
		}),
	))

	// 5. Shift — CRUD with ShiftRequest conversion
	meta = oncallMeta("Shift", "shift", "shifts")
	meta.Schema = adapter.SchemaFromType[Shift](meta.Descriptor)
	meta.Example = shiftExample()
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]Shift, error) { return c.ListShifts(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*Shift, error) { return c.GetShift(ctx, name) },
		withCreate(func(ctx context.Context, c OnCallAPI, item *Shift) (*Shift, error) {
			return c.CreateShift(ctx, shiftToRequest(item))
		}),
		withUpdate(func(ctx context.Context, c OnCallAPI, name string, item *Shift) (*Shift, error) {
			return c.UpdateShift(ctx, name, shiftToRequest(item))
		}),
		withDelete[Shift](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteShift(ctx, name)
		}),
	))

	// 6. Route — full CRUD
	meta = oncallMeta("Route", "route", "routes")
	meta.Schema = adapter.SchemaFromType[Route](meta.Descriptor)
	meta.Example = routeExample()
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]Route, error) { return c.ListRoutes(ctx, "") },
		func(ctx context.Context, c OnCallAPI, name string) (*Route, error) { return c.GetRoute(ctx, name) },
		withCreate(func(ctx context.Context, c OnCallAPI, item *Route) (*Route, error) {
			return c.CreateRoute(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c OnCallAPI, name string, item *Route) (*Route, error) {
			return c.UpdateRoute(ctx, name, *item)
		}),
		withDelete[Route](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteRoute(ctx, name)
		}),
	))

	// 7. Webhook — full CRUD
	meta = oncallMeta("Webhook", "webhook", "webhooks")
	meta.Schema = adapter.SchemaFromType[Webhook](meta.Descriptor)
	meta.Example = webhookExample()
	meta.URLTemplate = "/a/grafana-oncall-app/outgoing-webhooks/{name}"
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]Webhook, error) { return c.ListWebhooks(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*Webhook, error) {
			return c.GetWebhook(ctx, name)
		},
		withCreate(func(ctx context.Context, c OnCallAPI, item *Webhook) (*Webhook, error) {
			return c.CreateWebhook(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c OnCallAPI, name string, item *Webhook) (*Webhook, error) {
			return c.UpdateWebhook(ctx, name, *item)
		}),
		withDelete[Webhook](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteWebhook(ctx, name)
		}),
	))

	// 8. AlertGroup — read-only + delete
	meta = oncallMeta("AlertGroup", "alertgroup", "alertgroups")
	meta.Schema = adapter.SchemaFromType[AlertGroup](meta.Descriptor)
	meta.URLTemplate = "/a/grafana-oncall-app/alert-groups/{name}"
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]AlertGroup, error) { return c.ListAlertGroups(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*AlertGroup, error) {
			return c.GetAlertGroup(ctx, name)
		},
		withDelete[AlertGroup](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteAlertGroup(ctx, name)
		}),
	))

	// 9. User — read-only
	meta = oncallMeta("User", "oncalluser", "oncallusers")
	meta.Schema = adapter.SchemaFromType[User](meta.Descriptor)
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]User, error) { return c.ListUsers(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*User, error) { return c.GetUser(ctx, name) },
	))

	// 10. Team — read-only
	meta = oncallMeta("Team", "oncallteam", "oncallteams")
	meta.Schema = adapter.SchemaFromType[Team](meta.Descriptor)
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]Team, error) { return c.ListTeams(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*Team, error) { return c.GetTeam(ctx, name) },
	))

	// 11. UserGroup — list-only
	meta = oncallMeta("UserGroup", "usergroup", "usergroups")
	meta.Schema = adapter.SchemaFromType[UserGroup](meta.Descriptor)
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]UserGroup, error) { return c.ListUserGroups(ctx) },
		nil,
	))

	// 12. SlackChannel — list-only
	meta = oncallMeta("SlackChannel", "slackchannel", "slackchannels")
	meta.Schema = adapter.SchemaFromType[SlackChannel](meta.Descriptor)
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]SlackChannel, error) { return c.ListSlackChannels(ctx) },
		nil,
	))

	// 13. Alert — get-only
	meta = oncallMeta("Alert", "alert", "alerts")
	meta.Schema = adapter.SchemaFromType[Alert](meta.Descriptor)
	regs = append(regs, buildRegistration(loader, meta,
		nil,
		func(ctx context.Context, c OnCallAPI, name string) (*Alert, error) { return c.GetAlert(ctx, name) },
	))

	// 14. Organization — read-only (singular endpoint, no list)
	meta = oncallMeta("Organization", "organization", "organizations")
	meta.Schema = adapter.SchemaFromType[Organization](meta.Descriptor)
	regs = append(regs, buildRegistration[Organization](loader, meta,
		nil,
		func(ctx context.Context, c OnCallAPI, _ string) (*Organization, error) {
			return c.GetOrganization(ctx)
		},
	))

	// 15. ResolutionNote — CRUD with input conversion
	meta = oncallMeta("ResolutionNote", "resolutionnote", "resolutionnotes")
	meta.Schema = adapter.SchemaFromType[ResolutionNote](meta.Descriptor)
	meta.Example = resolutionNoteExample()
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]ResolutionNote, error) {
			return c.ListResolutionNotes(ctx, "")
		},
		func(ctx context.Context, c OnCallAPI, name string) (*ResolutionNote, error) {
			return c.GetResolutionNote(ctx, name)
		},
		withCreate(func(ctx context.Context, c OnCallAPI, item *ResolutionNote) (*ResolutionNote, error) {
			return c.CreateResolutionNote(ctx, CreateResolutionNoteInput{
				AlertGroup: item.AlertGroup,
				Text:       item.Text,
			})
		}),
		withUpdate(func(ctx context.Context, c OnCallAPI, name string, item *ResolutionNote) (*ResolutionNote, error) {
			return c.UpdateResolutionNote(ctx, name, UpdateResolutionNoteInput{
				Text: item.Text,
			})
		}),
		withDelete[ResolutionNote](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteResolutionNote(ctx, name)
		}),
	))

	// 16. ShiftSwap — CRUD with input conversion
	meta = oncallMeta("ShiftSwap", "shiftswap", "shiftswaps")
	meta.Schema = adapter.SchemaFromType[ShiftSwap](meta.Descriptor)
	meta.Example = shiftSwapExample()
	regs = append(regs, buildRegistration(loader, meta,
		func(ctx context.Context, c OnCallAPI) ([]ShiftSwap, error) { return c.ListShiftSwaps(ctx) },
		func(ctx context.Context, c OnCallAPI, name string) (*ShiftSwap, error) {
			return c.GetShiftSwap(ctx, name)
		},
		withCreate(func(ctx context.Context, c OnCallAPI, item *ShiftSwap) (*ShiftSwap, error) {
			return c.CreateShiftSwap(ctx, CreateShiftSwapInput{
				Schedule:    item.Schedule,
				SwapStart:   item.SwapStart,
				SwapEnd:     item.SwapEnd,
				Beneficiary: item.Beneficiary,
			})
		}),
		withUpdate(func(ctx context.Context, c OnCallAPI, name string, item *ShiftSwap) (*ShiftSwap, error) {
			return c.UpdateShiftSwap(ctx, name, UpdateShiftSwapInput{
				SwapStart: item.SwapStart,
				SwapEnd:   item.SwapEnd,
			})
		}),
		withDelete[ShiftSwap](func(ctx context.Context, c OnCallAPI, name string) error {
			return c.DeleteShiftSwap(ctx, name)
		}),
	))

	return regs
}

func shiftToRequest(s *Shift) ShiftRequest {
	return ShiftRequest{
		Name:          s.Name,
		Type:          s.Type,
		Schedule:      s.Schedule,
		PriorityLevel: s.PriorityLevel,
		ShiftStart:    s.ShiftStart,
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
			"type":      "web",
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
			"trigger_type":       "escalation",
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
