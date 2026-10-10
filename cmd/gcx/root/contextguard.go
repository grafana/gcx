package root

import (
	"fmt"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// EnforceContextSelection refuses an invocation that would silently fall back
// to current-context while strict context mode (GCX_REQUIRE_CONTEXT) is on.
//
// The check runs before Cobra dispatch rather than in the root command's
// PersistentPreRun because Cobra runs only the closest PersistentPreRun in the
// chain, and many provider groups define their own and chain back to root's by
// hand. A guard in that hook would be skipped by any subtree that forgot to
// chain — an omission that fails open, which is the one outcome a safety guard
// cannot have. Running pre-dispatch also routes the error through the same
// reporting path as ValidateArgs, so agent mode still gets a single JSON error
// whose exit code agrees with it.
//
// It builds its own command tree because resolving the invocation parses flags,
// which mutates flag state on the tree it walks.
func EnforceContextSelection(version string, args []string) error {
	if !strictContextEnabled() {
		return nil
	}

	return enforceContextSelection(Command(version), args)
}

// strictContextEnabled reads the switch straight from the environment rather
// than through LoadCLIOptions, so that a parse failure on an unrelated CLI
// option cannot lift the requirement. How the value is interpreted still lives
// with the option itself.
func strictContextEnabled() bool {
	opts := internalconfig.CLIOptions{RequireContext: os.Getenv("GCX_REQUIRE_CONTEXT")}
	return opts.RequireContextEnabled()
}

func enforceContextSelection(rootCmd *cobra.Command, args []string) error {
	if rootCmd == nil {
		return nil
	}

	trimmed, ok := trimLeadingRootFlags(rootCmd, args)
	if !ok {
		return nil // Cobra reports the flag error itself.
	}
	if len(trimmed) == 0 {
		return nil // Bare `gcx` prints help.
	}
	switch trimmed[0] {
	case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return nil
	}

	cmd, remaining, ok := traverseArgs(rootCmd, trimmed)
	if !ok || cmd == nil {
		return nil
	}

	// Cobra installs --help during execution, so it has to be registered here
	// for the parse below to recognize it rather than fail on an unknown flag.
	cmd.InitDefaultHelpFlag()
	if !parseGroupFlags(cmd, remaining) {
		return nil // Cobra reports the flag error itself.
	}

	// A group node or a help request only prints text.
	if !cmd.Runnable() || boolFlagSet(cmd, "help") {
		return nil
	}

	if !requiresExplicitContext(cmd) || contextIsExplicit(rootCmd, cmd) {
		return nil
	}

	return missingContextError(cmd)
}

// requiresExplicitContext reports whether cmd may reach a Grafana or Cloud
// environment, and so must name the one it means. Commands whose reach depends
// on their flags are decided here; everything else follows the path policy in
// internal/agent, which enforces any command it does not list as exempt.
func requiresExplicitContext(cmd *cobra.Command) bool {
	switch cmd.CommandPath() {
	case "gcx instrumentation check":
		// Validates the local workstation, and reaches a stack only to have
		// Grafana Assistant write the fix plan.
		return flagValue(cmd, "fix-plan") == "assistant"
	case "gcx commands":
		// A local catalog unless --validate checks it against a live instance.
		return boolFlagSet(cmd, "validate")
	}
	return agent.RequiresExplicitContextPath(cmd.CommandPath())
}

// contextIsExplicit reports whether the invocation names its target itself,
// rather than inheriting whichever context the config file currently selects.
func contextIsExplicit(rootCmd, cmd *cobra.Command) bool {
	// Both flag sets are consulted because subtrees such as `config` bind their
	// own --context, which shadows root's in the resolved command's flag set.
	// `gcx --context x config view` sets root's; `gcx config --context x view`
	// sets the subtree's.
	for _, flags := range []*pflag.FlagSet{cmd.Flags(), rootCmd.PersistentFlags()} {
		f := flags.Lookup("context")
		if f != nil && f.Changed && strings.TrimSpace(f.Value.String()) != "" {
			return true
		}
	}

	if cmd.CommandPath() == "gcx login" {
		return loginNamesTarget(cmd)
	}
	return false
}

// loginNamesTarget reports whether a `gcx login` invocation selects its target
// without current-context: login writes to the CONTEXT_NAME argument when one
// is given, otherwise to the context derived from --server or GRAFANA_SERVER,
// and only then falls back to current-context.
//
// GRAFANA_SERVER is honored here and nowhere else. On every other command it is
// overlaid onto the current context, which still supplies the credentials and
// the rest of the target, so it does not stop a retarget.
func loginNamesTarget(cmd *cobra.Command) bool {
	if len(cmd.Flags().Args()) > 0 {
		return true
	}
	if strings.TrimSpace(flagValue(cmd, "server")) != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv("GRAFANA_SERVER")) != ""
}

func missingContextError(cmd *cobra.Command) error {
	exitCode := gcxerrors.ExitUsageError

	return &gcxerrors.DetailedError{
		Summary: "This invocation does not name a context",
		Details: "GCX_REQUIRE_CONTEXT is set, so gcx will not fall back to current-context from the config " +
			"file. current-context is shared between sessions: another shell or agent can change it at any " +
			"time, which would send this command to a different environment than intended.",
		Suggestions: []string{
			fmt.Sprintf("Name the target explicitly: %s --context <name>", cmd.CommandPath()),
			"See the contexts you can choose from: gcx config list-contexts",
			"Drop the requirement for this shell: unset GCX_REQUIRE_CONTEXT",
		},
		ExitCode: &exitCode,
	}
}

func flagValue(cmd *cobra.Command, name string) string {
	f := cmd.Flags().Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}
