package metrics

import (
	"github.com/grafana/gcx/internal/agent"
	dsprometheus "github.com/grafana/gcx/internal/datasources/prometheus"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// SearchCommands returns the `search` subcommand group exposing the
// experimental Prometheus/Mimir search API under the cross-signal `gcx
// metrics` family: metric-names, label-names, label-values.
//
// These three leaf names don't pass the canonical-verb naming gate as
// children of a group (only "search-*" flat leaves would); all three full
// paths are grandfathered in
// cmd/gcx/root/testdata/non_canonical_command_operations.json as a
// deliberate, maintainer-requested exception, not an oversight.
func SearchCommands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "[experimental] Search for metric names, label names or label values",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Search for metric names, label names or label values via the experimental search API.

This API is experimental and disabled by default on both self-hosted
Prometheus (requires --enable-feature=search-api) and self-hosted Mimir
(requires -querier.experimental-search-api-enabled).`,
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
		},
	}

	cmd.AddCommand(searchMetricNamesCmd(loader), searchLabelNamesCmd(loader), searchLabelValuesCmd(loader))

	return cmd
}

// The leaves reuse the datasources/prometheus constructors, keeping their
// flags, validation and annotations, and only rewrite the name, examples and
// LLM hint to the metrics path.

func searchMetricNamesCmd(loader *providers.ConfigLoader) *cobra.Command {
	c := dsprometheus.SearchMetricNamesCmd(loader)
	c.Use = "metric-names TERM..."
	c.Example = `
  # Fuzzy search metric names (configured default datasource)
  gcx metrics search metric-names http

  # Search metric names seen in the last 7 days (default: the last hour)
  gcx metrics search metric-names http --since 7d

  # Refine fuzzy search algorithm
  gcx metrics search metric-names http --fuzz-alg=subsequence --fuzz-threshold=85

  # Limit result sets and control ordering
  gcx metrics search metric-names http --limit=10 --sort-by=alpha

  # Include relevance score and metric metadata
  gcx metrics search metric-names http --include-score --include-metadata

  # Output as JSON
  gcx metrics search metric-names http -o json`
	c.Annotations[agent.AnnotationLLMHint] = "gcx metrics search metric-names TERM -o json"
	return c
}

func searchLabelNamesCmd(loader *providers.ConfigLoader) *cobra.Command {
	c := dsprometheus.SearchLabelNamesCmd(loader)
	c.Use = "label-names [TERM...]"
	c.Example = `
  # Fuzzy search label names (configured default datasource)
  gcx metrics search label-names job

  # Show label names available on a given metric
  gcx metrics search label-names --metric http_requests_total

  # Show label names on series matching a selector
  gcx metrics search label-names --match '{job="api"}'

  # Search for label names on a given metric
  gcx metrics search label-names namespace --metric http_requests_total

  # Search for label names across a range of metrics
  gcx metrics search label-names namespace --metric-regex '.*kube.*'

  # Search label names seen in the last 7 days (default: the last hour)
  gcx metrics search label-names job --since 7d

  # Output as JSON
  gcx metrics search label-names job -o json`
	c.Annotations[agent.AnnotationLLMHint] = "gcx metrics search label-names TERM -o json"
	return c
}

func searchLabelValuesCmd(loader *providers.ConfigLoader) *cobra.Command {
	c := dsprometheus.SearchLabelValuesCmd(loader)
	c.Use = "label-values LABEL [TERM...]"
	c.Example = `
  # List values of the "job" label (configured default datasource)
  gcx metrics search label-values job

  # Fuzzy search values of the "job" label
  gcx metrics search label-values job pro

  # List "job" label values present on a specific metric
  gcx metrics search label-values job --metric http_requests_total

  # List "job" label values present on a range of metrics
  gcx metrics search label-values job --metric-regex '.*kube.*'

  # List "job" label values seen in the last 7 days (default: the last hour)
  gcx metrics search label-values job --since 7d

  # Output as JSON
  gcx metrics search label-values job pro -o json`
	c.Annotations[agent.AnnotationLLMHint] = "gcx metrics search label-values LABEL TERM -o json"
	return c
}
