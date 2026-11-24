package tempo

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const defaultExceptionsWindow = time.Hour

type exceptionsOpts struct {
	dsquery.TimeRangeOpts

	IO           cmdio.Options
	Datasource   string
	Expr         string
	Limit        int
	ErrorOnEmpty bool
}

// ExceptionsCmd returns the command that prints the Exceptions tab table.
func ExceptionsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &exceptionsOpts{}
	share := &dsquery.ExploreLinkOpts{}
	exceptions := &dsquery.DrilldownLinkOpts{}

	cmd := &cobra.Command{
		Use:   "exceptions [TRACEQL]",
		Short: "List grouped exception messages from error spans.",
		Long: `List exception groups from error spans on a Tempo datasource.

TRACEQL is an optional spanset such as '{ resource.service.name = "checkout" }'.
Omit it to include every service. The command adds status = error, samples at
most --limit traces (default 300, 10 spans each), and groups messages the way
the Exceptions tab does, with one row per service. Counts are occurrences of
that row. If Tempo omits matching spans, the counts are lower bounds; narrow
the spanset rather than raising --limit.

Each row links to the Exceptions tab for that service and exception type.
With no time flags, the window is the last hour. Use --share-link or --open
for the span query in Explore, and --drilldown-link or --open-drilldown for
this spanset in the Exceptions tab. Those tab URLs do not carry --limit.`,
		Example: `
  # Group error-span exceptions for one service over the last hour
  gcx datasources tempo exceptions '{ resource.service.name = "checkout" }' --since 1h

  # Every service
  gcx datasources tempo exceptions --since 1h

  # JSON for an agent, with a smaller sample
  gcx datasources tempo exceptions -d UID '{ resource.service.name = "checkout" }' --since 1h --limit 50 -o json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd, args, loader, share, exceptions)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources tempo exceptions -d UID '{ resource.service.name = "checkout" }' --since 1h --limit 50 -o json`,
	}

	opts.setup(cmd.Flags())
	share.Setup(cmd.Flags(), "executed query")
	exceptions.Setup(cmd.Flags(), "exception sample", "Traces Drilldown Exceptions")

	return cmd
}

func (opts *exceptionsOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterCSV(&opts.IO)
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	opts.SetupTimeFlags(flags)
	flags.StringVar(&opts.Expr, "expr", "", "TraceQL spanset (alternative to the positional argument). Omit both to include every service")
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	flags.IntVar(&opts.Limit, "limit", tempo.ExceptionsTraceLimit, fmt.Sprintf("Maximum traces to sample (1-%d). Each trace contributes at most 10 spans", tempo.ExceptionsTraceLimit))
	flags.BoolVar(&opts.ErrorOnEmpty, "error-on-empty", false, "Fail if the sample contains no exception groups")
}

func (opts *exceptionsOpts) run(cmd *cobra.Command, args []string, loader *providers.ConfigLoader, share *dsquery.ExploreLinkOpts, exceptions *dsquery.DrilldownLinkOpts) error {
	if err := opts.Validate(); err != nil {
		return gcxerrors.NewCommandUsageError(cmd, "", err)
	}
	filter, err := opts.filter(cmd.Flags(), args)
	if err != nil {
		return gcxerrors.NewCommandUsageError(cmd, "", err)
	}
	expr, err := tempo.BuildExceptionsQuery(filter)
	if err != nil {
		return gcxerrors.NewCommandUsageError(cmd, "", err)
	}

	now := time.Now()
	start, end, err := opts.ParseTimeRange(now)
	if err != nil {
		return gcxerrors.NewCommandUsageError(cmd, "", err)
	}
	if start.IsZero() && end.IsZero() {
		end = now
		start = now.Add(-defaultExceptionsWindow)
	}

	ctx := cmd.Context()
	cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
	if err != nil {
		return err
	}
	datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "tempo")
	if err != nil {
		return err
	}

	client, err := tempo.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	resp, err := client.SearchSpans(ctx, datasourceUID, tempo.SpanSearchRequest{
		Query: expr,
		Start: start,
		End:   end,
		Limit: opts.Limit,
	})
	if err != nil {
		return fmt.Errorf("span search failed: %w", err)
	}

	result := tempo.AggregateExceptions(resp, expr, opts.Limit)
	result.ListMeta = tempo.ExceptionListMeta(len(result.Exceptions), result.TracesSampled, opts.Limit, os.Args)

	from, to := opts.From, opts.To
	if from == "" {
		from = start.Format(time.RFC3339)
	}
	if to == "" {
		to = end.Format(time.RFC3339)
	}
	orgID := dsquery.OrgID(cfgCtx)
	attachExceptionLinks(result.Exceptions, cfg.GrafanaURL, orgID, datasourceUID, filter, start, end)
	exploreURL := ExceptionsExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
		DatasourceUID:  datasourceUID,
		DatasourceType: "tempo",
		Expr:           expr,
		From:           from,
		To:             to,
		OrgID:          orgID,
	}, opts.Limit)
	unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("exception sample")

	// With --error-on-empty an empty sample is the failure, so stdout must carry
	// only that error. Printing the empty result first would give scripts two
	// values to parse. Links still run, as they only write to stderr or open a
	// browser.
	var emptyErr error
	if opts.ErrorOnEmpty {
		emptyErr = dsquery.ErrorOnEmptyWithContext(result, dsquery.EmptyResultContext{
			Expr: expr, DatasourceUID: datasourceUID, Start: start, End: end,
		})
	}

	if err := dsquery.EncodeAndHandleExplore(cmd, func() error {
		if emptyErr != nil {
			return nil
		}
		return opts.IO.Encode(cmd.OutOrStdout(), result)
	}, *share, dsquery.ExploreLink{
		URL:            exploreURL,
		UnavailableMsg: unavailableMsg,
		FailedOpenMsg:  failedOpenMsg,
	}); err != nil {
		return err
	}

	exceptionsURL, _ := TracesExceptionsURL(cfg.GrafanaURL, orgID, datasourceUID, filter, start, end)
	exceptionsUnavailable, exceptionsFailedOpen := dsquery.DrilldownMessages("exception sample", "Traces Drilldown Exceptions")
	if err := dsquery.HandleDrilldownLinkWithExploreFallback(cmd, *exceptions, exceptionsURL, exceptionsUnavailable, exceptionsFailedOpen,
		*share, exploreURL, unavailableMsg, failedOpenMsg); err != nil {
		return err
	}
	emitExceptionSampleHint(cmd.ErrOrStderr(), result)
	emitOmittedSpansHint(cmd.ErrOrStderr(), result)

	return emptyErr
}

func (opts *exceptionsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Limit < 1 || opts.Limit > tempo.ExceptionsTraceLimit {
		return fmt.Errorf("--limit must be from 1 to %d; got %d", tempo.ExceptionsTraceLimit, opts.Limit)
	}
	return opts.ValidateTimeRange()
}

func (opts *exceptionsOpts) filter(flags *pflag.FlagSet, args []string) (string, error) {
	positional := ""
	if len(args) == 1 {
		positional = strings.TrimSpace(args[0])
		if positional == "" {
			return "", errors.New("exceptions filter must be one TraceQL spanset such as '{ resource.service.name = \"checkout\" }', or omitted to include every service")
		}
	}
	if flags.Changed("expr") {
		if strings.TrimSpace(opts.Expr) == "" {
			return "", errors.New("--expr must be a TraceQL spanset such as '{ resource.service.name = \"checkout\" }'; omit --expr to include every service")
		}
		if positional != "" {
			return "", errors.New("provide the spanset as a positional argument or via --expr, not both")
		}
		return strings.TrimSpace(opts.Expr), nil
	}
	return positional, nil
}

func attachExceptionLinks(groups []tempo.ExceptionGroup, host string, orgID int64, datasourceUID, filter string, start, end time.Time) {
	for i := range groups {
		filters, ok := ExceptionRowFilters(filter, groups[i].Service, groups[i].Type)
		if !ok {
			continue
		}
		url, ok := tracesDrilldownURL(host, orgID, datasourceUID, filters, start, end, "errors", "exceptions")
		if !ok {
			continue
		}
		groups[i].URL = url
	}
}

// emitOmittedSpansHint warns when Tempo omitted matching spans. A larger
// --limit does not fix this, so the hint points at narrowing the spanset.
func emitOmittedSpansHint(w io.Writer, result *tempo.ExceptionsResult) {
	if result == nil || result.SpansOmitted == 0 {
		return
	}
	cmdio.EmitHint(w, fmt.Sprintf(
		"Tempo left out %d matching spans (it returns at most %d per trace), so counts are lower bounds. Narrow the spanset; a larger --limit will not help",
		result.SpansOmitted, result.SpansPerTrace), "")
}

func emitExceptionSampleHint(w io.Writer, result *tempo.ExceptionsResult) {
	if result == nil || result.ListMeta == nil || !result.ListMeta.Truncated {
		return
	}
	summary := fmt.Sprintf("exception counts use the first %d traces (%d spans each)", result.TracesSampled, result.SpansPerTrace)
	if result.ListMeta.Cap > 0 {
		cmdio.EmitHint(w, summary+". Refine the spanset to narrow the sample", "")
		return
	}
	cmdio.EmitHint(w, summary+". See a larger sample with", result.ListMeta.Continue)
}
