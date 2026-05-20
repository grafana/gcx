package signals_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/signals"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestCommandBuildsSignalTree(t *testing.T) {
	desc := signals.Descriptor{
		Name:  "metrics",
		Short: "Query metrics",
		Commands: []signals.CommandSpec{
			{
				Build: func(*providers.ConfigLoader) *cobra.Command {
					return &cobra.Command{Use: "query"}
				},
				TokenCost: "medium",
				LLMHint:   "gcx metrics query -o json",
				Example:   "example text",
			},
		},
		ExtraCommands: []signals.CommandBuilder{
			func(*providers.ConfigLoader) *cobra.Command { return &cobra.Command{Use: "billing"} },
		},
		Adaptive: &signals.AdaptiveSpec{
			Build: func(*providers.ConfigLoader) *cobra.Command { return &cobra.Command{Use: "metrics"} },
			Use:   "adaptive",
			Short: "Manage Adaptive Metrics resources",
		},
	}

	cmd := signals.Command(desc)
	require.Equal(t, "metrics", cmd.Use)
	require.Equal(t, "Query metrics", cmd.Short)
	require.NotNil(t, cmd.PersistentPreRun)
	require.NotNil(t, cmd.PersistentFlags().Lookup("config"))

	query, _, err := cmd.Find([]string{"query"})
	require.NoError(t, err)
	assert.Equal(t, "example text", query.Example)
	assert.Equal(t, "medium", query.Annotations[agent.AnnotationTokenCost])
	assert.Equal(t, "gcx metrics query -o json", query.Annotations[agent.AnnotationLLMHint])

	billing, _, err := cmd.Find([]string{"billing"})
	require.NoError(t, err)
	assert.Equal(t, "billing", billing.Use)

	adaptive, _, err := cmd.Find([]string{"adaptive"})
	require.NoError(t, err)
	assert.Equal(t, "adaptive", adaptive.Use)
	assert.Equal(t, "Manage Adaptive Metrics resources", adaptive.Short)
}

func TestDatasourceProviderBuildsCommands(t *testing.T) {
	provider := signals.DatasourceProvider(
		"prometheus",
		"Query Prometheus datasources",
		func(*providers.ConfigLoader) *cobra.Command { return &cobra.Command{Use: "query"} },
		func(*providers.ConfigLoader) *cobra.Command { return &cobra.Command{Use: "labels"} },
		nil,
		func(*providers.ConfigLoader) *cobra.Command { return &cobra.Command{Use: "metadata"} },
	)

	assert.Equal(t, "prometheus", provider.Kind())
	assert.Equal(t, "Query Prometheus datasources", provider.ShortDesc())
	assert.Equal(t, "query", provider.QueryCmd(&providers.ConfigLoader{}).Use)

	extra := provider.ExtraCommands(&providers.ConfigLoader{})
	require.Len(t, extra, 2)
	assert.Equal(t, "labels", extra[0].Use)
	assert.Equal(t, "metadata", extra[1].Use)
}

func TestDescriptorTypedRegistrations(t *testing.T) {
	desc := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "example.grafana.app", Version: "v1alpha1"},
		Kind:         "Example",
		Singular:     "example",
		Plural:       "examples",
	}

	signal := signals.Descriptor{
		Registrations: func(*providers.ConfigLoader) []adapter.Registration {
			return []adapter.Registration{{Descriptor: desc, GVK: desc.GroupVersionKind()}}
		},
	}

	regs := signal.TypedRegistrations()
	require.Len(t, regs, 1)
	assert.Equal(t, desc.GroupVersionKind(), regs[0].GVK)

	assert.Nil(t, signals.Descriptor{}.TypedRegistrations())
}
