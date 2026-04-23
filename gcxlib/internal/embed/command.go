// Package embed provides the gcx cobra command tree for embedding, excluding
// development-only commands (dev/lint) that pull in heavy transitive deps.
package embed

import (
	"log/slog"
	"os"
	"path"

	"github.com/fatih/color"
	"github.com/go-logr/logr"
	"github.com/grafana/gcx/cmd/gcx/api"
	assistantcmd "github.com/grafana/gcx/cmd/gcx/assistant"
	"github.com/grafana/gcx/cmd/gcx/commands"
	"github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/dashboards"
	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/grafana/gcx/cmd/gcx/helptree"
	logincmd "github.com/grafana/gcx/cmd/gcx/login"
	cmdproviders "github.com/grafana/gcx/cmd/gcx/providers"
	"github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/cmd/gcx/setup"
	skillscmd "github.com/grafana/gcx/cmd/gcx/skills"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	_ "github.com/grafana/gcx/internal/datasources/providers"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/logs"
	"github.com/grafana/gcx/internal/providers"
	_ "github.com/grafana/gcx/internal/providers/aio11y"
	_ "github.com/grafana/gcx/internal/providers/alert"
	_ "github.com/grafana/gcx/internal/providers/appo11y"
	_ "github.com/grafana/gcx/internal/providers/faro"
	_ "github.com/grafana/gcx/internal/providers/fleet"
	_ "github.com/grafana/gcx/internal/providers/irm"
	_ "github.com/grafana/gcx/internal/providers/k6"
	_ "github.com/grafana/gcx/internal/providers/kg"
	_ "github.com/grafana/gcx/internal/providers/logs"
	_ "github.com/grafana/gcx/internal/providers/metrics"
	_ "github.com/grafana/gcx/internal/providers/profiles"
	_ "github.com/grafana/gcx/internal/providers/slo"
	_ "github.com/grafana/gcx/internal/providers/synth"
	_ "github.com/grafana/gcx/internal/providers/traces"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

// Command builds the gcx cobra command tree for embedding.
// It mirrors cmd/gcx/root.Command but excludes the dev subcommand
// (and its linter dependency) to avoid heavy transitive deps.
func Command(version string) *cobra.Command {
	pp := providers.All()

	noColors := false
	noTruncate := false
	agentFlag := false
	verbosity := 0
	contextName := ""
	logHTTPPayload := false

	rootCmd := &cobra.Command{
		Use:           path.Base(os.Args[0]),
		Short:         "Control plane for Grafana Cloud operations",
		Long:          "gcx is a unified CLI for managing Grafana resources, dashboards, datasources, alerting, and Cloud product APIs (SLO, IRM, Synthetic Monitoring, Fleet, k6, and more).",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			terminal.Detect()

			if agent.IsAgentMode() {
				terminal.SetPiped(true)
				terminal.SetNoTruncate(true)
				color.NoColor = true
				style.SetEnabled(false)
			}

			if noTruncate {
				terminal.SetNoTruncate(true)
			}

			if noColors || os.Getenv("NO_COLOR") != "" || terminal.IsPiped() {
				color.NoColor = true
				style.SetEnabled(false)
			}

			logLevel := new(slog.LevelVar)
			logLevel.Set(slog.LevelError)
			logLevel.Set(logLevel.Level() - slog.Level(min(verbosity, 3)*4))

			logHandler := logs.NewHandler(os.Stderr, &logs.Options{
				Level: logLevel,
			})
			logger := logging.NewSLogLogger(logHandler)
			klog.SetLoggerWithOptions(
				logr.FromSlogHandler(logHandler),
				klog.ContextualLogger(true),
			)

			ctx := logging.Context(cmd.Context(), logger)

			if contextName != "" {
				ctx = internalconfig.ContextWithName(ctx, contextName)
			}

			if logHTTPPayload {
				ctx = httputils.WithPayloadLogging(ctx, true)
			}

			cmd.SetContext(ctx)
		},
		Annotations: map[string]string{
			cobra.CommandDisplayNameAnnotation: "gcx",
		},
	}

	_ = agentFlag

	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(style.HelpFunc(defaultHelp))

	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.SetIn(os.Stdin)

	rootCmd.AddCommand(api.Command())
	rootCmd.AddCommand(assistantcmd.Command())
	rootCmd.AddCommand(logincmd.Command())
	rootCmd.AddCommand(config.Command())
	rootCmd.AddCommand(dashboards.Command())
	// dev.Command() intentionally omitted — avoids linter → Loki dep chain.
	rootCmd.AddCommand(setup.Command())
	rootCmd.AddCommand(skillscmd.Command())
	rootCmd.AddCommand(datasources.Command())
	rootCmd.AddCommand(resources.Command())

	rootCmd.AddCommand(cmdproviders.Command(pp))
	for _, p := range pp {
		if p == nil {
			continue
		}
		rootCmd.AddCommand(p.Commands()...)
	}

	commandsConfigOpts := &config.Options{}
	commandsCmd := commands.Command(rootCmd, commandsConfigOpts)
	commandsConfigOpts.BindFlags(commandsCmd.Flags())
	rootCmd.AddCommand(commandsCmd)

	rootCmd.AddCommand(helptree.Command(rootCmd))

	rootCmd.PersistentFlags().BoolVar(&noColors, "no-color", noColors, "Disable color output")
	rootCmd.PersistentFlags().BoolVar(&noTruncate, "no-truncate", false, "Disable table column truncation")
	rootCmd.PersistentFlags().BoolVar(&agentFlag, "agent", false, "Enable agent mode")
	rootCmd.PersistentFlags().CountVarP(&verbosity, "verbose", "v", "Verbose mode")
	rootCmd.PersistentFlags().StringVar(&contextName, "context", "", "Name of the context to use")
	rootCmd.PersistentFlags().BoolVar(&logHTTPPayload, "log-http-payload", false, "Log full HTTP request/response bodies")

	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	agent.ApplyAnnotations(rootCmd)
	return rootCmd
}
