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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Pre-parse --agent flag before Cobra sees it. This must happen before
	// root.Command() because io.Options.BindFlags() reads agent.IsAgentMode()
	// during command construction to set the default output format.
	preParseAgentFlag()

	handleError(root.Command(formatVersion()).ExecuteContext(ctx))
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

func handleError(err error) {
	if err == nil {
		return
	}

	// Fast-path: context cancellation (e.g., SIGINT).
	// Skip detailed error formatting — exit cleanly and quickly.
	if errors.Is(err, context.Canceled) {
		os.Exit(fail.ExitCancelled)
	}

	detailedErr := fail.ErrorToDetailedError(err)
	if detailedErr == nil {
		os.Exit(1)
	}

	exitCode := 1
	if detailedErr.ExitCode != nil {
		exitCode = *detailedErr.ExitCode
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

	os.Exit(exitCode)
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
func parsePseudoVersion(v string) (string, string) {
	// Split on "-" from the right: ...-YYYYMMDDHHMMSS-commithash
	parts := strings.Split(v, "-")
	if len(parts) < 3 {
		return "", ""
	}
	hash := parts[len(parts)-1]
	ts := parts[len(parts)-2]
	if len(hash) < 7 || len(ts) != 14 {
		return "", ""
	}
	commit := hash[:7]
	date, err := time.Parse("20060102150405", ts)
	if err != nil {
		return commit, ""
	}
	return commit, date.UTC().Format(time.RFC3339)
}
