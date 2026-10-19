package dbo11y

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
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
	o.IO.RegisterCustomCodec("table", &instancesEnvelopeCodec{name: "table"})
	o.IO.RegisterCustomCodec("wide", &instancesEnvelopeCodec{name: "wide"})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Restrict to instances matching label=value, e.g. --filter engine=postgres (repeatable, ANDed). Applied by the API against any label the monitored instances carry, including ones it does not report such as cluster or deployment_environment. Values are matched exactly and are case sensitive; the API has no regex or negation")
	flags.IntVar(&o.Limit, "limit", instancesListDefaultLimit, "Limit the number of instances returned (0 = unlimited)")
}

func (o *listOpts) Validate(cmd *cobra.Command) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.Limit < 0 {
		return gcxerrors.NewCommandUsageError(cmd, "--limit must be zero or positive", nil)
	}
	return nil
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Database Observability instances discovered from telemetry.",
		Long: `List the database instances Grafana Cloud Database Observability has discovered.

The inventory comes from the Database Observability API,
which resolves it from the stack's telemetry on your behalf — so this reports
whatever the product itself reports, for every engine it supports.

Each row also carries a server_id, the identifier the product keys an
instance by. "gcx dbo11y instances get" accepts either that or the instance
name.

The API reports no namespace or environment, so neither is shown. They
remain filterable, since --filter is matched against the telemetry rather
than against this output — "--filter deployment_environment=prod" works, it
just cannot be displayed.

Related: "gcx dbo11y instances get <instance>" drills into one instance's
query performance.

When no instances are found, this command checks whether Database
Observability is activated for the stack and, if not, exits non-zero with a
hint to activate it instead of a generic empty result.`,
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
			agent.AnnotationLLMHint:   `Database Observability instance inventory, read from the Database Observability API: one row per monitored database under {"items": [...]} with name, engine, engine_version, host, provider_name/_account/_region, instance_identifier and server_id. Covers Postgres, MySQL and SQL Server. The API reports no namespace, environment or raw labels, so those fields are not emitted. Pairs with 'gcx dbo11y instances get <instance>', which accepts either the name or the server_id. --filter label=value (repeatable, ANDed) is forwarded to the API, which matches it against the monitored instances' own labels — so it works on labels the response omits (cluster, dsn, job, service_namespace, deployment_environment) as well as the reported fields. Exact match only: != =~ !~ are usage errors, as the API has no equivalent. A label the telemetry does not carry matches nothing rather than erroring, so an empty result is echoed back with the filters that produced it. On an empty result this command also checks the stack's Database Observability activation status and, if not activated, exits 1 with a specific hint. Examples: gcx dbo11y instances list -o json; gcx dbo11y instances list --filter engine=postgres -o json`,
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
		filters, _, err := parseFilters(opts.Filters)
		if err != nil {
			return gcxerrors.NewCommandUsageError(cmd, "", err)
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

		client, err := newAPIClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create Database Observability client: %w", err)
		}

		items, err := fetchInstances(ctx, client, datasourceUID, filters)
		if err != nil {
			var apiErr *apiError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
				return err
			}
			if activated, actErr := checkActivation(ctx, cfg); actErr == nil && !activated {
				return reportNotActivated(cmd, &opts.IO, nil)
			}
			return err
		}

		// A filter that legitimately matches nothing is not a reason to
		// question whether the product is activated. Exact-match filters are
		// applied by the API, so an empty result here is only unambiguously
		// "nothing is monitored" when no filter was in play at all.
		if len(items) == 0 && len(filters) == 0 {
			// Best-effort diagnostic enrichment: it can only make the "no
			// instances" case more specific, never less — an inconclusive
			// check falls through to the generic table message below rather
			// than blocking.
			if activated, actErr := checkActivation(ctx, cfg); actErr == nil && !activated {
				return reportNotActivated(cmd, &opts.IO, items)
			}
		}

		// The API matches filters against the inventory metric's own labels,
		// which include labels the response never returns — so a filter
		// naming a label the metric does not carry is indistinguishable from
		// one that simply matched nothing. Echoing the filters back makes a
		// typo visible without rejecting a legitimate filter.
		if len(items) == 0 && len(filters) > 0 {
			applied := make([]string, 0, len(filters))
			for label, values := range filters {
				for _, v := range values {
					applied = append(applied, label+"="+v)
				}
			}
			sort.Strings(applied)
			cmdio.EmitHint(cmd.ErrOrStderr(),
				"no instances matched "+strings.Join(applied, " and "),
				"gcx dbo11y instances list")
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

// instancesEnvelopeCodec renders an InstancesResponse by delegating its items
// to the declared InstancesTable. The envelope is what `-o json`/`-o yaml`
// emit, so the table codec has to accept it too; the column declaration still
// lives in one place (ADR-024).
type instancesEnvelopeCodec struct {
	name string
}

func (c *instancesEnvelopeCodec) Format() format.Format { return format.Format(c.name) }

func (c *instancesEnvelopeCodec) Decode(io.Reader, any) error {
	return errors.New("instances table codec does not support decoding")
}

func (c *instancesEnvelopeCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*InstancesResponse)
	if !ok {
		return fmt.Errorf("invalid data type for instances table codec: %T", v)
	}
	return InstancesTable().Codec(c.name).Encode(w, resp.Items)
}

// InstancesTable declares the `instances list` table (ADR-024).
func InstancesTable() cmdio.Table[Instance] {
	return cmdio.Table[Instance]{
		Empty: func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "No database instances discovered. Verify Database Observability is configured for this stack.")
			return err
		},
		Columns: []cmdio.Column[Instance]{
			{Header: "NAME", Content: func(i Instance) string { return orDash(i.Name) }},
			{Header: "ENGINE", Content: func(i Instance) string { return orDash(i.Engine) }},
			{Header: "VERSION", Content: func(i Instance) string { return orDash(i.EngineVersion) }},
			{Header: "PROVIDER", Content: func(i Instance) string { return orDash(i.ProviderName) }},
			{Header: "HOST", Visible: cmdio.WideOnly, Content: func(i Instance) string { return orDash(i.Host) }},
			{Header: "IDENTIFIER", Visible: cmdio.WideOnly, Content: func(i Instance) string { return orDash(i.InstanceIdentifier) }},
			{Header: "REGION", Visible: cmdio.WideOnly, Content: func(i Instance) string { return orDash(i.ProviderRegion) }},
			{Header: "SERVER ID", Visible: cmdio.WideOnly, Content: func(i Instance) string { return orDash(i.ServerID) }},
		},
	}
}

// reportNotActivated warns that the product is switched off for the stack,
// writes the result document and returns the error to exit on. This leaf is
// `finite`, so stdout carries exactly one JSON value however it exits — and a
// reporter handed an EmittedError writes nothing further, so the document has
// to come from here.
func reportNotActivated(cmd *cobra.Command, io *cmdio.Options, items []Instance) error {
	cmdio.EmitWarn(cmd.ErrOrStderr(), notActivatedError().Error())
	if items == nil {
		items = []Instance{}
	}
	if encErr := io.Encode(cmd.OutOrStdout(), &InstancesResponse{Items: items}); encErr != nil {
		return encErr
	}
	return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, notActivatedError())
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

// boolYesNo renders a flag for the table codecs. Not orDash's empty-is-unknown
// treatment: a false here is an answer, not a missing value.
func boolYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
