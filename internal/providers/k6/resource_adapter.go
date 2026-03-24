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
	aliases  []string
	schema   json.RawMessage
	example  json.RawMessage
}

// allResources returns the definitions for all k6 resource types.
func allResources() []resourceDef {
	return []resourceDef{
		{
			kind: "Project", singular: "project", plural: "projects",
			aliases: []string{"k6projects", "k6project", "k6proj"},
			schema:  projectSchema(),
			example: projectExample(),
		},
		{
			kind: "LoadTest", singular: "loadtest", plural: "loadtests",
			aliases: []string{"k6loadtests", "k6loadtest", "k6lt"},
		},
		{
			kind: "Schedule", singular: "schedule", plural: "schedules",
			aliases: []string{"k6schedules", "k6schedule", "k6sched"},
		},
		{
			kind: "EnvVar", singular: "envvar", plural: "envvars",
			aliases: []string{"k6envvars", "k6envvar", "k6env"},
		},
		{
			kind: "LoadZone", singular: "loadzone", plural: "loadzones",
			aliases: []string{"k6loadzones", "k6loadzone", "k6lz"},
		},
	}
}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	loader := &providers.ConfigLoader{}
	for _, rd := range allResources() {
		desc := resources.Descriptor{
			GroupVersion: schema.GroupVersion{
				Group:   APIGroup,
				Version: APIVersionStr,
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

	client := NewClient(domain)
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
			return newProjectCRUD(client, namespace, desc, rd.aliases), nil
		case "LoadTest":
			return newLoadTestCRUD(client, namespace, desc, rd.aliases), nil
		case "Schedule":
			return newScheduleCRUD(client, namespace, desc, rd.aliases), nil
		case "EnvVar":
			return newEnvVarCRUD(client, namespace, desc, rd.aliases), nil
		case "LoadZone":
			return newLoadZoneCRUD(client, namespace, desc, rd.aliases), nil
		default:
			return nil, fmt.Errorf("k6: unknown resource kind %q", rd.kind)
		}
	}
}

// ---------------------------------------------------------------------------
// TypedCRUD constructors
// ---------------------------------------------------------------------------

func newProjectCRUD(c *Client, ns string, desc resources.Descriptor, aliases []string) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[Project]{
		NameFn: func(p Project) string { return strconv.Itoa(p.ID) },
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
		Namespace:     ns,
		StripFields:   []string{"id"},
		RestoreNameFn: func(name string, p *Project) { p.ID, _ = strconv.Atoi(name) },
		Descriptor:    desc,
		Aliases:       aliases,
	}
	return crud.AsAdapter()
}

func newLoadTestCRUD(c *Client, ns string, desc resources.Descriptor, aliases []string) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[LoadTest]{
		NameFn: func(lt LoadTest) string { return strconv.Itoa(lt.ID) },
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
		Namespace:     ns,
		StripFields:   []string{"id"},
		RestoreNameFn: func(name string, lt *LoadTest) { lt.ID, _ = strconv.Atoi(name) },
		Descriptor:    desc,
		Aliases:       aliases,
	}
	return crud.AsAdapter()
}

func newScheduleCRUD(c *Client, ns string, desc resources.Descriptor, aliases []string) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[Schedule]{
		NameFn: func(s Schedule) string { return strconv.Itoa(s.ID) },
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
		Namespace:     ns,
		StripFields:   []string{"id"},
		RestoreNameFn: func(name string, s *Schedule) { s.ID, _ = strconv.Atoi(name) },
		Descriptor:    desc,
		Aliases:       aliases,
	}
	return crud.AsAdapter()
}

func newEnvVarCRUD(c *Client, ns string, desc resources.Descriptor, aliases []string) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[EnvVar]{
		NameFn: func(ev EnvVar) string { return strconv.Itoa(ev.ID) },
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
		Namespace:     ns,
		StripFields:   []string{"id"},
		RestoreNameFn: func(name string, ev *EnvVar) { ev.ID, _ = strconv.Atoi(name) },
		Descriptor:    desc,
		Aliases:       aliases,
	}
	return crud.AsAdapter()
}

func newLoadZoneCRUD(c *Client, ns string, desc resources.Descriptor, aliases []string) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[LoadZone]{
		NameFn: func(lz LoadZone) string { return lz.Name },
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
		RestoreNameFn: func(name string, lz *LoadZone) {
			lz.Name = name
			lz.ID, _ = strconv.Atoi(name)
		},
		Descriptor: desc,
		Aliases:    aliases,
	}
	return crud.AsAdapter()
}

// ---------------------------------------------------------------------------
// Schema and Example helpers
// ---------------------------------------------------------------------------

// projectSchema returns a JSON Schema for the Project resource type.
func projectSchema() json.RawMessage {
	s := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/k6/Project",
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": APIVersion},
			"kind":       map[string]any{"type": "string", "const": Kind},
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
					"name":               map[string]any{"type": "string"},
					"is_default":         map[string]any{"type": "boolean"},
					"grafana_folder_uid": map[string]any{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("k6: failed to marshal schema: %v", err))
	}
	return b
}

// projectExample returns an example Project manifest as JSON.
func projectExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "12345",
		},
		"spec": map[string]any{
			"name":       "my-project",
			"is_default": false,
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("k6: failed to marshal example: %v", err))
	}
	return b
}
