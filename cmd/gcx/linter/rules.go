package linter

import (
	"github.com/grafana/gcx/internal/linter"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type rulesOpts struct {
	IO cmdio.Options

	rules []string
}

func (opts *rulesOpts) validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}

	return nil
}

func (opts *rulesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.DefaultFormat("yaml")
	opts.IO.BindFlags(flags)

	flags.StringArrayVarP(&opts.rules, "rules", "r", nil, "Path to custom rules.")
}

func rulesCmd() *cobra.Command {
	opts := rulesOpts{}

	cmd := &cobra.Command{
		Use:   "rules",
		Args:  cobra.NoArgs,
		Short: "List available linter rules",
		Long:  "List available linter rules.",
		Example: `
	# List built-in rules:

	gcx dev lint rules

	# List built-in and custom rules:

	gcx dev lint rules -r ./custom-rules
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.validate(); err != nil {
				return err
			}

			return listRules(cmd, opts)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func listRules(cmd *cobra.Command, opts rulesOpts) error {
	engine, err := linter.New(linter.WithCustomRules(opts.rules))
	if err != nil {
		return err
	}

	rules, err := engine.Rules(cmd.Context())
	if err != nil {
		return err
	}

	return opts.IO.Encode(cmd.OutOrStdout(), rules)
}
