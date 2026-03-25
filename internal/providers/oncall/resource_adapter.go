package oncall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// --- T1: Registration infrastructure ---

// resourceMeta holds metadata for registering an OnCall resource type.
type resourceMeta struct {
	Descriptor resources.Descriptor
	Aliases    []string
	Schema     json.RawMessage
	Example    json.RawMessage
}

// crudOption configures optional CRUD operations on a TypedCRUD instance.
// It receives the client so that closures can bind to the live client.
type crudOption[T any] func(client *Client, crud *adapter.TypedCRUD[T])

// withCreate returns a crudOption that wires a create function.
func withCreate[T any](fn func(ctx context.Context, c *Client, item *T) (*T, error)) crudOption[T] {
	return func(client *Client, crud *adapter.TypedCRUD[T]) {
		crud.CreateFn = func(ctx context.Context, item *T) (*T, error) {
			return fn(ctx, client, item)
		}
	}
}

// withUpdate returns a crudOption that wires an update function.
func withUpdate[T any](fn func(ctx context.Context, c *Client, name string, item *T) (*T, error)) crudOption[T] {
	return func(client *Client, crud *adapter.TypedCRUD[T]) {
		crud.UpdateFn = func(ctx context.Context, name string, item *T) (*T, error) {
			return fn(ctx, client, name, item)
		}
	}
}

// withDelete returns a crudOption that wires a delete function.
func withDelete[T any](fn func(ctx context.Context, c *Client, name string) error) crudOption[T] {
	return func(client *Client, crud *adapter.TypedCRUD[T]) {
		crud.DeleteFn = func(ctx context.Context, name string) error {
			return fn(ctx, client, name)
		}
	}
}

// registerOnCallResource registers a single OnCall resource type using TypedCRUD[T].
func registerOnCallResource[T any](
	loader OnCallConfigLoader,
	meta resourceMeta,
	nameFn func(T) string,
	listFn func(ctx context.Context, client *Client) ([]T, error),
	getFn func(ctx context.Context, client *Client, name string) (*T, error), // nil for list-only resources
	opts ...crudOption[T],
) {
	desc := meta.Descriptor
	adapter.Register(adapter.Registration{
		Factory: func(ctx context.Context) (adapter.ResourceAdapter, error) {
			client, namespace, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to load OnCall config for %s adapter: %w", desc.Kind, err)
			}

			crud := &adapter.TypedCRUD[T]{
				NameFn:      nameFn,
				ListFn:      func(ctx context.Context) ([]T, error) { return listFn(ctx, client) },
				StripFields: []string{"id", "password", "authorization_header"},
				Namespace:   namespace,
				Descriptor:  desc,
				Aliases:     meta.Aliases,
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
		Descriptor: desc,
		Aliases:    meta.Aliases,
		GVK:        desc.GroupVersionKind(),
		Schema:     meta.Schema,
		Example:    meta.Example,
	})
}

// onCallMeta creates a resourceMeta with the standard OnCall API group/version.
func onCallMeta(kind, singular, plural string, aliases []string) resourceMeta {
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
		Aliases: aliases,
	}
}

// --- T2: All 17 resource registrations ---

// RegisterAdapters registers all OnCall sub-resource adapters in the global registry.
//
//nolint:dupl,maintidx // Table-driven registration: each block configures a different type with identical structure.
func RegisterAdapters(loader OnCallConfigLoader) {
	// 1. Integration — full CRUD
	meta := onCallMeta("Integration", "integration", "integrations",
		[]string{"oncall-integrations", "oncall-integration"})
	meta.Schema = integrationSchema()
	meta.Example = integrationExample()
	registerOnCallResource(loader, meta,
		func(i Integration) string { return i.ID },
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
	)

	// 2. EscalationChain — full CRUD
	registerOnCallResource(loader,
		onCallMeta("EscalationChain", "escalationchain", "escalationchains",
			[]string{"oncall-escalationchains", "oncall-escalationchain", "oncall-ec"}),
		func(ec EscalationChain) string { return ec.ID },
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
	)

	// 3. EscalationPolicy — full CRUD (list with empty filter)
	registerOnCallResource(loader,
		onCallMeta("EscalationPolicy", "escalationpolicy", "escalationpolicies",
			[]string{"oncall-escalationpolicies", "oncall-escalationpolicy", "oncall-ep"}),
		func(ep EscalationPolicy) string { return ep.ID },
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
	)

	// 4. Schedule — full CRUD
	registerOnCallResource(loader,
		onCallMeta("Schedule", "schedule", "schedules",
			[]string{"oncall-schedules", "oncall-schedule"}),
		func(s Schedule) string { return s.ID },
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
	)

	// 5. Shift — CRUD with ShiftRequest conversion for create/update
	registerOnCallResource(loader,
		onCallMeta("Shift", "shift", "shifts",
			[]string{"oncall-shifts", "oncall-shift"}),
		func(s Shift) string { return s.ID },
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
	)

	// 6. Route — full CRUD (list with empty filter)
	registerOnCallResource(loader,
		onCallMeta("Route", "route", "routes",
			[]string{"oncall-routes", "oncall-route"}),
		func(r IntegrationRoute) string { return r.ID },
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
	)

	// 7. OutgoingWebhook — full CRUD
	registerOnCallResource(loader,
		onCallMeta("OutgoingWebhook", "outgoingwebhook", "outgoingwebhooks",
			[]string{"oncall-webhooks", "oncall-webhook"}),
		func(w OutgoingWebhook) string { return w.ID },
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
	)

	// 8. AlertGroup — read-only + delete
	registerOnCallResource(loader,
		onCallMeta("AlertGroup", "alertgroup", "alertgroups",
			[]string{"oncall-alertgroups", "oncall-alertgroup", "oncall-ag"}),
		func(ag AlertGroup) string { return ag.ID },
		func(ctx context.Context, c *Client) ([]AlertGroup, error) { return c.ListAlertGroups(ctx) }, // no filter
		func(ctx context.Context, c *Client, name string) (*AlertGroup, error) {
			return c.GetAlertGroup(ctx, name)
		},
		withDelete[AlertGroup](func(ctx context.Context, c *Client, name string) error {
			return c.DeleteAlertGroup(ctx, name)
		}),
	)

	// 9. User — read-only
	registerOnCallResource(loader,
		onCallMeta("User", "oncalluser", "oncallusers",
			[]string{"oncall-users", "oncall-user"}),
		func(u User) string { return u.ID },
		func(ctx context.Context, c *Client) ([]User, error) { return c.ListUsers(ctx) },
		func(ctx context.Context, c *Client, name string) (*User, error) { return c.GetUser(ctx, name) },
	)

	// 10. Team — read-only
	registerOnCallResource(loader,
		onCallMeta("Team", "oncallteam", "oncallteams",
			[]string{"oncall-teams", "oncall-team"}),
		func(t Team) string { return t.ID },
		func(ctx context.Context, c *Client) ([]Team, error) { return c.ListTeams(ctx) },
		func(ctx context.Context, c *Client, name string) (*Team, error) { return c.GetTeam(ctx, name) },
	)

	// 11. UserGroup — list-only (no Get client method)
	registerOnCallResource(loader,
		onCallMeta("UserGroup", "usergroup", "usergroups",
			[]string{"oncall-usergroups", "oncall-usergroup"}),
		func(ug UserGroup) string { return ug.ID },
		func(ctx context.Context, c *Client) ([]UserGroup, error) { return c.ListUserGroups(ctx) },
		nil, // no GetFn — registerOnCallResource returns ErrUnsupported
	)

	// 12. SlackChannel — list-only (no Get client method)
	registerOnCallResource(loader,
		onCallMeta("SlackChannel", "slackchannel", "slackchannels",
			[]string{"oncall-slackchannels", "oncall-slackchannel"}),
		func(sc SlackChannel) string { return sc.ID },
		func(ctx context.Context, c *Client) ([]SlackChannel, error) { return c.ListSlackChannels(ctx) },
		nil, // no GetFn — registerOnCallResource returns ErrUnsupported
	)

	// 13. Alert — read-only (list with empty filter)
	registerOnCallResource(loader,
		onCallMeta("Alert", "alert", "alerts",
			[]string{"oncall-alerts", "oncall-alert"}),
		func(a Alert) string { return a.ID },
		func(ctx context.Context, c *Client) ([]Alert, error) { return c.ListAlerts(ctx, "") },
		func(ctx context.Context, c *Client, name string) (*Alert, error) { return c.GetAlert(ctx, name) },
	)

	// 14. Organization — read-only
	registerOnCallResource(loader,
		onCallMeta("Organization", "organization", "organizations",
			[]string{"oncall-orgs", "oncall-org"}),
		func(o Organization) string { return o.ID },
		func(ctx context.Context, c *Client) ([]Organization, error) { return c.ListOrganizations(ctx) },
		func(ctx context.Context, c *Client, name string) (*Organization, error) {
			return c.GetOrganization(ctx, name)
		},
	)

	// 15. ResolutionNote — CRUD with Input type conversion (list with empty filter)
	registerOnCallResource(loader,
		onCallMeta("ResolutionNote", "resolutionnote", "resolutionnotes",
			[]string{"oncall-resolution-notes", "oncall-rn"}),
		func(rn ResolutionNote) string { return rn.ID },
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
	)

	// 16. ShiftSwap — CRUD with Input type conversion
	registerOnCallResource(loader,
		onCallMeta("ShiftSwap", "shiftswap", "shiftswaps",
			[]string{"oncall-shift-swaps", "oncall-ss"}),
		func(ss ShiftSwap) string { return ss.ID },
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
	)

	// 17. PersonalNotificationRule — full CRUD
	registerOnCallResource(loader,
		onCallMeta("PersonalNotificationRule", "personalnotificationrule", "personalnotificationrules",
			[]string{"oncall-notification-rules", "oncall-pnr"}),
		func(pnr PersonalNotificationRule) string { return pnr.ID },
		func(ctx context.Context, c *Client) ([]PersonalNotificationRule, error) {
			return c.ListPersonalNotificationRules(ctx)
		},
		func(ctx context.Context, c *Client, name string) (*PersonalNotificationRule, error) {
			return c.GetPersonalNotificationRule(ctx, name)
		},
		withCreate(func(ctx context.Context, c *Client, item *PersonalNotificationRule) (*PersonalNotificationRule, error) {
			return c.CreatePersonalNotificationRule(ctx, *item)
		}),
		withUpdate(func(ctx context.Context, c *Client, name string, item *PersonalNotificationRule) (*PersonalNotificationRule, error) {
			return c.UpdatePersonalNotificationRule(ctx, name, *item)
		}),
		withDelete[PersonalNotificationRule](func(ctx context.Context, c *Client, name string) error {
			return c.DeletePersonalNotificationRule(ctx, name)
		}),
	)
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

// --- Schema and Example helpers ---

func integrationSchema() json.RawMessage {
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/oncall/Integration",
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": APIVersion},
			"kind":       map[string]any{"type": "string", "const": "Integration"},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string"},
					"namespace": map[string]any{"type": "string"},
				},
			},
			"spec": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":              map[string]any{"type": "string"},
					"description_short": map[string]any{"type": "string"},
					"type":              map[string]any{"type": "string"},
					"team_id":           map[string]any{"type": "string"},
				},
				"required": []string{"name", "type"},
			},
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("oncall: failed to marshal integration schema: %v", err))
	}
	return b
}

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
