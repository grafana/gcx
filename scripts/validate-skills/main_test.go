package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTrackedSkillsLink(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     string
		target   string
		worktree string
		wantErr  bool
	}{
		{name: "symlink checkout", mode: "120000", target: "../.agents/skills", worktree: "symlink"},
		{name: "materialized directory", mode: "120000", target: "../.agents/skills", worktree: "directory"},
		{name: "materialized file", mode: "120000", target: "../.agents/skills", worktree: "file"},
		{name: "missing worktree link", mode: "120000", target: "../.agents/skills"},
		{name: "wrong staged target", mode: "120000", target: "../other", wantErr: true},
		{name: "staged regular file", mode: "100644", target: "../.agents/skills", wantErr: true},
		{name: "missing index entry", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			git := func(input string, args ...string) string {
				t.Helper()
				cmd := exec.CommandContext(t.Context(), "git", args...)
				cmd.Stdin = strings.NewReader(input)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("", "init", "--quiet")
			if tc.mode != "" {
				oid := git(tc.target, "hash-object", "-w", "--stdin")
				git("", "update-index", "--add", "--cacheinfo", tc.mode+","+oid+",.claude/skills")
			}
			if err := os.MkdirAll(".claude", 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(".claude", "skills")
			switch tc.worktree {
			case "symlink":
				if err := os.Symlink("../.agents/skills", path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			case "file":
				if err := os.WriteFile(path, []byte(tc.target), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := validateTrackedSkillsLink(); (err != nil) != tc.wantErr {
				t.Fatalf("validateTrackedSkillsLink() = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
