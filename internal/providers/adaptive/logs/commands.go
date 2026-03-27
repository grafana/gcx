package logs

import (
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
)

// Commands returns the logs command group for adaptive logs management.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Manage Adaptive Logs resources.",
	}
	h := &logsHelper{loader: loader}
	cmd.AddCommand(h.patternsCommand())
	return cmd
}

type logsHelper struct {
	loader *providers.ConfigLoader
}

func (h *logsHelper) patternsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "patterns",
		Short: "Manage adaptive log patterns.",
	}
	cmd.AddCommand(
		h.patternsShowCommand(),
		h.patternsApplyCommand(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// patterns show
// ---------------------------------------------------------------------------

type patternsShowOpts struct {
	IO cmdio.Options
}

func (o *patternsShowOpts) setup(cmd *cobra.Command) {
	o.IO.RegisterCustomCodec("table", &patternsTableCodec{wide: false})
	o.IO.RegisterCustomCodec("wide", &patternsTableCodec{wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(cmd.Flags())
}

func (h *logsHelper) patternsShowCommand() *cobra.Command {
	opts := &patternsShowOpts{}
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show adaptive log pattern recommendations.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "logs")
			if err != nil {
				return err
			}
			client := NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)

			recs, err := client.ListRecommendations(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), recs)
		},
	}
	opts.setup(cmd)
	return cmd
}

// patternsTableCodec renders LogRecommendations as a tabular table.
type patternsTableCodec struct{ wide bool }

func (c *patternsTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}

func (c *patternsTableCodec) Encode(w io.Writer, v any) error {
	recs, ok := v.([]LogRecommendation)
	if !ok {
		return errors.New("invalid data type for table codec: expected []LogRecommendation")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.wide {
		fmt.Fprintln(tw, "PATTERN\tDROP RATE\tRECOMMENDED RATE\tVOLUME\tINGESTED LINES\tQUERIED LINES\tLOCKED\tSUPERSEDED")
	} else {
		fmt.Fprintln(tw, "PATTERN\tDROP RATE\tRECOMMENDED RATE\tVOLUME\tLOCKED")
	}

	for _, rec := range recs {
		pattern := rec.Pattern
		if pattern == "" {
			pattern = rec.Label()
		}
		if c.wide {
			fmt.Fprintf(tw, "%s\t%.4f\t%.4f\t%d\t%d\t%d\t%v\t%v\n",
				pattern,
				rec.ConfiguredDropRate,
				rec.RecommendedDropRate,
				rec.Volume,
				rec.IngestedLines,
				rec.QueriedLines,
				rec.Locked,
				rec.Superseded,
			)
		} else {
			fmt.Fprintf(tw, "%s\t%.4f\t%.4f\t%d\t%v\n",
				pattern,
				rec.ConfiguredDropRate,
				rec.RecommendedDropRate,
				rec.Volume,
				rec.Locked,
			)
		}
	}

	return tw.Flush()
}

func (c *patternsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// patterns apply
// ---------------------------------------------------------------------------

type patternsApplyOpts struct {
	All     bool
	Rate    float32
	rateSet bool
	DryRun  bool
}

func (o *patternsApplyOpts) setup(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&o.All, "all", false, "Apply to all patterns")
	cmd.Flags().Float32Var(&o.Rate, "rate", 0, "Drop rate to apply (0.0–1.0); defaults to recommended_drop_rate if not set")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "Preview changes without making them")
}

func (h *logsHelper) patternsApplyCommand() *cobra.Command {
	opts := &patternsApplyOpts{}
	cmd := &cobra.Command{
		Use:   "apply [SUBSTRING]",
		Short: "Apply drop rate recommendations to adaptive log patterns.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hasSubstring := len(args) == 1
			if !hasSubstring && !opts.All {
				return errors.New("provide a pattern substring argument or use --all to match all patterns")
			}
			if hasSubstring && opts.All {
				return errors.New("--all and a substring argument are mutually exclusive")
			}

			opts.rateSet = cmd.Flags().Changed("rate")

			ctx := cmd.Context()

			signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "logs")
			if err != nil {
				return err
			}
			client := NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)

			recs, err := client.ListRecommendations(ctx)
			if err != nil {
				return fmt.Errorf("failed to list recommendations: %w", err)
			}

			var substring string
			if hasSubstring {
				substring = args[0]
			}

			// Identify matched patterns.
			matched := make([]int, 0)
			for i, rec := range recs {
				if opts.All || strings.Contains(strings.ToLower(rec.Pattern), strings.ToLower(substring)) {
					matched = append(matched, i)
				}
			}

			if len(matched) == 0 {
				cmdio.Info(cmd.OutOrStdout(), "No patterns matched.")
				return nil
			}

			if opts.DryRun {
				cmdio.Info(cmd.OutOrStdout(), "Dry-run: would apply to %d pattern(s):", len(matched))
				for _, i := range matched {
					rec := recs[i]
					newRate := float32(rec.RecommendedDropRate)
					if opts.rateSet {
						newRate = opts.Rate
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %.4f → %.4f\n", rec.Pattern, rec.ConfiguredDropRate, newRate)
				}
				return nil
			}

			// Apply the rate changes.
			for _, i := range matched {
				if opts.rateSet {
					recs[i].ConfiguredDropRate = opts.Rate
				} else {
					recs[i].ConfiguredDropRate = float32(recs[i].RecommendedDropRate)
				}
			}

			if err := client.ApplyRecommendations(ctx, recs); err != nil {
				return fmt.Errorf("failed to apply recommendations: %w", err)
			}

			cmdio.Success(cmd.OutOrStdout(), "Applied drop rate to %d pattern(s).", len(matched))
			return nil
		},
	}
	opts.setup(cmd)
	return cmd
}
