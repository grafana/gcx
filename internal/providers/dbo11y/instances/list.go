package instances

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// instancesListDefaultLimit caps the row count of the default `instances list`
// view. Mirrors appo11y services list.
const instancesListDefaultLimit = 50

type listOpts struct {
	IO         cmdio.Options
	Datasource string
	Filters    []string
	Limit      int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &instancesTableCodec{})
	o.IO.RegisterCustomCodec("wide", &instancesTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Restrict to instances matching a label matcher, e.g. --filter engine=postgres (repeatable)")
	flags.IntVar(&o.Limit, "limit", instancesListDefaultLimit, "Limit the number of instances returned (0 = unlimited)")
}

func (o *listOpts) Validate(cmd *cobra.Command) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.Limit < 0 {
		return fail.NewCommandUsageError(cmd, "--limit must be zero or positive", nil)
	}
	return nil
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Database Observability instances discovered from telemetry.",
		Long: `List the database instances Grafana Cloud Database Observability has discovered.

Discovery uses the database_observability_connection_info inventory metric
emitted by the database_observability.postgres Alloy component (job
"integrations/db-o11y"): one row per monitored database instance, with engine
and cloud-provider metadata.

Related: "gcx dbo11y instances get <name>" drills into one instance's health,
connections, wait events, and top queries by time share.

When no instances are found, this command checks whether Database
Observability has been activated for the stack and, if not, exits non-zero
with a hint to activate it in Grafana Cloud instead of a generic empty
result.`,
		Example: `
  # List all database instances in the current stack
  gcx dbo11y instances list

  # Filter to Postgres instances
  gcx dbo11y instances list --filter engine=postgres

  # Pin a datasource and output JSON
  gcx dbo11y instances list -d grafanacloud-prom -o json`,
		Args: cobra.NoArgs,
		RunE: runList(loader, opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Database Observability instance inventory from database_observability_connection_info: one row per monitored database (engine, version, cloud provider metadata). Pairs with 'gcx dbo11y instances get <name>' for health/connections/wait-events/top-queries. On an empty result this command also checks the stack's Database Observability activation status and, if not activated, exits 1 with a specific "not activated" hint instead of the generic empty-result message. Examples: gcx dbo11y instances list -o json; gcx dbo11y instances list --filter engine=postgres -o json`,
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runList(loader *providers.ConfigLoader, opts *listOpts) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		matchers, err := parseFilters(opts.Filters)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}

		ctx := cmd.Context()

		cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
		if err != nil {
			return err
		}

		datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "prometheus")
		if err != nil {
			return err
		}

		client, err := prometheus.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create prometheus client: %w", err)
		}

		expr, err := buildConnectionInfoQuery(matchers)
		if err != nil {
			return fmt.Errorf("failed to build instances query: %w", err)
		}
		resp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("instances query failed: %w", err)
		}

		items, err := parseInstancesResponse(resp)
		if err != nil {
			return fmt.Errorf("failed to parse instances response: %w", err)
		}

		if len(items) == 0 {
			// checkActivation is a best-effort diagnostic enrichment: it can
			// only make the "no instances" case more specific, never less —
			// an inconclusive check (activated=false, err!=nil) falls through
			// to the generic table message below rather than blocking.
			if activated, actErr := checkActivation(ctx, cfg); actErr == nil && !activated {
				cmdio.EmitWarn(cmd.ErrOrStderr(), notActivatedError().Error())
				if err := opts.IO.Encode(cmd.OutOrStdout(), &InstancesResponse{Items: items}); err != nil {
					return err
				}
				return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, notActivatedError())
			}
		}

		truncated := false
		if opts.Limit > 0 && len(items) > opts.Limit {
			items = items[:opts.Limit]
			truncated = true
		}
		if truncated {
			cmdio.EmitHint(cmd.ErrOrStderr(),
				fmt.Sprintf("showing first %d instances", opts.Limit),
				fmt.Sprintf("gcx dbo11y instances list --limit %d", opts.Limit*2))
		}

		return opts.IO.Encode(cmd.OutOrStdout(), &InstancesResponse{Items: items})
	}
}

// instancesTableCodec renders the instances inventory as a tabular view.
type instancesTableCodec struct {
	Wide bool
}

func (c *instancesTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *instancesTableCodec) Decode(io.Reader, any) error {
	return errors.New("instances table codec does not support decoding")
}

func (c *instancesTableCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*InstancesResponse)
	if !ok {
		return fmt.Errorf("invalid data type for instances table codec: %T", v)
	}
	if len(resp.Items) == 0 {
		_, err := fmt.Fprintln(w, "No database instances discovered. Verify Database Observability is configured for this stack.")
		return err
	}

	headers := []string{"NAME", "ENGINE", "VERSION", "ENVIRONMENT", "PROVIDER"}
	if c.Wide {
		headers = append(headers, "NAMESPACE", "HOST", "IDENTIFIER", "REGION")
	}

	t := style.NewTable(headers...)
	for _, inst := range resp.Items {
		row := []string{
			inst.Name,
			orDash(inst.Engine),
			orDash(inst.EngineVersion),
			orDash(inst.Environment),
			orDash(inst.ProviderName),
		}
		if c.Wide {
			row = append(row,
				orDash(inst.Namespace),
				orDash(inst.Host),
				orDash(inst.InstanceIdentifier),
				orDash(inst.ProviderRegion),
			)
		}
		t.Row(row...)
	}
	return t.Render(w)
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}
