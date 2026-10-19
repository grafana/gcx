//go:build !wasip1

// Package plugins wires the typed go-plugin experiment into gcx.
package plugins

import (
	"fmt"
	"os"
	"path/filepath"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	sdk "github.com/grafana/gcx/experimental/pluginsdk"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/pluginhost"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

const disclosure = "Third-party plugins run with your permissions and credentials. Grafana and gcx maintainers do not review or support their contents and are not responsible for consequences of running them. Use at your own risk."
const experimental = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.\n\n"

type runOpts struct {
	config cmdconfig.Options
	demo   bool
}

func (o *runOpts) setup(flags *pflag.FlagSet) {
	o.config.BindFlags(flags)
	flags.BoolVar(&o.demo, "demo", false, "Use offline host resource fixtures, without backend access")
}
func (o *runOpts) Validate() error { return nil }
func mark(c *cobra.Command) *cobra.Command {
	c.Short = "[experimental] " + c.Short
	c.Long = experimental + c.Long
	c.Annotations = map[string]string{agent.AnnotationStability: agent.StabilityExperimental, agent.AnnotationTokenCost: "large", agent.AnnotationLLMHint: "Run only trusted local plugins; output belongs to third-party code."}
	return c
}
func Command() *cobra.Command {
	c := mark(&cobra.Command{Use: "plugins", Short: "Run local plugins with typed host capabilities.", Long: disclosure})
	opts := &runOpts{}
	run := mark(&cobra.Command{Use: "run <executable> -- [args...]", Short: "Run a local Go plugin with typed resource callbacks.", Long: disclosure + "\n\nThe executable must implement the experimental plugin SDK. Host flags go before --; arguments after -- belong to the plugin. This PoC provides read-only resource capabilities.", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		binary, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), disclosure)
		var host sdk.ResourceHostServer = &pluginhost.Demo{}
		if !opts.demo {
			if opts.config.Context == "" {
				opts.config.Context = config.ContextNameFromCtx(cmd.Context())
			}
			cfg, err := opts.config.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			host, err = pluginhost.New(cmd.Context(), cfg)
			if err != nil {
				return err
			}
		}
		interactive := !agent.IsAgentMode() && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
		if interactive {
			state, err := term.GetState(int(os.Stdin.Fd()))
			if err != nil {
				return err
			}
			// A crashed or cancelled plugin cannot restore the host's terminal itself.
			defer func() { _ = term.Restore(int(os.Stdin.Fd()), state) }()
		}
		result, err := sdk.Run(cmd.Context(), sdk.Options{Binary: binary, Args: args[1:], Interactive: interactive, Diagnostics: cmd.ErrOrStderr()}, host)
		if err != nil {
			return err
		}
		if _, err = cmd.OutOrStdout().Write(result.GetStdout()); err != nil {
			return err
		}
		if result.GetStderr() != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), result.GetStderr())
		}
		if result.GetExitCode() != 0 {
			return gcxerrors.NewAlreadyReportedError(int(result.GetExitCode()))
		}
		return nil
	}})
	opts.setup(run.Flags())
	c.AddCommand(run)
	return c
}
