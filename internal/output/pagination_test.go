package output_test

import (
	"runtime"
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
	if runtime.GOOS == "windows" {
		want = "& '/tmp/my gcx' 'dashboards' 'list' '--limit' '50' '--continue' 'token with spaces'"
	}
	if got != want {
		t.Fatalf("BuildPaginationCommand() = %q, want %q", got, want)
	}
}

func TestBuildPaginationCommandQuotesApostrophe(t *testing.T) {
	got := output.BuildPaginationCommand([]string{"gcx", "dashboards", "list"}, 50, "next'page")
	want := "gcx dashboards list --limit 50 --continue 'next'\\''page'"
	if runtime.GOOS == "windows" {
		want = "& 'gcx' 'dashboards' 'list' '--limit' '50' '--continue' 'next''page'"
	}
	if got != want {
		t.Fatalf("BuildPaginationCommand() = %q, want %q", got, want)
	}
}

func TestBuildListLimitCommandQuotesPowerShellSyntax(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell command syntax applies only on Windows")
	}
	got := output.BuildListLimitCommand([]string{"C:\\Program Files\\gcx.exe", "dashboards", "list", "--selector=a,b", "--context", "team's prod"}, 25)
	want := "& 'C:\\Program Files\\gcx.exe' 'dashboards' 'list' '--selector=a,b' '--context' 'team''s prod' '--limit' '25'"
	if got != want {
		t.Fatalf("BuildListLimitCommand() = %q, want %q", got, want)
	}
}

func TestShellCommandQuotesWindowsNativeArguments(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell command syntax applies only on Windows")
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"path with spaces", []string{"gcx", "config", "edit", "--config", `C:\Program Files\gcx\config.yaml`}, `& 'gcx' 'config' 'edit' '--config' 'C:\Program Files\gcx\config.yaml'`},
		{"apostrophe", []string{"gcx", "login", "O'Brien"}, `& 'gcx' 'login' 'O''Brien'`},
		{"double quote", []string{"gcx", "login", `a"b`}, `& { $PSNativeCommandArgumentPassing = 'Legacy'; & 'gcx' '"login"' '"a\"b"' }`},
		{"empty argument", []string{"gcx", "config", "set", "key", ""}, `& { $PSNativeCommandArgumentPassing = 'Legacy'; & 'gcx' '"config"' '"set"' '"key"' '""' }`},
		{"trailing backslash", []string{"gcx", "login", `a"b`, `C:\path\`}, `& { $PSNativeCommandArgumentPassing = 'Legacy'; & 'gcx' '"login"' '"a\"b"' '"C:\path\\"' }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := output.ShellCommand(tc.args); got != tc.want {
				t.Fatalf("ShellCommand() = %q, want %q", got, tc.want)
			}
		})
	}
}
