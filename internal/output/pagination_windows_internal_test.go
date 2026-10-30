//go:build windows

package output

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	paginationArgsHelperEnv = "GCX_PAGINATION_ARGS_HELPER"
	paginationArgsMarker    = "pagination-test-args"
)

// TestMain handles helper processes before Go parses their command arguments.
// It prints base64 so the shell's native-output encoding cannot change the
// Unicode argument values before the parent checks them.
func TestMain(m *testing.M) {
	if os.Getenv(paginationArgsHelperEnv) != "1" {
		os.Exit(m.Run())
	}
	for i, arg := range os.Args {
		if arg != paginationArgsMarker {
			continue
		}
		payload, err := json.Marshal(os.Args[i+1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprint(os.Stdout, base64.StdEncoding.EncodeToString(payload))
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, "missing pagination argument marker")
	os.Exit(1)
}

func TestPowerShellPaginationArgumentsRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "ordinary", args: []string{"dashboards", "list", "--selector=a,b"}},
		{name: "shell expansion", args: []string{"%USERPROFILE%", "@context"}},
		{name: "empty", args: []string{"", "tail"}},
		{name: "double quote", args: []string{`a"b`, `C:\path\"quoted`}},
		{name: "trailing backslash", args: []string{`C:\folder name\`, "tail"}},
		{name: "apostrophe and Unicode", args: []string{"team's prod", "München \u96ea"}},
	}
	for _, shellName := range []string{"powershell.exe", "pwsh.exe"} {
		t.Run(shellName, func(t *testing.T) {
			shell, err := exec.LookPath(shellName)
			if err != nil {
				t.Skipf("%s is not installed: %v", shellName, err)
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					argv := append([]string{os.Args[0], paginationArgsMarker}, tt.args...)
					command := shellJoinForOS(argv, "windows")
					cmd := exec.CommandContext(t.Context(), shell, "-NoProfile", "-NonInteractive", "-Command", command)
					cmd.Env = append(os.Environ(), paginationArgsHelperEnv+"=1")
					output, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("%s failed: %v; output: %s", shellName, err, output)
					}
					decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
					if err != nil {
						t.Fatalf("decode helper output: %v; output: %s", err, output)
					}
					var got []string
					if err := json.Unmarshal(decoded, &got); err != nil {
						t.Fatalf("decode helper arguments: %v; output: %s", err, decoded)
					}
					if !reflect.DeepEqual(got, tt.args) {
						t.Fatalf("arguments = %#v, want %#v; command: %s", got, tt.args, command)
					}
				})
			}
		})
	}
}

// TestSimpleWindowsCommandRoundTrip checks the common hint in both Windows shells.
func TestSimpleWindowsCommandRoundTrip(t *testing.T) {
	// PATH lookup avoids quotes even if the test directory has spaces.
	dir := t.TempDir()
	data, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pagination-helper.exe"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"dashboards", "list", "--selector=a,b", "--context", "user@example.com", "--limit", "50"}
	argv := append([]string{"pagination-helper.exe", paginationArgsMarker}, args...)
	command := shellJoinForOS(argv, "windows")
	if command != strings.Join(argv, " ") {
		t.Fatalf("simple command uses shell quotes: %s", command)
	}
	for _, shellName := range []string{"cmd.exe", "powershell.exe", "pwsh.exe"} {
		t.Run(shellName, func(t *testing.T) {
			shell, err := exec.LookPath(shellName)
			if err != nil {
				t.Skipf("%s is not installed: %v", shellName, err)
			}
			shellArgs := []string{"-NoProfile", "-NonInteractive", "-Command", command}
			if shellName == "cmd.exe" {
				shellArgs = []string{"/d", "/c", command}
			}
			cmd := exec.CommandContext(t.Context(), shell, shellArgs...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), paginationArgsHelperEnv+"=1", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed: %v; output: %s", shellName, err, output)
			}
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
			if err != nil {
				t.Fatalf("decode helper output: %v; output: %s", err, output)
			}
			var got []string
			if err := json.Unmarshal(decoded, &got); err != nil {
				t.Fatalf("decode helper arguments: %v; output: %s", err, decoded)
			}
			if !reflect.DeepEqual(got, args) {
				t.Fatalf("arguments = %#v, want %#v; command: %s", got, args, command)
			}
		})
	}
}
