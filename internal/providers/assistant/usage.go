package assistant

import (
	"context"
	"errors"
	"time"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type usageGetOpts struct {
	IO  cmdio.Options
	top int
}

func (o *usageGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &assistant.UsageTextCodec{Top: &o.top})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.top, "top", 10, "Maximum users shown in text; 0 shows all")
}

func (o *usageGetOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.top < 0 {
		return errors.New("--top must not be negative; use --top 0 to show all users needing attention")
	}
	return nil
}

func usageCommand(loader *providers.ConfigLoader) *cobra.Command {
	group := &cobra.Command{Use: "usage", Short: "Read Grafana Assistant usage and limits"}
	opts := &usageGetOpts{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get Assistant usage and limits",
		Long: `Show your Grafana Assistant usage and monthly limits.

Admins also see organization usage, users at or above 80% of their token limit,
and pending requests to raise a limit. When a seat, token, or investigation limit
blocks you, the output names it and the command that asks your admins to raise it.
JSON groups metrics into yourUsage and orgUsage, with users needing attention
inside orgUsage.`,
		Args: cobra.NoArgs,
		Example: `  gcx assistant usage get
  gcx assistant usage get --context my-stack
  gcx assistant usage get -o json
  gcx assistant usage get --top 0`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := assistanthttp.NewClient(cfg)
			if err != nil {
				return err
			}
			usage, err := assistant.FetchUsage(ctx, client)
			if err != nil {
				return err
			}
			usage.Admin, err = assistant.FetchAdminUsage(ctx, client)
			if err != nil {
				if _, denied := errors.AsType[*assistanthttp.ForbiddenError](err); !denied {
					return err
				}
			}
			if err := assistant.FetchLimitContext(ctx, client, usage); err != nil {
				return err
			}
			if opts.IO.OutputFormat != "text" {
				return opts.IO.Encode(cmd.OutOrStdout(), usage.JSONView())
			}
			return opts.IO.Encode(cmd.OutOrStdout(), *usage)
		},
	}
	opts.setup(cmd.Flags())
	group.AddCommand(cmd)
	return group
}
