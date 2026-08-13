package root

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// telemetryTestTree builds gcx-shaped commands: root (with the display-name
// annotation the real root carries) > resources > get, plus completion > zsh.
func telemetryTestTree() (*cobra.Command, *cobra.Command, *cobra.Command) {
	rootCmd := &cobra.Command{
		Use:         "gcx",
		Annotations: map[string]string{cobra.CommandDisplayNameAnnotation: "gcx"},
	}
	resources := &cobra.Command{Use: "resources"}
	get := &cobra.Command{Use: "get", Run: func(*cobra.Command, []string) {}}
	resources.AddCommand(get)
	rootCmd.AddCommand(resources)

	completion := &cobra.Command{Use: "completion"}
	zsh := &cobra.Command{Use: "zsh", Run: func(*cobra.Command, []string) {}}
	completion.AddCommand(zsh)
	rootCmd.AddCommand(completion)

	return rootCmd, get, zsh
}

func TestTrimCommandRoot(t *testing.T) {
	rootCmd, get, _ := telemetryTestTree()

	assert.Equal(t, "resources get", trimCommandRoot(get))
	assert.Empty(t, trimCommandRoot(rootCmd))
}

func TestChangedFlagNames_SortedNamesOnly(t *testing.T) {
	_, get, _ := telemetryTestTree()
	get.Flags().String("output", "", "")
	get.Flags().Bool("dry-run", false, "")
	require.NoError(t, get.Flags().Set("output", "secret-value"))
	require.NoError(t, get.Flags().Set("dry-run", "true"))

	names := changedFlagNames(get)

	assert.Equal(t, "dry-run,output", names)
	assert.NotContains(t, names, "secret-value", "flag values must never be recorded")
}

// resolvedOutputFormat is the only guard stopping a filesystem path from
// reaching the wire through output_format: on some commands --output is a
// directory, not a rendering format (`gcx dev linter new --output <dir>`).
// Anything not in the allowlist must be dropped, not passed through.
func TestResolvedOutputFormat_AllowlistsFormatsAndDropsPaths(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"known format is recorded", "json", "json"},
		{"known format is lowercased", "YAML", "yaml"},
		{"absolute path is dropped", "/home/alice/generated-rules", ""},
		{"relative path is dropped", "./out/rules", ""},
		{"windows path is dropped", `C:\Users\alice\out`, ""},
		{"unknown value is dropped", "some-custom-thing", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, get, _ := telemetryTestTree()
			get.Flags().String("output", "", "")
			require.NoError(t, get.Flags().Set("output", tt.value))

			got := resolvedOutputFormat(get)

			// Asserting equality is the whole check: an allowlist miss yields
			// "". A follow-up NotContains on an already-empty string would add
			// nothing, which is how the first version of this test managed to
			// look like a privacy assertion while testing nothing.
			assert.Equal(t, tt.want, got)
		})
	}
}

// The --json path is a separate branch of resolvedOutputFormat and is where it
// is currently wrong for the commands this PR instruments: gcx registers --json
// as a string flag (fields to select, or "list" to discover), so boolFlagSet
// never fires for it. A command rendering JSON via --json is recorded with
// whatever --output says, which for the resources commands is the "text"
// default.
//
// Pinned rather than fixed here: output_format is pre-existing and changing what
// it reports is a wire-contract change, not part of adding batch volume. It
// matters because correlating batch volume against output_format is one of the
// first questions the new fields invite, and the answer is currently skewed.
//
// Tracked in #1178. This test asserts the wrong answer on purpose, so invert it
// as part of that fix rather than reading it as the intended contract.
func TestResolvedOutputFormat_JSONFlagIsNotReflected(t *testing.T) {
	_, get, _ := telemetryTestTree()
	get.Flags().String("output", "text", "")
	get.Flags().String("json", "", "")
	require.NoError(t, get.Flags().Set("json", "name,uid"))

	assert.Equal(t, "text", resolvedOutputFormat(get),
		"pinning current behaviour, which is wrong and should be fixed separately: "+
			"--json renders JSON but output_format reports --output's value")
}

func TestTelemetrySuppressed(t *testing.T) {
	rootCmd, get, zsh := telemetryTestTree()

	assert.False(t, telemetrySuppressed(get))
	assert.True(t, telemetrySuppressed(zsh), "completion subcommands are suppressed via the ancestor chain")

	version := &cobra.Command{Use: "version"}
	rootCmd.AddCommand(version)
	assert.True(t, telemetrySuppressed(version))

	telemetryCmd := &cobra.Command{Use: "telemetry"}
	rootCmd.AddCommand(telemetryCmd)
	assert.True(t, telemetrySuppressed(telemetryCmd), "opting out must never itself be recorded")
}

func TestRecordTelemetryInfo_HelpResolvesTarget(t *testing.T) {
	telemetryInfo.Store(nil)
	t.Cleanup(func() { telemetryInfo.Store(nil) })
	rootCmd, _, _ := telemetryTestTree()
	help := &cobra.Command{Use: "help"}
	rootCmd.AddCommand(help)

	recordTelemetryInfo(help, []string{"resources", "get"})

	info := CurrentTelemetryInfo()
	require.NotNil(t, info)
	assert.True(t, info.Help)
	assert.Equal(t, "resources get", info.Command)

	// Suppression follows the help target: asking for help about a suppressed
	// command must stay as unrecorded as running it.
	recordTelemetryInfo(help, []string{"completion", "zsh"})
	require.NotNil(t, CurrentTelemetryInfo())
	assert.True(t, CurrentTelemetryInfo().Suppress)
}

func TestFallbackTelemetryInfo(t *testing.T) {
	rootCmd, get, _ := telemetryTestTree()
	get.Flags().Bool("help", false, "")

	// --help on a resolved command: recorded as help with the resolved path.
	require.NoError(t, get.Flags().Set("help", "true"))
	info := FallbackTelemetryInfo(rootCmd, []string{"resources", "get", "--help"}, 0)
	assert.False(t, info.Suppress)
	assert.True(t, info.Help)
	assert.Equal(t, "resources get", info.Command)

	// `gcx resources bogus` fails with exit code 2, but Find still resolves
	// it to the "resources" group - the same thing bare `gcx resources` (a
	// help view) resolves to. The exit code is the only difference between
	// them, so the failure must be suppressed rather than counted as help.
	info = FallbackTelemetryInfo(rootCmd, []string{"resources", "bogus"}, 2)
	assert.True(t, info.Suppress)

	// Non-runnable command group: cobra prints help before the hooks run.
	info = FallbackTelemetryInfo(rootCmd, []string{"resources"}, 0)
	assert.False(t, info.Suppress)
	assert.True(t, info.Help)
	assert.Equal(t, "resources", info.Command)

	// Unknown commands are parse failures, suppressed until parse capture.
	info = FallbackTelemetryInfo(rootCmd, []string{"resourcse", "get"}, 0)
	assert.True(t, info.Suppress)

	// Suppressed commands stay suppressed on the fallback path too.
	info = FallbackTelemetryInfo(rootCmd, []string{"completion", "zsh", "--help"}, 0)
	assert.True(t, info.Suppress)
}
