package scripts_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Execute the workflow's actual shell with a fake GitHub API. In particular,
// an old review or a failed API call must never be enough to add the label.
func TestReviewWorkflowPublicationGate(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to execute the review workflow")
	}
	data, err := os.ReadFile("../.github/workflows/claude-code-review.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name  string `yaml:"name"`
				Run   string `yaml:"run"`
				Shell string `yaml:"shell"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	var script string
	for _, step := range workflow.Jobs["claude-review"].Steps {
		if step.Name == "Verify review and add claude-reviewed label" {
			if step.Shell != "bash" {
				t.Fatal("the publication gate needs explicit bash for pipefail")
			}
			script = step.Run
		}
	}
	if script == "" {
		t.Fatal("review publication gate is missing")
	}

	const review = `{"user":{"login":"claude[bot]"},"state":"COMMENTED","commit_id":"head","body":"No issues. <!-- gcx-claude-review:123:1 -->"}`
	const mockGH = `#!/usr/bin/env bash
set -euo pipefail
case "$1 $2" in
  "pr view") printf '%s\n' "$TEST_CURRENT_HEAD" ;;
  "api --paginate") cat "$TEST_REVIEWS_FILE"; exit "$TEST_API_EXIT" ;;
  "pr edit") printf '%s\n' "$*" > "$TEST_LABEL_FILE" ;;
  *) echo "Unexpected gh invocation: $*" >&2; exit 2 ;;
esac
`
	tests := []struct {
		name, pages, currentHead, apiExit string
		wantLabel                         bool
	}{
		{"clean review", "[[" + review + "]]", "head", "0", true},
		{"review on later page", "[[],[" + review + "]]", "head", "0", true},
		{"nothing published", "[[]]", "head", "0", false},
		{"previous run", "[[" + strings.ReplaceAll(review, "123:1", "122:1") + "]]", "head", "0", false},
		{"previous attempt", "[[" + strings.ReplaceAll(review, "123:1", "123:0") + "]]", "head", "0", false},
		{"wrong commit", "[[" + strings.ReplaceAll(review, `"head"`, `"old"`) + "]]", "head", "0", false},
		{"other author", "[[" + strings.ReplaceAll(review, "claude[bot]", "other[bot]") + "]]", "head", "0", false},
		{"approval", "[[" + strings.ReplaceAll(review, "COMMENTED", "APPROVED") + "]]", "head", "0", false},
		{"pending review", "[[" + strings.ReplaceAll(review, "COMMENTED", "PENDING") + "]]", "head", "0", false},
		{"head changed", "[[" + review + "]]", "new-head", "0", false},
		{"API failed after a page", "[[" + review + "]]", "head", "1", false},
		{"invalid API response", "not json", "head", "0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(mockGH), 0o700); err != nil { // #nosec G306 -- Executable test stub in t.TempDir.
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "reviews.json"), []byte(tt.pages), 0o600); err != nil {
				t.Fatal(err)
			}
			labelFile := filepath.Join(dir, "label")
			cmd := exec.CommandContext(t.Context(), "bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", script)
			cmd.Env = append(os.Environ(),
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GITHUB_REPOSITORY=grafana/gcx", "PR_NUMBER=1292", "REVIEW_HEAD=head",
				"REVIEW_MARKER=<!-- gcx-claude-review:123:1 -->",
				"TEST_CURRENT_HEAD="+tt.currentHead, "TEST_API_EXIT="+tt.apiExit,
				"TEST_REVIEWS_FILE="+filepath.Join(dir, "reviews.json"), "TEST_LABEL_FILE="+labelFile)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tt.wantLabel {
				t.Fatalf("workflow error = %v, want label %v; output: %s", err, tt.wantLabel, out)
			}
			label, readErr := os.ReadFile(labelFile)
			if tt.wantLabel {
				if readErr != nil || string(label) != "pr edit 1292 --repo grafana/gcx --add-label claude-reviewed\n" {
					t.Fatalf("label invocation = %q, read error = %v", label, readErr)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("label command ran despite failed verification: %q (%v)", label, readErr)
			}
		})
	}
}

// Run the skill's posting example against a fake gh, including shell syntax in
// the review text. It must reach stdin unchanged without creating payload files.
func TestReviewSkillPostsJSONOnStdin(t *testing.T) {
	data, err := os.ReadFile("../.claude/skills/review-pr/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(data), "```bash\ngh api ")
	if !ok {
		t.Fatal("review API example is missing")
	}
	block, _, ok = strings.Cut(block, "\n```")
	if !ok {
		t.Fatal("review API example is not closed")
	}
	dir := t.TempDir()
	body := "Use `agents` and $(touch " + filepath.Join(dir, "expanded") + ")."
	encodedBody, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	script := "gh api " + strings.NewReplacer(
		"{owner}", "grafana", "{repo}", "gcx", "{n}", "1292",
		`"<summary and, in CI, the review marker>"`, string(encodedBody),
	).Replace(block)
	mock := "#!/bin/sh\n[ \"$*\" = 'api repos/grafana/gcx/pulls/1292/reviews -X POST --input -' ] || exit 2\ncat\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(mock), 0o700); err != nil { // #nosec G306 -- Executable test stub in t.TempDir.
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "bash", "-e", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("posting example failed: %v: %s", err, out)
	}
	var payload struct {
		Event    string            `json:"event"`
		Body     string            `json:"body"`
		Comments []json.RawMessage `json:"comments"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Event != "COMMENT" || payload.Body != body || payload.Comments == nil {
		t.Fatalf("unexpected review payload: %s", out)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != "gh" {
		t.Fatalf("posting example created files: %v (error: %v)", files, err)
	}
}
