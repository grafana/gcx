package check

import (
	"context"

	otelchecks "github.com/grafana/otel-checker/checks"
	otelutils "github.com/grafana/otel-checker/checks/utils"
)

// checker is the minimal interface required to invoke the otel-checker
// library. Tests substitute a fake; the real implementation is
// otelchecks.Run.
type checker func(ctx context.Context, cmd otelutils.Commands) *otelutils.Reporter

// run executes the otel-checker library and returns the typed result
// snapshot. Slices are normalized to non-nil for F-AGENT-01 compliance
// (empty JSON arrays, not null).
func run(ctx context.Context, cmd otelutils.Commands) (otelutils.Results, error) {
	return runWith(ctx, cmd, otelchecks.Run)
}

// runWith is the testable seam for run; callers in tests pass a fake checker.
func runWith(ctx context.Context, cmd otelutils.Commands, c checker) (otelutils.Results, error) {
	reporter := c(ctx, cmd)
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
