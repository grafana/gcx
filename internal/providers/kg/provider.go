package kg

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

// Note: package init() is only in provider.go (calls providers.Register).
// Resource adapter registrations are in TypedRegistrations().

var _ providers.Provider = &KGProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&KGProvider{})
}

// KGProvider manages Grafana Knowledge Graph resources.
type KGProvider struct{}

// Name returns the unique identifier for this provider.
func (p *KGProvider) Name() string { return "kg" }

// ShortDesc returns a one-line description of the provider.
func (p *KGProvider) ShortDesc() string {
	return "Query and configure the Knowledge Graph: entities of any type, how they relate (what calls, depends on or affects what), environments and namespaces, and insights"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *KGProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	kgCmd := &cobra.Command{
		Use:     "kg",
		Short:   p.ShortDesc(),
		Aliases: []string{"knowledge-graph"},
		Long: `The Knowledge Graph is a graph of entities and the relationships between
them, with the insights (alerts and anomalies) attached to them. What entity
and relationship types exist depends on the stack; look them up with
'meta schema' instead of assuming.

Read it to answer what exists and how it is connected:
  - what an entity is connected to: who calls it, what it depends on, what a
    failure would affect, the path between two entities
  - how many entities there are, of which types, and which are in an env,
    namespace or site
  - which entity types, properties and relationship types exist, and which
    env, namespace and site values the stack has
  - which entities have active insights

It does not hold time series, log lines or spans: use 'gcx metrics',
'gcx logs' and 'gcx traces' for measured values and events, and
'gcx datasources' to list data sources.

Time window: graph reads (graph query, entities, stats, meta schema and meta
scopes) cover the last hour by default, so on older data an empty or
near-empty result means the window is wrong, not that nothing exists. Pass
--from/--to (or --since) to the command.

Read:
  graph query  what calls or depends on an entity, what a failure would
               affect, paths between entities, counts (read-only Cypher)
  entities     list entities of one type (--type is required), optionally
               within an env, namespace or site, with their properties and
               active insights (--insight); correlate maps an alert to its
               entity
  meta         entity and relationship types (meta schema), env/namespace/site
               values (meta scopes), and the entity-property to metric, log,
               trace and profile label mappings used to build queries (meta
               metrics|logs|traces|profiles)
  stats        entity and active-insight counts

Configure: prom-rules, model-rules, thresholds, suppressions and
notifications change how the graph is built and when it raises insights.
relabel-rules shows the relabel rules in effect (read-only). Custom entities
and relationships are written with 'entities upsert|delete' and
'relationships upsert|delete' (experimental).`,
	}

	loader.BindFlags(kgCmd.PersistentFlags())

	kgCmd.AddCommand(
		newStatusCommand(loader),
		newSchemasCommand(loader),
		// Configuration upload
		newRulesCommand(loader),
		newThresholdsCommand(loader),
		newModelRulesCommand(loader),
		newSuppressionsCommand(loader),
		newNotificationsCommand(loader),
		newRelabelRulesCommand(loader),
		// Entities
		newEntitiesCommand(loader),
		newGraphCommand(loader),
		// Relationships (custom, API-origin)
		newRelationshipsCommand(loader),
		// Assertions
		newAssertionsCommand(loader),
		newDescribeCommand(loader),
		// High-level
		newSummaryCommand(loader),
		newDiagnoseCommand(loader),
		newQualityCommand(loader),
		newOpenCommand(loader),
	)

	return []*cobra.Command{kgCmd}
}

// Validate checks that the given provider configuration is valid.
func (p *KGProvider) Validate(_ map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *KGProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for KG resource types.
func (p *KGProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    NewAdapterFactory(loader),
			Descriptor: staticDescriptor,
			GVK:        staticDescriptor.GroupVersionKind(),
			Schema:     RuleSchema,
			Example:    RuleExample(),
		},
		{
			Factory:    NewScopeAdapterFactory(loader),
			Descriptor: scopeDescriptor,
			GVK:        scopeDescriptor.GroupVersionKind(),
			Schema:     ScopeSchema,
		},
		{
			Factory:    NewModelRulesAdapterFactory(loader),
			Descriptor: modelRulesDescriptor,
			GVK:        modelRulesDescriptor.GroupVersionKind(),
			Schema:     ModelRulesSchema,
		},
	}
}
