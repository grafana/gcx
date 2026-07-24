package check

import (
	"context"
	"io"
	"os"

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
// to stdout. The library call therefore runs under captureStdout, which
// forwards any such stray prints to diag (stderr) as diagnostics.
func runWith(ctx context.Context, cmd otelutils.Commands, c checker, diag io.Writer) otelutils.Results {
	reporter := captureStdout(diag, func() *otelutils.Reporter {
		return c(ctx, cmd)
	})
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
	return results
}

// captureStdout redirects the process-global os.Stdout for the duration of fn
// and forwards everything written there to diag. If the capture pipe cannot
// be created, fn runs uncaptured — a stray diagnostics leak is preferable to
// failing the checks.
func captureStdout(diag io.Writer, fn func() *otelutils.Reporter) *otelutils.Reporter {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return fn()
	}
	os.Stdout = w

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(diag, r)
	}()

	defer func() {
		os.Stdout = orig
		_ = w.Close()
		<-done
		_ = r.Close()
	}()

	return fn()
}
