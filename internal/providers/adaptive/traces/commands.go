package traces

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/adaptive/auth"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Commands returns the traces command group for the adaptive provider.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "traces",
		Short: "Manage Adaptive Traces resources.",
	}
	h := &tracesHelper{loader: loader}
	cmd.AddCommand(h.recommendationsCommand())
	return cmd
}

type tracesHelper struct {
	loader *providers.ConfigLoader
}

func (h *tracesHelper) newClient(ctx context.Context) (*Client, error) {
	signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "traces")
	if err != nil {
		return nil, err
	}
	return NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient), nil
}

func (h *tracesHelper) recommendationsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recommendations",
		Short: "Manage Adaptive Traces recommendations.",
	}
	cmd.AddCommand(
		h.recommendationsShowCommand(),
		h.recommendationsApplyCommand(),
		h.recommendationsDismissCommand(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// recommendations show
// ---------------------------------------------------------------------------

type recommendationsShowOpts struct {
	IO cmdio.Options
}

func (o *recommendationsShowOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &recommendationTableCodec{})
	o.IO.RegisterCustomCodec("wide", &recommendationTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (h *tracesHelper) recommendationsShowCommand() *cobra.Command {
	opts := &recommendationsShowOpts{}
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show Adaptive Traces recommendations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			recs, err := client.ListRecommendations(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), recs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type recommendationTableCodec struct {
	Wide bool
}

func (c *recommendationTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *recommendationTableCodec) Encode(w io.Writer, v any) error {
	recs, ok := v.([]Recommendation)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Recommendation")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	if c.Wide {
		fmt.Fprintln(tw, "ID\tMESSAGE\tTAGS\tAPPLIED\tDISMISSED\tSTALE\tCREATED AT\tACTIONS")
	} else {
		fmt.Fprintln(tw, "ID\tMESSAGE\tTAGS\tAPPLIED\tDISMISSED\tSTALE\tCREATED AT")
	}

	for _, r := range recs {
		tags := strings.Join(r.Tags, ",")
		if c.Wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%v\t%v\t%s\t%d\n",
				r.ID, r.Message, tags, r.Applied, r.Dismissed, r.Stale, r.CreatedAt, len(r.Actions))
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%v\t%v\t%s\n",
				r.ID, r.Message, tags, r.Applied, r.Dismissed, r.Stale, r.CreatedAt)
		}
	}

	return tw.Flush()
}

func (c *recommendationTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// recommendations apply
// ---------------------------------------------------------------------------

type recommendationsApplyOpts struct {
	DryRun bool
}

func (o *recommendationsApplyOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview what would be applied without making changes")
}

func (h *tracesHelper) recommendationsApplyCommand() *cobra.Command {
	opts := &recommendationsApplyOpts{}
	cmd := &cobra.Command{
		Use:   "apply <id>",
		Short: "Apply an Adaptive Traces recommendation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			if opts.DryRun {
				cmdio.Info(cmd.OutOrStdout(), "[dry-run] Would apply recommendation %q", id)
				return nil
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			if err := client.ApplyRecommendation(ctx, id); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Applied recommendation %q", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// recommendations dismiss
// ---------------------------------------------------------------------------

type recommendationsDismissOpts struct {
	DryRun bool
}

func (o *recommendationsDismissOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview what would be dismissed without making changes")
}

func (h *tracesHelper) recommendationsDismissCommand() *cobra.Command {
	opts := &recommendationsDismissOpts{}
	cmd := &cobra.Command{
		Use:   "dismiss <id>",
		Short: "Dismiss an Adaptive Traces recommendation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			if opts.DryRun {
				cmdio.Info(cmd.OutOrStdout(), "[dry-run] Would dismiss recommendation %q", id)
				return nil
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			if err := client.DismissRecommendation(ctx, id); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Dismissed recommendation %q", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
