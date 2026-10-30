package judge

import (
	"errors"
	"strconv"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := agento11yhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the judge command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "judge",
		Short: "List LLM providers and models available for LLM-judge evaluators.",
		Long: `List LLM providers and models available for LLM-judge evaluators.

Use these values in the 'provider' and 'model' fields of an llm_judge evaluator config.`,
	}
	cmd.AddCommand(
		newProvidersCommand(loader),
		newModelsCommand(loader),
	)
	return cmd
}

// --- providers ---

type providersOpts struct {
	IO cmdio.Options
}

func (o *providersOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, ProvidersTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newProvidersCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &providersOpts{}
	cmd := &cobra.Command{
		Use:   "list-providers",
		Short: "List available judge providers.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			providers, err := client.ListProviders(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), providers)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- models ---

type modelsOpts struct {
	IO       cmdio.Options
	Provider string
}

func (o *modelsOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, ModelsTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Provider, "provider", "", "Provider ID (required, see 'judge list-providers')")
}

func (o *modelsOpts) Validate() error {
	if o.Provider == "" {
		return errors.New("--provider is required (see 'gcx agento11y judge list-providers')")
	}
	return o.IO.Validate()
}

func newModelsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &modelsOpts{}
	cmd := &cobra.Command{
		Use:   "list-models --provider <id>",
		Short: "List available judge models.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			models, err := client.ListModels(cmd.Context(), opts.Provider)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), models)
		},
	}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("provider")
	return cmd
}

// --- table codecs ---

func ProvidersTable() cmdio.Table[eval.JudgeProvider] {
	return cmdio.Table[eval.JudgeProvider]{Columns: []cmdio.Column[eval.JudgeProvider]{
		{Header: "ID", Content: func(r eval.JudgeProvider) string { return r.ID }},
		{Header: "NAME", Content: func(r eval.JudgeProvider) string { return r.Name }},
		{Header: "TYPE", Content: func(r eval.JudgeProvider) string { return r.Type }},
	}}
}

func ModelsTable() cmdio.Table[eval.JudgeModel] {
	return cmdio.Table[eval.JudgeModel]{Columns: []cmdio.Column[eval.JudgeModel]{
		{Header: "ID", Content: func(r eval.JudgeModel) string { return r.ID }},
		{Header: "NAME", Content: func(r eval.JudgeModel) string { return r.Name }},
		{Header: "PROVIDER", Content: func(r eval.JudgeModel) string { return r.Provider }},
		{Header: "CONTEXT WINDOW", Content: func(r eval.JudgeModel) string {
			if r.ContextWindow > 0 {
				return strconv.Itoa(r.ContextWindow)
			}
			return "-"
		}},
	}}
}
