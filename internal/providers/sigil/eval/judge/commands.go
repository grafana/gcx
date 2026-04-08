package judge

import (
	"errors"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := sigilhttp.NewClientFromCommand(cmd, loader)
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
	o.IO.RegisterCustomCodec("table", &ProvidersTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newProvidersCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &providersOpts{}
	cmd := &cobra.Command{
		Use:   "providers",
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
	o.IO.RegisterCustomCodec("table", &ModelsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Provider, "provider", "", "Provider ID (required, see 'judge providers')")
}

func (o *modelsOpts) Validate() error {
	if o.Provider == "" {
		return errors.New("--provider is required (see 'gcx sigil judge providers')")
	}
	return o.IO.Validate()
}

func newModelsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &modelsOpts{}
	cmd := &cobra.Command{
		Use:   "models --provider <id>",
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

// ProvidersTableCodec renders judge providers as a text table.
type ProvidersTableCodec struct{}

func (c *ProvidersTableCodec) Format() format.Format { return "table" }

func (c *ProvidersTableCodec) Encode(w io.Writer, v any) error {
	providers, ok := v.([]eval.JudgeProvider)
	if !ok {
		return errors.New("invalid data type for table codec: expected []JudgeProvider")
	}

	t := style.NewTable("ID", "NAME", "TYPE")
	for _, p := range providers {
		t.Row(p.ID, p.Name, p.Type)
	}
	return t.Render(w)
}

func (c *ProvidersTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ModelsTableCodec renders judge models as a text table.
type ModelsTableCodec struct{}

func (c *ModelsTableCodec) Format() format.Format { return "table" }

func (c *ModelsTableCodec) Encode(w io.Writer, v any) error {
	models, ok := v.([]eval.JudgeModel)
	if !ok {
		return errors.New("invalid data type for table codec: expected []JudgeModel")
	}

	t := style.NewTable("ID", "NAME", "PROVIDER", "CONTEXT WINDOW")
	for _, m := range models {
		ctx := "-"
		if m.ContextWindow > 0 {
			ctx = strconv.Itoa(m.ContextWindow)
		}
		t.Row(m.ID, m.Name, m.Provider, ctx)
	}
	return t.Render(w)
}

func (c *ModelsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
