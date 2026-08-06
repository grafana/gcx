package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/agentlog"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/telemetry"
	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/grafana/gcx/internal/terminal"
	appversion "github.com/grafana/gcx/internal/version"
	"github.com/spf13/cobra"
)

// diagnosticsConfig memoizes the layered config read shared by agentlog setup
// at startup and telemetry mode resolution at exit.
//
//nolint:gochecknoglobals
var diagnosticsConfig = sync.OnceValue(func() *internalconfig.DiagnosticsConfig {
	return internalconfig.LoadDiagnostics(context.Background())
})

// emitUsageEvent builds and emits the anonymous usage event for this
// invocation. It must never affect the command's exit code or prompt the user.
// It must only be called once per invocation.
func emitUsageEvent(cmd *cobra.Command, start time.Time, exitCode int) {
	info := root.CurrentTelemetryInfo()
	if info == nil {
		info = root.FallbackTelemetryInfo(cmd, os.Args[1:], exitCode)
	}
	if info.Suppress {
		return
	}

	mode := telemetry.ResolveMode(diagnosticsTelemetryValue)

	// One-time opt-out notice for interactive users; the command's own output
	// has already been written by this point. Gated on stderr's TTY state
	// because that is where the notice goes: piped stdout must not hide it,
	// and discarded stderr must not consume the one-shot flag.
	_, isCI := telemetry.DetectCI()
	telemetry.MaybeShowFirstRunNotice(os.Stderr, mode, terminal.StderrIsTerminal(), isCI, agent.IsAgentMode())

	switch mode {
	case telemetry.ModeLog:
		if data, err := json.Marshal(buildUsageEvent(info, start, exitCode)); err == nil {
			fmt.Fprintln(os.Stderr, string(data))
		}
	case telemetry.ModeEnabled:
		telemetry.Export(buildUsageEvent(info, start, exitCode))
	}
}

func buildUsageEvent(info *root.TelemetryInfo, start time.Time, exitCode int) telemetry.Event {
	event := telemetry.Event{
		Service: telemetry.ServiceName,
		Version: appversion.Get(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,

		Command:      info.Command,
		Flags:        info.Flags,
		ExitCode:     exitCode,
		DurationMS:   time.Since(start).Milliseconds(),
		OutputFormat: info.OutputFormat,

		IsTTY:      terminal.StdoutIsTerminal(),
		IsAgent:    agent.IsAgentMode(),
		Agent:      agent.Name(),
		TargetKind: internalconfig.CapturedTargetKind(),
	}

	// The provider is the top-level command, the first segment of the path.
	if fields := strings.Fields(info.Command); len(fields) > 0 {
		event.Provider = fields[0]
	}

	// Batch volume, present only when a resource operation ran to a finalized
	// count. It is deliberately not conditional on the result document having
	// been emitted: the work happened either way. Counts become bucket labels
	// here rather than at capture time, so the wire vocabulary stays in this
	// package alongside the rest of the privacy filtering.
	if b := capture.CurrentBatch(); b != nil {
		succeeded := telemetry.Bucket(b.Succeeded)
		failed := telemetry.Bucket(b.Failed)
		skipped := telemetry.Bucket(b.Skipped)
		dryRun := b.DryRun
		event.BatchSucceededBucket = &succeeded
		event.BatchFailedBucket = &failed
		event.BatchSkippedBucket = &skipped
		event.DryRun = &dryRun
	}

	event.DeviceID, event.DeviceIDPersisted = telemetry.DeviceID()
	event.CIProvider, event.IsCI = telemetry.DetectCI()

	switch {
	case info.Help && exitCode == 0:
		event.Outcome = telemetry.OutcomeHelp
	case exitCode == 0:
		event.Outcome = telemetry.OutcomeOK
	case exitCode == gcxerrors.ExitCancelled:
		// Classified on the final exit code, not on how it was reached, so an
		// interrupt, a declined confirmation prompt and a server-reported
		// cancellation all report the same outcome — and the event does not say
		// which. Stopping early is not a failure, so error_kind stays empty; it
		// has no omitempty, so the field is still on the wire.
		event.Outcome = telemetry.OutcomeCanceled
	default:
		event.Outcome = telemetry.OutcomeRuntimeError
		event.ErrorKind = agentlog.KindFromExitCode(exitCode)
	}

	return event
}

func diagnosticsTelemetryValue() string {
	if d := diagnosticsConfig(); d != nil {
		return d.Telemetry
	}
	return ""
}
