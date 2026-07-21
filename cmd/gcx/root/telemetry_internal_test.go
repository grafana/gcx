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
	flagFailure.Store(nil)
	t.Cleanup(func() { flagFailure.Store(nil) })
	rootCmd, get, _ := telemetryTestTree()
	get.Flags().Bool("help", false, "")

	// --help on a resolved command: recorded as help with the resolved path.
	require.NoError(t, get.Flags().Set("help", "true"))
	info := FallbackTelemetryInfo(rootCmd, []string{"resources", "get", "--help"}, 0)
	assert.False(t, info.Suppress)
	assert.True(t, info.Help)
	assert.Equal(t, "resources get", info.Command)

	// `gcx resources bogus` fails with exit code 2: an unknown-command parse
	// failure under the resources group (#578).
	info = FallbackTelemetryInfo(rootCmd, []string{"resources", "bogus"}, 2)
	assert.False(t, info.Suppress)
	require.NotNil(t, info.ParseError)
	assert.Equal(t, parseErrorUnknownCommand, info.ParseError.Kind)
	assert.Equal(t, "resources", info.ParseError.Parent)
	assert.Equal(t, "bogus", info.ParseError.Token)

	// Non-runnable command group: cobra prints help before the hooks run.
	info = FallbackTelemetryInfo(rootCmd, []string{"resources"}, 0)
	assert.False(t, info.Suppress)
	assert.True(t, info.Help)
	assert.Equal(t, "resources", info.Command)

	// Unknown command with a zero exit means cobra answered the invocation
	// itself; there is no parse failure to record.
	info = FallbackTelemetryInfo(rootCmd, []string{"resourcse", "get"}, 0)
	assert.True(t, info.Suppress)

	// Suppressed commands stay suppressed on the fallback path too.
	info = FallbackTelemetryInfo(rootCmd, []string{"completion", "zsh", "--help"}, 0)
	assert.True(t, info.Suppress)
}
