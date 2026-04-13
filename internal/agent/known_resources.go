package agent

// OperationHint describes agent metadata for a single resource operation.
type OperationHint struct {
	TokenCost string // "small", "medium", "large"
	LLMHint   string // example command for this operation
}

// KnownResource describes a well-known Grafana K8s resource type with agent metadata.
// These types are standard across all Grafana instances and can be annotated statically.
type KnownResource struct {
	Kind       string
	Group      string
	Version    string
	Aliases    []string
	Operations map[string]OperationHint // keyed by operation: "get", "push", "pull", "delete"
}

// resourceBuilder accumulates configuration for a KnownResource.
type resourceBuilder struct {
	kind      string
	group     string
	version   string
	aliases   []string
	ops       []string          // which operations this resource supports
	overrides map[string]string // operation -> token_cost override
}

// resource starts building a KnownResource with the given identity.
func resource(kind, group, version string, aliases ...string) *resourceBuilder {
	return &resourceBuilder{
		kind:      kind,
		group:     group,
		version:   version,
		aliases:   aliases,
		overrides: map[string]string{},
	}
}

// readOnly marks the resource as supporting only get (the default).
func (b *resourceBuilder) readOnly() *resourceBuilder {
	b.ops = []string{"get"}
	return b
}

// crud marks the resource as supporting get, push, pull, and delete.
func (b *resourceBuilder) crud() *resourceBuilder {
	b.ops = []string{"get", "push", "pull", "delete"}
	return b
}

// withOps sets exactly which operations this resource supports.
func (b *resourceBuilder) withOps(ops ...string) *resourceBuilder {
	b.ops = ops
	return b
}

// cost overrides the default token cost for a specific operation.
func (b *resourceBuilder) cost(op, cost string) *resourceBuilder {
	b.overrides[op] = cost
	return b
}

// build produces the final KnownResource with default hints derived from aliases.
func (b *resourceBuilder) build() KnownResource {
	if len(b.ops) == 0 {
		b.ops = []string{"get"} // default: read-only
	}

	if len(b.aliases) == 0 {
		panic("resource " + b.kind + " must have at least one alias")
	}
	plural := b.aliases[0] // first alias is the plural form

	// Default token costs per operation.
	defaultCosts := map[string]string{
		"get":    "medium",
		"push":   "small",
		"pull":   "medium",
		"delete": "small",
	}

	// Default LLM hints per operation, derived from plural name.
	defaultHints := map[string]string{
		"get":    "gcx resources get " + plural + " -o json",
		"push":   "gcx resources push -p ./" + plural,
		"pull":   "gcx resources pull " + plural + " -p ./" + plural,
		"delete": "gcx resources delete " + plural + "/NAME --dry-run",
	}

	ops := make(map[string]OperationHint, len(b.ops))
	for _, op := range b.ops {
		cost := defaultCosts[op]
		if override, ok := b.overrides[op]; ok {
			cost = override
		}
		ops[op] = OperationHint{
			TokenCost: cost,
			LLMHint:   defaultHints[op],
		}
	}

	return KnownResource{
		Kind:       b.kind,
		Group:      b.group,
		Version:    b.version,
		Aliases:    b.aliases,
		Operations: ops,
	}
}

// KnownResources is the static registry of well-known Grafana K8s resource types
// that are NOT backed by provider adapters. Provider-backed types (SLO, OnCall, Fleet,
// k6, KG, Incidents, Alert, Synth) are already registered via adapter.AllRegistrations().
//
// This list was verified against a live Grafana 13.0 stack.
var KnownResources = []KnownResource{ //nolint:gochecknoglobals // Static registry, same pattern as providers.registry.
	// Core Grafana resources
	resource("Dashboard", "dashboard.grafana.app", "v1beta1", "dashboards", "dashboard", "dash").crud().cost("get", "large").cost("pull", "large").build(),
	resource("Folder", "folder.grafana.app", "v1beta1", "folders", "folder").crud().build(),
	resource("Playlist", "playlist.grafana.app", "v0alpha1", "playlists").readOnly().build(),
	resource("Preferences", "preferences.grafana.app", "v1alpha1", "preferences").readOnly().build(),
	resource("ShortURL", "shorturl.grafana.app", "v1beta1", "shorturls").readOnly().build(),
	resource("Snapshot", "dashboard.grafana.app", "v0alpha1", "snapshots").readOnly().build(),

	// Alerting
	resource("AlertRule", "rules.alerting.grafana.app", "v0alpha1", "alertrules").crud().build(),
	resource("RecordingRule", "rules.alerting.grafana.app", "v0alpha1", "recordingrules").withOps("get", "push", "pull").build(),
	resource("AlertEnrichment", "alertenrichment.grafana.app", "v1beta1", "alert-enrichments").readOnly().build(),

	// Advisor
	resource("Check", "advisor.grafana.app", "v0alpha1", "checks").readOnly().build(),
	resource("CheckType", "advisor.grafana.app", "v0alpha1", "checktypes").readOnly().cost("get", "small").build(),

	// Provisioning
	resource("Repository", "provisioning.grafana.app", "v0alpha1", "repositories").withOps("get", "push").build(),
	resource("Job", "provisioning.grafana.app", "v0alpha1", "jobs").readOnly().build(),
	resource("Connection", "provisioning.grafana.app", "v0alpha1", "connections").readOnly().cost("get", "small").build(),

	// Plugins
	resource("Plugin", "plugins.grafana.app", "v0alpha1", "plugins").readOnly().build(),

	// Secrets
	resource("SecureValue", "secret.grafana.app", "v1beta1", "securevalues").withOps("get", "push").cost("get", "small").build(),
	resource("Keeper", "secret.grafana.app", "v1beta1", "keepers").readOnly().cost("get", "small").build(),

	// Queries
	resource("Query", "queries.grafana.app", "v1beta1", "queries").readOnly().build(),

	// Sandbox
	resource("SandboxSettings", "sandboxsettings.grafana.app", "v0alpha1", "sandbox-settings").readOnly().cost("get", "small").build(),

	// Banners
	resource("AnnouncementBanner", "banners.grafana.app", "v0alpha1", "announcement-banners").withOps("get", "push").cost("get", "small").build(),
}
