package judge

import (
	"context"
	"errors"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
)

func newClient(ctx context.Context, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromContext(ctx, loader)
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

func newProvidersCommand(loader *providers.ConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[[]eval.JudgeProvider]{
		Use:        "providers",
		Short:      "List available judge providers.",
		Args:       cobra.NoArgs,
		DefaultFmt: "table",
		Codecs:     []format.Codec{&ProvidersTableCodec{}},
		Fetch: func(ctx context.Context, _ []string) ([]eval.JudgeProvider, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.ListProviders(ctx)
		},
	})
}

// --- models ---

func newModelsCommand(loader *providers.ConfigLoader) *cobra.Command {
	var provider string
	cmd := crudcmd.NewGetCommand(crudcmd.GetConfig[[]eval.JudgeModel]{
		Use:        "models --provider <id>",
		Short:      "List available judge models.",
		Args:       cobra.NoArgs,
		DefaultFmt: "table",
		Codecs:     []format.Codec{&ModelsTableCodec{}},
		Fetch: func(ctx context.Context, _ []string) ([]eval.JudgeModel, error) {
			if provider == "" {
				return nil, errors.New("--provider is required (see 'gcx aio11y judge providers')")
			}
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.ListModels(ctx, provider)
		},
	})
	cmd.Flags().StringVar(&provider, "provider", "", "Provider ID (required, see 'judge providers')")
	_ = cmd.MarkFlagRequired("provider")
	return cmd
}

// --- table codecs ---

// ProvidersTableCodec renders judge providers as a text table.
type ProvidersTableCodec struct{}

func (c *ProvidersTableCodec) Format() format.Format { return "table" }

func (c *ProvidersTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeTable(w, v, "JudgeProvider", []string{"ID", "NAME", "TYPE"}, func(t *style.TableBuilder, p eval.JudgeProvider) {
		t.Row(p.ID, p.Name, p.Type)
	})
}

func (c *ProvidersTableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}

// ModelsTableCodec renders judge models as a text table.
type ModelsTableCodec struct{}

func (c *ModelsTableCodec) Format() format.Format { return "table" }

func (c *ModelsTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeTable(w, v, "JudgeModel", []string{"ID", "NAME", "PROVIDER", "CONTEXT WINDOW"}, func(t *style.TableBuilder, m eval.JudgeModel) {
		ctxWindow := "-"
		if m.ContextWindow > 0 {
			ctxWindow = strconv.Itoa(m.ContextWindow)
		}
		t.Row(m.ID, m.Name, m.Provider, ctxWindow)
	})
}

func (c *ModelsTableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}
