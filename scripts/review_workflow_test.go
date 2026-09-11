package scripts_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	workflowPath = "../.github/workflows/claude-code-review.yml"
	skillPath    = "../.claude/skills/review-pr/SKILL.md"
	publishStep  = "Publish review"
	reviewHead   = "abc1230000000000000000000000000000000000"
	movedHead    = "def4560000000000000000000000000000000000"
)

// The review job, as the workflow declares it.
type reviewWorkflow struct {
	On map[string]struct {
		Types []string `yaml:"types"`
	} `yaml:"on"`
	Jobs map[string]struct {
		If    string            `yaml:"if"`
		Env   map[string]string `yaml:"env"`
		Steps []struct {
			Name  string         `yaml:"name"`
			If    string         `yaml:"if"`
			Run   string         `yaml:"run"`
			Shell string         `yaml:"shell"`
			With  map[string]any `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func loadWorkflow(t *testing.T) reviewWorkflow {
	t.Helper()
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	var workflow reviewWorkflow
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	if _, ok := workflow.Jobs["claude-review"]; !ok {
		t.Fatal("the claude-review job is missing")
	}
	return workflow
}

// The publish step's shell, which the publication tests below execute for real.
func publishScript(t *testing.T) string {
	t.Helper()
	for _, step := range loadWorkflow(t).Jobs["claude-review"].Steps {
		if step.Name == publishStep {
			if step.Shell != "bash" {
				t.Fatalf("%q needs explicit bash for pipefail", publishStep)
			}
			return step.Run
		}
	}
	t.Fatalf("the %q step is missing", publishStep)
	return ""
}

// A submitted review, as GitHub returns it from the create-review endpoint.
func submitted(commit string) string {
	return `{"id":5176163313,"state":"COMMENTED","commit_id":"` + commit +
		`","submitted_at":"2026-09-11T07:48:23Z"}`
}

// GitHub's 422 for inline comments anchored outside the diff. Captured shapes:
// the errors array carries either a PullRequestReviewThread resource or a
// field naming the rejected location.
const (
	errThreadLine = `{"message":"Validation Failed","errors":[{"resource":"PullRequestReviewThread",` +
		`"field":"line","code":"invalid","message":"Pull request review thread line must be part of the diff"}],` +
		`"documentation_url":"https://docs.github.com/rest/pulls/reviews","status":"422"}`
	errThreadComments = `{"message":"Validation Failed","errors":[{"resource":"PullRequestReview",` +
		`"field":"comments","code":"invalid"}],"status":"422"}`
	// 422 is also how GitHub reports spam protection and unrelated validation
	// failures. Neither is a reason to drop the inline findings.
	errAbuse = `{"message":"Validation Failed","errors":[{"resource":"PullRequestReview",` +
		`"code":"abuse"}],"status":"422"}`
	errBodyField = `{"message":"Validation Failed","errors":[{"resource":"PullRequestReview",` +
		`"field":"body","code":"missing_field"}],"status":"422"}`
	// The same rejection with no errors array at all, which is how GitHub
	// often reports it. The location is only in the message.
	errThreadMessageOnly = `{"message":"pull_request_review_thread.line must be part of the diff",` +
		`"documentation_url":"https://docs.github.com/rest/pulls/reviews","status":"422"}`
	// And with no status field either, so the HTTP code has to come from gh.
	errThreadNoStatus = `{"message":"Validation Failed","errors":[{"resource":"PullRequestReviewThread",` +
		`"field":"line","code":"invalid"}]}`
	errForbidden = `{"message":"Resource not accessible by integration","status":"403"}`
	errServer    = `<html>502 Bad Gateway</html>`
)

// Stands in for gh: records every review POST, replays a scripted response per
// call, and answers the head-SHA read from a per-call file.
const mockGH = `#!/usr/bin/env bash
set -uo pipefail
case "$*" in
  "api repos/grafana/gcx/pulls/1292/reviews -X POST --input -")
    n=$(( $(cat "$TEST_DIR/post_count" 2>/dev/null || echo 0) + 1 ))
    printf '%s' "$n" > "$TEST_DIR/post_count"
    cat > "$TEST_DIR/post_$n.json"
    [ -f "$TEST_DIR/post_${n}_response" ] && cat "$TEST_DIR/post_${n}_response"
    code=$(cat "$TEST_DIR/post_${n}_exit" 2>/dev/null || echo 0)
    if [ "$code" != 0 ]; then
      { cat "$TEST_DIR/post_${n}_stderr" 2>/dev/null || echo "gh: HTTP error from the stub"; } >&2
    fi
    exit "$code"
    ;;
  "api repos/grafana/gcx/pulls/1292 --jq "*)
    n=$(( $(cat "$TEST_DIR/head_count" 2>/dev/null || echo 0) + 1 ))
    printf '%s' "$n" > "$TEST_DIR/head_count"
    code=$(cat "$TEST_DIR/head_${n}_exit" 2>/dev/null || echo 0)
    if [ "$code" != 0 ]; then echo "gh: could not read the PR" >&2; exit "$code"; fi
    body="$TEST_DIR/pr_$n.json"
    [ -f "$body" ] || body="$TEST_DIR/pr_default.json"
    request="$*"
    jq -r "${request#api repos/grafana/gcx/pulls/1292 --jq }" "$body"
    ;;
  *) echo "unexpected gh invocation: $*" >&2; exit 2 ;;
esac
`

// The payload the reviewer returns. A finding spans lines to prove the
// fallback keeps multi-line bodies readable.
const (
	cleanOutput = `{"review":{"body":"No findings. The change is clean.","comments":[]}}`
	findings    = `{"review":{"body":"**Intent.** Adds a command.","comments":[` +
		`{"path":"cmd/gcx/a.go","line":12,"side":"RIGHT","body":"**required** — drops the error\nsecond line"},` +
		`{"path":"internal/b.go","line":90,"side":"RIGHT","body":"**nit** — rename this"}]}}`
	rangeFinding = `{"review":{"body":"**Intent.** Adds a command.","comments":[` +
		`{"path":"cmd/gcx/a.go","line":52,"side":"RIGHT","start_line":40,"start_side":"RIGHT",` +
		`"body":"**required** — the whole block"}]}}`
)

// One publication scenario: what the session returned, how GitHub answered,
// and what must reach the PR.
type publishCase struct {
	name string
	// structured_output from the review session.
	output string
	// Response body and exit code for the first and second POST.
	post1, post2         string
	post1Exit, post2Exit string
	// Head SHA for the first and second read; empty means reviewHead.
	head1, head2 string
	head1Exit    string
	head1Blank   bool   // the API answers without a head SHA at all
	post1Stderr  string // what gh prints on the first rejection

	wantPublished bool
	wantPosts     int
	wantOutput    string   // substring of the step's own output
	wantInBody    []string // substrings of the last posted review body
	wantComments  int      // inline comments on the last posted review
	wantSummary   string   // substring of $GITHUB_STEP_SUMMARY
	wantNoSummary bool     // the step summary must claim nothing
}

func publicationCases() []publishCase {
	return []publishCase{{
		name:   "clean review publishes a summary with no comments",
		output: cleanOutput, post1: submitted(reviewHead),
		wantPublished: true, wantPosts: 1, wantComments: 0,
		wantInBody: []string{"No findings. The change is clean.", "<!-- gcx-claude-review:123:1 -->"},
	}, {
		name:   "findings publish as inline comments",
		output: findings, post1: submitted(reviewHead),
		wantPublished: true, wantPosts: 1, wantComments: 2,
		wantInBody: []string{"**Intent.** Adds a command."},
	}, {
		name:      "an incomplete review is not published",
		output:    `{"review":null}`,
		wantPosts: 0, wantOutput: "the review did not complete",
	}, {
		name:      "a malformed payload is not published",
		output:    `{"review":{"body":`,
		wantPosts: 0, wantOutput: "the review did not complete",
	}, {
		name:      "absent structured output is not published",
		output:    "",
		wantPosts: 0, wantOutput: "the review did not complete",
	}, {
		name:      "a blank summary is not published",
		output:    `{"review":{"body":"   \n","comments":[]}}`,
		wantPosts: 0, wantOutput: "the review did not complete",
	}, {
		name:   "a rejected inline location retries once with every finding in the body",
		output: findings, post1: errThreadLine, post1Exit: "1", post2: submitted(reviewHead),
		wantPublished: true, wantPosts: 2, wantComments: 0,
		wantOutput: "rejected the inline comment locations",
		wantInBody: []string{
			"**Intent.** Adds a command.",
			"**cmd/gcx/a.go:12**", "drops the error", "second line",
			"**internal/b.go:90**", "rename this",
			reviewHead,
		},
	}, {
		name:   "a rejected comments field retries the same way",
		output: findings, post1: errThreadComments, post1Exit: "1", post2: submitted(reviewHead),
		wantPublished: true, wantPosts: 2, wantComments: 0,
		wantInBody: []string{"**cmd/gcx/a.go:12**", "**internal/b.go:90**"},
	}, {
		name:   "a location named only in the message retries the same way",
		output: findings, post1: errThreadMessageOnly, post1Exit: "1", post2: submitted(reviewHead),
		wantPublished: true, wantPosts: 2, wantComments: 0,
		wantInBody:  []string{"**cmd/gcx/a.go:12**", "**internal/b.go:90**"},
		wantSummary: "published without inline comments",
	}, {
		name:   "a rejection with no status field takes the code from gh",
		output: findings, post1: errThreadNoStatus, post1Exit: "1",
		post1Stderr: "gh: Validation Failed (HTTP 422)", post2: submitted(reviewHead),
		wantPublished: true, wantPosts: 2, wantComments: 0,
		wantInBody: []string{"**cmd/gcx/a.go:12**"},
	}, {
		name:   "a range finding keeps its range in the fallback",
		output: rangeFinding, post1: errThreadLine, post1Exit: "1", post2: submitted(reviewHead),
		wantPublished: true, wantPosts: 2, wantComments: 0,
		wantInBody: []string{"**cmd/gcx/a.go:40-52**", "the whole block"},
	}, {
		name:   "spam protection fails the run instead of dropping the findings",
		output: findings, post1: errAbuse, post1Exit: "1",
		wantPosts: 1, wantOutput: "GitHub rejected the review",
	}, {
		name:   "an unrelated 422 fails the run",
		output: findings, post1: errBodyField, post1Exit: "1",
		wantPosts: 1, wantOutput: "GitHub rejected the review",
	}, {
		name:   "a 403 fails the run",
		output: findings, post1: errForbidden, post1Exit: "1",
		wantPosts: 1, wantOutput: "GitHub rejected the review",
	}, {
		name:   "an unparseable error body fails the run",
		output: findings, post1: errServer, post1Exit: "1",
		wantPosts: 1, wantOutput: "GitHub rejected the review",
	}, {
		name:   "a review with no comments cannot fall back",
		output: cleanOutput, post1: errThreadLine, post1Exit: "1",
		wantPosts: 1, wantOutput: "GitHub rejected the review",
	}, {
		name:   "a failed retry fails the run",
		output: findings, post1: errThreadLine, post1Exit: "1", post2: errForbidden, post2Exit: "1",
		wantPosts: 2, wantOutput: "rejected the review summary as well",
		wantInBody:    []string{"**cmd/gcx/a.go:12**", "**internal/b.go:90**"},
		wantNoSummary: true,
	}, {
		name:   "a head that moved before publication publishes nothing",
		output: findings, head1: movedHead,
		wantPosts: 0, wantOutput: "The PR changed during the review",
	}, {
		name:   "a head that cannot be read publishes nothing",
		output: findings, head1Exit: "1",
		wantPosts: 0, wantOutput: "could not read the PR head SHA",
	}, {
		name:   "a response without a head SHA is a read failure, not a moved head",
		output: findings, head1Blank: true,
		wantPosts: 0, wantOutput: "could not read the PR head SHA",
	}, {
		name:   "a head that moved after publication keeps the review and warns",
		output: findings, post1: submitted(reviewHead), head2: movedHead,
		wantPublished: true, wantPosts: 1, wantComments: 2,
		wantOutput: "those commits are unreviewed",
	}, {
		name:   "a review on another commit is not confirmed",
		output: cleanOutput, post1: submitted(movedHead),
		wantPosts: 1, wantOutput: "did not confirm a submitted review",
	}, {
		name:      "a pending review is not confirmed",
		output:    cleanOutput,
		post1:     `{"id":1,"state":"PENDING","commit_id":"` + reviewHead + `","submitted_at":""}`,
		wantPosts: 1, wantOutput: "did not confirm a submitted review",
	}, {
		name:      "a response without a review id is not confirmed",
		output:    cleanOutput,
		post1:     `{"state":"COMMENTED","commit_id":"` + reviewHead + `","submitted_at":"2026-09-11T07:48:23Z"}`,
		wantPosts: 1, wantOutput: "did not confirm a submitted review",
	}, {
		name:          "shell syntax in a finding reaches the API unexpanded",
		output:        `{"review":{"body":"Uses $(id) and ` + "`whoami`" + ` and ${HOME} and {\"k\":\"v\"}.","comments":[]}}`,
		post1:         submitted(reviewHead),
		wantPublished: true, wantPosts: 1, wantComments: 0,
		wantInBody: []string{`Uses $(id) and ` + "`whoami`" + ` and ${HOME} and {"k":"v"}.`},
	}}
}

// Execute the real publish step against the stub, with one scripted response
// per POST and per head read.
func TestReviewWorkflowPublication(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to execute the review workflow")
	}
	script := publishScript(t)
	for _, tt := range publicationCases() {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, content string) {
				if content == "" {
					return
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(mockGH), 0o700); err != nil { // #nosec G306 -- Executable test stub in t.TempDir.
				t.Fatal(err)
			}
			pr := func(sha string) string {
				if sha == "" {
					return `{"number":1292,"head":{}}`
				}
				return `{"number":1292,"head":{"sha":"` + sha + `"}}`
			}
			write("pr_default.json", pr(reviewHead))
			if tt.head1Blank {
				write("pr_1.json", pr(""))
			}
			if tt.head1 != "" {
				write("pr_1.json", pr(tt.head1))
			}
			if tt.head2 != "" {
				write("pr_2.json", pr(tt.head2))
			}
			write("post_1_stderr", tt.post1Stderr)
			write("head_1_exit", tt.head1Exit)
			write("post_1_response", tt.post1)
			write("post_2_response", tt.post2)
			write("post_1_exit", tt.post1Exit)
			write("post_2_exit", tt.post2Exit)

			cmd := exec.CommandContext(t.Context(), "bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", script)
			cmd.Env = append(os.Environ(),
				"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TEST_DIR="+dir,
				"GITHUB_REPOSITORY=grafana/gcx",
				"PR_NUMBER=1292",
				"REVIEW_HEAD="+reviewHead,
				"REVIEW_MARKER=<!-- gcx-claude-review:123:1 -->",
				"REVIEW_OUTPUT="+tt.output,
				"GITHUB_OUTPUT="+filepath.Join(dir, "step_output"),
				"GITHUB_STEP_SUMMARY="+filepath.Join(dir, "step_summary"),
			)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tt.wantPublished {
				t.Fatalf("step error = %v, want published %v; output:\n%s", err, tt.wantPublished, out)
			}
			if tt.wantOutput != "" && !strings.Contains(string(out), tt.wantOutput) {
				t.Fatalf("output does not mention %q:\n%s", tt.wantOutput, out)
			}

			summary, _ := os.ReadFile(filepath.Join(dir, "step_summary"))
			if tt.wantSummary != "" && !strings.Contains(string(summary), tt.wantSummary) {
				t.Errorf("step summary is missing %q:\n%s", tt.wantSummary, summary)
			}
			if tt.wantNoSummary && len(strings.TrimSpace(string(summary))) != 0 {
				t.Errorf("step summary claims something about an unpublished review:\n%s", summary)
			}

			posts, _ := os.ReadDir(dir)
			count := 0
			for _, entry := range posts {
				if strings.HasPrefix(entry.Name(), "post_") && strings.HasSuffix(entry.Name(), ".json") { //nolint:goconst // one literal, one use
					count++
				}
			}
			if count != tt.wantPosts {
				t.Fatalf("posted %d reviews, want %d; output:\n%s", count, tt.wantPosts, out)
			}
			if tt.wantPosts == 0 {
				return
			}

			last, err := os.ReadFile(filepath.Join(dir, "post_"+strconv.Itoa(tt.wantPosts)+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var review struct {
				Event    string            `json:"event"`
				CommitID string            `json:"commit_id"`
				Body     string            `json:"body"`
				Comments []json.RawMessage `json:"comments"`
			}
			if err := json.Unmarshal(last, &review); err != nil {
				t.Fatalf("posted payload is not JSON: %v: %s", err, last)
			}
			if review.Event != "COMMENT" {
				t.Errorf("posted event = %q, want COMMENT", review.Event)
			}
			if review.CommitID != reviewHead {
				t.Errorf("posted commit_id = %q, want the reviewed head", review.CommitID)
			}
			if tt.wantPublished && len(review.Comments) != tt.wantComments {
				t.Errorf("posted %d inline comments, want %d", len(review.Comments), tt.wantComments)
			}
			for _, want := range tt.wantInBody {
				if !strings.Contains(review.Body, want) {
					t.Errorf("posted body is missing %q:\n%s", want, review.Body)
				}
			}
		})
	}
}

// The workflow and the skill describe one delivery contract, and the schema
// the action enforces cannot drift away from the shape the skill documents.
func TestReviewPayloadContract(t *testing.T) {
	workflow := loadWorkflow(t)
	job := workflow.Jobs["claude-review"]

	schema := job.Env["REVIEW_SCHEMA"]
	if schema == "" {
		t.Fatal("REVIEW_SCHEMA is missing from the job env")
	}
	// A YAML folded scalar keeps the line breaks of any line indented deeper
	// than its first, and claude_args is tokenised by a shell parser. Keep both
	// on one line rather than relying on how it treats an embedded newline.
	if strings.Contains(schema, "\n") {
		t.Error("REVIEW_SCHEMA folded to more than one line; indent every line the same")
	}

	var parsed struct {
		Properties struct {
			Review struct {
				AnyOf []struct {
					Type       string `json:"type"`
					Properties struct {
						Comments struct {
							Items struct {
								Required   []string                   `json:"required"`
								Properties map[string]json.RawMessage `json:"properties"`
							} `json:"items"`
						} `json:"comments"`
					} `json:"properties"`
				} `json:"anyOf"`
			} `json:"review"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
		t.Fatalf("REVIEW_SCHEMA is not valid JSON: %v", err)
	}

	// An incomplete review must stay expressible, and a complete one must
	// carry the fields the publish step and the fallback both read.
	variants := parsed.Properties.Review.AnyOf
	if len(variants) != 2 {
		t.Fatalf("review must be null or an object, got %d variants", len(variants))
	}
	if variants[0].Type != "null" {
		t.Error(`the schema must admit {"review": null} for a review that did not complete`)
	}
	item := variants[1].Properties.Comments.Items
	for _, field := range []string{"path", "line", "side", "body"} {
		if !slices.Contains(item.Required, field) {
			t.Errorf("an inline comment must require %q", field)
		}
		if _, ok := item.Properties[field]; !ok {
			t.Errorf("an inline comment must define %q", field)
		}
	}

	skill, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	delivery := section(string(skill), "### Workflow delivery")
	if delivery == "" {
		t.Fatal("the skill does not document Workflow delivery")
	}
	// Every field the workflow enforces has to appear where the reviewer is
	// told what to return, including the optional range fields.
	for _, field := range []string{"path", "line", "side", "body", "start_line", "start_side", `{"review": null}`} {
		if !strings.Contains(delivery, field) {
			t.Errorf("Workflow delivery does not document %q", field)
		}
	}
	// The reviewer must not be told to publish in this mode.
	if !strings.Contains(delivery, "Publish nothing in this mode") {
		t.Error("Workflow delivery must tell the reviewer not to publish")
	}
}

// What the workflow must not grow back: the plugin that ended the session, a
// review-outcome label, a publication path for the model, or a gate that lets
// a failed artifact upload swallow a finished review.
func TestReviewWorkflowShape(t *testing.T) {
	workflow := loadWorkflow(t)
	job := workflow.Jobs["claude-review"]

	// The upstream plugin's command ends the session it is invoked from, which
	// is why the review never reached the publish step. It stays uninstalled.
	var args string
	for _, step := range job.Steps {
		for _, gone := range []string{"plugins", "plugin_marketplaces"} {
			if _, ok := step.With[gone]; ok {
				t.Errorf("the workflow must not install the review plugin: step %q sets %q", step.Name, gone)
			}
		}
		if with, ok := step.With["claude_args"].(string); ok {
			args = with
		}
	}

	// Publication is the workflow's job and '@claude review' is the retry, so
	// no review-outcome label survives. Read the parsed workflow, not its text:
	// a comment may name the label it explains the absence of.
	if slices.Contains(workflow.On["pull_request"].Types, "unlabeled") {
		t.Error("the unlabeled trigger is back; '@claude review' is the retry path")
	}
	if strings.Contains(job.If, "label") {
		t.Error("the job condition reads a label again")
	}
	for _, step := range job.Steps {
		for _, gone := range []string{"claude-reviewed", "add-label", "remove-label"} {
			if strings.Contains(step.Run, gone) {
				t.Errorf("step %q carries review-outcome label work: found %q", step.Name, gone)
			}
		}
	}

	// A failed transcript upload must not be able to skip publication, and a
	// re-run must not fail that upload on the duplicate artifact name.
	for _, step := range job.Steps {
		switch step.Name {
		case publishStep:
			// GitHub implicitly adds success() unless the condition contains a
			// status function. Keep publication independent of artifact failure,
			// but skip it when the run is cancelled or the review failed.
			const want = "${{ !cancelled() && steps.claude-review.outcome == 'success' }}"
			if step.If != want {
				t.Errorf("%q condition = %q, want %q to publish after artifact failure without publishing cancelled or failed reviews", publishStep, step.If, want)
			}
		case "Upload Claude session transcript":
			if step.With["overwrite"] != true {
				t.Error("the transcript upload must set overwrite: a re-run reuses the run id")
			}
		}
	}
	if args == "" {
		t.Fatal("the review step passes no claude_args")
	}
	if strings.Contains(args, "\n") {
		t.Error("claude_args folded to more than one line; indent every line the same")
	}
	if !strings.Contains(args, "--json-schema") || !strings.Contains(args, "env.REVIEW_SCHEMA") {
		t.Error("claude_args must request structured output against REVIEW_SCHEMA")
	}
	for _, denied := range []string{"Write", "Bash(gh api *)", "mcp__github_inline_comment__create_inline_comment"} {
		if !strings.Contains(args, denied) {
			t.Errorf("claude_args must deny %q: the shared allowlist admits publishing for local runs", denied)
		}
	}
}

// The body of one markdown section, up to the next heading of any level. A
// heading inside a fenced block is content, not a terminator: a shell example
// starting with a comment would otherwise cut the section short.
func section(doc, heading string) string {
	_, after, ok := strings.Cut(doc, heading)
	if !ok {
		return ""
	}
	var body []string
	var fenced bool
	for line := range strings.SplitSeq(after, "\n") {
		switch {
		case strings.HasPrefix(strings.TrimSpace(line), "```"):
			fenced = !fenced
		case !fenced && strings.HasPrefix(line, "#"):
			return strings.Join(body, "\n")
		}
		body = append(body, line)
	}
	return strings.Join(body, "\n")
}

// Run the skill's posting example against a fake gh, including shell syntax in
// the review text. It must reach stdin unchanged without creating payload files.
func TestReviewSkillPostsJSONOnStdin(t *testing.T) {
	data, err := os.ReadFile(skillPath)
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
		`"<the summary>"`, string(encodedBody),
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
