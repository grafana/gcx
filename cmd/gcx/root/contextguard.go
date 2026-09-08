package root

import (
	"fmt"
	"os"
	"strings"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// contextExemptRoutes are the command routes that may run without naming a
// context under strict context mode, expressed relative to the root command. A
// route covers its whole subtree.
//
// Two kinds of command belong here: those that never reach a Grafana or Cloud
// environment (local metadata, local file operations, shell plumbing), and
// bootstrapping commands that carry their own destination — `login` takes the
// context name as its argument, so it cannot inherit the wrong one.
//
//nolint:gochecknoglobals // Static policy table, read through requiresExplicitContext.
var contextExemptRoutes = []string{
	"agent",       // Local: skills installer, invocation-log pruning.
	"cloud login", // Bootstrapping: names its own destination.
	"commands",    // Local: command catalog for agents.
	"completion",  // Shell plumbing.
	"config",      // Operates on the config file itself (see contextRequiredRoutes).
	"dev generate",
	"dev lint",
	"dev scaffold",
	"help",      // Cobra's help command.
	"help-tree", // Local: command tree for agent context.
	"instrumentation check",
	"instrumentation explain",
	"instrumentation list-explanations",
	"login",     // Bootstrapping: names its own destination.
	"providers", // Local: registered provider list.
	"resources list-examples",
	"version",
}

// contextRequiredRoutes are enforced even though an ancestor route is exempt.
//
//nolint:gochecknoglobals // Static policy table, read through requiresExplicitContext.
var contextRequiredRoutes = []string{
	"config check", // Connects to the environment to verify it.
}

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

	// A group node or a help/version request only prints text.
	if !cmd.Runnable() || boolFlagSet(cmd, "help") || boolFlagSet(cmd, "version") {
		return nil
	}

	if !requiresExplicitContext(cmd) || contextIsExplicit(rootCmd, cmd) {
		return nil
	}

	return missingContextError(cmd)
}

// requiresExplicitContext reports whether cmd may reach a Grafana or Cloud
// environment, and so must name the one it means. Commands are enforced unless
// listed as exempt, so a newly added command is covered by default.
func requiresExplicitContext(cmd *cobra.Command) bool {
	route := commandRoute(cmd)

	// `instrumentation check` validates the local workstation and reaches a
	// stack only to have Grafana Assistant write the fix plan.
	if route == "instrumentation check" {
		return flagValue(cmd, "fix-plan") == "assistant"
	}

	for _, required := range contextRequiredRoutes {
		if routeCovers(required, route) {
			return true
		}
	}
	for _, exempt := range contextExemptRoutes {
		if routeCovers(exempt, route) {
			return false
		}
	}
	return true
}

// contextIsExplicit reports whether the invocation names its target itself,
// rather than inheriting whichever context the config file currently selects.
//
// GRAFANA_SERVER counts: it overrides the destination for the invocation, so
// the command cannot be redirected by another session's `config use-context`.
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

// commandRoute returns cmd's path relative to the root command, e.g.
// "config check". The root's own name is dropped because it comes from
// path.Base(os.Args[0]) and therefore changes when the binary is renamed.
func commandRoute(cmd *cobra.Command) string {
	names := []string{}
	for c := cmd; c.HasParent(); c = c.Parent() {
		names = append([]string{c.Name()}, names...)
	}
	return strings.Join(names, " ")
}

// routeCovers reports whether route is prefix, or a command below it. Matching
// is per path component, so "dev lint" does not cover "dev lint-preview".
func routeCovers(prefix, route string) bool {
	return route == prefix || strings.HasPrefix(route, prefix+" ")
}

func flagValue(cmd *cobra.Command, name string) string {
	f := cmd.Flags().Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}
