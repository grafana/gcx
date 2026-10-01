package check

import (
	"context"
	"io"

	"github.com/grafana/gcx/internal/host"
	otelutils "github.com/grafana/otel-checker/checks/utils"
)

// checker is the minimal interface required to invoke the otel-checker
// library. Tests substitute a fake; the real implementation is
// otelchecks.Run.
type checker func(ctx context.Context, cmd otelutils.Commands) *otelutils.Reporter

// runWith executes the checker and returns the typed result snapshot. Slices
// are normalized to non-nil for F-AGENT-01 compliance (empty JSON arrays,
// not null). Production code passes otelchecks.Run (via Command →
// commandWith); tests pass a fake checker.
//
// The otel-checker library prints some diagnostics directly to the
// process-global os.Stdout (e.g. "Error parsing JSON: ..." on Java/Maven
// dependency parse failures, checks/sdk/java/maven.go). Left alone, those
// bytes would interleave with the single result document this command writes
// to stdout. The library call therefore runs under host.CaptureStdout, which
// forwards any such stray prints to diag (stderr) as diagnostics. The library
// inspects the local machine (env, files, toolchain subprocesses), so inside
// a sandbox it is not run at all and runWith returns an error.
func runWith(ctx context.Context, cmd otelutils.Commands, c checker, diag io.Writer) (otelutils.Results, error) {
	var reporter *otelutils.Reporter
	if err := host.CaptureStdout(ctx, diag, func() {
		reporter = c(ctx, cmd)
	}); err != nil {
		return otelutils.Results{}, err
	}
	results := reporter.Results()

	if results.Checks == nil {
		results.Checks = []otelutils.ComponentResult{}
	}
	if results.Warnings == nil {
		results.Warnings = []otelutils.ComponentResult{}
	}
	if results.Errors == nil {
		results.Errors = []otelutils.ComponentResult{}
	}
	return results, nil
}
