package root

import (
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/suggest"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagUsageError enriches pflag unknown-flag errors with did-you-mean
// suggestions matched against the failing command's local and inherited
// flags, plus ready-to-run corrections built from the original invocation.
// Other flag errors are returned unchanged and keep Cobra's default handling.
func flagUsageError(cmd *cobra.Command, err error, invocationArgs []string) error {
	var notExist *pflag.NotExistError
	if cmd == nil || !errors.As(err, &notExist) {
		return err
	}

	suggestions := []string{}
	corrections := []gcxerrors.Correction{}
	// Fuzzy matching only makes sense for long flags; a mistyped shorthand
	// is a single character away from most valid shorthands.
	if notExist.GetSpecifiedShortnames() == "" {
		unknown := "--" + notExist.GetSpecifiedName()
		for _, candidate := range suggest.Candidates(unknown, availableFlagNames(cmd)) {
			suggestions = append(suggestions, fmt.Sprintf("Did you mean '%s'?", candidate))
			correction := gcxerrors.Correction{Hint: flagUsageHint(cmd, candidate)}
			if corrected, ok := substituteFlag(invocationArgs, unknown, candidate); ok {
				correction.Command = shellJoin(append([]string{cmd.Root().Name()}, corrected...))
				corrections = append(corrections, correction)
			}
		}
	}

	commandPath := strings.TrimSpace(cmd.CommandPath())
	message := err.Error()
	if commandPath != "" {
		message = fmt.Sprintf("%s for %q", message, commandPath)
		suggestions = append(suggestions, fmt.Sprintf("Run '%s --help' for full usage and examples", commandPath))
	}

	return &fail.UsageError{
		Message:     message,
		Suggestions: suggestions,
		Corrections: corrections,
		Cause:       err,
	}
}

// availableFlagNames returns the "--name" tokens of the command's visible
// local and inherited flags.
func availableFlagNames(cmd *cobra.Command) []string {
	names := []string{}
	collect := func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		names = append(names, "--"+f.Name)
	}
	cmd.Flags().VisitAll(collect)
	cmd.InheritedFlags().VisitAll(collect)
	return names
}

// flagUsageHint returns the usage string of the named flag ("--name"),
// checked against both local and inherited flags.
func flagUsageHint(cmd *cobra.Command, name string) string {
	name = strings.TrimPrefix(name, "--")
	if f := cmd.Flags().Lookup(name); f != nil {
		return f.Usage
	}
	if f := cmd.InheritedFlags().Lookup(name); f != nil {
		return f.Usage
	}
	return ""
}

// substituteFlag replaces the unknown flag token with the candidate in a copy
// of the original invocation. It only substitutes when the token appears
// exactly once (as "--flag" or "--flag=value"); otherwise it reports false so
// no misleading correction is emitted.
func substituteFlag(args []string, unknown, candidate string) ([]string, bool) {
	matched := -1
	for i, arg := range args {
		if arg != unknown && !strings.HasPrefix(arg, unknown+"=") {
			continue
		}
		if matched != -1 {
			return nil, false
		}
		matched = i
	}
	if matched == -1 {
		return nil, false
	}

	corrected := make([]string, len(args))
	copy(corrected, args)
	corrected[matched] = candidate + strings.TrimPrefix(args[matched], unknown)
	return corrected, true
}

// shellJoin joins argv tokens into a shell command string, single-quoting any
// token a POSIX shell would not pass through verbatim, so emitted corrections
// stay runnable as-is.
func shellJoin(tokens []string) string {
	quoted := make([]string, len(tokens))
	for i, token := range tokens {
		quoted[i] = shellQuote(token)
	}
	return strings.Join(quoted, " ")
}

const shellSafeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./=:@%+,"

func shellQuote(token string) string {
	if token != "" && strings.Trim(token, shellSafeChars) == "" {
		return token
	}
	return "'" + strings.ReplaceAll(token, "'", `'\''`) + "'"
}
