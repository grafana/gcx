package experiments_test

import (
	"bytes"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/experiments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommands_HasExpectedLeaves(t *testing.T) {
	cmd := experiments.Commands(nil)
	require.Equal(t, "experiments", cmd.Name())

	for _, sub := range []string{"list", "get", "create", "update", "cancel", "list-scores", "get-report", "list-trials", "export-conversations", "test-suites", "trials"} {
		c, _, err := cmd.Find([]string{sub})
		require.NoError(t, err, "subcommand %q must exist", sub)
		require.NotNil(t, c)
		require.Equal(t, sub, c.Name())
	}
}

func TestCommands_HasExpectedNestedExperimentLeaves(t *testing.T) {
	cmd := experiments.Commands(nil)

	for _, path := range [][]string{
		{"test-suites", "list"},
		{"test-suites", "get"},
		{"test-suites", "create"},
		{"test-suites", "update"},
		{"test-suites", "versions", "create"},
		{"test-suites", "versions", "publish"},
		{"test-suites", "cases", "list"},
		{"test-suites", "cases", "get"},
		{"test-suites", "cases", "upsert"},
		{"test-suites", "cases", "update"},
		{"test-suites", "cases", "delete"},
		{"trials", "get"},
		{"trials", "create"},
		{"trials", "update"},
		{"trials", "list-scores"},
		{"trials", "list-artifacts"},
	} {
		c, _, err := cmd.Find(path)
		require.NoError(t, err, "subcommand path %q must exist", strings.Join(path, " "))
		require.NotNil(t, c)
		require.Equal(t, path[len(path)-1], c.Name())
	}
}

func TestCreateCommand_RequiresFilename(t *testing.T) {
	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"create"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--filename/-f is required")
}

func TestCreateCommand_FileRequiresName(t *testing.T) {
	path := writeTempFile(t, "experiment-*.json", `{"run_id":"run-1"}`)

	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"create", "-f", path})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestSuitesCreateCommand_FileRequiresName(t *testing.T) {
	path := writeTempFile(t, "suite-*.json", `{"suite_id":"suite-1"}`)

	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"test-suites", "create", "-f", path})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestUpdateCommand_RequiresMutableField(t *testing.T) {
	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"update", "r-1"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--name, --description, or --tag is required")
}

func TestUpdateCommand_RejectsRemovedStatusAndErrorFlags(t *testing.T) {
	// --status and --error were intentionally removed: they're server-managed
	// lifecycle fields; users should drive transitions via `cancel`.
	for _, flag := range []string{"--status", "--error"} {
		t.Run(flag, func(t *testing.T) {
			cmd := experiments.Commands(nil)
			cmd.SetArgs([]string{"update", "r-1", flag, "x"})

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unknown flag")
		})
	}
}

func TestGetCommand_RequiresArg(t *testing.T) {
	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"get"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

func TestCancelCommand_RequiresArg(t *testing.T) {
	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"cancel"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

func TestCancelCommand_AbortsWithoutForce(t *testing.T) {
	// Without --force, the confirmation gate must run before any client call,
	// so an unconfigured loader (nil) never trips a network/auth path. A "n"
	// on stdin aborts; non-TTY stdin without --force errors with "use --force".
	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"cancel", "r-1"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("n\n"))

	err := cmd.Execute()
	if err != nil {
		assert.Contains(t, err.Error(), "use --force")
	} else {
		assert.Contains(t, stderr.String(), "Aborted")
	}
}

func TestListScoresCommand_RequiresArg(t *testing.T) {
	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"list-scores"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

func TestTrialsListCommand_RequiresExperimentIDWithSuggestion(t *testing.T) {
	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"list-trials"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected format: gcx agento11y experiments list-trials <run-id>")
}

func TestExportConversationsCommand_RequiresRunIDWithSuggestion(t *testing.T) {
	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"export-conversations"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected format: gcx agento11y experiments export-conversations <run-id> -d <directory>")
}

func TestCasesListCommand_RequiresSuiteAndVersionWithSuggestion(t *testing.T) {
	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"test-suites", "cases", "list"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected format: gcx agento11y experiments test-suites cases list <suite-id> <version>")
}

func TestReportCommand_RequiresArg(t *testing.T) {
	cmd := experiments.Commands(nil)
	cmd.SetArgs([]string{"get-report"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

func TestTableCodec_Format(t *testing.T) {
	assert.Equal(t, "table", string((&experiments.TableCodec{}).Format()))
	assert.Equal(t, "wide", string((&experiments.TableCodec{Wide: true}).Format()))
}

func TestTableCodec_Encode(t *testing.T) {
	completed := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	passRate := 0.5
	items := []experiments.Experiment{
		{
			ExperimentID: "r-1",
			Name:         "exp-1",
			Status:       "running",
			Tags:         []string{"support", "prompt-v2"},
			Description:  "Nightly regression run",
			Result:       &experiments.ExperimentReportSummary{TrialCount: 2, PassRate: &passRate},
			CreatedAt:    time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
			CompletedAt:  &completed,
			Error:        "something",
		},
		{ExperimentID: "r-2", Name: "exp-2", Status: "pending", ResultStatus: "failed", ResultError: "rollup timed out"},
		{ExperimentID: "r-3", Name: "exp-3", Status: "running", Result: &experiments.ExperimentReportSummary{TrialCount: 4}},
	}

	tests := []struct {
		name    string
		wide    bool
		want    []string
		notWant []string
		// wantCells maps an experiment ID to the value each named column holds
		// in that row. Every fixture row renders "-" somewhere, so a "-" is only
		// meaningful when it is pinned to one column.
		wantCells map[string]map[string]string
	}{
		{
			name:    "table format reports trials and pass rate",
			wide:    false,
			want:    []string{"EXPERIMENT-ID", "NAME", "STATUS", "SUITE", "VERSION", "TRIALS", "PASS"},
			notWant: []string{"SCORES", "TAGS", "support, prompt-v2"},
			wantCells: map[string]map[string]string{
				"r-1": {"NAME": "exp-1", "STATUS": "running", "TRIALS": "2", "PASS": "50.00%"},
				// r-2 carries no result rollup, so its trial count is unknown.
				"r-2": {"NAME": "exp-2", "STATUS": "pending", "TRIALS": "-", "PASS": "-"},
			},
		},
		{
			name:    "wide adds TAGS, ERROR, COMPLETED, and DESCRIPTION",
			wide:    true,
			want:    []string{"TAGS", "support, prompt-v2", "ERROR", "COMPLETED", "DESCRIPTION", "Nightly regression run", "2026-04-02 12:00"},
			notWant: []string{"SCORES"},
			wantCells: map[string]map[string]string{
				"r-1": {"TRIALS": "2", "PASS": "50.00%", "ERROR": "something"},
				// r-2's rollup failed while the experiment itself reported no
				// error, so the ERROR column carries the rollup's reason.
				"r-2": {"TRIALS": "-", "PASS": "-", "ERROR": "rollup timed out"},
				// r-3 has trials but no measured pass rate.
				"r-3": {"TRIALS": "4", "PASS": "-"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &experiments.TableCodec{Wide: tc.wide}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, items))
			out := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, out, s)
			}
			for _, s := range tc.notWant {
				assert.NotContains(t, out, s)
			}
			for id, cells := range tc.wantCells {
				for column, want := range cells {
					assert.Equal(t, want, tableCell(t, out, id, column), "row %s column %s", id, column)
				}
			}
		})
	}
}

// tableCell returns the cell under the named column of the row whose first cell
// is id. The plain renderer pads columns apart with at least two spaces, so a
// value holding one space (a tag list, a timestamp, an error message) stays in
// a single cell.
func tableCell(t *testing.T, out, id, column string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.NotEmpty(t, lines, "table output is empty")
	headers := splitCells(lines[0])
	index := slices.Index(headers, column)
	require.GreaterOrEqual(t, index, 0, "no column %q in header %q", column, lines[0])
	for _, line := range lines[1:] {
		cells := splitCells(line)
		if cells[0] != id {
			continue
		}
		require.Len(t, cells, len(headers), "row %q does not fill every column", id)
		return cells[index]
	}
	t.Fatalf("no row for %q in:\n%s", id, out)
	return ""
}

var columnGap = regexp.MustCompile(` {2,}`)

func splitCells(line string) []string {
	return columnGap.Split(strings.TrimSpace(line), -1)
}

func TestTrialsTableCodec_Encode(t *testing.T) {
	totalTokens := int64(12161)
	durationMS := int64(578)
	items := []experiments.TestCaseTrial{
		{TrialID: "trial-1", ExperimentID: "exp-1", TestCaseID: "case-1", Attempt: 1, Status: "completed", TotalTokens: &totalTokens, DurationMS: &durationMS},
		{TrialID: "trial-2", ExperimentID: "exp-1", TestCaseID: "case-2", Attempt: 1, Status: "running"},
	}

	tests := []struct {
		name      string
		wide      bool
		want      []string
		notWant   []string
		wantCells map[string]map[string]string
	}{
		{
			name:      "table format omits usage",
			wide:      false,
			want:      []string{"TRIAL-ID", "case-1"},
			notWant:   []string{"TOTAL-TOKENS"},
			wantCells: map[string]map[string]string{"trial-1": {"EXPERIMENT-ID": "exp-1", "STATUS": "completed"}},
		},
		{
			name: "wide reports total tokens",
			wide: true,
			want: []string{"TOTAL-TOKENS"},
			wantCells: map[string]map[string]string{
				"trial-1": {"TOTAL-TOKENS": "12161", "DURATION-MS": "578"},
				// trial-2 reported no usage.
				"trial-2": {"STATUS": "running", "TOTAL-TOKENS": "-", "DURATION-MS": "-"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &experiments.TrialsTableCodec{Wide: tc.wide}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, items))
			out := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, out, s)
			}
			for _, s := range tc.notWant {
				assert.NotContains(t, out, s)
			}
			for id, cells := range tc.wantCells {
				for column, want := range cells {
					assert.Equal(t, want, tableCell(t, out, id, column), "row %s column %s", id, column)
				}
			}
		})
	}
}

func TestTableCodec_WrongType(t *testing.T) {
	codec := &experiments.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []Experiment")
}

func TestScoresTableCodec_Encode(t *testing.T) {
	value := 0.95
	passed := true
	items := []experiments.ScoreItem{
		{
			ScoreID:      "s-1",
			EvaluatorID:  "ev-1",
			ScoreKey:     "quality",
			Value:        experiments.ScoreValue{Number: &value},
			Passed:       &passed,
			GenerationID: "gen-1",
			Explanation:  "looks good",
		},
		{ScoreID: "s-2", EvaluatorID: "ev-2", ScoreKey: "tone"},
	}

	tests := []struct {
		name string
		wide bool
		want []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"SCORE-ID", "EVALUATOR", "VALUE", "s-1", "ev-1", "0.95", "true", "gen-1"},
		},
		{
			name: "wide adds explanation",
			wide: true,
			want: []string{"EXPLANATION", "looks good"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &experiments.ScoresTableCodec{Wide: tc.wide}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, items))
			out := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, out, s)
			}
		})
	}
}

func TestScoresTableCodec_WrongType(t *testing.T) {
	codec := &experiments.ScoresTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []ScoreItem")
}

func TestReportTextCodec_Encode(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		errText string
		summary experiments.ExperimentReportSummary
		want    []string
		notWant []string
	}{
		{
			name: "complete run",
			summary: experiments.ExperimentReportSummary{
				TestCaseCount: 2, TrialCount: 4, CompletedCount: 4, FailedCount: 1, CanceledCount: 1,
				PassRate: new(0.8), FinalScoreAvg: new(0.85),
				TotalCost: new(0.1234), TotalTokens: new(int64(42)),
				CostCoverage: "complete", TokenCoverage: "complete",
			},
			want:    []string{"r-1", "exp-1", "Test cases:", "Trials:", "Failed:", "Canceled:", "Pass rate:", "80.00%", "Final avg:      0.85", "Cost:", "$0.1234", "Tokens:", "42"},
			notWant: []string{"coverage:", "Scores:", "Conversations:", "Generations:", "Mean score", "Breakdowns:"},
		},
		{
			name: "no test case produced a verdict",
			summary: experiments.ExperimentReportSummary{
				TestCaseCount: 2, TrialCount: 2, CompletedCount: 2,
				TotalTokens: new(int64(20077)), CostCoverage: "none", TokenCoverage: "complete",
			},
			want:    []string{"Pass rate:      -", "Cost:           -", "Tokens:         20077"},
			notWant: []string{"Final avg:"},
		},
		{
			name: "every test case failed and nothing reported usage",
			summary: experiments.ExperimentReportSummary{
				TestCaseCount: 1, TrialCount: 1, CompletedCount: 1,
				PassRate: new(0.0), PassDenominator: 1, CostCoverage: "none", TokenCoverage: "none",
			},
			want: []string{"Pass rate:      0.00%", "Cost:           -", "Tokens:         -"},
		},
		{
			// The API sums input and output tokens when no trial carries a total
			// of its own, and returns that total with token_coverage "none".
			name: "no trial reported a token total of its own",
			summary: experiments.ExperimentReportSummary{
				TestCaseCount: 1, TrialCount: 1, CompletedCount: 1,
				TotalTokens: new(int64(20077)), CostCoverage: "none", TokenCoverage: "none",
			},
			want: []string{"Tokens:         20077 (coverage: none)"},
		},
		{
			name: "cost and tokens cover only some trials",
			summary: experiments.ExperimentReportSummary{
				TestCaseCount: 2, TrialCount: 2, CompletedCount: 2,
				TotalCost: new(1.25), TotalTokens: new(int64(42)),
				CostCoverage: "partial", TokenCoverage: "partial",
			},
			want: []string{"Cost:           $1.2500 (coverage: partial)", "Tokens:         42 (coverage: partial)"},
		},
		{
			name:    "experiment failed",
			status:  "failed",
			errText: "runner crashed",
			summary: experiments.ExperimentReportSummary{TestCaseCount: 1, TrialCount: 1},
			want:    []string{"Status:         failed", "Error:          runner crashed"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := tc.status
			if status == "" {
				status = "completed"
			}
			report := &experiments.ExperimentReport{
				Experiment: experiments.Experiment{ExperimentID: "r-1", Name: "exp-1", Status: status, Error: tc.errText},
				Summary:    tc.summary,
			}
			codec := &experiments.ReportTextCodec{}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, report))
			out := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, out, s)
			}
			for _, s := range tc.notWant {
				assert.NotContains(t, out, s)
			}
		})
	}
}

func TestReportTextCodec_Encode_Value(t *testing.T) {
	// Codec should also accept a non-pointer ExperimentReport.
	passRate := 1.0
	report := experiments.ExperimentReport{
		Summary: experiments.ExperimentReportSummary{TrialCount: 3, PassRate: &passRate},
	}
	codec := &experiments.ReportTextCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, report))
	assert.Contains(t, buf.String(), "Trials:")
}

func TestReportTextCodec_WrongType(t *testing.T) {
	codec := &experiments.ReportTextCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-report")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected *ExperimentReport")
}

func writeTempFile(t *testing.T, pattern, contents string) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), pattern)
	require.NoError(t, err)
	_, err = file.WriteString(contents)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	return file.Name()
}
