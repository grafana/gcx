package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/adaptive/auth"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

// Commands returns the logs command group for adaptive logs management.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Manage Adaptive Logs resources.",
	}
	h := &logsHelper{loader: loader}
	cmd.AddCommand(
		h.patternsCommand(),
		h.exemptionsCommand(),
		h.segmentsCommand(),
	)
	return cmd
}

type logsHelper struct {
	loader *providers.ConfigLoader
}

func (h *logsHelper) newClient(ctx context.Context) (*Client, error) {
	signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "logs")
	if err != nil {
		return nil, err
	}
	return NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient), nil
}

func (h *logsHelper) patternsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "patterns",
		Short: "Manage adaptive log patterns.",
	}
	cmd.AddCommand(
		h.patternsShowCommand(),
		h.patternsStatsCommand(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// patterns show
// ---------------------------------------------------------------------------

type patternsShowOpts struct {
	IO        cmdio.Options
	SegmentID string
	TopN      int
}

func (o *patternsShowOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.SegmentID, "segment", "", "Only include patterns for this segment (SEGMENT ID column from patterns stats, or API map key / selector)")
	cmd.Flags().IntVar(&o.TopN, "top", 10, "Table only: show top N patterns by volume; 0 shows all rows with no rollup")
	o.IO.RegisterCustomCodec("table", &patternsTableCodec{wide: false, opts: o})
	o.IO.RegisterCustomCodec("wide", &patternsTableCodec{wide: true, opts: o})
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

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			recs, err := client.ListRecommendations(ctx)
			if err != nil {
				return err
			}

			if opts.SegmentID != "" {
				segments, err := client.ListSegments(ctx)
				if err != nil {
					return err
				}
				recs = filterPatternsBySegment(recs, opts.SegmentID, segments)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), recs)
		},
	}
	opts.setup(cmd)
	return cmd
}

// ---------------------------------------------------------------------------
// patterns stats
// ---------------------------------------------------------------------------

type patternsStatsOpts struct {
	IO cmdio.Options
}

func (o *patternsStatsOpts) setup(cmd *cobra.Command) {
	o.IO.RegisterCustomCodec("table", &segmentStatsTableCodec{wide: false, opts: o})
	o.IO.RegisterCustomCodec("wide", &segmentStatsTableCodec{wide: true, opts: o})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(cmd.Flags())
}

func (h *logsHelper) patternsStatsCommand() *cobra.Command {
	opts := &patternsStatsOpts{}
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Summarize pattern volume aggregated by segment.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			var recs []LogRecommendation
			var segments []LogSegment

			g, gctx := errgroup.WithContext(ctx)
			g.Go(func() error {
				var err error
				recs, err = client.ListRecommendations(gctx)
				return err
			})
			g.Go(func() error {
				var err error
				segments, err = client.ListSegments(gctx)
				return err
			})
			if err := g.Wait(); err != nil {
				return err
			}

			stats := AggregateSegmentVolumes(recs, segments)
			return opts.IO.Encode(cmd.OutOrStdout(), stats)
		},
	}
	opts.setup(cmd)
	return cmd
}

type segmentStatsTableCodec struct {
	wide bool
	opts *patternsStatsOpts
}

func (c *segmentStatsTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}

func (c *segmentStatsTableCodec) Encode(w io.Writer, v any) error {
	stats, ok := v.([]SegmentPatternStat)
	if !ok {
		return errors.New("invalid data type for table codec: expected []SegmentPatternStat")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SEGMENT ID\tSEGMENT\tNAME\tVOLUME")
	noTruncate := c.opts != nil && c.opts.IO.NoTruncate
	for _, s := range stats {
		idCol := s.SegmentID
		if idCol == "" {
			idCol = "-"
		}
		keyCol := s.ID
		if !noTruncate {
			if c.wide {
				keyCol = truncate(keyCol, 120)
			} else {
				keyCol = truncate(keyCol, 80)
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", idCol, keyCol, s.Name, humanBytes(s.Volume))
	}
	return tw.Flush()
}

func (c *segmentStatsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// patternsTableCodec renders LogRecommendations as a tabular table.
type patternsTableCodec struct {
	wide bool
	opts *patternsShowOpts
}

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

	sorted := append([]LogRecommendation(nil), recs...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Volume > sorted[j].Volume
	})

	topN := 0
	if c.opts != nil {
		topN = c.opts.TopN
	}

	var head, tail []LogRecommendation
	if topN <= 0 || len(sorted) <= topN {
		head = sorted
	} else {
		head = sorted[:topN]
		tail = sorted[topN:]
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.wide {
		fmt.Fprintln(tw, "PATTERN\tDROP RATE\tRECOMMENDED RATE\tVOLUME\tINGESTED LINES\tQUERIED LINES\tLOCKED\tSUPERSEDED")
	} else {
		fmt.Fprintln(tw, "PATTERN\tDROP RATE\tRECOMMENDED RATE\tVOLUME\tLOCKED")
	}

	for _, rec := range head {
		c.writePatternRow(tw, rec)
	}

	if len(tail) > 0 {
		var vol, ing, q uint64
		for _, rec := range tail {
			vol += rec.Volume
			ing += rec.IngestedLines
			q += rec.QueriedLines
		}
		pattern := "Everything else"
		if !c.wide {
			pattern = truncate(pattern, 80)
		}
		if c.wide {
			fmt.Fprintf(tw, "%s\t-\t-\t%s\t%d\t%d\t-\t-\n",
				pattern, humanBytes(vol), ing, q)
		} else {
			fmt.Fprintf(tw, "%s\t-\t-\t%s\t-\n",
				pattern, humanBytes(vol))
		}
	}

	return tw.Flush()
}

func (c *patternsTableCodec) writePatternRow(tw *tabwriter.Writer, rec LogRecommendation) {
	pattern := rec.Pattern
	if pattern == "" {
		pattern = rec.Label()
	}
	if !c.wide {
		pattern = truncate(pattern, 80)
	}
	if c.wide {
		fmt.Fprintf(tw, "%s\t%.2f\t%.2f\t%s\t%d\t%d\t%v\t%v\n",
			pattern,
			rec.ConfiguredDropRate,
			rec.RecommendedDropRate,
			humanBytes(rec.Volume),
			rec.IngestedLines,
			rec.QueriedLines,
			rec.Locked,
			rec.Superseded,
		)
	} else {
		fmt.Fprintf(tw, "%s\t%.2f\t%.2f\t%s\t%v\n",
			pattern,
			rec.ConfiguredDropRate,
			rec.RecommendedDropRate,
			humanBytes(rec.Volume),
			rec.Locked,
		)
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit-3] + "..."
}

const (
	kb float64 = 1 << 10
	mb float64 = 1 << 20
	gb float64 = 1 << 30
	tb float64 = 1 << 40
	pb float64 = 1 << 50
)

func humanBytes(b uint64) string {
	v := float64(b)
	switch {
	case v >= pb:
		return fmt.Sprintf("%.2f PB", v/pb)
	case v >= tb:
		return fmt.Sprintf("%.2f TB", v/tb)
	case v >= gb:
		return fmt.Sprintf("%.2f GB", v/gb)
	case v >= mb:
		return fmt.Sprintf("%.2f MB", v/mb)
	case v >= kb:
		return fmt.Sprintf("%.2f KB", v/kb)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func (c *patternsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// exemptions
// ---------------------------------------------------------------------------

func (h *logsHelper) exemptionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exemptions",
		Short: "Manage adaptive log exemptions.",
	}
	cmd.AddCommand(
		h.exemptionsListCommand(),
		h.exemptionsCreateCommand(),
		h.exemptionsUpdateCommand(),
		h.exemptionsDeleteCommand(),
	)
	return cmd
}

// exemptions list

type exemptionsListOpts struct {
	IO cmdio.Options
}

func (o *exemptionsListOpts) setup(cmd *cobra.Command) {
	o.IO.RegisterCustomCodec("table", &exemptionsTableCodec{wide: false})
	o.IO.RegisterCustomCodec("wide", &exemptionsTableCodec{wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(cmd.Flags())
}

func (h *logsHelper) exemptionsListCommand() *cobra.Command {
	opts := &exemptionsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List adaptive log exemptions.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			exemptions, err := client.ListExemptions(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), exemptions)
		},
	}
	opts.setup(cmd)
	return cmd
}

type exemptionsTableCodec struct{ wide bool }

func (c *exemptionsTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}

func (c *exemptionsTableCodec) Encode(w io.Writer, v any) error {
	exemptions, ok := v.([]Exemption)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Exemption")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.wide {
		fmt.Fprintln(tw, "ID\tSTREAM SELECTOR\tREASON\tCREATED AT\tMANAGED BY\tEXPIRES AT\tACTIVE INTERVAL\tCREATED BY\tUPDATED AT")
	} else {
		fmt.Fprintln(tw, "ID\tSTREAM SELECTOR\tREASON\tCREATED AT\tMANAGED BY")
	}

	for _, e := range exemptions {
		if c.wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				e.ID, e.StreamSelector, e.Reason, e.CreatedAt, e.ManagedBy,
				e.ExpiresAt, e.ActiveInterval, e.CreatedBy, e.UpdatedAt)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				e.ID, e.StreamSelector, e.Reason, e.CreatedAt, e.ManagedBy)
		}
	}

	return tw.Flush()
}

func (c *exemptionsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// exemptions create

type exemptionsCreateOpts struct {
	StreamSelector string
	Reason         string
	IO             cmdio.Options
}

func (o *exemptionsCreateOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.StreamSelector, "stream-selector", "", "Log stream selector (required)")
	cmd.Flags().StringVar(&o.Reason, "reason", "", "Reason for the exemption")
	_ = cmd.MarkFlagRequired("stream-selector")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(cmd.Flags())
}

func (h *logsHelper) exemptionsCreateCommand() *cobra.Command {
	opts := &exemptionsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an adaptive log exemption.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			created, err := client.CreateExemption(ctx, &Exemption{
				StreamSelector: opts.StreamSelector,
				Reason:         opts.Reason,
			})
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd)
	return cmd
}

// exemptions update

type exemptionsUpdateOpts struct {
	StreamSelector string
	Reason         string
	IO             cmdio.Options
}

func (o *exemptionsUpdateOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.StreamSelector, "stream-selector", "", "Log stream selector")
	cmd.Flags().StringVar(&o.Reason, "reason", "", "Reason for the exemption")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(cmd.Flags())
}

func (h *logsHelper) exemptionsUpdateCommand() *cobra.Command {
	opts := &exemptionsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update an adaptive log exemption.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			changedStream := cmd.Flags().Changed("stream-selector")
			changedReason := cmd.Flags().Changed("reason")
			if !changedStream && !changedReason {
				return errors.New("specify at least one of --stream-selector or --reason")
			}

			patch := &Exemption{}
			if changedStream {
				patch.StreamSelector = opts.StreamSelector
			}
			if changedReason {
				patch.Reason = opts.Reason
			}

			updated, err := client.UpdateExemption(ctx, args[0], patch)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd)
	return cmd
}

// exemptions delete

func (h *logsHelper) exemptionsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete an adaptive log exemption.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			if err := client.DeleteExemption(ctx, args[0]); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Deleted exemption %q", args[0])
			return nil
		},
	}
	return cmd
}

// ---------------------------------------------------------------------------
// segments
// ---------------------------------------------------------------------------

func (h *logsHelper) segmentsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "segments",
		Short: "Manage adaptive log segments.",
	}
	cmd.AddCommand(
		h.segmentsListCommand(),
		h.segmentsCreateCommand(),
		h.segmentsUpdateCommand(),
		h.segmentsDeleteCommand(),
	)
	return cmd
}

// segments list

type segmentsListOpts struct {
	IO cmdio.Options
}

func (o *segmentsListOpts) setup(cmd *cobra.Command) {
	o.IO.RegisterCustomCodec("table", &segmentsTableCodec{wide: false})
	o.IO.RegisterCustomCodec("wide", &segmentsTableCodec{wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(cmd.Flags())
}

func (h *logsHelper) segmentsListCommand() *cobra.Command {
	opts := &segmentsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List adaptive log segments.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			segments, err := client.ListSegments(ctx)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), segments)
		},
	}
	opts.setup(cmd)
	return cmd
}

type segmentsTableCodec struct{ wide bool }

func (c *segmentsTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}

func (c *segmentsTableCodec) Encode(w io.Writer, v any) error {
	segments, ok := v.([]LogSegment)
	if !ok {
		return errors.New("invalid data type for table codec: expected []LogSegment")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.wide {
		fmt.Fprintln(tw, "ID\tNAME\tSELECTOR\tFALLBACK\tIS EARLY\tCREATED AT\tUPDATED AT")
	} else {
		fmt.Fprintln(tw, "ID\tNAME\tSELECTOR\tFALLBACK")
	}

	for _, s := range segments {
		if c.wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%v\t%s\t%s\n",
				s.ID, s.Name, s.Selector, s.FallbackToDefault, s.IsEarly, s.CreatedAt, s.UpdatedAt)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n",
				s.ID, s.Name, s.Selector, s.FallbackToDefault)
		}
	}

	return tw.Flush()
}

func (c *segmentsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// segments create

type segmentsCreateOpts struct {
	Name              string
	Selector          string
	FallbackToDefault bool
	IO                cmdio.Options
}

func (o *segmentsCreateOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Name, "name", "", "Segment name (required)")
	cmd.Flags().StringVar(&o.Selector, "selector", "", "Log stream selector (required)")
	cmd.Flags().BoolVar(&o.FallbackToDefault, "fallback-to-default", false, "Fall back to default segment")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("selector")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(cmd.Flags())
}

func (h *logsHelper) segmentsCreateCommand() *cobra.Command {
	opts := &segmentsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an adaptive log segment.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			created, err := client.CreateSegment(ctx, &LogSegment{
				Name:              opts.Name,
				Selector:          opts.Selector,
				FallbackToDefault: opts.FallbackToDefault,
			})
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd)
	return cmd
}

// segments update

type segmentsUpdateOpts struct {
	Name              string
	Selector          string
	FallbackToDefault bool
	IO                cmdio.Options
}

func (o *segmentsUpdateOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Name, "name", "", "Segment name")
	cmd.Flags().StringVar(&o.Selector, "selector", "", "Log stream selector")
	cmd.Flags().BoolVar(&o.FallbackToDefault, "fallback-to-default", false, "Fall back to default segment")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(cmd.Flags())
}

func (h *logsHelper) segmentsUpdateCommand() *cobra.Command {
	opts := &segmentsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update an adaptive log segment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			existing, err := client.GetSegment(ctx, args[0])
			if err != nil {
				return fmt.Errorf("failed to fetch existing segment for merge: %w", err)
			}

			if cmd.Flags().Changed("name") {
				existing.Name = opts.Name
			}
			if cmd.Flags().Changed("selector") {
				existing.Selector = opts.Selector
			}
			if cmd.Flags().Changed("fallback-to-default") {
				existing.FallbackToDefault = opts.FallbackToDefault
			}

			updated, err := client.UpdateSegment(ctx, args[0], existing)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd)
	return cmd
}

// segments delete

func (h *logsHelper) segmentsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete an adaptive log segment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			if err := client.DeleteSegment(ctx, args[0]); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Deleted segment %q", args[0])
			return nil
		},
	}
	return cmd
}
