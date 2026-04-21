package setup

import (
	"fmt"
	"os"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// isInteractiveFn is a package-level hook that overrides terminal TTY detection for the run command.
// When nil, framework.Run uses terminal.StdinIsTerminal. Only set in tests.
var isInteractiveFn func() bool //nolint:gochecknoglobals

// SetIsInteractiveForTest overrides the TTY check used by NewRunCommand for testing.
// Call with nil to restore the default.
func SetIsInteractiveForTest(fn func() bool) {
	isInteractiveFn = fn
}

type runOpts struct {
	IO cmdio.Options
}

func (o *runOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &statusTextCodec{})
	o.IO.RegisterCustomCodec("wide", &statusTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *runOpts) Validate() error {
	return o.IO.Validate()
}

// NewRunCommand returns the `setup run` subcommand. Exported for testing.
func NewRunCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &runOpts{}
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Interactive orchestrator for product setup.",
		Long: `Run the interactive setup flow to onboard and configure Grafana Cloud products.

This command is interactive and requires a terminal (stdin must be a TTY).
In agent mode or non-interactive environments, use the per-product setup commands instead:
  gcx <product> setup

For example:
  gcx slo setup
  gcx synth setup`,
		Annotations: map[string]string{agent.AnnotationTokenCost: "small"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if agent.IsAgentMode() {
				cmd.PrintErrln("gcx setup run is not available in agent mode.")
				cmd.PrintErrln("Use per-product setup commands instead: gcx <product> setup")
				ec := fail.ExitUsageError
				return &fail.DetailedError{
					Summary:  "setup run is not available in agent mode",
					ExitCode: &ec,
				}
			}

			fopts := framework.Options{
				In:            cmd.InOrStdin(),
				StdinFile:     os.Stdin,
				Out:           cmd.OutOrStdout(),
				Err:           cmd.ErrOrStderr(),
				IsInteractive: isInteractiveFn,
			}
			summary, err := framework.Run(cmd.Context(), fopts)
			if err != nil {
				ec := fail.ExitUsageError
				return &fail.DetailedError{
					Summary:  err.Error(),
					ExitCode: &ec,
				}
			}

			if len(summary.Cancelled) > 0 || cmd.Context().Err() != nil {
				ec := fail.ExitCancelled
				return &fail.DetailedError{
					Summary:  "setup cancelled",
					ExitCode: &ec,
				}
			}

			if len(summary.Failed) > 0 {
				ec := fail.ExitPartialFailure
				return &fail.DetailedError{
					Summary:  "one or more setup operations failed",
					ExitCode: &ec,
				}
			}

			statuses := framework.AggregateStatus(cmd.Context())
			if encErr := opts.IO.Encode(cmd.OutOrStdout(), statuses); encErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not render status: %v\n", encErr)
			}

			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
