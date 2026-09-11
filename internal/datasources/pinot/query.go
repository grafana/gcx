package pinot

import (
	"fmt"
	"io"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pinot"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type queryOpts struct {
	dsquery.SharedOpts

	Datasource string
	Table      string
	Limit      int
}

func (opts *queryOpts) setup(flags *pflag.FlagSet) {
	opts.Setup(flags, false)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pinot is configured)")
	flags.StringVar(&opts.Table, "table", "", "StarTree table name when the SQL has no extractable FROM (required in that case)")
	flags.IntVar(&opts.Limit, "limit", pinot.DefaultLimit, pinot.LimitFlagUsage(pinot.MaxLimit))
}

func (opts *queryOpts) Validate() error {
	if opts.Limit < 0 {
		return fmt.Errorf("--limit must be >= 0, got %d", opts.Limit)
	}
	return opts.SharedOpts.Validate()
}

// QueryCmd returns the `query` subcommand for a Pinot datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &queryOpts{}
	share := &dsquery.ExploreLinkOpts{}

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a PinotQL query against a StarTree Pinot datasource",
		Long: `Execute a PinotQL query against a StarTree Pinot datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.pinot in your context.
Server-side macros ($__timeFilter, $__timeGroup, etc.) are supported.

Table name (StarTree tableName field):
  - StarTree requires tableName to load schema and expand macros before it runs
    pinotQlCode (sent alongside your SQL in the query request).
  - By default, gcx derives tableName from the first real FROM in the SQL.
  - Derivation supports schema.table, double-quoted identifiers, and the inner
    table in FROM (subquery) … (for example SELECT … FROM (SELECT … FROM events)).
  - Ignored for derivation: FROM inside string literals, line or block comments,
    or EXTRACT/TRIM/SUBSTRING/OVERLAY calls (they are not table clauses).
  - When the SQL has no extractable table (for example SELECT 1), pass --table
    with a table the query actually uses; the command fails otherwise.
  - --table overrides any name that would be derived from the SQL.

Row limit (--limit):
  - Default 100 when --limit is omitted on this command and the expresion does not have a LIMIT; generic gcx datasources
    query uses the same Pinot default when the datasource kind is pinot.
  - --limit 0 disables enforcement (SQL is sent unchanged) and prints no notice.
  - Requests above 1000 are capped to 1000 in the emitted LIMIT when the SQL
    can be rewritten. --limit never overwrites an existing LIMIT n that is
    already at or below 1000.
  - Only SELECT/WITH-shaped PinotQL may be rewritten; optional leading SET …;
    prefixes are ignored for this check.
  - Left unchanged (no append): UNION, OFFSET, LIMIT offset,count, OPTION(…),
    trailing line comments, LIMIT before a trailing comment, unclosed block
    comments; keywords only in string literals or comments do not trigger these.
  - A LIMIT inside a comment (LIMIT /* note */ n) still counts as an existing
    LIMIT. The number is read after comments are blanked but the SQL is not rewritten because it is not safe to modify.

  Stderr notices (one line, never the full SQL):

  When --limit N is set:
    | Query                         | Sent            | Notice |
    |-------------------------------|-----------------|--------|
    | no LIMIT, safe                | append LIMIT N  | Query adjusted: appended LIMIT N (--limit). Use --limit 0 to disable enforcement. |
    | no LIMIT, not safe            | unchanged       | You asked for --limit N, but a row limit was not appended (this query shape is not safe to modify). Add LIMIT in the SQL or use --limit 0. |
    | has LIMIT, n == N             | unchanged       | (none) |
    | has LIMIT, n != N, n <= 1000  | unchanged       | You requested --limit N but the query already has a LIMIT, so no change was applied. |
    | bare LIMIT n, n > 1000        | LIMIT 1000      | You requested --limit N but the existing LIMIT was above the maximum, so it was reduced to 1000. |
    | LIMIT /* … */ n, n > 1000     | unchanged       | The query has a LIMIT above 1000 that should be reduced to 1000, but the query is not safe to modify. |

  When --limit is omitted (default 100):
    | Query                         | Sent            | Notice |
    |-------------------------------|-----------------|--------|
    | no LIMIT, safe                | append LIMIT 100 | Query adjusted: appended default LIMIT 100. Use --limit 0 to disable enforcement. |
    | no LIMIT, not safe            | unchanged       | (none) |
    | has LIMIT, n <= 1000          | unchanged       | (none) |
    | bare LIMIT n, n > 1000        | LIMIT 1000      | Query adjusted: LIMIT reduced to 1000 (maximum). Use --limit 0 to disable enforcement. |
    | LIMIT /* … */ n, n > 1000     | unchanged       | The query has a LIMIT above 1000 that should be reduced to 1000, but the query is not safe to modify. |

Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Simple query
  gcx datasources pinot query -d UID 'SELECT count(*) FROM events'

  # With time range
  gcx datasources pinot query -d UID --since 7d \
    'SELECT count(*) FROM events WHERE $__timeFilter("timestamp")'

  # Output as JSON
  gcx datasources pinot query -d UID 'SELECT 1 FROM events' -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources pinot query -d UID 'SELECT 1 FROM events' --share-link

  # Disable limit enforcement
  gcx datasources pinot query -d UID 'SELECT * FROM events' --limit 0

  # SQL with no extractable table
  gcx datasources pinot query -d UID --table events 'SELECT 1'`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			expr, err := opts.ResolveExpr(args, 0)
			if err != nil {
				return err
			}

			sql, capped := pinot.EnforceLimit(expr, opts.Limit, pinot.MaxLimit)
			warnLimitEnforcement(cmd.ErrOrStderr(), expr, sql, capped, opts.Limit, cmd.Flags().Changed("limit"))

			tableName, err := pinot.ResolveTableName(sql, opts.Table)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pinot")
			if err != nil {
				return err
			}

			now := time.Now()
			start, end, step, err := opts.ParseTimes(now)
			if err != nil {
				return err
			}

			var intervalMs int64
			if step > 0 {
				intervalMs = step.Milliseconds()
			}

			client, err := pinot.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, pinot.QueryRequest{
				RawSQL:     sql,
				TableName:  tableName,
				Start:      start,
				End:        end,
				IntervalMs: intervalMs,
			})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			exploreURL := QueryExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
				DatasourceUID:  datasourceUID,
				DatasourceType: dsType,
				Expr:           sql,
				From:           opts.From,
				To:             opts.To,
				OrgID:          dsquery.OrgID(cfgCtx),
				TableName:      tableName,
			})
			unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("query")

			return dsquery.EncodeAndHandleExplore(cmd, func() error {
				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}, *share, dsquery.ExploreLink{
				URL:            exploreURL,
				UnavailableMsg: unavailableMsg,
				FailedOpenMsg:  failedOpenMsg,
			})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources pinot query -d UID 'SELECT count(*) FROM events' -o json`,
	}

	opts.setup(cmd.Flags())
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}

func warnLimitEnforcement(w io.Writer, expr, sql string, capped bool, limit int, limitSet bool) {
	if msg := pinot.LimitEnforcementNotice(expr, sql, capped, limit, pinot.MaxLimit, limitSet); msg != "" {
		cmdio.Warning(w, "%s", msg)
	}
}
