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
	"github.com/grafana/gcx/internal/telemetry"
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

	switch telemetry.ResolveMode(diagnosticsTelemetryValue) {
	case telemetry.ModeLog:
		if data, err := json.Marshal(buildUsageEvent(info, start, exitCode)); err == nil {
			fmt.Fprintln(os.Stderr, string(data))
		}
	case telemetry.ModeEnabled:
		telemetry.Export(buildUsageEvent(info, start, exitCode), start)
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

		IsTTY:   terminal.StdoutIsTerminal(),
		IsAgent: agent.IsAgentMode(),
		Agent:   agent.Name(),
		// TargetKind needs the resolved config context; resolving it requires
		// a keychain-free context loader, which is still a follow-up. Empty
		// until then.
	}

	// The provider is the top-level command, the first segment of the path.
	if fields := strings.Fields(info.Command); len(fields) > 0 {
		event.Provider = fields[0]
	}

	event.DeviceID, event.DeviceIDPersisted = telemetry.DeviceID()
	event.CIProvider, event.IsCI = telemetry.DetectCI()

	switch {
	case info.Help && exitCode == 0:
		event.Outcome = telemetry.OutcomeHelp
	case exitCode == 0:
		event.Outcome = telemetry.OutcomeOK
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
