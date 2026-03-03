package root

import (
	"log/slog"
	"os"
	"path"

	"github.com/fatih/color"
	"github.com/go-logr/logr"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/grafana/grafanactl/cmd/grafanactl/config"
	cmdproviders "github.com/grafana/grafanactl/cmd/grafanactl/providers"
	"github.com/grafana/grafanactl/cmd/grafanactl/resources"
	"github.com/grafana/grafanactl/internal/logs"
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

// Command builds the root cobra command for the given version using the
// compile-time registered provider list.
func Command(version string) *cobra.Command {
	return newCommand(version, providers.All())
}

// newCommand builds the root cobra command with an explicit provider list.
// Callers that need to inject providers (e.g. tests) should use this directly.
// Nil entries in pp are silently skipped.
func newCommand(version string, pp []providers.Provider) *cobra.Command {
	noColors := false
	verbosity := 0

	rootCmd := &cobra.Command{
		Use:           path.Base(os.Args[0]),
		SilenceUsage:  true,
		SilenceErrors: true, // We want to print errors ourselves
		Version:       version,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if noColors {
				color.NoColor = true // globally disables colorized output
			}

			logLevel := new(slog.LevelVar)
			logLevel.Set(slog.LevelWarn)
			// Multiplying the number of occurrences of the `-v` flag by 4 (gap between log levels in slog)
			// allows us to increase the logger's verbosity.
			logLevel.Set(logLevel.Level() - slog.Level(min(verbosity, 3)*4))

			logHandler := logs.NewHandler(os.Stderr, &logs.Options{
				Level: logLevel,
			})
			logger := logging.NewSLogLogger(logHandler)

			// Also set klog logger (used by k8s/client-go).
			klog.SetLoggerWithOptions(
				logr.FromSlogHandler(logHandler),
				klog.ContextualLogger(true),
			)

			cmd.SetContext(logging.Context(cmd.Context(), logger))
		},
		Annotations: map[string]string{
			cobra.CommandDisplayNameAnnotation: "grafanactl",
		},
	}

	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.SetIn(os.Stdin)

	rootCmd.AddCommand(config.Command())
	rootCmd.AddCommand(resources.Command())

	rootCmd.AddCommand(cmdproviders.Command(pp))
	for _, p := range pp {
		if p == nil {
			continue
		}
		rootCmd.AddCommand(p.Commands()...)
	}

	rootCmd.PersistentFlags().BoolVar(&noColors, "no-color", noColors, "Disable color output")
	rootCmd.PersistentFlags().CountVarP(&verbosity, "verbose", "v", "Verbose mode. Multiple -v options increase the verbosity (maximum: 3).")

	return rootCmd
}
