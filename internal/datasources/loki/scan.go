package loki

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

const defaultScanBytes int64 = 10_000_000_000

// ScanOpts is shared by all CLI routes that execute Loki queries. Approval is
// deliberately independent of destructive-operation/global auto-approval flags.
type ScanOpts struct {
	Estimate       bool
	Yes            bool
	ApproveUnknown bool
	flags          *pflag.FlagSet
}

func (o *ScanOpts) Setup(flags *pflag.FlagSet) {
	o.flags = flags
	flags.BoolVar(&o.Estimate, "estimate-scan", false, "Estimate indexed log volume to scan, in bytes, without executing the query")
	flags.BoolVar(&o.Yes, "yes", false, "Approve this Loki query if its estimated scan exceeds 10GB (not a runtime ceiling)")
	flags.BoolVar(&o.ApproveUnknown, "approve-unknown-scan", false, "Approve this Loki query when scan volume cannot be estimated")
}

func (o *ScanOpts) Requested() bool {
	return o.flags != nil && (o.flags.Changed("estimate-scan") || o.flags.Changed("yes") || o.flags.Changed("approve-unknown-scan"))
}

func (o *ScanOpts) Validate(output string) error {
	if o.Estimate && (output == "raw" || output == "graph") {
		return scanUsage("--estimate-scan requires table, wide, json, yaml, or agents output")
	}
	return nil
}

// Run returns either an estimate or the normal query response. Output encoding
// remains the caller's responsibility, including in the generic dispatcher.
func (o *ScanOpts) Run(ctx context.Context, client *loki.Client, uid string, req loki.QueryRequest, metric bool, in io.Reader, out io.Writer) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.IsRange() {
		req.Start = req.Start.Truncate(time.Millisecond)
		req.End = req.End.Truncate(time.Millisecond)
		if !req.End.After(req.Start) {
			return nil, scanUsage("Loki query end must be later than start")
		}
	}
	if req.EvaluationTime.IsZero() {
		req.EvaluationTime = time.Now()
	}
	req.EvaluationTime = req.EvaluationTime.Truncate(time.Millisecond)
	estimate, err := client.EstimateScan(ctx, uid, req)
	if err != nil {
		var apiErr *queryerror.APIError
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			(errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden)) {
			return nil, err
		}
		estimate.Reason = err.Error()
		estimate.Caveat = "Scan volume is unknown; approval does not establish a runtime ceiling."
		estimate.Hints = []string{loki.ScanStatsRecoveryHint}
	}
	estimate.Threshold = defaultScanBytes
	estimate.ApprovalRequired = estimate.Bytes == nil || *estimate.Bytes > defaultScanBytes
	if o.Estimate {
		return estimate, nil
	}
	if err := o.authorize(estimate, in, out); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if metric {
		resp, err := client.MetricQuery(ctx, uid, req)
		if err == nil {
			reportScan(out, resp.Data.Stats)
		}
		return resp, err
	}
	resp, err := client.Query(ctx, uid, req)
	if err == nil {
		reportScan(out, resp.Data.Stats)
	}
	return resp, err
}

func (o *ScanOpts) authorize(e *loki.ScanEstimate, in io.Reader, out io.Writer) error {
	if !e.ApprovalRequired || e.Bytes == nil && o.ApproveUnknown || e.Bytes != nil && o.Yes {
		return nil
	}
	details := fmt.Sprintf("Datasource: %s\nQuery: %s\nRange: %s to %s\n%s", e.DatasourceUID, e.Query, e.Start.Format(time.RFC3339Nano), e.End.Format(time.RFC3339Nano), e.Caveat)
	if e.ScanStart != nil && e.ScanEnd != nil {
		details += fmt.Sprintf("\nScan range: %s to %s", e.ScanStart.Format(time.RFC3339Nano), e.ScanEnd.Format(time.RFC3339Nano))
	}
	var suggestion string
	if e.Bytes == nil {
		details += "\nEstimated scan: unknown. " + e.Reason
		suggestion = "After obtaining approval for unknown volume, repeat this invocation with --approve-unknown-scan."
	} else {
		details += fmt.Sprintf("\nEstimated scan: %.3f GB (%d bytes); approval threshold: %d bytes.", float64(*e.Bytes)/1e9, *e.Bytes, defaultScanBytes)
		suggestion = "After obtaining approval, repeat this invocation with --yes."
	}
	if agent.IsAgentMode() || !scanTerminal(in) || !scanTerminal(out) {
		return &gcxerrors.DetailedError{Summary: "Query scan approval required", Details: details,
			Suggestions: append(append([]string{}, e.Hints...), suggestion), ExitCode: new(gcxerrors.ExitUsageError)}
	}
	fmt.Fprintln(out, details)
	return o.prompt(e, in, out)
}

func (o *ScanOpts) prompt(e *loki.ScanEstimate, in io.Reader, out io.Writer) error {
	if e.Bytes == nil {
		fmt.Fprint(out, "Approve unknown scan volume? [y/N] ")
	} else {
		fmt.Fprint(out, "Execute this query with an estimated scan above 10GB? [y/N] ")
	}
	answer, err := bufio.NewReader(in).ReadString('\n')
	answer = strings.TrimSpace(answer)
	if err != nil || !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
		return &gcxerrors.DetailedError{Summary: "Query cancelled", ExitCode: new(gcxerrors.ExitCancelled)}
	}
	return nil
}

func scanTerminal(v any) bool {
	f, ok := v.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(f.Fd()))
}

func scanUsage(message string) error {
	return &gcxerrors.DetailedError{Summary: "Invalid command usage", Details: message, ExitCode: new(gcxerrors.ExitUsageError)}
}

func reportScan(out io.Writer, stats *loki.QueryStats) {
	if stats == nil {
		fmt.Fprintln(out, "Loki processed volume: unknown (backend did not provide scan statistics).")
		return
	}
	fmt.Fprintf(out, "Loki processed %.3f GB in %.3fs; processed volume is not billable GB.\n", float64(stats.Summary.TotalBytesProcessed)/1e9, stats.Summary.ExecTime)
}
