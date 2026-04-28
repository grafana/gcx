package metrics

import (
	"github.com/grafana/gcx/internal/agent"
	dsprometheus "github.com/grafana/gcx/internal/datasources/prometheus"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// BillingDatasourceUID is the name of the billing-metrics Prometheus datasource
// auto-provisioned on every Grafana Cloud stack.
const BillingDatasourceUID = "grafanacloud-usage"

// BillingCommands returns the `billing` subcommand group that exposes Grafana
// Cloud billing (grafanacloud_*) metrics via the pre-provisioned
// grafanacloud-usage datasource.
func BillingCommands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "billing",
		Short: "Query Grafana Cloud billing metrics (grafanacloud_*)",
		Long: `Query Grafana Cloud billing metrics via the grafanacloud-usage datasource
that ships pre-provisioned on every Grafana Cloud stack.

These commands are thin conveniences over the generic metrics subcommands;
the --datasource flag defaults to "grafanacloud-usage" but can be overridden.`,
	}

	cmd.AddCommand(billingQueryCmd(loader), billingLabelsCmd(loader), billingSeriesCmd(loader))

	return cmd
}

func billingQueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	c := dsprometheus.QueryCmdWithDefault(loader, BillingDatasourceUID)
	c.Short = "Execute a PromQL query against billing metrics"
	c.Example = `
  # Active series right now
  gcx metrics billing query 'grafanacloud_instance_active_series'

  # Active series over the last hour
  gcx metrics billing query 'grafanacloud_instance_active_series' --since 1h --step 1m

  # Print a Grafana Explore share link for the executed query
  gcx metrics billing query 'grafanacloud_instance_active_series' --share-link

  # Output as JSON
  gcx metrics billing query 'grafanacloud_instance_active_series' -o json`
	c.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx metrics billing query 'grafanacloud_instance_active_series' --since 1h -o json`,
	}
	return c
}

func billingLabelsCmd(loader *providers.ConfigLoader) *cobra.Command {
	c := dsprometheus.LabelsCmdWithDefault(loader, BillingDatasourceUID)
	c.Short = "List label names available on billing metrics"
	c.Example = `
  # All billing label names
  gcx metrics billing labels

  # Values for a single label
  gcx metrics billing labels --label product

  # Output as JSON
  gcx metrics billing labels -o json`
	c.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
	}
	return c
}

func billingSeriesCmd(loader *providers.ConfigLoader) *cobra.Command {
	c := newSeriesCmd(loader, BillingDatasourceUID)
	c.Short = "List billing time series matching a selector"
	c.Example = `
  # All billing series
  gcx metrics billing series '{__name__=~"grafanacloud_.*"}' --since 1h

  # Filter to a specific product
  gcx metrics billing series '{__name__=~"grafanacloud_.*",product="metrics"}' --since 1h

  # Output as JSON
  gcx metrics billing series '{__name__=~"grafanacloud_.*"}' --since 1h -o json`
	c.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx metrics billing series '{__name__=~"grafanacloud_.*"}' --since 1h -o json`,
	}
	return c
}
