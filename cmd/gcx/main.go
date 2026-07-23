package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/agentlog"
	"github.com/grafana/gcx/internal/gcxerrors"
	appversion "github.com/grafana/gcx/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/mod/module"
)

// Version variables which are set at build time.
var (
	version string
	//nolint:gochecknoglobals
	commit string
	//nolint:gochecknoglobals
	date string
)

func main() {
	// used to measure command execution time
	start := time.Now()

	// stop is deliberately not deferred: every path out of main ends in
	// os.Exit (exitWith or the cancellation fast path), so a defer would
	// never run, and signal-handler cleanup is moot at process exit.
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)

	// Pre-parse --agent flag before Cobra sees it. This must happen before
	// root.Command() because io.Options.BindFlags() reads agent.IsAgentMode()
	// during command construction to set the default output format.
	preParseAgentFlag()
	if agent.IsAgentMode() {
		agentlog.Configure(loadDiagnosticsConfig())
	}

	formattedVersion := formatVersion()
	appversion.Set(version)
	appversion.SetBuildInfo(commit, date)

	cmd := root.Command(formattedVersion)
	boolFlags := collectBoolFlags(cmd)
	subCmds := collectSubCmds(cmd)

	// prefer sticking to err != nil format, than optimizing for calling exitWith
	// once
	if err := root.ValidateArgs(cmd, os.Args[1:]); err != nil {
		exitWith(cmd, start, reportError(err, boolFlags, subCmds))
	}

	err := cmd.ExecuteContext(ctx)

	// quick exit on context canceled
	if errors.Is(err, context.Canceled) {
		os.Exit(gcxerrors.ExitCancelled)
	}

	exitWith(cmd, start, reportError(err, boolFlags, subCmds))
}

// exitWith emits the usage event for this invocation, then exits. Every
// invocation ends here, except the cancellation fast path above.
func exitWith(cmd *cobra.Command, start time.Time, exitCode int) {
	emitUsageEvent(cmd, start, exitCode)
	os.Exit(exitCode)
}

// preParseAgentFlag scans os.Args for --agent / --agent=true / --agent=false
// and calls agent.SetFlag() accordingly. This runs before Cobra's flag parsing
// so that agent mode state is available during command construction.
func preParseAgentFlag() {
	for _, arg := range os.Args[1:] {
		if arg == "--" {
			return // stop scanning after double-dash
		}

		switch {
		case arg == "--agent":
			agent.SetFlag(true)
			return
		case strings.HasPrefix(arg, "--agent="):
			val := strings.ToLower(strings.TrimPrefix(arg, "--agent="))
			agent.SetFlag(val == "true" || val == "1" || val == "yes")
			return
		}
	}
}

// reportError prints the error (if any) to the right stream for the consumer,
// appends the agent invocation log entry, and returns the process exit code.
// It never exits; context cancellation is already handled in main before this
// is called.
func reportError(err error, boolFlags map[string]struct{}, subCmds map[string]bool) int {
	if err == nil {
		return 0
	}
	if exitCode, ok := gcxerrors.AlreadyReportedExitCode(err); ok {
		return exitCode
	}

	detailedErr := fail.ErrorToDetailedError(err)
	if detailedErr == nil {
		return 1
	}

	exitCode := 1
	if detailedErr.ExitCode != nil {
		exitCode = *detailedErr.ExitCode
	}

	if agent.IsAgentMode() && agentlog.IsEnabled() {
		_ = agentlog.Append(agentlog.Entry{
			Timestamp: time.Now(),
			Version:   appversion.Get(),
			Args:      agentlog.StripArgValues(os.Args[1:], boolFlags, subCmds),
			ErrorKind: agentlog.KindFromExitCode(exitCode),
			Error:     truncate(detailedErr.Summary, 200),
			ExitCode:  exitCode,
		})
	}

	if agent.IsAgentMode() || root.IsJSONFlagActive() {
		// Machine consumers get JSON on stdout only — the human-formatted
		// stderr error is noise for agents and scripts.
		if writeErr := detailedErr.WriteJSON(os.Stdout, exitCode); writeErr != nil {
			fmt.Fprintln(os.Stderr, detailedErr.Error())
		}
	} else {
		// Human consumers get the formatted error on stderr.
		fmt.Fprintln(os.Stderr, detailedErr.Error())
	}

	return exitCode
}

// collectBoolFlags walks the full command tree and returns a set of all boolean
// flag names (and their shorthands) so StripArgValues can skip consuming the
// next token for flags that take no value argument.
func collectBoolFlags(cmd *cobra.Command) map[string]struct{} {
	bools := make(map[string]struct{})
	var visit func(c *cobra.Command)
	visit = func(c *cobra.Command) {
		addBools := func(f *pflag.Flag) {
			if f.Value.Type() == "bool" || f.NoOptDefVal != "" {
				bools[f.Name] = struct{}{}
				if f.Shorthand != "" {
					bools[f.Shorthand] = struct{}{}
				}
			}
		}
		c.Flags().VisitAll(addBools)
		c.PersistentFlags().VisitAll(addBools)
		for _, sub := range c.Commands() {
			visit(sub)
		}
	}
	visit(cmd)
	return bools
}

// collectSubCmds walks the full command tree and returns a set of all registered
// subcommand names (and aliases). Positional args matching this set are safe to
// log; all other positionals are treated as values and redacted.
func collectSubCmds(cmd *cobra.Command) map[string]bool {
	names := make(map[string]bool)
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			names[sub.Name()] = true
			for _, alias := range sub.Aliases {
				names[alias] = true
			}
			walk(sub)
		}
	}
	walk(cmd)
	return names
}

// loadDiagnosticsConfig reads diagnostics settings from the layered gcx config.
// It runs on every invocation, so it uses a memoized result to avoid excessive
// reads.
func loadDiagnosticsConfig() agentlog.Config {
	d := diagnosticsConfig()
	if d == nil || !d.AgentInvocationLog {
		return agentlog.Config{}
	}
	return agentlog.Config{Enabled: true, LogDir: d.LogDir}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func formatVersion() string {
	// Fall back to build info when ldflags are not set (e.g. go install).
	if version == "" || commit == "" || date == "" {
		v, c, d := vcsInfo()
		if version == "" {
			version = v
		}
		if commit == "" {
			commit = c
		}
		if date == "" {
			date = d
		}
	}

	if version == "" {
		version = "SNAPSHOT"
	}

	return fmt.Sprintf("%s built from %s on %s", version, commit, date)
}

// vcsInfo extracts version, short commit hash, and timestamp from build info.
func vcsInfo() (string, string, string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", ""
	}
	var v, c, d string
	v = info.Main.Version
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if s.Value != "" {
				c = s.Value[:min(7, len(s.Value))]
			}
		case "vcs.time":
			d = s.Value
		}
	}
	// For go install builds, VCS settings are absent but the pseudo-version
	// contains the commit and timestamp: vX.Y.Z-0.YYYYMMDDHHMMSS-abcdef123456
	if c == "" || d == "" {
		pc, pd := parsePseudoVersion(v)
		if c == "" {
			c = pc
		}
		if d == "" {
			d = pd
		}
	}
	return v, c, d
}

// parsePseudoVersion extracts the short commit hash and timestamp from a Go
// pseudo-version string (e.g. v0.1.1-0.20260401105553-2fbda4a2dd27).
// Returns empty strings for non-pseudo versions.
func parsePseudoVersion(v string) (string, string) {
	// Strip +dirty or other non-standard build metadata that Go embeds
	// for local builds, as it is not valid semver and rejected by the module package.
	if i := strings.LastIndex(v, "+"); i > 0 {
		v = v[:i]
	}
	var c, d string
	if rev, err := module.PseudoVersionRev(v); err == nil && rev != "" {
		c = rev[:min(7, len(rev))]
	}
	if t, err := module.PseudoVersionTime(v); err == nil {
		d = t.UTC().Format(time.RFC3339)
	}
	return c, d
}
