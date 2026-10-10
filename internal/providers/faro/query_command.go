package faro

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pinot"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const multistagePrefix = "SET useMultistageEngine = true;\n"

// frontendQueryTables returns the Frontend Observability tables gcx frontend
// query may read.
func frontendQueryTables() []string {
	return []string{
		"faro_pinot_events_v1",
		"faro_pinot_events_v2",
		"faro_pinot_exceptions_v1",
		"faro_pinot_measurements_v1",
		"faro_exception_groups",
	}
}

// multistageHintRE flags constructs that need the multi-stage engine. It is a
// hint only, so a match inside a string literal is harmless.
var multistageHintRE = regexp.MustCompile(`(?i)\b(JOIN|WITH|UNION)\b|\bOVER\s*\(`)

// timeFilterRE matches the two time predicates the datasource understands.
// Without one, --since/--from/--to have no effect and the statement scans the
// full retention.
var timeFilterRE = regexp.MustCompile(`(?i)\$__timeFilter\s*\(|\bago\s*\(`)

func hasTimeFilter(sql string) bool { return timeFilterRE.MatchString(sql) }

const noTimeFilterWarning = `the statement has no $__timeFilter(...) or ago(...) predicate, so --since/--from/--to are ignored and the full 30-day retention is scanned; add AND $__timeFilter("timestamp") to the WHERE clause`

// queryFailureError keeps the converter's rendering of the broker error and
// adds the hint as a suggestion, so agents reading the error JSON see it.
func queryFailureError(err error, hint string) error {
	detailed := fail.ErrorToDetailedError(err)
	if detailed == nil {
		detailed = &gcxerrors.DetailedError{Summary: "Pinot query failed", Details: err.Error(), Parent: err}
	}
	if detailed.Parent == nil {
		detailed.Parent = err
	}
	if hint != "" {
		detailed.Suggestions = append(detailed.Suggestions, hint)
	}
	return detailed
}

type frontendQueryOpts struct {
	frontendOpts

	Expr       string
	Table      string
	Limit      int
	Multistage bool
}

func (o *frontendQueryOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&o.IO, false)
	o.IO.BindFlags(flags)

	o.frontendOpts.setup(flags, false)
	flags.StringVar(&o.Expr, "expr", "", "PinotQL statement (alternative to the positional argument)")
	flags.StringVar(&o.Table, "table", "", "StarTree table name when the SQL has no extractable FROM; must be a Frontend Observability table")
	flags.IntVar(&o.Limit, "limit", pinot.DefaultLimit, pinot.LimitFlagUsage(pinot.MaxLimit))
	flags.BoolVar(&o.Multistage, "multistage", false, "Run the statement on Pinot's multi-stage engine (needed for JOIN, WITH, UNION, and window functions)")
}

func (o *frontendQueryOpts) Validate() error {
	if o.Limit < 0 {
		return fmt.Errorf("--limit must be >= 0, got %d", o.Limit)
	}
	o.Table = strings.TrimSpace(o.Table)
	return o.frontendOpts.Validate()
}

func (o *frontendQueryOpts) resolveExpr(args []string) (string, error) {
	haveFlag := strings.TrimSpace(o.Expr) != ""
	haveArg := len(args) > 0 && strings.TrimSpace(args[0]) != ""
	switch {
	case haveFlag && haveArg:
		return "", errors.New("provide the SQL as a positional argument or via --expr, not both")
	case haveFlag:
		return o.Expr, nil
	case haveArg:
		return args[0], nil
	default:
		return "", errors.New("SQL is required: provide it as a positional argument or via --expr")
	}
}

// checkFrontendQueryTable requires the statement's table (or --table) to be a
// Frontend Observability table. It does not rewrite the SQL.
func checkFrontendQueryTable(sql, tableOverride string) (string, error) {
	table, err := pinot.ResolveTableName(sql, tableOverride)
	if err != nil {
		return "", err
	}
	if !slices.Contains(frontendQueryTables(), table) {
		return "", fmt.Errorf("table %q is not a Frontend Observability table; use one of %s", table, strings.Join(frontendQueryTables(), ", "))
	}
	return table, nil
}

// checkAppIDPredicate requires the literal appId = <id> in the SQL. It is a
// usability guard against querying the wrong app, not tenant isolation.
func checkAppIDPredicate(sql string, appID int64) error {
	if !hasAppIDPredicate(sql, appID) {
		return fmt.Errorf("query must filter on appId = %d; add it to the WHERE clause", appID)
	}
	return nil
}

func hasAppIDPredicate(sql string, appID int64) bool {
	re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])"?appId"?\s*=\s*` + strconv.FormatInt(appID, 10) + `([^0-9]|$)`)
	return re.MatchString(sql)
}

// brokerErrorHint returns one hint for common broker failures, or "".
// suggestMultistage is false when --multistage is already set.
func brokerErrorHint(msg string, suggestMultistage bool) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "numgroupslimit"):
		return "The query hit Pinot's group limit: narrow --since or add a WHERE filter"
	case suggestMultistage && (strings.Contains(lower, "multi-stage") || strings.Contains(lower, "multistage") ||
		strings.Contains(lower, "join") || strings.Contains(lower, "window")):
		return "This construct needs the multi-stage engine: rerun with --multistage"
	case strings.Contains(lower, "unknown column") || strings.Contains(lower, "column not found") ||
		(strings.Contains(lower, "column") && strings.Contains(lower, "not found")):
		return "Check the column name against the table schema: Pinot column names are case-sensitive and flattened JSON columns are quoted (\"attributesJson.http.host\")"
	default:
		return ""
	}
}

func newFrontendQueryCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &frontendQueryOpts{}
	cmd := &cobra.Command{
		Use:   "query [SQL]",
		Short: "Run one PinotQL statement against the Frontend Observability tables.",
		Long: `Run one PinotQL statement against the Frontend Observability Pinot tables.

This is the escape hatch for questions the named commands (errors, pages) do
not answer. Start from a statement printed by --sql on one of them.

Guard rails, checked before the request is sent:
  - The statement must read from faro_pinot_events_v1, faro_pinot_events_v2,
    faro_pinot_exceptions_v1, faro_pinot_measurements_v1, or
    faro_exception_groups.
  - With --app, the SQL must contain appId = <id>; the command refuses rather
    than rewriting your SQL. Without --app, the query is unscoped and a
    warning is printed.

Put AND $__timeFilter("timestamp") in the WHERE clause: the datasource expands
it from --since/--from/--to (default 24h, at most 30 days). Without it, or
ago(...), the time flags have no effect and the statement scans the full
retention; a warning says so. --limit follows gcx datasources pinot query:
default 100, capped at 1000, with the same stderr notices.

--multistage runs the statement on Pinot's multi-stage engine by prefixing
SET useMultistageEngine = true;. Use it only for JOIN, WITH, UNION or window
functions.`,
		Example: `  # Error count per type for one app
  gcx frontend query --app 187 'SELECT exceptionType, count(*) AS n FROM faro_pinot_exceptions_v1 WHERE appId = 187 AND $__timeFilter("timestamp") GROUP BY exceptionType ORDER BY n DESC'

  # Multi-stage join over a week
  gcx frontend query --app 187 --since 7d --multistage --expr 'WITH e AS (SELECT sessionId FROM faro_pinot_exceptions_v1 WHERE appId = 187 AND $__timeFilter("timestamp")) SELECT count(*) FROM e'`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			expr, err := opts.resolveExpr(args)
			if err != nil {
				return err
			}
			stderr := cmd.ErrOrStderr()
			table, err := checkFrontendQueryTable(expr, opts.Table)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			t, err := opts.resolve(ctx, loader)
			if err != nil {
				return err
			}

			if opts.App != "" {
				if err := checkAppIDPredicate(expr, t.app.ID); err != nil {
					return err
				}
			} else {
				cmdio.EmitWarn(stderr, "no --app given: the query is not checked for an appId filter")
			}
			if !hasTimeFilter(expr) {
				cmdio.EmitWarn(stderr, noTimeFilterWarning)
			}

			sql, capped := pinot.EnforceLimit(expr, opts.Limit, pinot.MaxLimit)
			if msg := pinot.LimitEnforcementNotice(expr, sql, capped, opts.Limit, pinot.MaxLimit, cmd.Flags().Changed("limit")); msg != "" {
				cmdio.EmitWarn(stderr, msg)
			}
			if opts.Multistage {
				sql = multistagePrefix + sql
			} else if multistageHintRE.MatchString(expr) {
				cmdio.EmitWarn(stderr, "the statement looks like it needs the multi-stage engine (JOIN, WITH, UNION, or a window function); rerun with --multistage if it fails")
			}

			resp, err := t.pinot.Query(ctx, t.dsUID, pinot.QueryRequest{
				RawSQL:    sql,
				TableName: table,
				Start:     t.start,
				End:       t.end,
			})
			if err != nil {
				return queryFailureError(err, brokerErrorHint(err.Error(), !opts.Multistage))
			}
			warnPossibleTruncation(stderr, len(resp.Rows), sql)

			primary := statement{Name: "query", Table: table, SQL: sql}
			return dsquery.EncodeAndHandleExplore(cmd, func() error {
				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}, opts.Share, opts.exploreLink(t, primary, "query"))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// warnPossibleTruncation notes when the result filled the statement's
// trailing LIMIT, so more rows may exist.
func warnPossibleTruncation(w io.Writer, rows int, sql string) {
	m := trailingLimitRE.FindStringSubmatch(strings.TrimRight(sql, "; \t\r\n"))
	if m == nil {
		return
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n == 0 || rows < n {
		return
	}
	noun := "rows"
	if rows == 1 {
		noun = "row"
	}
	cmdio.EmitWarn(w, fmt.Sprintf("result has %d %s, the statement's LIMIT; more rows may exist", rows, noun))
}

var trailingLimitRE = regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)$`)
