package loki

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
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
	ApproveScan    string
	ApproveUnknown bool
	threshold      int64
	flags          *pflag.FlagSet
}

func (o *ScanOpts) Setup(flags *pflag.FlagSet) {
	o.flags = flags
	flags.BoolVar(&o.Estimate, "estimate", false, "Estimate Loki indexed scan volume without executing the query")
	flags.StringVar(&o.ApproveScan, "approve-scan", "", "Approve up to this estimated Loki volume for this invocation (e.g. 25GB); default gate is 10GB, not a runtime ceiling")
	flags.BoolVar(&o.ApproveUnknown, "approve-unknown-scan", false, "Approve this Loki query when scan volume cannot be estimated")
}

func (o *ScanOpts) Requested() bool {
	return o.flags != nil && (o.flags.Changed("estimate") || o.flags.Changed("approve-scan") || o.flags.Changed("approve-unknown-scan"))
}

func (o *ScanOpts) Validate(output string) error {
	o.threshold = defaultScanBytes
	if o.flags != nil && o.flags.Changed("approve-scan") || o.ApproveScan != "" {
		if o.ApproveUnknown {
			return scanUsage("--approve-scan and --approve-unknown-scan cannot be combined")
		}
		v, err := parseScanBytes(o.ApproveScan)
		if err != nil {
			return scanUsage(err.Error())
		}
		o.threshold = v
	}
	if o.Estimate && (output == "raw" || output == "graph") {
		return scanUsage("--estimate requires table, wide, json, yaml, or agents output")
	}
	return nil
}

// Run returns either an estimate or the normal query response. Output encoding
// remains the caller's responsibility, including in the generic dispatcher.
func (o *ScanOpts) Run(ctx context.Context, client *loki.Client, uid string, req loki.QueryRequest, metric bool, in io.Reader, out io.Writer) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.threshold == 0 {
		o.threshold = defaultScanBytes
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
		estimate = &loki.ScanEstimate{DatasourceUID: uid, Query: req.Query, Start: req.Start, End: req.End,
			Reason: err.Error(), Caveat: "Scan volume is unknown; approval does not establish a runtime ceiling.",
			Hints: []string{"Retry with --estimate, narrow --since, or explicitly approve unknown volume."}}
	}
	if !req.IsRange() {
		estimate.Start = req.EvaluationTime.Add(-time.Minute)
		estimate.End = req.EvaluationTime
	}
	estimate.Threshold = o.threshold
	estimate.ApprovalRequired = estimate.Bytes == nil || *estimate.Bytes > o.threshold
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
	if !e.ApprovalRequired || e.Bytes == nil && o.ApproveUnknown {
		return nil
	}
	details := fmt.Sprintf("Datasource: %s\nQuery: %s\nRange: %s to %s\n%s", e.DatasourceUID, e.Query, e.Start.Format(time.RFC3339Nano), e.End.Format(time.RFC3339Nano), e.Caveat)
	var suggestion string
	if e.Bytes == nil {
		details += "\nEstimated scan: unknown. " + e.Reason
		suggestion = "After obtaining approval for unknown volume, repeat this invocation with --approve-unknown-scan."
	} else {
		details += fmt.Sprintf("\nEstimated scan: %.3f GB (%d bytes); approval threshold: %d bytes.", float64(*e.Bytes)/1e9, *e.Bytes, o.threshold)
		suggestion = fmt.Sprintf("After obtaining approval, repeat this invocation with --approve-scan=%dB or a larger finite budget.", *e.Bytes)
	}
	if o.ApproveScan != "" || agent.IsAgentMode() || !scanTerminal(in) || !scanTerminal(out) {
		return &gcxerrors.DetailedError{Summary: "Query scan approval required", Details: details,
			Suggestions: []string{"Repeat this invocation with --estimate to inspect volume, or narrow --since and indexed labels.", suggestion}, ExitCode: new(gcxerrors.ExitUsageError)}
	}
	fmt.Fprintln(out, details)
	return o.prompt(e, in, out)
}

func (o *ScanOpts) prompt(e *loki.ScanEstimate, in io.Reader, out io.Writer) error {
	if e.Bytes == nil {
		fmt.Fprint(out, "Approve unknown scan volume? [y/N] ")
	} else {
		fmt.Fprint(out, "Enter the finite volume to approve (e.g. 25GB), or Enter to cancel: ")
	}
	answer, err := bufio.NewReader(in).ReadString('\n')
	answer = strings.TrimSpace(answer)
	if err != nil || answer == "" || e.Bytes == nil && !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
		return &gcxerrors.DetailedError{Summary: "Query cancelled", ExitCode: new(gcxerrors.ExitCancelled)}
	}
	if e.Bytes == nil {
		return nil
	}
	budget, err := parseScanBytes(answer)
	if err != nil {
		return scanUsage(err.Error())
	}
	if budget < *e.Bytes {
		return scanUsage("Approved volume is smaller than the estimated scan; query was not executed")
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

func parseScanBytes(s string) (int64, error) {
	units := []struct {
		suffix string
		size   int64
	}{{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}, {"B", 1}}
	for _, unit := range units {
		if !strings.HasSuffix(s, unit.suffix) {
			continue
		}
		number := strings.TrimSuffix(s, unit.suffix)
		if number == "" || strings.Trim(number, "0123456789.") != "" {
			break
		}
		n, ok := new(big.Rat).SetString(number)
		if !ok {
			break
		}
		n.Mul(n, new(big.Rat).SetInt64(unit.size))
		if n.Sign() > 0 && n.IsInt() && n.Num().IsInt64() {
			return n.Num().Int64(), nil
		}
		break
	}
	return 0, fmt.Errorf("--approve-scan requires a positive finite byte size using B, KB, MB, GB, or TB (e.g. 25GB); got %q", s)
}

func reportScan(out io.Writer, stats *loki.QueryStats) {
	if stats == nil {
		fmt.Fprintln(out, "Loki processed volume: unknown (backend did not provide scan statistics).")
		return
	}
	fmt.Fprintf(out, "Loki processed %.3f GB in %.3fs; processed volume is not billable GB.\n", float64(stats.Summary.TotalBytesProcessed)/1e9, stats.Summary.ExecTime)
}
