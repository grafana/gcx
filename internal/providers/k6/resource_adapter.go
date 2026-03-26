package k6

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// resourceDef defines a single k6 sub-resource type for adapter registration.
type resourceDef struct {
	kind     string
	singular string
	plural   string
	schema   json.RawMessage
	example  json.RawMessage
}

// allResources returns the definitions for all k6 resource types.
func allResources() []resourceDef {
	return []resourceDef{
		{
			kind: "Project", singular: "project", plural: "projects",
			schema:  projectSchema(),
			example: projectExample(),
		},
		{
			kind: "LoadTest", singular: "loadtest", plural: "loadtests",
			schema:  loadTestSchema(),
			example: loadTestExample(),
		},
		{
			kind: "Schedule", singular: "schedule", plural: "schedules",
			schema:  scheduleSchema(),
			example: scheduleExample(),
		},
		{
			kind: "EnvVar", singular: "envvar", plural: "envvars",
			schema:  envVarSchema(),
			example: envVarExample(),
		},
		{
			kind: "LoadZone", singular: "loadzone", plural: "loadzones",
			schema:  loadZoneSchema(),
			example: loadZoneExample(),
		},
	}
}

// CloudConfigLoader can load Grafana Cloud config (token + stack info via GCOM).
type CloudConfigLoader interface {
	LoadCloudConfig(ctx context.Context) (providers.CloudRESTConfig, error)
}

// authenticatedClient loads cloud config, resolves the k6 API domain from provider config,
// performs k6 token exchange, and returns an authenticated client.
func authenticatedClient(ctx context.Context, loader CloudConfigLoader) (*Client, string, error) {
	cfg, err := loader.LoadCloudConfig(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("k6: load cloud config: %w", err)
	}

	domain := DefaultAPIDomain
	if k6Cfg := cfg.ProviderConfig("k6"); k6Cfg != nil {
		if d := k6Cfg["api-domain"]; d != "" {
			domain = d
		}
	}

	// K6 API uses its own auth (X-Grafana-Key token exchange), not the Grafana
	// bearer token. Using rest.HTTPClientFor() would inject the Grafana bearer
	// token via the k8s transport round-tripper, causing 401 from the K6 API.
	httpClient := providers.ExternalHTTPClient()

	client := NewClient(domain, httpClient)
	if err := client.Authenticate(ctx, cfg.Token, cfg.Stack.ID); err != nil {
		return nil, "", fmt.Errorf("k6 auth failed (PUT %s): %w -- ensure your token has k6 scopes", authPath, err)
	}

	return client, cfg.Namespace, nil
}

// newSubResourceFactory returns a lazy adapter.Factory for a specific k6 resource.
func newSubResourceFactory(loader CloudConfigLoader, rd resourceDef) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		client, namespace, err := authenticatedClient(ctx, loader)
		if err != nil {
			return nil, fmt.Errorf("k6: failed to load config for %s adapter: %w", rd.kind, err)
		}

		desc := resources.Descriptor{
			GroupVersion: schema.GroupVersion{
				Group:   APIGroup,
				Version: APIVersionStr,
			},
			Kind:     rd.kind,
			Singular: rd.singular,
			Plural:   rd.plural,
		}

		switch rd.kind {
		case "Project":
			return newProjectCRUD(client, namespace, desc), nil
		case "LoadTest":
			return newLoadTestCRUD(client, namespace, desc), nil
		case "Schedule":
			return newScheduleCRUD(client, namespace, desc), nil
		case "EnvVar":
			return newEnvVarCRUD(client, namespace, desc), nil
		case "LoadZone":
			return newLoadZoneCRUD(client, namespace, desc), nil
		default:
			return nil, fmt.Errorf("k6: unknown resource kind %q", rd.kind)
		}
	}
}

// ---------------------------------------------------------------------------
// TypedCRUD constructors
// ---------------------------------------------------------------------------

func newProjectCRUD(c *Client, ns string, desc resources.Descriptor) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[Project]{
		ListFn: c.ListProjects,
		GetFn: func(ctx context.Context, name string) (*Project, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid project ID %q: %w", name, err)
			}
			return c.GetProject(ctx, id)
		},
		CreateFn: func(ctx context.Context, p *Project) (*Project, error) {
			return c.CreateProject(ctx, p.Name)
		},
		UpdateFn: func(ctx context.Context, name string, p *Project) (*Project, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid project ID %q: %w", name, err)
			}
			if err := c.UpdateProject(ctx, id, p.Name); err != nil {
				return nil, err
			}
			return c.GetProject(ctx, id)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, err := strconv.Atoi(name)
			if err != nil {
				return fmt.Errorf("k6: invalid project ID %q: %w", name, err)
			}
			return c.DeleteProject(ctx, id)
		},
		Namespace:   ns,
		StripFields: []string{"id"},
		Descriptor:  desc,
	}
	return crud.AsAdapter()
}

func newLoadTestCRUD(c *Client, ns string, desc resources.Descriptor) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[LoadTest]{
		ListFn: c.ListLoadTests,
		GetFn: func(ctx context.Context, name string) (*LoadTest, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid load test ID %q: %w", name, err)
			}
			return c.GetLoadTest(ctx, id)
		},
		CreateFn: func(ctx context.Context, lt *LoadTest) (*LoadTest, error) {
			return c.CreateLoadTest(ctx, lt.Name, lt.ProjectID, lt.Script)
		},
		UpdateFn: func(ctx context.Context, name string, lt *LoadTest) (*LoadTest, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid load test ID %q: %w", name, err)
			}
			if err := c.UpdateLoadTest(ctx, id, lt.Name, lt.Script); err != nil {
				return nil, err
			}
			return c.GetLoadTest(ctx, id)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, err := strconv.Atoi(name)
			if err != nil {
				return fmt.Errorf("k6: invalid load test ID %q: %w", name, err)
			}
			return c.DeleteLoadTest(ctx, id)
		},
		Namespace:   ns,
		StripFields: []string{"id"},
		Descriptor:  desc,
	}
	return crud.AsAdapter()
}

func newScheduleCRUD(c *Client, ns string, desc resources.Descriptor) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[Schedule]{
		ListFn: c.ListSchedules,
		GetFn: func(ctx context.Context, name string) (*Schedule, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid schedule ID %q: %w", name, err)
			}
			return c.GetSchedule(ctx, id)
		},
		CreateFn: func(ctx context.Context, s *Schedule) (*Schedule, error) {
			req := ScheduleRequest{
				Starts:         s.Starts,
				RecurrenceRule: s.RecurrenceRule,
			}
			return c.CreateSchedule(ctx, s.LoadTestID, req)
		},
		UpdateFn: func(ctx context.Context, name string, s *Schedule) (*Schedule, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid schedule ID %q: %w", name, err)
			}
			req := ScheduleRequest{
				Starts:         s.Starts,
				RecurrenceRule: s.RecurrenceRule,
			}
			return c.UpdateScheduleByID(ctx, id, req)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, err := strconv.Atoi(name)
			if err != nil {
				return fmt.Errorf("k6: invalid schedule ID %q: %w", name, err)
			}
			// Get the schedule first to find its load test ID for deletion.
			s, err := c.GetSchedule(ctx, id)
			if err != nil {
				return err
			}
			return c.DeleteScheduleByLoadTest(ctx, s.LoadTestID)
		},
		Namespace:   ns,
		StripFields: []string{"id"},
		Descriptor:  desc,
	}
	return crud.AsAdapter()
}

func newEnvVarCRUD(c *Client, ns string, desc resources.Descriptor) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[EnvVar]{
		ListFn: c.ListEnvVars,
		GetFn: func(ctx context.Context, name string) (*EnvVar, error) {
			// EnvVars don't have a single-get endpoint; list-then-filter.
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid env var ID %q: %w", name, err)
			}
			envVars, err := c.ListEnvVars(ctx)
			if err != nil {
				return nil, err
			}
			for _, ev := range envVars {
				if ev.ID == id {
					return &ev, nil
				}
			}
			return nil, fmt.Errorf("k6: env var %d not found", id)
		},
		CreateFn: func(ctx context.Context, ev *EnvVar) (*EnvVar, error) {
			return c.CreateEnvVar(ctx, ev.Name, ev.Value, ev.Description)
		},
		UpdateFn: func(ctx context.Context, name string, ev *EnvVar) (*EnvVar, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid env var ID %q: %w", name, err)
			}
			if err := c.UpdateEnvVar(ctx, id, ev.Name, ev.Value, ev.Description); err != nil {
				return nil, err
			}
			// Re-fetch after update.
			envVars, err := c.ListEnvVars(ctx)
			if err != nil {
				return nil, err
			}
			for _, e := range envVars {
				if e.ID == id {
					return &e, nil
				}
			}
			return nil, fmt.Errorf("k6: env var %d not found after update", id)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, err := strconv.Atoi(name)
			if err != nil {
				return fmt.Errorf("k6: invalid env var ID %q: %w", name, err)
			}
			return c.DeleteEnvVar(ctx, id)
		},
		Namespace:   ns,
		StripFields: []string{"id"},
		Descriptor:  desc,
	}
	return crud.AsAdapter()
}

func newLoadZoneCRUD(c *Client, ns string, desc resources.Descriptor) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[LoadZone]{
		ListFn: c.ListLoadZones,
		GetFn: func(ctx context.Context, name string) (*LoadZone, error) {
			// List-then-filter by name.
			zones, err := c.ListLoadZones(ctx)
			if err != nil {
				return nil, err
			}
			for _, lz := range zones {
				if lz.Name == name {
					return &lz, nil
				}
			}
			return nil, fmt.Errorf("k6: load zone %q not found", name)
		},
		// CreateFn and UpdateFn are nil — PLZ creation uses a different request type
		// and is exposed via CLI commands, not the generic resource adapter.
		DeleteFn: func(ctx context.Context, name string) error {
			return c.DeleteLoadZone(ctx, name)
		},
		Namespace:   ns,
		StripFields: []string{"id"},
		Descriptor:  desc,
	}
	return crud.AsAdapter()
}

// ---------------------------------------------------------------------------
// Schema and Example helpers
// ---------------------------------------------------------------------------

// mustMarshalJSON marshals v to JSON or panics. Used for static schema/example data.
func mustMarshalJSON(label string, v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("k6: failed to marshal %s: %v", label, err))
	}
	return b
}

// resourceSchema builds a JSON Schema for a k6 resource with the given kind and spec properties.
func resourceSchema(kind string, specProps map[string]any, specRequired []string) json.RawMessage {
	spec := map[string]any{
		"type":       "object",
		"properties": specProps,
	}
	if len(specRequired) > 0 {
		spec["required"] = specRequired
	}
	return mustMarshalJSON(kind+" schema", map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/k6/" + kind,
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": APIVersion},
			"kind":       map[string]any{"type": "string", "const": kind},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string"},
					"namespace": map[string]any{"type": "string"},
				},
			},
			"spec": spec,
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	})
}

// resourceExample builds an example manifest for a k6 resource.
func resourceExample(kind string, name string, spec map[string]any) json.RawMessage {
	return mustMarshalJSON(kind+" example", map[string]any{
		"apiVersion": APIVersion,
		"kind":       kind,
		"metadata":   map[string]any{"name": name},
		"spec":       spec,
	})
}

func projectSchema() json.RawMessage {
	return resourceSchema(Kind, map[string]any{
		"name":               map[string]any{"type": "string"},
		"is_default":         map[string]any{"type": "boolean"},
		"grafana_folder_uid": map[string]any{"type": "string"},
	}, []string{"name"})
}

func projectExample() json.RawMessage {
	return resourceExample(Kind, "12345", map[string]any{
		"name":       "my-project",
		"is_default": false,
	})
}

func loadTestSchema() json.RawMessage {
	return resourceSchema("LoadTest", map[string]any{
		"name":       map[string]any{"type": "string"},
		"project_id": map[string]any{"type": "integer"},
		"script":     map[string]any{"type": "string"},
	}, []string{"name", "project_id"})
}

func loadTestExample() json.RawMessage {
	return resourceExample("LoadTest", "12345", map[string]any{
		"name":       "my-load-test",
		"project_id": 1,
		"script":     "export default function() {}",
	})
}

func scheduleSchema() json.RawMessage {
	return resourceSchema("Schedule", map[string]any{
		"load_test_id": map[string]any{"type": "integer"},
		"starts":       map[string]any{"type": "string"},
		"recurrence_rule": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"frequency": map[string]any{"type": "string"},
				"interval":  map[string]any{"type": "integer"},
			},
		},
		"deactivated": map[string]any{"type": "boolean"},
	}, []string{"load_test_id"})
}

func scheduleExample() json.RawMessage {
	return resourceExample("Schedule", "12345", map[string]any{
		"load_test_id": 1,
		"starts":       "2026-01-01T00:00:00Z",
		"recurrence_rule": map[string]any{
			"frequency": "DAILY",
			"interval":  1,
		},
	})
}

func envVarSchema() json.RawMessage {
	return resourceSchema("EnvVar", map[string]any{
		"name":        map[string]any{"type": "string"},
		"value":       map[string]any{"type": "string"},
		"description": map[string]any{"type": "string"},
	}, []string{"name", "value"})
}

func envVarExample() json.RawMessage {
	return resourceExample("EnvVar", "12345", map[string]any{
		"name":        "MY_VAR",
		"value":       "my-value",
		"description": "Example environment variable",
	})
}

func loadZoneSchema() json.RawMessage {
	return resourceSchema("LoadZone", map[string]any{
		"name":            map[string]any{"type": "string"},
		"k6_load_zone_id": map[string]any{"type": "string"},
	}, []string{"name"})
}

func loadZoneExample() json.RawMessage {
	return resourceExample("LoadZone", "my-zone", map[string]any{
		"name":            "my-zone",
		"k6_load_zone_id": "my-zone-id",
	})
}

// ---------------------------------------------------------------------------
// NewTypedCRUD factories for CLI commands
// ---------------------------------------------------------------------------

// NewTypedCRUD[Project] creates a TypedCRUD for K6 projects.
func NewTypedCRUDProject(ctx context.Context, loader CloudConfigLoader) (*adapter.TypedCRUD[Project], string, error) {
	client, ns, err := authenticatedClient(ctx, loader)
	if err != nil {
		return nil, "", err
	}
	crud := &adapter.TypedCRUD[Project]{
		ListFn: client.ListProjects,
		GetFn: func(ctx context.Context, name string) (*Project, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid project ID %q: %w", name, err)
			}
			return client.GetProject(ctx, id)
		},
		CreateFn: func(ctx context.Context, p *Project) (*Project, error) {
			return client.CreateProject(ctx, p.Name)
		},
		UpdateFn: func(ctx context.Context, name string, p *Project) (*Project, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid project ID %q: %w", name, err)
			}
			if err := client.UpdateProject(ctx, id, p.Name); err != nil {
				return nil, err
			}
			return client.GetProject(ctx, id)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, err := strconv.Atoi(name)
			if err != nil {
				return fmt.Errorf("k6: invalid project ID %q: %w", name, err)
			}
			return client.DeleteProject(ctx, id)
		},
	}
	return crud, ns, nil
}

// NewTypedCRUD[LoadTest] creates a TypedCRUD for K6 load tests.
func NewTypedCRUDLoadTest(ctx context.Context, loader CloudConfigLoader) (*adapter.TypedCRUD[LoadTest], string, error) {
	client, ns, err := authenticatedClient(ctx, loader)
	if err != nil {
		return nil, "", err
	}
	crud := &adapter.TypedCRUD[LoadTest]{
		ListFn: client.ListAllLoadTests,
		GetFn: func(ctx context.Context, name string) (*LoadTest, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid load test ID %q: %w", name, err)
			}
			return client.GetLoadTest(ctx, id)
		},
		CreateFn: func(ctx context.Context, lt *LoadTest) (*LoadTest, error) {
			return client.CreateLoadTest(ctx, lt.Name, lt.ProjectID, lt.Script)
		},
		UpdateFn: func(ctx context.Context, name string, lt *LoadTest) (*LoadTest, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid load test ID %q: %w", name, err)
			}
			if err := client.UpdateLoadTest(ctx, id, lt.Name, lt.Script); err != nil {
				return nil, err
			}
			return client.GetLoadTest(ctx, id)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, err := strconv.Atoi(name)
			if err != nil {
				return fmt.Errorf("k6: invalid load test ID %q: %w", name, err)
			}
			return client.DeleteLoadTest(ctx, id)
		},
	}
	return crud, ns, nil
}

// NewTypedCRUD[Schedule] creates a TypedCRUD for K6 schedules.
func NewTypedCRUDSchedule(ctx context.Context, loader CloudConfigLoader) (*adapter.TypedCRUD[Schedule], string, error) {
	client, ns, err := authenticatedClient(ctx, loader)
	if err != nil {
		return nil, "", err
	}
	crud := &adapter.TypedCRUD[Schedule]{
		ListFn: client.ListSchedules,
		GetFn: func(ctx context.Context, name string) (*Schedule, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid schedule ID %q: %w", name, err)
			}
			return client.GetSchedule(ctx, id)
		},
		CreateFn: func(ctx context.Context, s *Schedule) (*Schedule, error) {
			req := ScheduleRequest{
				Starts:         s.Starts,
				RecurrenceRule: s.RecurrenceRule,
			}
			return client.CreateSchedule(ctx, s.LoadTestID, req)
		},
		UpdateFn: func(ctx context.Context, name string, s *Schedule) (*Schedule, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid schedule ID %q: %w", name, err)
			}
			req := ScheduleRequest{
				Starts:         s.Starts,
				RecurrenceRule: s.RecurrenceRule,
			}
			return client.UpdateScheduleByID(ctx, id, req)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, err := strconv.Atoi(name)
			if err != nil {
				return fmt.Errorf("k6: invalid schedule ID %q: %w", name, err)
			}
			s, err := client.GetSchedule(ctx, id)
			if err != nil {
				return err
			}
			return client.DeleteScheduleByLoadTest(ctx, s.LoadTestID)
		},
	}
	return crud, ns, nil
}

// NewTypedCRUD[EnvVar] creates a TypedCRUD for K6 environment variables.
func NewTypedCRUDEnvVar(ctx context.Context, loader CloudConfigLoader) (*adapter.TypedCRUD[EnvVar], string, error) {
	client, ns, err := authenticatedClient(ctx, loader)
	if err != nil {
		return nil, "", err
	}
	crud := &adapter.TypedCRUD[EnvVar]{
		ListFn: client.ListEnvVars,
		GetFn: func(ctx context.Context, name string) (*EnvVar, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid env var ID %q: %w", name, err)
			}
			envVars, err := client.ListEnvVars(ctx)
			if err != nil {
				return nil, err
			}
			for _, ev := range envVars {
				if ev.ID == id {
					return &ev, nil
				}
			}
			return nil, fmt.Errorf("k6: env var %d not found", id)
		},
		CreateFn: func(ctx context.Context, ev *EnvVar) (*EnvVar, error) {
			return client.CreateEnvVar(ctx, ev.Name, ev.Value, ev.Description)
		},
		UpdateFn: func(ctx context.Context, name string, ev *EnvVar) (*EnvVar, error) {
			id, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("k6: invalid env var ID %q: %w", name, err)
			}
			if err := client.UpdateEnvVar(ctx, id, ev.Name, ev.Value, ev.Description); err != nil {
				return nil, err
			}
			envVars, err := client.ListEnvVars(ctx)
			if err != nil {
				return nil, err
			}
			for _, e := range envVars {
				if e.ID == id {
					return &e, nil
				}
			}
			return nil, fmt.Errorf("k6: env var %d not found after update", id)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			id, err := strconv.Atoi(name)
			if err != nil {
				return fmt.Errorf("k6: invalid env var ID %q: %w", name, err)
			}
			return client.DeleteEnvVar(ctx, id)
		},
	}
	return crud, ns, nil
}

// NewTypedCRUD[LoadZone] creates a TypedCRUD for K6 load zones.
func NewTypedCRUDLoadZone(ctx context.Context, loader CloudConfigLoader) (*adapter.TypedCRUD[LoadZone], string, error) {
	client, ns, err := authenticatedClient(ctx, loader)
	if err != nil {
		return nil, "", err
	}
	crud := &adapter.TypedCRUD[LoadZone]{
		ListFn: client.ListLoadZones,
		GetFn: func(ctx context.Context, name string) (*LoadZone, error) {
			zones, err := client.ListLoadZones(ctx)
			if err != nil {
				return nil, err
			}
			for _, lz := range zones {
				if lz.Name == name {
					return &lz, nil
				}
			}
			return nil, fmt.Errorf("k6: load zone %q not found", name)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			return client.DeleteLoadZone(ctx, name)
		},
	}
	return crud, ns, nil
}
