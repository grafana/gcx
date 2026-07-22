package resources

import (
	"io"

	"github.com/grafana/gcx/internal/config"
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

// writeMutationSummary writes the "N resources <verb>, M errors" line for push and delete,
// picking the style from the counts and adding the skipped note when the guard skipped any.
func writeMutationSummary(w io.Writer, verb string, summary *remote.OperationSummary) {
	skipped := summary.SkippedCount()

	printer := cmdio.Success
	if summary.FailedCount() != 0 {
		printer = cmdio.Warning
		if summary.SuccessCount() == 0 {
			printer = cmdio.Error
		}
	} else if skipped > 0 && summary.SuccessCount() == 0 {
		// Nothing was actually verified; don't style it as a clean success.
		printer = cmdio.Warning
	}

	if skipped > 0 {
		printer(w, "%d resources %s, %d errors (%d skipped: not server-verified)",
			summary.SuccessCount(), verb, summary.FailedCount(), skipped)
		return
	}
	printer(w, "%d resources %s, %d errors", summary.SuccessCount(), verb, summary.FailedCount())
}
