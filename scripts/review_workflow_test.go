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

// Execute the workflow's actual shell with a fake GitHub API. Only the response
// to this run's POST can authorize the label, and both head checks must pass.
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
				Name  string            `yaml:"name"`
				Run   string            `yaml:"run"`
				Shell string            `yaml:"shell"`
				Env   map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	var script string
	for _, step := range workflow.Jobs["claude-review"].Steps {
		if step.Name == "Publish review and add claude-reviewed label" {
			if step.Shell != "bash" {
				t.Fatal("the publication gate needs explicit bash for pipefail")
			}
			// The composite action revokes its App token before this step runs.
			if step.Env["GH_TOKEN"] != "${{ github.token }}" {
				t.Fatal("publication must use the job token, not the revoked action output token")
			}
			script = step.Run
		}
	}
	if script == "" {
		t.Fatal("review publication gate is missing")
	}

	const marker = "<!-- gcx-claude-review:123:1 -->"
	const review = `{"id":42,"submitted_at":"2026-09-11T10:00:00Z","user":{"login":"github-actions[bot]"},"state":"COMMENTED","commit_id":"head","body":"No issues. ` + marker + `"}`
	const clean = `{"review":{"body":"No issues.","comments":[]}}`
	const findings = `{"review":{"body":"Fix ` + "`" + `agents` + "`" + ` and $(touch expanded).","comments":[{"path":"a.go","line":2,"side":"RIGHT","body":"Use $(literal), $values, and ` + "`code`" + `."}]}}`
	const mockGH = `#!/usr/bin/env bash
set -euo pipefail
case "$1 $2" in
  "pr view")
    if [[ -f "$TEST_HEAD_FILE" ]]; then
      printf '%s\n' "$TEST_HEAD_AFTER"
    else
      touch "$TEST_HEAD_FILE"
      printf '%s\n' "$TEST_HEAD_BEFORE"
    fi
    exit "$TEST_HEAD_EXIT" ;;
  "api repos/grafana/gcx/pulls/1292/reviews")
    [[ "$*" == 'api repos/grafana/gcx/pulls/1292/reviews -X POST --input -' ]] || exit 2
    cat > "$TEST_POST_FILE"
    cat "$TEST_REVIEW_FILE"
    exit "$TEST_API_EXIT" ;;
  "pr edit") printf '%s\n' "$*" > "$TEST_LABEL_FILE"; exit "$TEST_LABEL_EXIT" ;;
  *) echo "Unexpected gh invocation: $*" >&2; exit 2 ;;
esac
`
	tests := []struct {
		name, output, response, headBefore, headAfter, headExit, apiExit, labelExit string
		wantPost, wantLabel, wantSuccess                                            bool
	}{
		{name: "clean review", wantPost: true, wantLabel: true, wantSuccess: true},
		{name: "findings preserve literal shell syntax", output: findings, wantPost: true, wantLabel: true, wantSuccess: true},
		{name: "incomplete correctness pass", output: `{"review":null}`},
		{name: "missing payload", output: `{}`},
		{name: "invalid payload", output: `not json`},
		{name: "empty body", output: `{"review":{"body":"","comments":[]}}`},
		{name: "missing comments", output: `{"review":{"body":"No issues."}}`},
		{name: "previous run", response: strings.ReplaceAll(review, "123:1", "122:1"), wantPost: true},
		{name: "previous attempt", response: strings.ReplaceAll(review, "123:1", "123:0"), wantPost: true},
		{name: "wrong commit", response: strings.ReplaceAll(review, `"head"`, `"old"`), wantPost: true},
		{name: "Claude action identity", response: strings.ReplaceAll(review, "github-actions[bot]", "claude[bot]"), wantPost: true},
		{name: "other author", response: strings.ReplaceAll(review, "github-actions[bot]", "other[bot]"), wantPost: true},
		{name: "approval", response: strings.ReplaceAll(review, "COMMENTED", "APPROVED"), wantPost: true},
		{name: "pending review", response: strings.ReplaceAll(review, "COMMENTED", "PENDING"), wantPost: true},
		{name: "missing submission timestamp", response: strings.ReplaceAll(review, `"2026-09-11T10:00:00Z"`, `null`), wantPost: true},
		{name: "invalid review id", response: strings.ReplaceAll(review, `"id":42`, `"id":0`), wantPost: true},
		{name: "head changed before posting", headBefore: "new-head"},
		{name: "head changed after posting", headAfter: "new-head", wantPost: true},
		{name: "head API failed", headExit: "1"},
		{name: "POST failed with valid response", apiExit: "1", wantPost: true},
		{name: "invalid API response", response: "not json", wantPost: true},
		{name: "label API failed", labelExit: "1", wantPost: true, wantLabel: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaultString := func(value, fallback string) string {
				if value == "" {
					return fallback
				}
				return value
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(mockGH), 0o700); err != nil { // #nosec G306 -- Executable test stub in t.TempDir.
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(defaultString(tt.response, review)), 0o600); err != nil {
				t.Fatal(err)
			}
			labelFile := filepath.Join(dir, "label")
			postFile := filepath.Join(dir, "posted.json")
			output := defaultString(tt.output, clean)
			cmd := exec.CommandContext(t.Context(), "bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GITHUB_REPOSITORY=grafana/gcx", "PR_NUMBER=1292", "REVIEW_HEAD=head",
				"REVIEW_MARKER="+marker, "REVIEW_OUTPUT="+output,
				"GITHUB_OUTPUT="+filepath.Join(dir, "output"),
				"TEST_HEAD_BEFORE="+defaultString(tt.headBefore, "head"), "TEST_HEAD_AFTER="+defaultString(tt.headAfter, "head"),
				"TEST_HEAD_EXIT="+defaultString(tt.headExit, "0"), "TEST_API_EXIT="+defaultString(tt.apiExit, "0"),
				"TEST_LABEL_EXIT="+defaultString(tt.labelExit, "0"), "TEST_HEAD_FILE="+filepath.Join(dir, "head-read"),
				"TEST_REVIEW_FILE="+filepath.Join(dir, "review.json"), "TEST_LABEL_FILE="+labelFile, "TEST_POST_FILE="+postFile)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tt.wantSuccess {
				t.Fatalf("workflow error = %v, want success %v; output: %s", err, tt.wantSuccess, out)
			}
			posted, readErr := os.ReadFile(postFile)
			if tt.wantPost {
				if readErr != nil {
					t.Fatal(readErr)
				}
				assertPostedReview(t, posted, output, marker)
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("review was posted despite invalid payload or head: %q (%v)", posted, readErr)
			}
			label, readErr := os.ReadFile(labelFile)
			if tt.wantLabel {
				if readErr != nil || string(label) != "pr edit 1292 --repo grafana/gcx --add-label claude-reviewed\n" {
					t.Fatalf("label invocation = %q, read error = %v", label, readErr)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("label command ran despite failed verification: %q (%v)", label, readErr)
			}
			if _, err := os.Stat(filepath.Join(dir, "expanded")); !os.IsNotExist(err) {
				t.Fatalf("review text executed as shell syntax: %v", err)
			}
		})
	}
}

func assertPostedReview(t *testing.T, posted []byte, output, marker string) {
	t.Helper()
	var got struct {
		Event    string            `json:"event"`
		CommitID string            `json:"commit_id"`
		Body     string            `json:"body"`
		Comments []json.RawMessage `json:"comments"`
	}
	var source struct {
		Review struct {
			Body     string            `json:"body"`
			Comments []json.RawMessage `json:"comments"`
		} `json:"review"`
	}
	if err := json.Unmarshal(posted, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &source); err != nil {
		t.Fatal(err)
	}
	wantComments, err := json.Marshal(source.Review.Comments)
	if err != nil {
		t.Fatal(err)
	}
	gotComments, err := json.Marshal(got.Comments)
	if err != nil {
		t.Fatal(err)
	}
	if got.Event != "COMMENT" || got.CommitID != "head" || got.Body != source.Review.Body+"\n\n"+marker || string(gotComments) != string(wantComments) {
		t.Fatalf("unexpected posted review: %s", posted)
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
		`"<review summary>"`, string(encodedBody),
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
