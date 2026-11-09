package output

import "testing"

func TestShellJoinForOS(t *testing.T) {
	args := []string{`C:\Program Files\gcx.exe`, "--selector=a,b", "--context", "team's prod"}
	tests := []struct {
		name string
		goos string
		want string
	}{
		{
			name: "PowerShell",
			goos: "windows",
			want: `& 'C:\Program Files\gcx.exe' '--selector=a,b' '--context' 'team''s prod'`,
		},
		{
			name: "POSIX shell",
			goos: "linux",
			want: `'C:\Program Files\gcx.exe' --selector=a,b --context 'team'\''s prod'`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellJoinForOS(args, tt.goos); got != tt.want {
				t.Fatalf("shellJoinForOS() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShellJoinForOSPreservesLegacyNativeArguments(t *testing.T) {
	args := []string{`C:\Program Files\gcx.exe`, `a"b`, "", `tail\`, "team's prod"}
	want := `& { $PSNativeCommandArgumentPassing = 'Legacy'; & 'C:\Program Files\gcx.exe' '"a\"b"' '""' '"tail\\"' '"team''s prod"' }`
	if got := shellJoinForOS(args, "windows"); got != want {
		t.Fatalf("shellJoinForOS() = %q, want %q", got, want)
	}
}

func TestShellJoinForOSTrailingBackslash(t *testing.T) {
	args := []string{"gcx", `C:\folder name\`, "tail"}
	want := `& { $PSNativeCommandArgumentPassing = 'Legacy'; & 'gcx' '"C:\folder name\\"' '"tail"' }`
	if got := shellJoinForOS(args, "windows"); got != want {
		t.Fatalf("shellJoinForOS() = %q, want %q", got, want)
	}
}

func TestShellJoinForOSSimpleCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no arguments", want: ""},
		{name: "pagination", args: []string{"gcx", "dashboards", "list", "--limit", "50", "--continue", "next-token"}, want: "gcx dashboards list --limit 50 --continue next-token"},
		{name: "selector and email", args: []string{"gcx", "datasources", "list", "--selector=a,b", "--context", "user@example.com"}, want: "gcx datasources list --selector=a,b --context user@example.com"},
	}
	for _, goos := range []string{"windows", "linux", "darwin"} {
		for _, tt := range tests {
			t.Run(goos+"/"+tt.name, func(t *testing.T) {
				if got := shellJoinForOS(tt.args, goos); got != tt.want {
					t.Fatalf("shellJoinForOS() = %q, want %q", got, tt.want)
				}
			})
		}
	}
}

func TestShellJoinForOSQuotesWindowsExpansion(t *testing.T) {
	for _, arg := range []string{"%USERPROFILE%", "@context"} {
		t.Run(arg, func(t *testing.T) {
			want := "& 'gcx' 'login' '" + arg + "'"
			if got := shellJoinForOS([]string{"gcx", "login", arg}, "windows"); got != want {
				t.Fatalf("shellJoinForOS() = %q, want %q", got, want)
			}
		})
	}
}
