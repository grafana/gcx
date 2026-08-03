package experiments_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/aio11y/eval/experiments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommands_HasExpectedLeaves(t *testing.T) {
	cmd := experiments.Commands(nil)
	require.Equal(t, "experiments", cmd.Name())

	for _, sub := range []string{"list", "get", "create", "update", "cancel", "list-scores", "get-report", "check", "list-trials", "test-suites", "trials"} {
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

// TestCheckCommand_RejectsInvalidInvocation proves every rejection happens
// before any client call: the command group is built with a nil loader, so a
// request would fail loudly rather than silently pass. The exit code each
// rejection carries is asserted end to end in cmd/gcx/root.
func TestCheckCommand_RejectsInvalidInvocation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing run id",
			args:    []string{"check"},
			wantErr: "accepts 1 arg",
		},
		{
			name:    "missing --min-pass-rate",
			args:    []string{"check", "run-123"},
			wantErr: `required flag(s) "min-pass-rate" not set`,
		},
		{
			name:    "--min-pass-rate above 1",
			args:    []string{"check", "run-123", "--min-pass-rate", "1.1"},
			wantErr: "invalid --min-pass-rate value 1.1",
		},
		{
			name:    "--min-pass-rate below 0",
			args:    []string{"check", "run-123", "--min-pass-rate", "-0.5"},
			wantErr: "invalid --min-pass-rate value -0.5",
		},
		{
			name:    "--min-pass-rate NaN",
			args:    []string{"check", "run-123", "--min-pass-rate", "NaN"},
			wantErr: "must be a finite value from 0 through 1",
		},
		{
			name:    "--min-pass-rate Inf",
			args:    []string{"check", "run-123", "--min-pass-rate", "Inf"},
			wantErr: "must be a finite value from 0 through 1",
		},
		{
			name:    "--min-verdict-coverage above 1",
			args:    []string{"check", "run-123", "--min-pass-rate", "0.9", "--min-verdict-coverage", "2"},
			wantErr: "invalid --min-verdict-coverage value 2",
		},
		{
			name:    "--min-verdict-coverage below 0",
			args:    []string{"check", "run-123", "--min-pass-rate", "0.9", "--min-verdict-coverage", "-1"},
			wantErr: "invalid --min-verdict-coverage value -1",
		},
		{
			name:    "--min-verdict-coverage NaN",
			args:    []string{"check", "run-123", "--min-pass-rate", "0.9", "--min-verdict-coverage", "NaN"},
			wantErr: "must be a finite value from 0 through 1",
		},
		{
			name:    "unknown --on-unknown mode",
			args:    []string{"check", "run-123", "--min-pass-rate", "0.9", "--on-unknown", "ignore"},
			wantErr: `invalid --on-unknown value "ignore"`,
		},
		{
			name:    "--on-unknown is case sensitive",
			args:    []string{"check", "run-123", "--min-pass-rate", "0.9", "--on-unknown", "FAIL"},
			wantErr: `invalid --on-unknown value "FAIL"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := experiments.Commands(nil)
			cmd.SetArgs(tc.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			// The group under test has no SilenceUsage, so cobra prints its usage
			// text here; what matters is that no check document does.
			assert.NotContains(t, stdout.String(), `"verdict"`,
				"no check document may be written for an invalid invocation")
		})
	}
}

func TestCheckTextCodec_Format(t *testing.T) {
	assert.Equal(t, "text", string((&experiments.CheckTextCodec{}).Format()))
}

func TestCheckTextCodec_Encode(t *testing.T) {
	threshold := 0.9
	actual := 0.8

	tests := []struct {
		name   string
		result any
		want   []string
	}{
		{
			name: "threshold check reports both numbers",
			result: &experiments.CheckResult{
				ExperimentID:     "r-1",
				ExperimentStatus: "completed",
				Verdict:          experiments.CheckVerdictFail,
				TestCaseCount:    10,
				PassCount:        8,
				PassDenominator:  10,
				Checks: []experiments.Check{{
					Name:      "min_pass_rate",
					Verdict:   experiments.CheckVerdictFail,
					Threshold: &threshold,
					Actual:    &actual,
					Observed:  "pass rate 80.00% (8/10 test cases passed on the first completed attempt)",
				}},
			},
			want: []string{
				"Experiment:", "r-1", "Status:", "completed", "Quality check:", "fail",
				"Test cases:", "10", "Checks:", "min_pass_rate fail", "threshold 90.00%", "80.00%",
			},
		},
		{
			name: "unknown verdict is rendered as a value, passed by value",
			result: experiments.CheckResult{
				ExperimentID:     "r-1",
				ExperimentStatus: "completed",
				Verdict:          experiments.CheckVerdictFail,
				Checks: []experiments.Check{{
					Name:      "min_pass_rate",
					Verdict:   experiments.CheckVerdictUnknown,
					Threshold: &threshold,
					Observed:  "no test case produced a pass verdict",
				}},
			},
			want: []string{"min_pass_rate unknown", "no test case produced a pass verdict"},
		},
		{
			name: "status check has no numbers",
			result: &experiments.CheckResult{
				ExperimentID:     "r-1",
				ExperimentStatus: "failed",
				Verdict:          experiments.CheckVerdictFail,
				Checks: []experiments.Check{{
					Name:     "experiment_status",
					Verdict:  experiments.CheckVerdictFail,
					Observed: `experiment status is "failed", not "completed"`,
				}},
			},
			want: []string{"Status:", "failed", "experiment_status fail", `not "completed"`},
		},
		{
			name:   "no checks still reports the verdict",
			result: &experiments.CheckResult{ExperimentID: "r-1", Verdict: experiments.CheckVerdictPass},
			want:   []string{"Quality check:", "pass"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &experiments.CheckTextCodec{}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, tc.result))
			out := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, out, s)
			}
		})
	}
}

func TestCheckTextCodec_WrongType(t *testing.T) {
	codec := &experiments.CheckTextCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-check-result")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected *CheckResult")
}

func TestCheckTextCodec_Decode(t *testing.T) {
	err := (&experiments.CheckTextCodec{}).Decode(strings.NewReader("{}"), &experiments.CheckResult{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support decoding")
}

func TestTableCodec_Format(t *testing.T) {
	assert.Equal(t, "table", string((&experiments.TableCodec{}).Format()))
	assert.Equal(t, "wide", string((&experiments.TableCodec{Wide: true}).Format()))
}

func TestTableCodec_Encode(t *testing.T) {
	completed := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	items := []experiments.Experiment{
		{
			RunID:        "r-1",
			Name:         "exp-1",
			Status:       "running",
			Source:       "external",
			CollectionID: "c-1",
			Tags:         []string{"support", "prompt-v2"},
			Description:  "Nightly regression run",
			ScoreCount:   5,
			CreatedAt:    time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
			CompletedAt:  &completed,
			Error:        "something",
		},
		{RunID: "r-2", Name: "exp-2", Status: "pending"},
	}

	tests := []struct {
		name string
		wide bool
		want []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"EXPERIMENT-ID", "NAME", "STATUS", "SUITE", "VERSION", "TAGS", "r-1", "exp-1", "running", "support, prompt-v2"},
		},
		{
			name: "wide adds ERROR, COMPLETED, and DESCRIPTION",
			wide: true,
			want: []string{"ERROR", "COMPLETED", "DESCRIPTION", "something", "Nightly regression run", "2026-04-02 12:00"},
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
	passRate := 0.8
	totalTokens := int64(42)
	report := &experiments.ExperimentReport{
		Run: experiments.Experiment{
			RunID:  "r-1",
			Name:   "exp-1",
			Status: "succeeded",
		},
		Summary: experiments.ExperimentReportSummary{
			NConversations:  2,
			NGenerations:    3,
			NScores:         10,
			PassRate:        &passRate,
			PassCount:       8,
			PassDenominator: 10,
			MeanScore:       0.72,
			TotalCostUSD:    0.1234,
			TotalTokens:     &totalTokens,
		},
		Breakdowns: experiments.ExperimentReportBreakdowns{
			ByEvaluator: []experiments.ExperimentReportBreakdown{
				{Key: "ev-1", Count: 5, PassRate: 0.8, MeanScore: 0.75, TotalCostUSD: 0.1234, TotalTokens: 42},
			},
			ByScoreKey: []experiments.ExperimentReportBreakdown{
				{Key: "quality", Count: 5, PassRate: 0.8, MeanScore: 0.75},
			},
		},
	}

	codec := &experiments.ReportTextCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, report))
	out := buf.String()
	for _, s := range []string{"r-1", "exp-1", "Scores:", "10", "Conversations:", "2", "Pass rate", "80.00%", "Mean score", "0.72", "Cost:", "$0.1234", "Tokens:", "42", "Breakdowns:", "evaluator/ev-1", "cost=$0.1234", "tokens=42", "score_key/quality"} {
		assert.Contains(t, out, s)
	}
}

func TestReportTextCodec_Encode_Value(t *testing.T) {
	// Codec should also accept a non-pointer ExperimentReport.
	passRate := 1.0
	report := experiments.ExperimentReport{
		Summary: experiments.ExperimentReportSummary{NScores: 3, PassRate: &passRate},
	}
	codec := &experiments.ReportTextCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, report))
	assert.Contains(t, buf.String(), "Scores:")
}

// TestReportTextCodec_NullableAggregates covers the display side of the
// nullable aggregates: a measured 0% renders as a rate, an absent verdict says
// so, and a present zero token count renders as 0.
func TestReportTextCodec_NullableAggregates(t *testing.T) {
	zeroRate := 0.0
	zeroTokens := int64(0)

	tests := []struct {
		name    string
		summary experiments.ExperimentReportSummary
		want    []string
		notWant []string
	}{
		{
			name:    "measured zero pass rate renders as 0.00%",
			summary: experiments.ExperimentReportSummary{PassRate: &zeroRate, PassDenominator: 3},
			want:    []string{"Pass rate:", "0.00%"},
			notWant: []string{"no pass verdict"},
		},
		{
			name:    "absent pass rate renders as n/a",
			summary: experiments.ExperimentReportSummary{},
			want:    []string{"Pass rate:", "n/a (no pass verdict)"},
			notWant: []string{"0.00%"},
		},
		{
			// A denominator with no rate is an inconsistent payload. The line
			// must still say what the codec knows.
			name:    "absent pass rate with a non-zero denominator still renders a line",
			summary: experiments.ExperimentReportSummary{PassDenominator: 3},
			want:    []string{"Pass rate:", "n/a (no pass verdict)"},
		},
		{
			name:    "present zero token count renders",
			summary: experiments.ExperimentReportSummary{TotalTokens: &zeroTokens},
			want:    []string{"Tokens:", "0"},
		},
		{
			name:    "absent token count omits the line",
			summary: experiments.ExperimentReportSummary{},
			notWant: []string{"Tokens:"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &experiments.ReportTextCodec{}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, &experiments.ExperimentReport{Summary: tc.summary}))
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
