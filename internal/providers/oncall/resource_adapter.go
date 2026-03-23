package oncall

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// resourceDef defines a single OnCall sub-resource type for adapter registration.
type resourceDef struct {
	kind     string
	singular string
	plural   string
	aliases  []string
	idField  string
	schema   json.RawMessage
	example  json.RawMessage
}

// allResources returns the definitions for all OnCall sub-resources.
func allResources() []resourceDef {
	return []resourceDef{
		{
			kind: "Integration", singular: "integration", plural: "integrations",
			aliases: []string{"oncall-integrations", "oncall-integration"}, idField: "id",
			schema:  integrationSchema(),
			example: integrationExample(),
		},
		{
			kind: "EscalationChain", singular: "escalationchain", plural: "escalationchains",
			aliases: []string{"oncall-escalationchains", "oncall-escalationchain", "oncall-ec"}, idField: "id",
		},
		{
			kind: "EscalationPolicy", singular: "escalationpolicy", plural: "escalationpolicies",
			aliases: []string{"oncall-escalationpolicies", "oncall-escalationpolicy", "oncall-ep"}, idField: "id",
		},
		{
			kind: "Schedule", singular: "schedule", plural: "schedules",
			aliases: []string{"oncall-schedules", "oncall-schedule"}, idField: "id",
		},
		{
			kind: "Shift", singular: "shift", plural: "shifts",
			aliases: []string{"oncall-shifts", "oncall-shift"}, idField: "id",
		},
		{
			kind: "Route", singular: "route", plural: "routes",
			aliases: []string{"oncall-routes", "oncall-route"}, idField: "id",
		},
		{
			kind: "OutgoingWebhook", singular: "outgoingwebhook", plural: "outgoingwebhooks",
			aliases: []string{"oncall-webhooks", "oncall-webhook"}, idField: "id",
		},
		{
			kind: "AlertGroup", singular: "alertgroup", plural: "alertgroups",
			aliases: []string{"oncall-alertgroups", "oncall-alertgroup", "oncall-ag"}, idField: "id",
		},
		{
			kind: "User", singular: "oncalluser", plural: "oncallusers",
			aliases: []string{"oncall-users", "oncall-user"}, idField: "id",
		},
		{
			kind: "Team", singular: "oncallteam", plural: "oncallteams",
			aliases: []string{"oncall-teams", "oncall-team"}, idField: "id",
		},
		{
			kind: "UserGroup", singular: "usergroup", plural: "usergroups",
			aliases: []string{"oncall-usergroups", "oncall-usergroup"}, idField: "id",
		},
		{
			kind: "SlackChannel", singular: "slackchannel", plural: "slackchannels",
			aliases: []string{"oncall-slackchannels", "oncall-slackchannel"}, idField: "id",
		},
		{
			kind: "Alert", singular: "alert", plural: "alerts",
			aliases: []string{"oncall-alerts", "oncall-alert"}, idField: "id",
		},
		{
			kind: "Organization", singular: "organization", plural: "organizations",
			aliases: []string{"oncall-orgs", "oncall-org"}, idField: "id",
		},
		{
			kind: "ResolutionNote", singular: "resolutionnote", plural: "resolutionnotes",
			aliases: []string{"oncall-resolution-notes", "oncall-rn"}, idField: "id",
		},
		{
			kind: "ShiftSwap", singular: "shiftswap", plural: "shiftswaps",
			aliases: []string{"oncall-shift-swaps", "oncall-ss"}, idField: "id",
		},
		{
			kind: "PersonalNotificationRule", singular: "personalnotificationrule", plural: "personalnotificationrules",
			aliases: []string{"oncall-notification-rules", "oncall-pnr"}, idField: "id",
		},
	}
}

// RegisterAdapters registers all OnCall sub-resource adapters in the global registry.
func RegisterAdapters(loader OnCallConfigLoader) {
	for _, rd := range allResources() {
		desc := resources.Descriptor{
			GroupVersion: schema.GroupVersion{
				Group:   APIGroup,
				Version: Version,
			},
			Kind:     rd.kind,
			Singular: rd.singular,
			Plural:   rd.plural,
		}
		adapter.Register(adapter.Registration{
			Factory:    newSubResourceFactory(loader, rd),
			Descriptor: desc,
			Aliases:    rd.aliases,
			GVK:        desc.GroupVersionKind(),
			Schema:     rd.schema,
			Example:    rd.example,
		})
	}
}

// newSubResourceFactory returns a lazy adapter.Factory for a specific sub-resource.
func newSubResourceFactory(loader OnCallConfigLoader, rd resourceDef) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		client, namespace, err := loader.LoadOnCallClient(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load OnCall config for %s adapter: %w", rd.kind, err)
		}

		return &subResourceAdapter{
			client:    client,
			namespace: namespace,
			def:       rd,
		}, nil
	}
}

// subResourceAdapter bridges a specific OnCall sub-resource to the resources pipeline.
type subResourceAdapter struct {
	client    *Client
	namespace string
	def       resourceDef
}

var _ adapter.ResourceAdapter = &subResourceAdapter{}

func (a *subResourceAdapter) Descriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   APIGroup,
			Version: Version,
		},
		Kind:     a.def.kind,
		Singular: a.def.singular,
		Plural:   a.def.plural,
	}
}

func (a *subResourceAdapter) Aliases() []string {
	return a.def.aliases
}

func (a *subResourceAdapter) List(ctx context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	items, err := a.listRaw(ctx)
	if err != nil {
		return nil, err
	}

	result := &unstructured.UnstructuredList{}
	for _, item := range items {
		res, err := a.itemToResource(item)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, res.ToUnstructured())
	}

	return result, nil
}

func (a *subResourceAdapter) Get(ctx context.Context, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	item, err := a.getRaw(ctx, name)
	if err != nil {
		return nil, err
	}

	res, err := a.itemToResource(item)
	if err != nil {
		return nil, err
	}

	obj := res.ToUnstructured()
	return &obj, nil
}

func (a *subResourceAdapter) Create(ctx context.Context, obj *unstructured.Unstructured, _ metav1.CreateOptions) (*unstructured.Unstructured, error) {
	res, err := resources.FromUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured to resource: %w", err)
	}

	created, err := a.createRaw(ctx, res)
	if err != nil {
		return nil, err
	}

	createdRes, err := a.itemToResource(created)
	if err != nil {
		return nil, err
	}

	createdObj := createdRes.ToUnstructured()
	return &createdObj, nil
}

func (a *subResourceAdapter) Update(ctx context.Context, obj *unstructured.Unstructured, _ metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	res, err := resources.FromUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured to resource: %w", err)
	}

	updated, err := a.updateRaw(ctx, obj.GetName(), res)
	if err != nil {
		return nil, err
	}

	updatedRes, err := a.itemToResource(updated)
	if err != nil {
		return nil, err
	}

	updatedObj := updatedRes.ToUnstructured()
	return &updatedObj, nil
}

func (a *subResourceAdapter) Delete(ctx context.Context, name string, _ metav1.DeleteOptions) error {
	return a.deleteRaw(ctx, name)
}

// listRaw dispatches to the appropriate client method based on resource kind.
func (a *subResourceAdapter) listRaw(ctx context.Context) ([]any, error) {
	switch a.def.kind {
	case "Integration":
		return toAnySlice(a.client.ListIntegrations(ctx))
	case "EscalationChain":
		return toAnySlice(a.client.ListEscalationChains(ctx))
	case "EscalationPolicy":
		return toAnySlice(a.client.ListEscalationPolicies(ctx, ""))
	case "Schedule":
		return toAnySlice(a.client.ListSchedules(ctx))
	case "Shift":
		return toAnySlice(a.client.ListShifts(ctx))
	case "Route":
		return toAnySlice(a.client.ListRoutes(ctx, ""))
	case "OutgoingWebhook":
		return toAnySlice(a.client.ListOutgoingWebhooks(ctx))
	case "AlertGroup":
		return toAnySlice(a.client.ListAlertGroups(ctx))
	case "User":
		return toAnySlice(a.client.ListUsers(ctx))
	case "Team":
		return toAnySlice(a.client.ListTeams(ctx))
	case "UserGroup":
		return toAnySlice(a.client.ListUserGroups(ctx))
	case "SlackChannel":
		return toAnySlice(a.client.ListSlackChannels(ctx))
	case "Alert":
		return toAnySlice(a.client.ListAlerts(ctx, ""))
	case "Organization":
		return toAnySlice(a.client.ListOrganizations(ctx))
	case "ResolutionNote":
		return toAnySlice(a.client.ListResolutionNotes(ctx, ""))
	case "ShiftSwap":
		return toAnySlice(a.client.ListShiftSwaps(ctx))
	case "PersonalNotificationRule":
		return toAnySlice(a.client.ListPersonalNotificationRules(ctx))
	default:
		return nil, fmt.Errorf("oncall: list not supported for %s", a.def.kind)
	}
}

// getRaw dispatches to the appropriate client Get method.
func (a *subResourceAdapter) getRaw(ctx context.Context, name string) (any, error) {
	switch a.def.kind {
	case "Integration":
		return a.client.GetIntegration(ctx, name)
	case "EscalationChain":
		return a.client.GetEscalationChain(ctx, name)
	case "EscalationPolicy":
		return a.client.GetEscalationPolicy(ctx, name)
	case "Schedule":
		return a.client.GetSchedule(ctx, name)
	case "Shift":
		return a.client.GetShift(ctx, name)
	case "Route":
		return a.client.GetRoute(ctx, name)
	case "OutgoingWebhook":
		return a.client.GetOutgoingWebhook(ctx, name)
	case "AlertGroup":
		return a.client.GetAlertGroup(ctx, name)
	case "User":
		return a.client.GetUser(ctx, name)
	case "Team":
		return a.client.GetTeam(ctx, name)
	case "Alert":
		return a.client.GetAlert(ctx, name)
	case "Organization":
		return a.client.GetOrganization(ctx, name)
	case "ResolutionNote":
		return a.client.GetResolutionNote(ctx, name)
	case "ShiftSwap":
		return a.client.GetShiftSwap(ctx, name)
	case "PersonalNotificationRule":
		return a.client.GetPersonalNotificationRule(ctx, name)
	default:
		return nil, fmt.Errorf("oncall: get not supported for %s", a.def.kind)
	}
}

// createRaw dispatches to the appropriate client Create method.
func (a *subResourceAdapter) createRaw(ctx context.Context, res *resources.Resource) (any, error) {
	switch a.def.kind {
	case "Integration":
		item, err := fromResource[Integration](res)
		if err != nil {
			return nil, err
		}
		return a.client.CreateIntegration(ctx, *item)
	case "EscalationChain":
		item, err := fromResource[EscalationChain](res)
		if err != nil {
			return nil, err
		}
		return a.client.CreateEscalationChain(ctx, *item)
	case "Schedule":
		item, err := fromResource[Schedule](res)
		if err != nil {
			return nil, err
		}
		return a.client.CreateSchedule(ctx, *item)
	case "OutgoingWebhook":
		item, err := fromResource[OutgoingWebhook](res)
		if err != nil {
			return nil, err
		}
		return a.client.CreateOutgoingWebhook(ctx, *item)
	case "EscalationPolicy":
		item, err := fromResource[EscalationPolicy](res)
		if err != nil {
			return nil, err
		}
		return a.client.CreateEscalationPolicy(ctx, *item)
	case "Route":
		item, err := fromResource[IntegrationRoute](res)
		if err != nil {
			return nil, err
		}
		return a.client.CreateRoute(ctx, *item)
	case "Shift":
		item, err := fromResource[Shift](res)
		if err != nil {
			return nil, err
		}
		// Convert Shift to ShiftRequest for API call
		data, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("oncall: marshal shift: %w", err)
		}
		var sr ShiftRequest
		if err := json.Unmarshal(data, &sr); err != nil {
			return nil, fmt.Errorf("oncall: unmarshal shift to request: %w", err)
		}
		return a.client.CreateShift(ctx, sr)
	default:
		return nil, fmt.Errorf("oncall: create not supported for %s", a.def.kind)
	}
}

// updateRaw dispatches to the appropriate client Update method.
func (a *subResourceAdapter) updateRaw(ctx context.Context, name string, res *resources.Resource) (any, error) {
	switch a.def.kind {
	case "Integration":
		item, err := fromResource[Integration](res)
		if err != nil {
			return nil, err
		}
		return a.client.UpdateIntegration(ctx, name, *item)
	case "EscalationChain":
		item, err := fromResource[EscalationChain](res)
		if err != nil {
			return nil, err
		}
		return a.client.UpdateEscalationChain(ctx, name, *item)
	case "Schedule":
		item, err := fromResource[Schedule](res)
		if err != nil {
			return nil, err
		}
		return a.client.UpdateSchedule(ctx, name, *item)
	case "OutgoingWebhook":
		item, err := fromResource[OutgoingWebhook](res)
		if err != nil {
			return nil, err
		}
		return a.client.UpdateOutgoingWebhook(ctx, name, *item)
	case "EscalationPolicy":
		item, err := fromResource[EscalationPolicy](res)
		if err != nil {
			return nil, err
		}
		return a.client.UpdateEscalationPolicy(ctx, name, *item)
	case "Route":
		item, err := fromResource[IntegrationRoute](res)
		if err != nil {
			return nil, err
		}
		return a.client.UpdateRoute(ctx, name, *item)
	case "Shift":
		item, err := fromResource[Shift](res)
		if err != nil {
			return nil, err
		}
		// Convert Shift to ShiftRequest for API call
		data, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("oncall: marshal shift: %w", err)
		}
		var sr ShiftRequest
		if err := json.Unmarshal(data, &sr); err != nil {
			return nil, fmt.Errorf("oncall: unmarshal shift to request: %w", err)
		}
		return a.client.UpdateShift(ctx, name, sr)
	default:
		return nil, fmt.Errorf("oncall: update not supported for %s", a.def.kind)
	}
}

// deleteRaw dispatches to the appropriate client Delete method.
func (a *subResourceAdapter) deleteRaw(ctx context.Context, name string) error {
	switch a.def.kind {
	case "Integration":
		return a.client.DeleteIntegration(ctx, name)
	case "EscalationChain":
		return a.client.DeleteEscalationChain(ctx, name)
	case "Schedule":
		return a.client.DeleteSchedule(ctx, name)
	case "OutgoingWebhook":
		return a.client.DeleteOutgoingWebhook(ctx, name)
	case "EscalationPolicy":
		return a.client.DeleteEscalationPolicy(ctx, name)
	case "Route":
		return a.client.DeleteRoute(ctx, name)
	case "Shift":
		return a.client.DeleteShift(ctx, name)
	case "AlertGroup":
		return a.client.DeleteAlertGroup(ctx, name)
	default:
		return fmt.Errorf("oncall: delete not supported for %s", a.def.kind)
	}
}

// itemToResource converts a raw OnCall item (any concrete type) to a Resource.
func (a *subResourceAdapter) itemToResource(item any) (*resources.Resource, error) {
	// Marshal the item to get its ID, then create the resource envelope.
	data, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("oncall: marshal %s: %w", a.def.kind, err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("oncall: unmarshal %s to map: %w", a.def.kind, err)
	}

	id := ""
	if v, ok := m[a.def.idField]; ok {
		id = fmt.Sprint(v)
	}

	// Strip the ID field from the spec.
	delete(m, a.def.idField)

	envelope := map[string]any{
		"apiVersion": APIVersion,
		"kind":       a.def.kind,
		"metadata": map[string]any{
			"name":      id,
			"namespace": a.namespace,
		},
		"spec": m,
	}

	return resources.MustFromObject(envelope, resources.SourceInfo{}), nil
}

// toAnySlice converts a typed slice to []any for the generic adapter.
func toAnySlice[T any](items []T, err error) ([]any, error) {
	if err != nil {
		return nil, err
	}
	result := make([]any, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result, nil
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
