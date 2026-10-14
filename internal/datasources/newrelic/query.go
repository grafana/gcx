package newrelic

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/newrelic"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type queryOpts struct {
	dsquery.SharedOpts

	Datasource     string
	AccountID      int64
	TimeoutSeconds int
}

func (o *queryOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&o.IO, false)
	o.IO.BindFlags(flags)
	o.SetupTimeFlags(flags)
	o.SetupExprFlag(flags)
	flags.StringVarP(&o.Datasource, "datasource", "d", "", "New Relic plugin datasource UID (auto-discovers one if omitted)")
	flags.Int64Var(&o.AccountID, "account-id", 0, "New Relic account ID override (positive integer; omit to use plugin configuration)")
	flags.IntVar(&o.TimeoutSeconds, "timeout-seconds", 0, "Query timeout override (5-120 seconds; omit to use plugin default)")
}

func (o *queryOpts) Validate(flags *pflag.FlagSet) error {
	if err := o.SharedOpts.Validate(); err != nil {
		return err
	}
	if flags.Changed("account-id") && o.AccountID == 0 {
		return errors.New("--account-id must be positive")
	}
	if flags.Changed("timeout-seconds") && o.TimeoutSeconds == 0 {
		return errors.New("--timeout-seconds must be between 5 and 120")
	}
	if flags.Changed("datasource") && strings.TrimSpace(o.Datasource) == "" {
		return errors.New("--datasource must be a nonempty UID")
	}
	return nil
}

// QueryCmd builds the New Relic NRQL command.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	o := &queryOpts{}
	share := &dsquery.ExploreLinkOpts{}
	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Run NRQL through an open-source New Relic plugin datasource",
		Long: `Query the open-source New Relic Grafana datasource plugin with NRQL.
The datasource is selected by -d UID, contexts.<name>.datasources.newrelic,
or discovery when exactly one matching plugin instance is installed.
The plugin uses the selected Grafana time range
unless NRQL has SINCE or COMPARE WITH.
With no time flags, the range defaults to the last hour.
Use --share-link to print an Explore URL.
Use --open to open it after the query succeeds.`,
		Example: `  gcx datasources newrelic query -d UID 'SELECT count(*) FROM Transaction' --since 1h
  gcx datasources newrelic query -d UID 'SELECT * FROM Transaction' --account-id 12345 --timeout-seconds 30 -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Validate(cmd.Flags()); err != nil {
				return err
			}
			expr, err := o.ResolveExpr(args, 0)
			if err != nil {
				return err
			}
			req := newrelic.QueryRequest{QueryText: expr, AccountID: o.AccountID, TimeoutSeconds: o.TimeoutSeconds}
			if err := req.Validate(); err != nil {
				return err
			}
			req.Start, req.End, err = o.ParseTimeRange(time.Now())
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}
			uid, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, o.Datasource, cfgCtx, cfg, "newrelic")
			if err != nil {
				return err
			}
			client, err := newrelic.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}
			resp, err := client.Query(ctx, uid, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}
			link := QueryExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
				DatasourceUID: uid, From: o.From, To: o.To,
				OrgID: dsquery.OrgID(cfgCtx),
			}, req)
			unavailable, failedOpen := dsquery.ExploreMessages("query")
			return dsquery.EncodeAndHandleExplore(cmd, func() error {
				return o.IO.Encode(cmd.OutOrStdout(), resp)
			}, *share, dsquery.ExploreLink{URL: link, UnavailableMsg: unavailable, FailedOpenMsg: failedOpen})
		},
	}
	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "large",
		agent.AnnotationLLMHint:   `gcx datasources newrelic query -d UID 'SELECT * FROM Transaction LIMIT 100' --since 1h -o json`,
	}
	o.setup(cmd.Flags())
	share.Setup(cmd.Flags(), "executed query")
	return cmd
}
