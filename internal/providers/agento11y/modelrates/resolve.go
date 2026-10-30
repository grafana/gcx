package modelrates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ResolveItem identifies a price lookup. An omitted timestamp means server time.
type ResolveItem struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	At       string `json:"at,omitempty"`
}

type ResolveRequest struct {
	Items []ResolveItem `json:"items"`
}

// ResolvedRate retains the server's per-token pricing and source, including
// unknown pricing fields rather than silently dropping future rate dimensions.
type ResolvedRate struct {
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	At       string          `json:"at"`
	Source   string          `json:"source"`
	Pricing  json.RawMessage `json:"pricing"`
}

type ResolveResponse struct {
	Items []ResolvedRate `json:"items"`
}

// Resolve prices one model at one moment, from the tenant override if present
// and the catalog otherwise.
func (c *Client) Resolve(ctx context.Context, item ResolveItem) (*ResolveResponse, error) {
	result, err := agento11yhttp.DoJSON[ResolveRequest, ResolveResponse](ctx, c.base, http.MethodPost, basePath+":resolve", &ResolveRequest{Items: []ResolveItem{item}}, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

type resolveOpts struct {
	IO       cmdio.Options
	Provider string
	Model    string
	At       string
}

func (o *resolveOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Provider, "provider", "", "Provider as recorded in telemetry (required)")
	flags.StringVar(&o.Model, "model", "", "Model as recorded in telemetry (required)")
	flags.StringVar(&o.At, "at", "", "RFC3339 timestamp; omit to resolve at server time")
}

func (o *resolveOpts) Validate(flags *pflag.FlagSet) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Provider) == "" {
		return errors.New("--provider is required")
	}
	if strings.TrimSpace(o.Model) == "" {
		return errors.New("--model is required")
	}
	if flags.Changed("at") && o.At == "" {
		return errors.New("--at cannot be empty")
	}
	if o.At != "" {
		if _, err := time.Parse(time.RFC3339Nano, o.At); err != nil {
			return fmt.Errorf("--at must be RFC3339: %w", err)
		}
	}
	return nil
}

func newResolveCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &resolveOpts{}
	cmd := &cobra.Command{
		Use: "get-effective", Args: cobra.NoArgs,
		Short:   "Read the effective model price and whether it came from an override or catalog.",
		Long:    "Resolve a model price without changing it. Returns the pricing source and per-token rates (not per-million). An unknown price is not zero cost. Omit --at to use server time; pass a telemetry timestamp for historical pricing.",
		Example: "  gcx agento11y model-rates get-effective --provider openai --model gpt-5.5 -o json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd.Flags()); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			result, err := client.Resolve(cmd.Context(), ResolveItem{Provider: strings.TrimSpace(opts.Provider), Model: strings.TrimSpace(opts.Model), At: opts.At})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
