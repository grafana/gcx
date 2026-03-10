package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/grafana/grafanactl/cmd/grafanactl/fail"
	"github.com/grafana/grafanactl/cmd/grafanactl/root"
	"github.com/grafana/grafanactl/internal/agent"
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

	exitCode := 1
	detailedErr := fail.ErrorToDetailedError(err)

	if detailedErr == nil {
		os.Exit(exitCode)
	}

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
	if version == "" {
		version = "SNAPSHOT"
	}

	return fmt.Sprintf("%s built from %s on %s", version, commit, date)
}
