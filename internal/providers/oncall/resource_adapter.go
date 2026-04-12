package oncall

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
		adapter.SpecFieldKey("name"),
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

// --- T1: Registration infrastructure ---

// resourceMeta holds metadata for registering an OnCall resource type.
type resourceMeta struct {
	Descriptor  resources.Descriptor
	Schema      json.RawMessage
	Example     json.RawMessage
	URLTemplate string // Deep link URL template (e.g., "/a/grafana-oncall-app/schedules/{name}").
}

// crudOption configures optional CRUD operations on a TypedCRUD instance.
// It receives the client so that closures can bind to the live client.
type crudOption[T adapter.ResourceNamer] func(client *Client, crud *adapter.TypedCRUD[T])

// withCreate returns a crudOption that wires a create function.
func withCreate[T adapter.ResourceNamer](fn func(ctx context.Context, c *Client, item *T) (*T, error)) crudOption[T] {
	return func(client *Client, crud *adapter.TypedCRUD[T]) {
		crud.CreateFn = func(ctx context.Context, item *T) (*T, error) {
			return fn(ctx, client, item)
		}
	}
}

// withUpdate returns a crudOption that wires an update function.
func withUpdate[T adapter.ResourceNamer](fn func(ctx context.Context, c *Client, name string, item *T) (*T, error)) crudOption[T] {
	return func(client *Client, crud *adapter.TypedCRUD[T]) {
		crud.UpdateFn = func(ctx context.Context, name string, item *T) (*T, error) {
			return fn(ctx, client, name, item)
		}
	}
}

// withDelete returns a crudOption that wires a delete function.
func withDelete[T adapter.ResourceNamer](fn func(ctx context.Context, c *Client, name string) error) crudOption[T] {
	return func(client *Client, crud *adapter.TypedCRUD[T]) {
		crud.DeleteFn = func(ctx context.Context, name string) error {
			return fn(ctx, client, name)
		}
	}
}

// buildOnCallRegistration builds a single OnCall resource registration using TypedCRUD[T].
// It returns the Registration but does NOT call adapter.Register() — that is done by TypedRegistrations().
func buildOnCallRegistration[T adapter.ResourceNamer](
	loader OnCallConfigLoader,
	meta resourceMeta,
	listFn func(ctx context.Context, client *Client) ([]T, error),
	getFn func(ctx context.Context, client *Client, name string) (*T, error), // nil for list-only resources
	opts ...crudOption[T],
) adapter.Registration {
	desc := meta.Descriptor
	return adapter.Registration{
		Factory: func(ctx context.Context) (adapter.ResourceAdapter, error) {
			client, namespace, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to load OnCall config for %s adapter: %w", desc.Kind, err)
			}

			crud := &adapter.TypedCRUD[T]{
				StripFields: []string{"id", "password", "authorization_header"},
				Namespace:   namespace,
				Descriptor:  desc,
			}

			if listFn != nil {
				crud.ListFn = adapter.LimitedListFn(func(ctx context.Context) ([]T, error) {
					return listFn(ctx, client)
				})
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

// onCallMeta creates a resourceMeta with the standard OnCall API group/version.
func onCallMeta(kind, singular, plural string) resourceMeta {
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

// --- T2: All 17 resource registrations ---

// buildOnCallRegistrations builds all OnCall sub-resource registrations.
// Returns []adapter.Registration which are registered globally by providers.Register().
//
//nolint:dupl,maintidx // Table-driven registration: each block configures a different type with identical structure.
func buildOnCallRegistrations(loader OnCallConfigLoader) []adapter.Registration {
	var regs []adapter.Registration
	// 1. Integration — full CRUD
	meta := onCallMeta("Integration", "integration", "integrations")
	meta.Schema = adapter.SchemaFromType[Integration](meta.Descriptor)
	meta.Example = integrationExample()
	meta.URLTemplate = "/a/grafana-oncall-app/integrations/{name}"
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]Integration, error) { return c.ListIntegrations(ctx) },
		func(ctx context.Context, c *Client, name string) (*Integration, error) {
			return c.GetIntegration(ctx, name)
		},
		withCreate(func(ctx context.Context, c *Client, item *Integration) (*Integration, error) {
			return c.CreateIntegration(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c *Client, name string, item *Integration) (*Integration, error) {
			return c.UpdateIntegration(ctx, name, *item)
		}),
		withDelete[Integration](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteIntegration(ctx, name)
		}),
	))

	// 2. EscalationChain — full CRUD
	meta = onCallMeta("EscalationChain", "escalationchain", "escalationchains")
	meta.Schema = adapter.SchemaFromType[EscalationChain](meta.Descriptor)
	meta.Example = escalationChainExample()
	meta.URLTemplate = "/a/grafana-oncall-app/escalation-chains/{name}"
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]EscalationChain, error) { return c.ListEscalationChains(ctx) },
		func(ctx context.Context, c *Client, name string) (*EscalationChain, error) {
			return c.GetEscalationChain(ctx, name)
		},
		withCreate(func(ctx context.Context, c *Client, item *EscalationChain) (*EscalationChain, error) {
			return c.CreateEscalationChain(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c *Client, name string, item *EscalationChain) (*EscalationChain, error) {
			return c.UpdateEscalationChain(ctx, name, *item)
		}),
		withDelete[EscalationChain](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteEscalationChain(ctx, name)
		}),
	))

	// 3. EscalationPolicy — full CRUD (list with empty filter)
	meta = onCallMeta("EscalationPolicy", "escalationpolicy", "escalationpolicies")
	meta.Schema = adapter.SchemaFromType[EscalationPolicy](meta.Descriptor)
	meta.Example = escalationPolicyExample()
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]EscalationPolicy, error) {
			return c.ListEscalationPolicies(ctx, "")
		},
		func(ctx context.Context, c *Client, name string) (*EscalationPolicy, error) {
			return c.GetEscalationPolicy(ctx, name)
		},
		withCreate(func(ctx context.Context, c *Client, item *EscalationPolicy) (*EscalationPolicy, error) {
			return c.CreateEscalationPolicy(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c *Client, name string, item *EscalationPolicy) (*EscalationPolicy, error) {
			return c.UpdateEscalationPolicy(ctx, name, *item)
		}),
		withDelete[EscalationPolicy](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteEscalationPolicy(ctx, name)
		}),
	))

	// 4. Schedule — full CRUD
	meta = onCallMeta("Schedule", "schedule", "schedules")
	meta.Schema = adapter.SchemaFromType[Schedule](meta.Descriptor)
	meta.Example = scheduleExample()
	meta.URLTemplate = "/a/grafana-oncall-app/schedules/{name}"
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]Schedule, error) { return c.ListSchedules(ctx) },
		func(ctx context.Context, c *Client, name string) (*Schedule, error) { return c.GetSchedule(ctx, name) },
		withCreate(func(ctx context.Context, c *Client, item *Schedule) (*Schedule, error) {
			return c.CreateSchedule(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c *Client, name string, item *Schedule) (*Schedule, error) {
			return c.UpdateSchedule(ctx, name, *item)
		}),
		withDelete[Schedule](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteSchedule(ctx, name)
		}),
	))

	// 5. Shift — CRUD with ShiftRequest conversion for create/update
	meta = onCallMeta("Shift", "shift", "shifts")
	meta.Schema = adapter.SchemaFromType[Shift](meta.Descriptor)
	meta.Example = shiftExample()
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]Shift, error) { return c.ListShifts(ctx) },
		func(ctx context.Context, c *Client, name string) (*Shift, error) { return c.GetShift(ctx, name) },
		withCreate(func(ctx context.Context, c *Client, item *Shift) (*Shift, error) {
			sr, err := shiftToRequest(item)
			if err != nil {
				return nil, err
			}
			return c.CreateShift(ctx, sr)
		}),
		withUpdate(func(ctx context.Context, c *Client, name string, item *Shift) (*Shift, error) {
			sr, err := shiftToRequest(item)
			if err != nil {
				return nil, err
			}
			return c.UpdateShift(ctx, name, sr)
		}),
		withDelete[Shift](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteShift(ctx, name)
		}),
	))

	// 6. Route — full CRUD (list with empty filter)
	meta = onCallMeta("Route", "route", "routes")
	meta.Schema = adapter.SchemaFromType[IntegrationRoute](meta.Descriptor)
	meta.Example = routeExample()
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]IntegrationRoute, error) { return c.ListRoutes(ctx, "") },
		func(ctx context.Context, c *Client, name string) (*IntegrationRoute, error) {
			return c.GetRoute(ctx, name)
		},
		withCreate(func(ctx context.Context, c *Client, item *IntegrationRoute) (*IntegrationRoute, error) {
			return c.CreateRoute(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c *Client, name string, item *IntegrationRoute) (*IntegrationRoute, error) {
			return c.UpdateRoute(ctx, name, *item)
		}),
		withDelete[IntegrationRoute](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteRoute(ctx, name)
		}),
	))

	// 7. OutgoingWebhook — full CRUD
	meta = onCallMeta("OutgoingWebhook", "outgoingwebhook", "outgoingwebhooks")
	meta.Schema = adapter.SchemaFromType[OutgoingWebhook](meta.Descriptor)
	meta.Example = outgoingWebhookExample()
	meta.URLTemplate = "/a/grafana-oncall-app/outgoing-webhooks/{name}"
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]OutgoingWebhook, error) { return c.ListOutgoingWebhooks(ctx) },
		func(ctx context.Context, c *Client, name string) (*OutgoingWebhook, error) {
			return c.GetOutgoingWebhook(ctx, name)
		},
		withCreate(func(ctx context.Context, c *Client, item *OutgoingWebhook) (*OutgoingWebhook, error) {
			return c.CreateOutgoingWebhook(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c *Client, name string, item *OutgoingWebhook) (*OutgoingWebhook, error) {
			return c.UpdateOutgoingWebhook(ctx, name, *item)
		}),
		withDelete[OutgoingWebhook](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteOutgoingWebhook(ctx, name)
		}),
	))

	// 8. AlertGroup — read-only + delete
	meta = onCallMeta("AlertGroup", "alertgroup", "alertgroups")
	meta.Schema = adapter.SchemaFromType[AlertGroup](meta.Descriptor)
	meta.URLTemplate = "/a/grafana-oncall-app/alert-groups/{name}"
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]AlertGroup, error) { return c.ListAlertGroups(ctx) }, // no filter
		func(ctx context.Context, c *Client, name string) (*AlertGroup, error) {
			return c.GetAlertGroup(ctx, name)
		},
		withDelete[AlertGroup](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteAlertGroup(ctx, name)
		}),
	))

	// 9. User — read-only
	meta = onCallMeta("User", "oncalluser", "oncallusers")
	meta.Schema = adapter.SchemaFromType[User](meta.Descriptor)
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]User, error) { return c.ListUsers(ctx) },
		func(ctx context.Context, c *Client, name string) (*User, error) { return c.GetUser(ctx, name) },
	))

	// 10. Team — read-only
	meta = onCallMeta("Team", "oncallteam", "oncallteams")
	meta.Schema = adapter.SchemaFromType[Team](meta.Descriptor)
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]Team, error) { return c.ListTeams(ctx) },
		func(ctx context.Context, c *Client, name string) (*Team, error) { return c.GetTeam(ctx, name) },
	))

	// 11. UserGroup — list-only (no Get client method)
	meta = onCallMeta("UserGroup", "usergroup", "usergroups")
	meta.Schema = adapter.SchemaFromType[UserGroup](meta.Descriptor)
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]UserGroup, error) { return c.ListUserGroups(ctx) },
		nil, // no GetFn — buildOnCallRegistration returns ErrUnsupported
	))

	// 12. SlackChannel — list-only (no Get client method)
	meta = onCallMeta("SlackChannel", "slackchannel", "slackchannels")
	meta.Schema = adapter.SchemaFromType[SlackChannel](meta.Descriptor)
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]SlackChannel, error) { return c.ListSlackChannels(ctx) },
		nil, // no GetFn — buildOnCallRegistration returns ErrUnsupported
	))

	// 13. Alert — get-only (list requires alert_group_id, handled by CLI command)
	meta = onCallMeta("Alert", "alert", "alerts")
	meta.Schema = adapter.SchemaFromType[Alert](meta.Descriptor)
	regs = append(regs, buildOnCallRegistration(loader, meta,
		nil, // ListFn nil — the OnCall API requires alert_group_id to list alerts
		func(ctx context.Context, c *Client, name string) (*Alert, error) { return c.GetAlert(ctx, name) },
	))

	// 14. Organization — read-only
	meta = onCallMeta("Organization", "organization", "organizations")
	meta.Schema = adapter.SchemaFromType[Organization](meta.Descriptor)
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]Organization, error) { return c.ListOrganizations(ctx) },
		func(ctx context.Context, c *Client, name string) (*Organization, error) {
			return c.GetOrganization(ctx, name)
		},
	))

	// 15. ResolutionNote — CRUD with Input type conversion (list with empty filter)
	meta = onCallMeta("ResolutionNote", "resolutionnote", "resolutionnotes")
	meta.Schema = adapter.SchemaFromType[ResolutionNote](meta.Descriptor)
	meta.Example = resolutionNoteExample()
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]ResolutionNote, error) {
			return c.ListResolutionNotes(ctx, "")
		},
		func(ctx context.Context, c *Client, name string) (*ResolutionNote, error) {
			return c.GetResolutionNote(ctx, name)
		},
		withCreate(func(ctx context.Context, c *Client, item *ResolutionNote) (*ResolutionNote, error) {
			return c.CreateResolutionNote(ctx, CreateResolutionNoteInput{
				AlertGroupID: item.AlertGroupID,
				Text:         item.Text,
			})
		}),
		withUpdate(func(ctx context.Context, c *Client, name string, item *ResolutionNote) (*ResolutionNote, error) {
			return c.UpdateResolutionNote(ctx, name, UpdateResolutionNoteInput{
				Text: item.Text,
			})
		}),
		withDelete[ResolutionNote](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteResolutionNote(ctx, name)
		}),
	))

	// 16. ShiftSwap — CRUD with Input type conversion
	meta = onCallMeta("ShiftSwap", "shiftswap", "shiftswaps")
	meta.Schema = adapter.SchemaFromType[ShiftSwap](meta.Descriptor)
	meta.Example = shiftSwapExample()
	regs = append(regs, buildOnCallRegistration(loader, meta,
		func(ctx context.Context, c *Client) ([]ShiftSwap, error) { return c.ListShiftSwaps(ctx) },
		func(ctx context.Context, c *Client, name string) (*ShiftSwap, error) {
			return c.GetShiftSwap(ctx, name)
		},
		withCreate(func(ctx context.Context, c *Client, item *ShiftSwap) (*ShiftSwap, error) {
			return c.CreateShiftSwap(ctx, CreateShiftSwapInput{
				Schedule:    item.Schedule,
				SwapStart:   item.SwapStart,
				SwapEnd:     item.SwapEnd,
				Beneficiary: item.Beneficiary,
			})
		}),
		withUpdate(func(ctx context.Context, c *Client, name string, item *ShiftSwap) (*ShiftSwap, error) {
			return c.UpdateShiftSwap(ctx, name, UpdateShiftSwapInput{
				SwapStart: item.SwapStart,
				SwapEnd:   item.SwapEnd,
			})
		}),
		withDelete[ShiftSwap](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteShiftSwap(ctx, name)
		}),
	))

	// PersonalNotificationRule removed: OnCall API rejects SA tokens for this
	// endpoint (403 "Invalid token"). Client methods retained in client.go for
	// when user-token auth is added. See follow-up bead.

	return regs
}

// shiftToRequest converts a Shift to a ShiftRequest via JSON round-trip.
func shiftToRequest(s *Shift) (ShiftRequest, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return ShiftRequest{}, fmt.Errorf("oncall: marshal shift: %w", err)
	}
	var sr ShiftRequest
	if err := json.Unmarshal(data, &sr); err != nil {
		return ShiftRequest{}, fmt.Errorf("oncall: unmarshal shift to request: %w", err)
	}
	return sr, nil
}

// --- Example helpers ---

func integrationExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       "Integration",
		"metadata": map[string]any{
			"name": "my-alertmanager",
		},
		"spec": map[string]any{
			"name":              "my-alertmanager",
			"description_short": "Receives alerts from Alertmanager",
			"type":              "alertmanager",
			"default_route": map[string]any{
				"escalation_chain_id": "ABCD1234",
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("oncall: failed to marshal integration example: %v", err))
	}
	return b
}

func escalationChainExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       "EscalationChain",
		"metadata":   map[string]any{"name": "my-chain"},
		"spec":       map[string]any{"name": "my-chain"},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("oncall: failed to marshal escalation chain example: %v", err))
	}
	return b
}

func escalationPolicyExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       "EscalationPolicy",
		"metadata":   map[string]any{"name": "my-policy"},
		"spec": map[string]any{
			"escalation_chain_id": "ABCD1234",
			"position":            0,
			"type":                "notify_persons",
			"persons_to_notify":   []string{"U1234"},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("oncall: failed to marshal escalation policy example: %v", err))
	}
	return b
}

func scheduleExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       "Schedule",
		"metadata":   map[string]any{"name": "my-schedule"},
		"spec": map[string]any{
			"name":      "my-schedule",
			"type":      "web",
			"time_zone": "UTC",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("oncall: failed to marshal schedule example: %v", err))
	}
	return b
}

func shiftExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       "Shift",
		"metadata":   map[string]any{"name": "my-shift"},
		"spec": map[string]any{
			"name":      "my-shift",
			"type":      "rolling_users",
			"start":     "2026-01-01T00:00:00",
			"duration":  86400,
			"frequency": "weekly",
			"interval":  1,
			"by_day":    []string{"MO", "TU", "WE", "TH", "FR"},
			"users":     []string{"U1234"},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("oncall: failed to marshal shift example: %v", err))
	}
	return b
}

func routeExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       "Route",
		"metadata":   map[string]any{"name": "my-route"},
		"spec": map[string]any{
			"integration_id": "INT1234",
			"routing_regex":  "severity=critical",
			"position":       0,
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("oncall: failed to marshal route example: %v", err))
	}
	return b
}

func outgoingWebhookExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       "OutgoingWebhook",
		"metadata":   map[string]any{"name": "my-webhook"},
		"spec": map[string]any{
			"name":               "my-webhook",
			"url":                "https://example.com/webhook",
			"trigger_type":       "escalation",
			"is_webhook_enabled": true,
			"http_method":        "POST",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("oncall: failed to marshal outgoing webhook example: %v", err))
	}
	return b
}

func resolutionNoteExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       "ResolutionNote",
		"metadata":   map[string]any{"name": "my-note"},
		"spec": map[string]any{
			"alert_group_id": "AG1234",
			"text":           "Root cause identified: memory leak in auth service. Fix deployed.",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("oncall: failed to marshal resolution note example: %v", err))
	}
	return b
}

func shiftSwapExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       "ShiftSwap",
		"metadata":   map[string]any{"name": "my-swap"},
		"spec": map[string]any{
			"schedule":    "SCHED1234",
			"swap_start":  "2026-04-01T00:00:00Z",
			"swap_end":    "2026-04-02T00:00:00Z",
			"beneficiary": "U1234",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("oncall: failed to marshal shift swap example: %v", err))
	}
	return b
}

// --- T3: NewTypedCRUD factories for CLI commands ---

// NewTypedCRUD[T adapter.ResourceNamer] creates a TypedCRUD instance for CLI commands.
// It mirrors buildOnCallRegistration but returns TypedCRUD directly instead of Registration.
// This factory pattern allows commands to use typed methods without going through the adapter.
// Returns (crud, namespace, grafanaHost, error).
func NewTypedCRUD[T adapter.ResourceNamer](
	ctx context.Context,
	loader OnCallConfigLoader,
	listFn func(ctx context.Context, client *Client) ([]T, error),
	getFn func(ctx context.Context, client *Client, name string) (*T, error), // nil for list-only resources
	opts ...crudOption[T],
) (*adapter.TypedCRUD[T], string, string, error) {
	client, namespace, err := loader.LoadOnCallClient(ctx)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to load OnCall config: %w", err)
	}

	crud := &adapter.TypedCRUD[T]{
		ListFn: adapter.LimitedListFn(func(ctx context.Context) ([]T, error) {
			return listFn(ctx, client)
		}),
		StripFields: []string{"id", "password", "authorization_header"},
		Namespace:   namespace,
	}

	if getFn != nil {
		crud.GetFn = func(ctx context.Context, name string) (*T, error) { return getFn(ctx, client, name) }
	} else {
		crud.GetFn = func(_ context.Context, _ string) (*T, error) { return nil, errors.ErrUnsupported }
	}

	for _, opt := range opts {
		opt(client, crud)
	}

	return crud, namespace, client.GrafanaURL(), nil
}
