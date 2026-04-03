package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	auth "github.com/grafana/gcx/internal/auth/adaptive"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
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
		h.dropRulesCommand(),
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
	cmd.Flags().StringVar(&o.SegmentID, "segment", "", "Only include patterns for this segment (ID column from patterns stats, or API map key / selector)")
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

	volW := segmentVolumeColumnWidth(stats)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\tNAME\tSEGMENT\t%s\n", rightAlign("VOLUME", volW))
	noTruncate := c.opts != nil && c.opts.IO.NoTruncate
	for _, s := range stats {
		idCol := s.SegmentID
		if idCol == "" {
			idCol = "-"
		}
		segCol := s.ID
		if segCol == defaultSegmentStatsKey && s.Name == "Default" {
			segCol = "—"
		}
		if !noTruncate {
			if c.wide {
				segCol = truncate(segCol, 120)
			} else {
				segCol = truncate(segCol, 80)
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", idCol, s.Name, segCol, rightAlign(humanBytes(s.Volume), volW))
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

	cw := computePatternColWidths(c.wide, head, tail)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.wide {
		fmt.Fprintf(tw, "PATTERN\tQUERIED\t%s\t%s\t%s\t%s\t%s\t%s\n",
			rightAlign("VOLUME", cw.volume),
			rightAlign("DROP RATE", cw.dropRate),
			rightAlign("RECOMMENDED RATE", cw.recRate),
			rightAlign("INGESTED LINES", cw.ingested),
			rightAlign("QUERIED LINES", cw.queried),
			rightAlign("SUPERSEDED", cw.superseded),
		)
	} else {
		fmt.Fprintf(tw, "PATTERN\tQUERIED\t%s\t%s\t%s\n",
			rightAlign("VOLUME", cw.volume),
			rightAlign("DROP RATE", cw.dropRate),
			rightAlign("RECOMMENDED RATE", cw.recRate),
		)
	}

	var anyRecRateMark bool
	for _, rec := range head {
		if c.writePatternRow(tw, rec, cw) {
			anyRecRateMark = true
		}
	}

	if len(tail) > 0 {
		var vol, ing, q uint64
		for _, rec := range tail {
			vol += rec.Volume
			ing += rec.IngestedLines
			q += rec.QueriedLines
		}
		n := len(tail)
		patLabel := "patterns"
		if n == 1 {
			patLabel = "pattern"
		}
		pattern := fmt.Sprintf("Everything else (%d %s)", n, patLabel)
		if !c.wide {
			pattern = truncate(pattern, 80)
		}
		if c.wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				pattern,
				queryIngestLabel(q, ing),
				rightAlign(humanBytes(vol), cw.volume),
				rightAlign("-", cw.dropRate),
				rightAlign("-", cw.recRate),
				rightAlign(strconv.FormatUint(ing, 10), cw.ingested),
				rightAlign(strconv.FormatUint(q, 10), cw.queried),
				rightAlign("-", cw.superseded),
			)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				pattern,
				queryIngestLabel(q, ing),
				rightAlign(humanBytes(vol), cw.volume),
				rightAlign("-", cw.dropRate),
				rightAlign("-", cw.recRate),
			)
		}
	}

	if err := tw.Flush(); err != nil {
		return err
	}
	if anyRecRateMark {
		fmt.Fprintln(w, "\n* Recommended rate differs from drop rate by more than 10 percentage points.")
	}
	return nil
}

// recommendedRateCell formats the recommended drop rate and marks when it differs from the
// configured drop rate by more than 10 percentage points. The number is right-aligned in a
// fixed inner width; a trailing " *" marks divergence and unmarked rows use "  " in the same
// two-byte suffix slot so columns stay aligned.
func recommendedRateCell(configured float32, recommended float64, innerWidth int) (string, bool) {
	num := rightAlign(fmt.Sprintf("%.2f", recommended), innerWidth)
	if math.Abs(float64(configured)-recommended) > 10 {
		return num + " *", true
	}
	return num + "  ", false
}

// writePatternRow renders one recommendation row. It returns true when the recommended rate
// cell includes a divergence marker.
func (c *patternsTableCodec) writePatternRow(tw *tabwriter.Writer, rec LogRecommendation, cw patternColWidths) bool {
	pattern := rec.Pattern
	if pattern == "" {
		pattern = rec.Label()
	}
	if !c.wide {
		pattern = truncate(pattern, 80)
	}
	recCell, marked := recommendedRateCell(rec.ConfiguredDropRate, rec.RecommendedDropRate, cw.recRate-2)
	queried := queryIngestLabel(rec.QueriedLines, rec.IngestedLines)
	dropStr := fmt.Sprintf("%.2f", rec.ConfiguredDropRate)
	if c.wide {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			pattern,
			queried,
			rightAlign(humanBytes(rec.Volume), cw.volume),
			rightAlign(dropStr, cw.dropRate),
			rightAlign(recCell, cw.recRate),
			rightAlign(strconv.FormatUint(rec.IngestedLines, 10), cw.ingested),
			rightAlign(strconv.FormatUint(rec.QueriedLines, 10), cw.queried),
			rightAlign(strconv.FormatBool(rec.Superseded), cw.superseded),
		)
	} else {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			pattern,
			queried,
			rightAlign(humanBytes(rec.Volume), cw.volume),
			rightAlign(dropStr, cw.dropRate),
			rightAlign(recCell, cw.recRate),
		)
	}
	return marked
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit-3] + "..."
}

// rightAlign left-pads s with spaces so the string has byte length width (for tabular numeric columns).
func rightAlign(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

func segmentVolumeColumnWidth(stats []SegmentPatternStat) int {
	w := len("VOLUME")
	for _, s := range stats {
		if l := len(humanBytes(s.Volume)); l > w {
			w = l
		}
	}
	return w
}

// patternColWidths holds precomputed byte widths for right-aligned numeric columns in patterns show.
type patternColWidths struct {
	volume     int
	dropRate   int
	recRate    int
	ingested   int
	queried    int
	superseded int
}

func computePatternColWidths(wide bool, head, tail []LogRecommendation) patternColWidths {
	var cw patternColWidths

	cw.volume = len("VOLUME")
	for _, rec := range head {
		if l := len(humanBytes(rec.Volume)); l > cw.volume {
			cw.volume = l
		}
	}
	var tailVol, tailIng, tailQ uint64
	for _, rec := range tail {
		tailVol += rec.Volume
		tailIng += rec.IngestedLines
		tailQ += rec.QueriedLines
	}
	if len(tail) > 0 {
		if l := len(humanBytes(tailVol)); l > cw.volume {
			cw.volume = l
		}
	}

	cw.dropRate = len("DROP RATE")
	recNumW := 0
	for _, rec := range head {
		if l := len(fmt.Sprintf("%.2f", rec.ConfiguredDropRate)); l > cw.dropRate {
			cw.dropRate = l
		}
		if l := len(fmt.Sprintf("%.2f", rec.RecommendedDropRate)); l > recNumW {
			recNumW = l
		}
	}
	cw.recRate = len("RECOMMENDED RATE")
	if w := 2 + recNumW; w > cw.recRate {
		cw.recRate = w
	}

	if wide {
		widenPatternColWidths(&cw, head, tail, tailIng, tailQ)
	}
	return cw
}

func widenPatternColWidths(cw *patternColWidths, head, tail []LogRecommendation, tailIng, tailQ uint64) {
	cw.ingested = len("INGESTED LINES")
	cw.queried = len("QUERIED LINES")
	cw.superseded = len("SUPERSEDED")
	for _, rec := range head {
		if l := len(strconv.FormatUint(rec.IngestedLines, 10)); l > cw.ingested {
			cw.ingested = l
		}
		if l := len(strconv.FormatUint(rec.QueriedLines, 10)); l > cw.queried {
			cw.queried = l
		}
		if l := len(strconv.FormatBool(rec.Superseded)); l > cw.superseded {
			cw.superseded = l
		}
	}
	if len(tail) == 0 {
		return
	}
	if l := len(strconv.FormatUint(tailIng, 10)); l > cw.ingested {
		cw.ingested = l
	}
	if l := len(strconv.FormatUint(tailQ, 10)); l > cw.queried {
		cw.queried = l
	}
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
			crud, _, err := NewExemptionTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx)
			if err != nil {
				return err
			}
			exemptions := make([]Exemption, len(typedObjs))
			for i := range typedObjs {
				exemptions[i] = typedObjs[i].Spec
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
			crud, _, err := NewExemptionTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			created, err := crud.Create(ctx, &adapter.TypedObject[Exemption]{
				Spec: Exemption{
					StreamSelector: opts.StreamSelector,
					Reason:         opts.Reason,
				},
			})
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), created.Spec)
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
			crud, _, err := NewExemptionTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			if !cmd.Flags().Changed("stream-selector") && !cmd.Flags().Changed("reason") {
				return errors.New("specify at least one of --stream-selector or --reason")
			}

			existing, err := crud.Get(ctx, args[0])
			if err != nil {
				return fmt.Errorf("failed to fetch existing exemption for merge: %w", err)
			}

			if cmd.Flags().Changed("stream-selector") {
				existing.Spec.StreamSelector = opts.StreamSelector
			}
			if cmd.Flags().Changed("reason") {
				existing.Spec.Reason = opts.Reason
			}

			updated, err := crud.Update(ctx, args[0], existing)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), updated.Spec)
		},
	}
	opts.setup(cmd)
	return cmd
}

// exemptions delete

type exemptionsDeleteOpts struct{}

func (o *exemptionsDeleteOpts) setup(_ *cobra.Command) {}

func (o *exemptionsDeleteOpts) Validate() error { return nil }

func (h *logsHelper) exemptionsDeleteCommand() *cobra.Command {
	opts := &exemptionsDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete an adaptive log exemption.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewExemptionTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			if err := crud.Delete(ctx, args[0]); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Deleted exemption %q", args[0])
			return nil
		},
	}
	opts.setup(cmd)
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
			crud, _, err := NewSegmentTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx)
			if err != nil {
				return err
			}
			segments := make([]LogSegment, len(typedObjs))
			for i := range typedObjs {
				segments[i] = typedObjs[i].Spec
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
			crud, _, err := NewSegmentTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			created, err := crud.Create(ctx, &adapter.TypedObject[LogSegment]{
				Spec: LogSegment{
					Name:              opts.Name,
					Selector:          opts.Selector,
					FallbackToDefault: opts.FallbackToDefault,
				},
			})
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), created.Spec)
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
			crud, _, err := NewSegmentTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("selector") && !cmd.Flags().Changed("fallback-to-default") {
				return errors.New("specify at least one of --name, --selector, or --fallback-to-default")
			}

			existing, err := crud.Get(ctx, args[0])
			if err != nil {
				return fmt.Errorf("failed to fetch existing segment for merge: %w", err)
			}

			if cmd.Flags().Changed("name") {
				existing.Spec.Name = opts.Name
			}
			if cmd.Flags().Changed("selector") {
				existing.Spec.Selector = opts.Selector
			}
			if cmd.Flags().Changed("fallback-to-default") {
				existing.Spec.FallbackToDefault = opts.FallbackToDefault
			}

			updated, err := crud.Update(ctx, args[0], existing)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), updated.Spec)
		},
	}
	opts.setup(cmd)
	return cmd
}

// segments delete

type segmentsDeleteOpts struct{}

func (o *segmentsDeleteOpts) setup(_ *cobra.Command) {}

func (o *segmentsDeleteOpts) Validate() error { return nil }

func (h *logsHelper) segmentsDeleteCommand() *cobra.Command {
	opts := &segmentsDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete an adaptive log segment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewSegmentTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			if err := crud.Delete(ctx, args[0]); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Deleted segment %q", args[0])
			return nil
		},
	}
	opts.setup(cmd)
	return cmd
}

// ---------------------------------------------------------------------------
// drop-rules
// ---------------------------------------------------------------------------

func (h *logsHelper) dropRulesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drop-rules",
		Short: "Manage adaptive log drop rules.",
		Long: "Manage adaptive log drop rules.\n\n" +
			"Listing via `gcx resources get droprules` returns all rules for the tenant (no segment filter). " +
			"`gcx logs adaptive drop-rules` subcommands operate on the " + GlobalDropRuleSegmentID + " segment only.\n\n" +
			"Create and update load a rule definition from a file (`--filename` / `-f`), same pattern as Adaptive Traces policies.",
	}
	cmd.AddCommand(
		h.dropRulesListCommand(),
		h.dropRulesCreateCommand(),
		h.dropRulesUpdateCommand(),
		h.dropRulesDeleteCommand(),
	)
	return cmd
}

// dropRules list

type dropRulesListOpts struct {
	IO               cmdio.Options
	ExpirationFilter string
}

func (o *dropRulesListOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.ExpirationFilter, "expiration-filter", "all",
		"Filter by expiration: all, active, or expired")
	o.IO.RegisterCustomCodec("table", &dropRulesTableCodec{wide: false})
	o.IO.RegisterCustomCodec("wide", &dropRulesTableCodec{wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(cmd.Flags())
}

func (h *logsHelper) dropRulesListCommand() *cobra.Command {
	opts := &dropRulesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List adaptive log drop rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := h.newClient(ctx)
			if err != nil {
				return err
			}

			rules, err := client.ListDropRules(ctx, DropRuleListQuery{
				SegmentID:        GlobalDropRuleSegmentID,
				ExpirationFilter: opts.ExpirationFilter,
			})
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), rules)
		},
	}
	opts.setup(cmd)
	return cmd
}

type dropRulesTableCodec struct{ wide bool }

func (c *dropRulesTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}

func (c *dropRulesTableCodec) Encode(w io.Writer, v any) error {
	rules, ok := v.([]DropRule)
	if !ok {
		return errors.New("invalid data type for table codec: expected []DropRule")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.wide {
		fmt.Fprintln(tw, "ID\tNAME\tSEGMENT\tVERSION\tDISABLED\tEXPIRES AT\tCREATED AT\tUPDATED AT")
	} else {
		fmt.Fprintln(tw, "ID\tNAME\tSEGMENT\tVERSION\tDISABLED\tEXPIRES AT")
	}

	for _, r := range rules {
		exp := r.ExpiresAt
		if exp == "" {
			exp = "-"
		}
		disabled := strconv.FormatBool(r.Disabled)
		if c.wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
				r.ID, r.Name, r.SegmentID, r.Version, disabled, exp, r.CreatedAt, r.UpdatedAt)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n",
				r.ID, r.Name, r.SegmentID, r.Version, disabled, exp)
		}
	}

	return tw.Flush()
}

func (c *dropRulesTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// readDropRuleFromFile reads and decodes a DropRuleFileSpec from a file path or stdin ("-").
func readDropRuleFromFile(filePath string, stdin io.Reader) (*DropRuleFileSpec, error) {
	var reader io.Reader
	if filePath == "-" {
		reader = stdin
	} else {
		f, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("opening file %s: %w", filePath, err)
		}
		defer f.Close()
		reader = f
	}

	return ReadDropRuleFileSpecFromReader(reader)
}

// dropRules create

type dropRulesCreateOpts struct {
	File string
	IO   cmdio.Options
}

func (o *dropRulesCreateOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.File, "filename", "f", "", "File containing the drop rule definition (use - for stdin)")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(cmd.Flags())
}

func (o *dropRulesCreateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func (h *logsHelper) dropRulesCreateCommand() *cobra.Command {
	opts := &dropRulesCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an adaptive log drop rule from a file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			spec, err := readDropRuleFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if err := validateDropRuleFileSpecCreate(spec); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewDropRuleTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			created, err := crud.Create(ctx, &adapter.TypedObject[DropRule]{
				Spec: dropRuleFileSpecToCreate(spec),
			})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Created drop rule %q (id=%s)", created.Spec.Name, created.Spec.ID)
			return opts.IO.Encode(cmd.OutOrStdout(), created.Spec)
		},
	}
	opts.setup(cmd)
	return cmd
}

// dropRules update

type dropRulesUpdateOpts struct {
	File string
	IO   cmdio.Options
}

func (o *dropRulesUpdateOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.File, "filename", "f", "", "File containing the drop rule definition (use - for stdin)")
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(cmd.Flags())
}

func (o *dropRulesUpdateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func (h *logsHelper) dropRulesUpdateCommand() *cobra.Command {
	opts := &dropRulesUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update ID",
		Short: "Update an adaptive log drop rule by ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			spec, err := readDropRuleFromFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if err := validateDropRuleFileSpecUpdate(spec); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewDropRuleTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			updated, err := crud.Update(ctx, args[0], &adapter.TypedObject[DropRule]{
				Spec: dropRuleFileSpecToUpdate(spec),
			})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Updated drop rule %q (id=%s)", updated.Spec.Name, updated.Spec.ID)
			return opts.IO.Encode(cmd.OutOrStdout(), updated.Spec)
		},
	}
	opts.setup(cmd)
	return cmd
}

// dropRules delete

type dropRulesDeleteOpts struct{}

func (o *dropRulesDeleteOpts) setup(_ *cobra.Command) {}

func (o *dropRulesDeleteOpts) Validate() error { return nil }

func (h *logsHelper) dropRulesDeleteCommand() *cobra.Command {
	opts := &dropRulesDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete an adaptive log drop rule.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewDropRuleTypedCRUD(ctx, h.loader)
			if err != nil {
				return err
			}

			if err := crud.Delete(ctx, args[0]); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Deleted drop rule %q", args[0])
			return nil
		},
	}
	opts.setup(cmd)
	return cmd
}
