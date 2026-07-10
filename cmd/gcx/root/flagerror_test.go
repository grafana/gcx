package root_test

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFlagTestCommand(t *testing.T) *cobra.Command {
	t.Helper()

	rootCmd := &cobra.Command{Use: "gcx"}
	rootCmd.PersistentFlags().String("context", "", "Config context to use")

	getCmd := &cobra.Command{
		Use:  "get",
		RunE: func(_ *cobra.Command, _ []string) error { return nil },
	}
	getCmd.Flags().StringP("output", "o", "", "Output format")
	getCmd.Flags().String("format", "", "Rendering format")

	groupCmd := &cobra.Command{Use: "resources"}
	groupCmd.AddCommand(getCmd)
	rootCmd.AddCommand(groupCmd)

	return getCmd
}

func parseFlagError(t *testing.T, cmd *cobra.Command, args []string) error {
	t.Helper()
	err := cmd.ParseFlags(args)
	require.Error(t, err)
	return err
}

func TestFlagUsageError_UnknownLongFlagSuggestsAndCorrects(t *testing.T) {
	cmd := newFlagTestCommand(t)
	parseErr := parseFlagError(t, cmd, []string{"--formt", "json"})

	err := root.FlagUsageErrorForTest(cmd, parseErr, []string{"resources", "get", "dashboards", "--formt", "json"})

	usageErr := &fail.UsageError{}
	require.ErrorAs(t, err, &usageErr)
	assert.Equal(t, `unknown flag: --formt for "gcx resources get"`, usageErr.Message)
	assert.Contains(t, usageErr.Suggestions, "Did you mean '--format'?")
	assert.Contains(t, usageErr.Suggestions, "Run 'gcx resources get --help' for full usage and examples")
	require.Len(t, usageErr.Corrections, 1)
	assert.Equal(t, gcxerrors.Correction{
		Command: "gcx resources get dashboards --format json",
		Hint:    "Rendering format",
	}, usageErr.Corrections[0])
}

func TestFlagUsageError_EqualsSyntaxPreservedInCorrection(t *testing.T) {
	cmd := newFlagTestCommand(t)
	parseErr := parseFlagError(t, cmd, []string{"--formt=json"})

	err := root.FlagUsageErrorForTest(cmd, parseErr, []string{"resources", "get", "--formt=json"})

	usageErr := &fail.UsageError{}
	require.ErrorAs(t, err, &usageErr)
	require.Len(t, usageErr.Corrections, 1)
	assert.Equal(t, "gcx resources get --format=json", usageErr.Corrections[0].Command)
}

func TestFlagUsageError_SpacedValueQuotedInCorrection(t *testing.T) {
	cmd := newFlagTestCommand(t)
	parseErr := parseFlagError(t, cmd, []string{"--formt", "up == 1"})

	err := root.FlagUsageErrorForTest(cmd, parseErr, []string{"resources", "get", "--formt", "up == 1", "it's"})

	usageErr := &fail.UsageError{}
	require.ErrorAs(t, err, &usageErr)
	require.Len(t, usageErr.Corrections, 1)
	assert.Equal(t, `gcx resources get --format 'up == 1' 'it'\''s'`, usageErr.Corrections[0].Command)
}

func TestFlagUsageError_RedactsSensitiveFlagValueInCorrection(t *testing.T) {
	cmd := newFlagTestCommand(t)
	cmd.Flags().String("token", "", "Grafana service account token")
	parseErr := parseFlagError(t, cmd, []string{"--formt", "json", "--token", "sekrit-value"})

	err := root.FlagUsageErrorForTest(cmd, parseErr, []string{"resources", "get", "--formt", "json", "--token", "sekrit-value"})

	usageErr := &fail.UsageError{}
	require.ErrorAs(t, err, &usageErr)
	require.Len(t, usageErr.Corrections, 1)
	assert.NotContains(t, usageErr.Corrections[0].Command, "sekrit-value")
	assert.Equal(t, "gcx resources get --format json --token '<redacted>'", usageErr.Corrections[0].Command)
}

func TestFlagUsageError_RedactsSensitiveFlagValueWithEqualsSyntax(t *testing.T) {
	cmd := newFlagTestCommand(t)
	cmd.Flags().String("token", "", "Grafana service account token")
	parseErr := parseFlagError(t, cmd, []string{"--formt=json", "--token=sekrit-value"})

	err := root.FlagUsageErrorForTest(cmd, parseErr, []string{"resources", "get", "--formt=json", "--token=sekrit-value"})

	usageErr := &fail.UsageError{}
	require.ErrorAs(t, err, &usageErr)
	require.Len(t, usageErr.Corrections, 1)
	assert.NotContains(t, usageErr.Corrections[0].Command, "sekrit-value")
	assert.Equal(t, "gcx resources get --format=json '--token=<redacted>'", usageErr.Corrections[0].Command)
}

func TestFlagUsageError_MatchesInheritedFlags(t *testing.T) {
	cmd := newFlagTestCommand(t)
	parseErr := parseFlagError(t, cmd, []string{"--contxt", "dev"})

	err := root.FlagUsageErrorForTest(cmd, parseErr, []string{"resources", "get", "--contxt", "dev"})

	usageErr := &fail.UsageError{}
	require.ErrorAs(t, err, &usageErr)
	assert.Contains(t, usageErr.Suggestions, "Did you mean '--context'?")
	require.Len(t, usageErr.Corrections, 1)
	assert.Equal(t, "gcx resources get --context dev", usageErr.Corrections[0].Command)
	assert.Equal(t, "Config context to use", usageErr.Corrections[0].Hint)
}

func TestFlagUsageError_UnknownShorthandWrappedWithoutFuzzyMatch(t *testing.T) {
	cmd := newFlagTestCommand(t)
	parseErr := parseFlagError(t, cmd, []string{"-z"})

	err := root.FlagUsageErrorForTest(cmd, parseErr, []string{"resources", "get", "-z"})

	usageErr := &fail.UsageError{}
	require.ErrorAs(t, err, &usageErr)
	assert.Contains(t, usageErr.Message, `unknown shorthand flag: 'z' in -z for "gcx resources get"`)
	assert.Equal(t, []string{"Run 'gcx resources get --help' for full usage and examples"}, usageErr.Suggestions)
	assert.Empty(t, usageErr.Corrections)
}

func TestFlagUsageError_NonFlagErrorsPassThrough(t *testing.T) {
	cmd := newFlagTestCommand(t)
	original := errors.New("flag needs an argument: --output")

	assert.Same(t, original, root.FlagUsageErrorForTest(cmd, original, nil))
}

func TestRedactSensitiveValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "bare token flag redacted",
			args: []string{"login", "--token", "sekrit-value"},
			want: []string{"login", "--token", "<redacted>"},
		},
		{
			name: "equals syntax redacted",
			args: []string{"login", "--token=sekrit-value"},
			want: []string{"login", "--token=<redacted>"},
		},
		{
			name: "non-sensitive flags untouched",
			args: []string{"resources", "get", "--format", "json"},
			want: []string{"resources", "get", "--format", "json"},
		},
		{
			name: "password and secret also redacted",
			args: []string{"--password", "hunter2", "--secret=abc", "--api-key", "xyz"},
			want: []string{"--password", "<redacted>", "--secret=<redacted>", "--api-key", "<redacted>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, root.RedactSensitiveValuesForTest(tt.args))
		})
	}
}

func TestSubstituteFlag(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		unknown   string
		candidate string
		want      []string
		ok        bool
	}{
		{
			name:      "bare flag",
			args:      []string{"get", "--formt", "json"},
			unknown:   "--formt",
			candidate: "--format",
			want:      []string{"get", "--format", "json"},
			ok:        true,
		},
		{
			name:      "equals value",
			args:      []string{"get", "--formt=json"},
			unknown:   "--formt",
			candidate: "--format",
			want:      []string{"get", "--format=json"},
			ok:        true,
		},
		{
			name:      "duplicate occurrences skip correction",
			args:      []string{"--formt", "a", "--formt", "b"},
			unknown:   "--formt",
			candidate: "--format",
			ok:        false,
		},
		{
			name:      "absent token",
			args:      []string{"get"},
			unknown:   "--formt",
			candidate: "--format",
			ok:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := root.SubstituteFlagForTest(tt.args, tt.unknown, tt.candidate)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
