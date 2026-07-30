package resources

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/spf13/pflag"
)

const (
	assumeServerDryRunFlag  = "assume-server-dry-run"
	assumeServerDryRunUsage = "Assert that the given resources honor server-side dry-run, augmenting the built-in allowlist. " +
		"Repeatable or comma-separated, each value a GroupResource string (<resource>.<group>), e.g. alertrules.rules.alerting.grafana.app"
)

// bindAssumeServerDryRunFlag registers the --assume-server-dry-run flag on the given flag
// set. Only the mutating dry-run commands (push, delete, validate) bind it; it is not a
// persistent flag on the resources group because it is meaningless for get/pull/list-types/etc.
func bindAssumeServerDryRunFlag(flags *pflag.FlagSet, target *[]string) {
	flags.StringSliceVar(target, assumeServerDryRunFlag, nil, assumeServerDryRunUsage)
}

// dryRunGuardConfig builds the guard config from the per-context config list merged with the
// --assume-server-dry-run flag, sending guard warnings to warn (stderr).
func dryRunGuardConfig(current *config.Context, flagValues []string, warn io.Writer) remote.GuardConfig {
	var assumed []string
	if current != nil {
		assumed = append(assumed, current.AssumeServerDryRun()...)
	}
	assumed = append(assumed, flagValues...)
	return remote.GuardConfig{AssumeServerDryRun: assumed, Warn: warn}
}

// partialBatchFailure reports a partial batch failure after the result
// document was already written: a typed stderr diagnostic (JSONL in agent
// mode, prose on a TTY) plus an EmittedError so the process exits
// ExitPartialFailure without a second stdout document. The old bare
// PartialFailureError return let reportError render an "Error: ..." stderr
// line; EmittedError suppresses that rendering, so the diagnostic is
// emitted explicitly here — mirroring get's partialGetFailure.
func partialBatchFailure(stderr io.Writer, op string, total, failed int) error {
	perr := gcxerrors.NewPartialFailureError(op, total, failed)
	cmdio.EmitWarn(stderr, perr.Error())
	return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, perr)
}

// captureBatchVolume records a completed batch operation's finalized counts for
// the anonymous usage event; buildUsageEvent turns them into bucket labels at
// exit.
//
// The contract is deliberately about the operation, not about the output. Call
// this once the operation itself has completed and its counts are final, which
// for pull means after the filesystem writes, since the receipt moves
// fetched-but-unwritten resources from succeeded to failed.
//
// Two consequences, both intended:
//
//   - A hard abort reports nothing. This is a choice, not a limitation: push,
//     delete and pull all still hold a usable summary on their abort paths, and
//     delete even prints those counts to stderr. Reporting them would make an
//     absent field mean either "not a batch command" or "aborted after doing
//     some work", so absence is kept to the single meaning "no finalized
//     count". The cost is that partial work done before an abort is invisible,
//     which the usage-statistics page states outright.
//   - A later output failure changes nothing. If 47 dashboards were pushed and
//     then rendering or the stdout write failed, those 47 are still on the
//     server. Suppressing the count would understate work that really happened.
//     This is why capture does not depend on the encode: Encode also returns nil
//     for --jq and --json discovery, which print something other than the result
//     document, so its return value never was evidence about the operation.
//
// Telemetry must never affect the command's outcome, so this returns nothing and
// cannot fail.
func captureBatchVolume(summary cmdio.MutationSummary, dryRun bool) {
	capture.SetBatch(capture.Batch{
		Succeeded: summary.Succeeded,
		Failed:    summary.Failed,
		Skipped:   summary.Skipped,
		DryRun:    dryRun,
	})
}

// batchMutationFromSummary converts an OperationSummary into the shared
// BatchMutation result: counts plus enumerated failures (successes and skips
// are counted, not listed). This value is what push/delete write to stdout
// through the codec system — the agents codec and explicit -o json/yaml get
// the structured document; the text codec below reproduces the human line.
func batchMutationFromSummary(action string, summary *remote.OperationSummary, dryRun bool) cmdio.BatchMutation {
	result := cmdio.NewBatchMutation(action)
	result.Summary = cmdio.MutationSummary{
		Succeeded: summary.SuccessCount(),
		Failed:    summary.FailedCount(),
		Skipped:   summary.SkippedCount(),
	}
	result.DryRun = dryRun
	for _, failure := range summary.Failures() {
		target := cmdio.MutationTarget{}
		if failure.Resource != nil {
			target.Kind = failure.Resource.Kind()
			target.Name = failure.Resource.Name()
		}
		msg := ""
		if failure.Error != nil {
			msg = failure.Error.Error()
		}
		result.Failures = append(result.Failures, cmdio.MutationFailure{Target: target, Error: msg})
	}
	return result
}

// mutationSummaryCodec is the human "text" codec for BatchMutation values:
// it renders exactly the one-line "N resources <verb>, M errors" summary
// push and delete have always printed (style picked from the counts, skipped
// note added when the guard skipped any), so default human stdout stays
// byte-identical to the pre-codec output.
type mutationSummaryCodec struct{}

func (c *mutationSummaryCodec) Format() format.Format { return "text" }

func (c *mutationSummaryCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *mutationSummaryCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(cmdio.BatchMutation)
	if !ok {
		return errors.New("invalid data type for mutation summary codec: expected BatchMutation")
	}

	skipped := result.Summary.Skipped

	printer := cmdio.Success
	if result.Summary.Failed != 0 {
		printer = cmdio.Warning
		if result.Summary.Succeeded == 0 {
			printer = cmdio.Error
		}
	} else if skipped > 0 && result.Summary.Succeeded == 0 {
		// Nothing was actually verified; don't style it as a clean success.
		printer = cmdio.Warning
	}

	if skipped > 0 {
		printer(w, "%d resources %s, %d errors (%d skipped: not server-verified)",
			result.Summary.Succeeded, result.Action, result.Summary.Failed, skipped)
		return nil
	}
	printer(w, "%d resources %s, %d errors", result.Summary.Succeeded, result.Action, result.Summary.Failed)
	return nil
}
