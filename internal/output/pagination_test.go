package output_test

import (
	"testing"

	"github.com/grafana/gcx/internal/output"
)

func TestBuildPaginationCommandPreservesArgvAndReplacesPaginationFlags(t *testing.T) {
	got := output.BuildPaginationCommand(
		[]string{"gcx", "--context", "dev", "dashboards", "list", "--api-version=dashboard.grafana.app/v2", "--limit", "50", "--continue=old"},
		100,
		"next-token",
	)
	want := "gcx --context dev dashboards list --api-version=dashboard.grafana.app/v2 --limit 100 --continue next-token"
	if got != want {
		t.Fatalf("BuildPaginationCommand() = %q, want %q", got, want)
	}
}

func TestBuildPaginationCommandQuotesUnsafeArgs(t *testing.T) {
	got := output.BuildPaginationCommand(
		[]string{"/tmp/my gcx", "dashboards", "list", "--limit=50"},
		50,
		"token with spaces",
	)
	want := "'/tmp/my gcx' dashboards list --limit 50 --continue 'token with spaces'"
	if got != want {
		t.Fatalf("BuildPaginationCommand() = %q, want %q", got, want)
	}
}
