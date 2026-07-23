package resources

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/remote"
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
