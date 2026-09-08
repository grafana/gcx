package k6

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

type listRunLogsOpts struct {
	IO        cmdio.Options
	TimeRange dsquery.TimeRangeOpts
	Direction string
	Limit     int
}

func (o *listRunLogsOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&o.IO, false)
	o.IO.RegisterCustomCodec("raw", loki.NewRawQueryCodec())
	o.IO.BindFlags(flags)
	o.TimeRange.SetupTimeFlags(flags)
	flags.StringVar(&o.Direction, "direction", logDirectionBack, "Read logs forward or backward")
	flags.IntVar(&o.Limit, "limit", defaultLogLimit, "Maximum number of log entries")
}

func (o *listRunLogsOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if err := o.TimeRange.ValidateTimeRange(); err != nil {
		return err
	}
	if o.Direction != "forward" && o.Direction != logDirectionBack {
		return fmt.Errorf("invalid --direction %q: must be forward or backward", o.Direction)
	}
	if o.Limit <= 0 {
		return fmt.Errorf("invalid --limit %d: must be greater than 0", o.Limit)
	}
	return nil
}

func defaultRunLogTimeRange(run *TestRun, now time.Time) (time.Time, time.Time, error) {
	start, err := time.Parse(time.RFC3339Nano, run.Created)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("k6: parse test run created time %q: %w", run.Created, err)
	}
	if run.Ended == nil || *run.Ended == "" {
		return start, now, nil
	}
	end, err := time.Parse(time.RFC3339Nano, *run.Ended)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("k6: parse test run ended time %q: %w", *run.Ended, err)
	}
	return start, end, nil
}

func newRunsListLogsCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &listRunLogsOpts{}
	cmd := &cobra.Command{
		Use:   "list-logs <run-id> [PIPELINE]",
		Short: "List logs for a k6 test run.",
		Long:  "List logs for one k6 test run. The optional LogQL pipeline must start with '|'. The command always limits the query to the selected run.",
		Example: "  gcx k6 runs list-logs 12345\n" +
			"  gcx k6 runs list-logs 12345 '|= `error`' --since 15m\n" +
			"  gcx k6 runs list-logs 12345 --direction forward --limit 100 -o raw",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			hasTimeRange := opts.TimeRange.From != "" || opts.TimeRange.To != "" || opts.TimeRange.Since != ""
			if err := opts.Validate(); err != nil {
				return err
			}
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			query := fmt.Sprintf(defaultLogQuery, runID)
			if len(args) == 2 {
				pipeline := strings.TrimSpace(args[1])
				if !strings.HasPrefix(pipeline, "|") {
					return errors.New("log pipeline must start with '|'")
				}
				query += " " + pipeline
			}

			var start, end time.Time
			if hasTimeRange {
				start, end, err = opts.TimeRange.ParseTimeRange(time.Now())
				if err != nil {
					return err
				}
			}

			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			if !hasTimeRange {
				run, getErr := client.GetTestRun(cmd.Context(), runID)
				if getErr != nil {
					return getErr
				}
				start, end, err = defaultRunLogTimeRange(run, time.Now())
				if err != nil {
					return err
				}
			}

			resp, err := client.ListRunLogs(cmd.Context(), runID, RunLogsRequest{
				Query: query, Start: start, End: end, Direction: opts.Direction, Limit: opts.Limit,
			})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type runInsightsTableCodec struct{}

func (*runInsightsTableCodec) Format() format.Format { return "table" }

func (*runInsightsTableCodec) Encode(w io.Writer, data any) error {
	insights, ok := data.(*RunInsights)
	if !ok {
		return errors.New("invalid data type for run insights table codec")
	}
	if len(insights.Audits) == 0 && len(insights.UnmatchedResults) == 0 {
		_, err := fmt.Fprintln(w, "No insights")
		return err
	}
	table := style.NewTable("ID", "TITLE", "STATUS", "SCORE", "ACTIONS")
	for _, audit := range insights.Audits {
		status, score, actions := "", "", ""
		if audit.Result != nil {
			status = audit.Result.Status
			score = formatInsightScore(audit.Result.Score)
			actions = formatInsightActions(audit.Result.Actions)
		}
		table.Row(audit.Definition.ID, audit.Definition.Title, status, score, actions)
	}
	for _, result := range insights.UnmatchedResults {
		table.Row(result.AuditID, "", result.Status, formatInsightScore(result.Score), formatInsightActions(result.Actions))
	}
	return table.Render(w)
}

func (*runInsightsTableCodec) Decode(io.Reader, any) error {
	return errors.New("run insights table codec does not support decoding")
}

func formatInsightScore(score *InsightScore) string {
	if score == nil || len(score.Value) == 0 || string(score.Value) == "null" {
		return ""
	}
	value := strings.TrimSpace(string(score.Value))
	if score.Type == "" {
		return value
	}
	return score.Type + ":" + value
}

func formatInsightActions(actions json.RawMessage) string {
	if len(actions) == 0 || string(actions) == "null" {
		return ""
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, actions); err != nil {
		return strings.TrimSpace(string(actions))
	}
	return compact.String()
}

type getRunInsightsOpts struct {
	IO cmdio.Options
}

func (o *getRunInsightsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &runInsightsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *getRunInsightsOpts) Validate() error { return o.IO.Validate() }

func newRunsGetInsightsCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &getRunInsightsOpts{}
	cmd := &cobra.Command{
		Use:   "get-insights <run-id>",
		Short: "Get Cloud Insights for a k6 test run.",
		Long:  "Get the latest Cloud Insights execution for one k6 test run. The command joins audit definitions with their results.",
		Example: "  gcx k6 runs get-insights 12345\n" +
			"  gcx k6 runs get-insights 12345 -o json",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			executions, err := client.ListInsightExecutions(cmd.Context(), runID)
			if err != nil {
				return err
			}
			if len(executions.Executions) == 0 {
				return opts.IO.Encode(cmd.OutOrStdout(), &RunInsights{
					Audits: []JoinedInsightAudit{}, UnmatchedResults: []InsightAuditResult{},
				})
			}

			execution := executions.Executions[len(executions.Executions)-1]
			var definitions *InsightAuditsResponse
			var results *InsightAuditResultsResponse
			group, groupContext := errgroup.WithContext(cmd.Context())
			group.SetLimit(2)
			group.Go(func() error {
				var fetchErr error
				definitions, fetchErr = client.ListInsightAudits(groupContext, runID, execution.ID)
				return fetchErr
			})
			group.Go(func() error {
				var fetchErr error
				results, fetchErr = client.ListInsightAuditResults(groupContext, runID, execution.ID)
				return fetchErr
			})
			if err := group.Wait(); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), joinRunInsights(
				&execution, executions.Schema, definitions, results,
			))
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
