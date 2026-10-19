//go:build !wasip1

// Package ext wires the experimental local extension PoC into gcx.
package ext

import (
	"fmt"
	"os"
	"strconv"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/ext"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const CommandName = "ext"
const experimental = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.\n\n"

func Verbs() []string {
	return []string{"install", "list", "uninstall", "update", "search", "scaffold"}
}

func mark(cmd *cobra.Command) *cobra.Command {
	cmd.Short = "[experimental] " + cmd.Short
	cmd.Long = experimental + cmd.Long
	cmd.Annotations = map[string]string{agent.AnnotationStability: agent.StabilityExperimental, agent.AnnotationTokenCost: "small"}
	return cmd
}

func Command() *cobra.Command {
	cmd := mark(&cobra.Command{
		Use: "ext [name] [args...]", Short: "Install and run local third-party extensions.",
		Long: ext.Disclosure + "\n\nPoC: compiled binaries installed from local git checkouts only. Put gcx global flags before ext; arguments after the name belong to the extension.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			store, err := ext.DefaultStore()
			if err != nil {
				return err
			}
			if _, err := store.Lookup(args[0]); err != nil {
				return fail.NewCommandUsageError(cmd, err.Error(), err)
			}
			binary, err := os.Executable()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), ext.Disclosure)
			code, err := store.Run(cmd.Context(), args[0], ext.RunOptions{Args: args[1:], Binary: binary, Context: config.ContextNameFromCtx(cmd.Context()), AgentMode: strconv.FormatBool(agent.IsAgentMode()), Version: version.Get(), Stdin: cmd.InOrStdin(), Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr()})
			if err != nil {
				return err
			}
			if code != 0 {
				return gcxerrors.NewAlreadyReportedError(code)
			}
			return nil
		},
	})
	cmd.Annotations[agent.AnnotationTokenCost] = "large"
	cmd.Annotations[agent.AnnotationLLMHint] = "Extension output belongs to third-party code; consult its help before running it."
	cmd.AddCommand(installCommand(), listCommand())
	return cmd
}

type installOpts struct {
	yes    bool
	output cmdio.Options
}

func (o *installOpts) setup(flags *pflag.FlagSet) {
	o.output.DefaultFormat("json")
	o.output.BindFlags(flags)
	flags.BoolVar(&o.yes, "yes", false, "Approve installing third-party code")
	flags.BoolVar(&o.yes, "force", false, "Approve installing third-party code")
}

func (o *installOpts) Validate() error { return o.output.Validate() }

func installCommand() *cobra.Command {
	opts := &installOpts{}
	cmd := mark(&cobra.Command{
		Use: "install <local-checkout>", Short: "Install a compiled extension from a local git checkout.", Long: ext.Disclosure + "\n\nReads gcx-extension.yaml at HEAD and copies its local platform binary. Installation never builds or executes extension code.", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			plan, err := ext.Prepare(cmd.Context(), args[0], version.Get())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "%s %s from %s at %s\n%s\n", plan.Name, plan.Version, plan.Source, plan.Commit, ext.Disclosure)
			store, err := ext.DefaultStore()
			if err != nil {
				return err
			}
			items, err := store.List()
			if err != nil {
				return err
			}
			for _, item := range items {
				if item.Name == plan.Name {
					fmt.Fprintf(cmd.ErrOrStderr(), "Replaces %s %s from %s at %s\n", item.Name, item.Version, item.Source, item.Commit)
				}
			}
			ok, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.yes, "Install this extension?")
			if err != nil {
				return err
			}
			if !ok {
				return gcxerrors.NewAlreadyReportedError(gcxerrors.ExitCancelled)
			}
			installed, err := store.Install(plan)
			if err != nil {
				return err
			}
			return opts.output.Encode(cmd.OutOrStdout(), installed)
		},
	})
	opts.setup(cmd.Flags())
	return cmd
}

func listCommand() *cobra.Command {
	opts := &cmdio.Options{}
	opts.DefaultFormat("json")
	cmd := mark(&cobra.Command{Use: "list", Short: "List installed extensions.", Long: "Read this PoC's local installation index.", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			store, err := ext.DefaultStore()
			if err != nil {
				return err
			}
			items, err := store.List()
			if err != nil {
				return err
			}
			return opts.Encode(cmd.OutOrStdout(), items)
		},
	})
	opts.BindFlags(cmd.Flags())
	return cmd
}
