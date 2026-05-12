package agentlog_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agentlog"
)

func TestStripArgValues(t *testing.T) {
	noBools := map[string]struct{}{}
	bools := map[string]struct{}{"agent": {}, "json": {}, "v": {}}
	// Subset of real gcx subcommand names used in tests below.
	subCmds := map[string]bool{
		"kg": true, "list": true, "get": true, "dashboards": true,
		"config": true, "set": true, "run": true,
	}

	tests := []struct {
		name      string
		in        []string
		boolFlags map[string]struct{}
		subCmds   map[string]bool
		want      []string
	}{
		{
			name:      "command only",
			in:        []string{"kg", "list"},
			boolFlags: noBools,
			subCmds:   subCmds,
			want:      []string{"kg", "list"},
		},
		{
			name:      "long flag space separated",
			in:        []string{"kg", "list", "--format", "json"},
			boolFlags: noBools,
			subCmds:   subCmds,
			want:      []string{"kg", "list", "--format", "<value>"},
		},
		{
			name:      "long flag equals form",
			in:        []string{"kg", "list", "--format=json"},
			boolFlags: noBools,
			subCmds:   subCmds,
			want:      []string{"kg", "list", "--format=<value>"},
		},
		{
			name:      "short flag space separated",
			in:        []string{"kg", "list", "-n", "myns"},
			boolFlags: noBools,
			subCmds:   subCmds,
			want:      []string{"kg", "list", "-n", "<value>"},
		},
		{
			name:      "short flag equals form",
			in:        []string{"-t=mysecrettoken"},
			boolFlags: noBools,
			subCmds:   nil,
			want:      []string{"-t=<value>"},
		},
		{
			name:      "short flag attached value (POSIX)",
			in:        []string{"-tmysecrettoken"},
			boolFlags: noBools,
			subCmds:   nil,
			want:      []string{"-t<value>"},
		},
		{
			name:      "token flag value hidden",
			in:        []string{"--token", "mysecrettoken"},
			boolFlags: noBools,
			subCmds:   nil,
			want:      []string{"--token", "<value>"},
		},
		{
			name:      "token flag equals value hidden",
			in:        []string{"--token=mysecrettoken"},
			boolFlags: noBools,
			subCmds:   nil,
			want:      []string{"--token=<value>"},
		},
		{
			name:      "double dash stops processing",
			in:        []string{"run", "--", "--format", "json"},
			boolFlags: noBools,
			subCmds:   subCmds,
			want:      []string{"run"},
		},
		{
			name:      "no args",
			in:        []string{},
			boolFlags: noBools,
			subCmds:   nil,
			want:      []string{},
		},
		{
			name:      "flag at end with no value",
			in:        []string{"list", "--json"},
			boolFlags: noBools,
			subCmds:   subCmds,
			want:      []string{"list", "--json"},
		},
		{
			name:      "bool flag does not consume next positional",
			in:        []string{"--agent", "get", "dashboards"},
			boolFlags: bools,
			subCmds:   subCmds,
			want:      []string{"--agent", "get", "dashboards"},
		},
		{
			name:      "bool flag at end",
			in:        []string{"list", "--json"},
			boolFlags: bools,
			subCmds:   subCmds,
			want:      []string{"list", "--json"},
		},
		{
			name:      "short bool flag does not consume next positional",
			in:        []string{"-v", "get", "dashboards"},
			boolFlags: bools,
			subCmds:   subCmds,
			want:      []string{"-v", "get", "dashboards"},
		},
		{
			name:      "positional value after subcommand chain is redacted",
			in:        []string{"config", "set", "cloud.token", "glsa_MYTOKEN"},
			boolFlags: noBools,
			subCmds:   subCmds,
			want:      []string{"config", "set", "<value>", "<value>"},
		},
		{
			name:      "nil subCmds keeps all positionals (backward compat)",
			in:        []string{"config", "set", "cloud.token", "glsa_MYTOKEN"},
			boolFlags: noBools,
			subCmds:   nil,
			want:      []string{"config", "set", "cloud.token", "glsa_MYTOKEN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agentlog.StripArgValues(tt.in, tt.boolFlags, tt.subCmds)
			if len(got) != len(tt.want) {
				t.Fatalf("len(got)=%d, len(want)=%d: got=%v, want=%v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d]=%q, want[%d]=%q", i, got[i], i, tt.want[i])
				}
			}
		})
	}
}

func TestKindFromExitCode(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, "error"}, // success shouldn't be logged but map defensively
		{1, "error"},
		{2, "usage_error"},
		{3, "auth_failure"},
		{4, "partial_failure"},
		{6, "version_incompatible"},
		{99, "error"},
	}
	for _, tt := range tests {
		if got := agentlog.KindFromExitCode(tt.code); got != tt.want {
			t.Errorf("KindFromExitCode(%d)=%q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestAppend(t *testing.T) {
	dir := t.TempDir()
	agentlog.Configure(agentlog.Config{Enabled: true, LogDir: dir})
	t.Cleanup(func() { agentlog.Configure(agentlog.Config{}) })

	e1 := agentlog.Entry{
		Timestamp: time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC),
		Version:   "0.2.12",
		Args:      []string{"kg", "nonexistent"},
		ErrorKind: "usage_error",
		Error:     `unknown command "nonexistent" for "gcx kg"`,
		ExitCode:  2,
	}
	e2 := agentlog.Entry{
		Timestamp: time.Date(2026, 5, 6, 11, 0, 0, 0, time.UTC),
		Version:   "0.2.12",
		Args:      []string{"metrics", "query", "--datasource=<value>"},
		ErrorKind: "error",
		Error:     "datasource not found",
		ExitCode:  1,
	}

	if err := agentlog.Append(e1); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := agentlog.Append(e2); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agent-invocation-errors.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := splitLines(data)
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}

	var got1, got2 agentlog.Entry
	if err := json.Unmarshal([]byte(lines[0]), &got1); err != nil {
		t.Fatalf("parse line 1: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &got2); err != nil {
		t.Fatalf("parse line 2: %v", err)
	}

	if got1.ErrorKind != "usage_error" {
		t.Errorf("line1 error_kind=%q, want usage_error", got1.ErrorKind)
	}
	if got2.ExitCode != 1 {
		t.Errorf("line2 exit_code=%d, want 1", got2.ExitCode)
	}

	// Verify file permissions.
	info, _ := os.Stat(filepath.Join(dir, "agent-invocation-errors.jsonl"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode=%o, want 0600", info.Mode().Perm())
	}
}

func splitLines(data []byte) []string {
	var out []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				out = append(out, string(data[start:i]))
			}
			start = i + 1
		}
	}
	return out
}
