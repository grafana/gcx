package root

import (
	"sort"
	"strings"
	"sync/atomic"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TelemetryInfo contains the usage stats properties that PersistentPreRun
// records for an anonymous usage event. No argument or flag values must be
// stored.
type TelemetryInfo struct {
	Command      string // command path without the root name, e.g. "resources get"
	Flags        string // sorted, comma-separated names of the flags the user set
	OutputFormat string
	Help         bool // did the user call gcx help or use --help?
	Suppress     bool // this event must not get emitted
}

//nolint:gochecknoglobals // written once per process from PersistentPreRun.
var telemetryInfo atomic.Pointer[TelemetryInfo]

// CurrentTelemetryInfo returns the telemetry info for the current invocation.
func CurrentTelemetryInfo() *TelemetryInfo {
	return telemetryInfo.Load()
}

// recordTelemetryInfo writes telemetryInfo from the current command and args.
func recordTelemetryInfo(cmd *cobra.Command, args []string) {
	isHelp := cmd.Name() == "help"

	// For `gcx help <path>` the resolved command is help itself; record the
	// command the user asked about instead.
	//
	// `gcx help resources get` records "resources get", not "help"
	target := cmd
	if isHelp {
		if found, _, err := cmd.Root().Find(args); err == nil {
			target = found
		}
	}

	telemetryInfo.Store(&TelemetryInfo{
		Command:      trimCommandRoot(target),
		Flags:        changedFlagNames(cmd),
		OutputFormat: resolvedOutputFormat(cmd),
		Help:         isHelp,
		// Suppression applies to the help target too: `gcx help telemetry`
		// must stay as unrecorded as `gcx telemetry --help`.
		Suppress: telemetrySuppressed(cmd) || telemetrySuppressed(target),
	})
}

// FallbackTelemetryInfo creates the usage-event info when PersistentPreRun
// never ran, so nothing was recorded. That happens in two ways:
//
//   - The command line failed to parse (unknown command, unknown flag) and
//     the exit code is nonzero. These return Suppress, so the caller emits
//     no event. Recording parse failures as their own outcome is a TODO in
//     #578.
//   - Cobra answered the invocation itself before any hooks could run:
//     things like --help or --version, or a command with no action of its
//     own (like `gcx` or `gcx resources`). The exit code is zero and
//     cobra printed the help screen (or the version string) as the
//     command's only output.
//
// For the zero-exit case, the command is re-resolved with Find and its flags
// read as cobra parsed them. Scanning os.Args instead would misread forms
// like --help=true.
func FallbackTelemetryInfo(rootCmd *cobra.Command, args []string, exitCode int) *TelemetryInfo {
	if exitCode != 0 {
		return &TelemetryInfo{Suppress: true}
	}
	target, _, err := rootCmd.Find(args)
	if err != nil {
		return &TelemetryInfo{Suppress: true}
	}
	if telemetrySuppressed(target) {
		return &TelemetryInfo{Suppress: true}
	}
	if boolFlagSet(target, "help") || !target.Runnable() {
		return &TelemetryInfo{Command: trimCommandRoot(target), Help: true}
	}
	return &TelemetryInfo{Suppress: true}
}

// trimCommandRoot returns the command path without the root name, e.g.
// "resources get" for `gcx resources get`. The root prefix is its own
// CommandPath, which honours the display-name annotation where Name does not.
func trimCommandRoot(cmd *cobra.Command) string {
	return strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), cmd.Root().CommandPath()))
}

// changedFlagNames returns the names of the flags the user set, sorted and
// comma-separated. Names only: values may identify people or resources.
func changedFlagNames(cmd *cobra.Command) string {
	var names []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		names = append(names, f.Name)
	})
	sort.Strings(names)
	return strings.Join(names, ",")
}

// knownOutputFormats guards the privacy invariant: --output is a rendering
// format on most commands, but on some it is a filesystem path (`gcx dev
// linter new --output <dir>`), so only enumerated formats are ever recorded.
//
//nolint:gochecknoglobals
var knownOutputFormats = map[string]bool{
	"agents": true, "compact": true, "graph": true, "json": true,
	"pretty": true, "table": true, "text": true, "wide": true, "yaml": true,
}

func resolvedOutputFormat(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("output"); f != nil {
		if v := strings.ToLower(f.Value.String()); knownOutputFormats[v] {
			return v
		}
		return ""
	}
	if boolFlagSet(cmd, "json") {
		return "json"
	}
	return ""
}

// isShellPlumbingCommand reports whether name is a shell-integration command:
// completion and cobra's hidden completion helpers.
func isShellPlumbingCommand(name string) bool {
	switch name {
	case "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	}
	return false
}

// telemetrySuppressed reports whether the invocation must emit nothing: shell
// plumbing, version, and the telemetry command itself. The ancestor chain is
// checked, so subcommands like `completion zsh` are suppressed too.
func telemetrySuppressed(cmd *cobra.Command) bool {
	for c := cmd; c.HasParent(); c = c.Parent() {
		if isShellPlumbingCommand(c.Name()) || c.Name() == "version" || c.Name() == "telemetry" {
			return true
		}
	}
	return boolFlagSet(cmd, "version")
}

func boolFlagSet(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	return f != nil && f.Changed && f.Value.String() == "true"
}
