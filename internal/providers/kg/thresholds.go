package kg

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newThresholdsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thresholds",
		Short: "Manage Knowledge Graph threshold rules.",
		Long: `Read Asserts threshold rules over the v1 threshold config API.

This targets v1 (/v1/config/threshold-rules) — the version the Asserts app UI and all
user-configured thresholds run on.`,
	}

	getOpts := &thresholdsGetOpts{}
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get the whole threshold config as YAML.",
		Long: `Fetches the entire threshold configuration. The wire shape is identical to
gcx kg prom-rules (a PrometheusRulesDto), so -o json and -o yaml render the same
named rule-group structure.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := getOpts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			rule, err := client.GetThresholds(ctx)
			if err != nil {
				return err
			}
			res, err := RuleToResource(*rule, cfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert threshold config to resource: %w", err)
			}
			// Encode a *pointer*: unstructured.Unstructured implements
			// MarshalJSON on the pointer receiver, so a bare value passed as
			// any falls back to struct-field encoding and leaks the wrapper as
			// a top-level "Object" key.
			obj := res.ToUnstructured()
			return getOpts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	getOpts.setup(getCmd.Flags())

	listOpts := &thresholdsListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List threshold rules for a category (request or resource).",
		Long: `Lists the structured per-category threshold view, split into custom and global
thresholds. Only the request and resource categories exist in v1.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := listOpts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			dto, err := client.GetThresholdsByCategory(ctx, listOpts.Category)
			if err != nil {
				return err
			}
			// Zero results must serialize as [] in machine formats, not null.
			if dto.CustomThresholds == nil {
				dto.CustomThresholds = []Threshold{}
			}
			if dto.GlobalThresholds == nil {
				dto.GlobalThresholds = []Threshold{}
			}
			return encodeThresholds(&listOpts.IO, cmd.OutOrStdout(), dto)
		},
	}
	listOpts.setup(listCmd.Flags())

	cmd.AddCommand(getCmd, listCmd)
	return cmd
}

type thresholdsGetOpts struct {
	IO cmdio.Options
}

func (o *thresholdsGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

type thresholdsListOpts struct {
	IO       cmdio.Options
	Category string
}

func (o *thresholdsListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, thresholdTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Category, "category", "", "Threshold category to list: request or resource (required)")
}

func (o *thresholdsListOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	switch o.Category {
	case "request", "resource":
		return nil
	case "":
		return errors.New("--category is required (one of: request, resource)")
	default:
		return fmt.Errorf("invalid --category %q: must be one of: request, resource", o.Category)
	}
}

// thresholdRow is a flattened view of a single threshold for table rendering.
type thresholdRow struct {
	scope  string // custom | global
	record string
	expr   string
	active bool
	labels map[string]string
}

// flattenThresholds flattens the custom and global threshold lists into rows,
// tagging each with its scope. Rows are ordered custom-first, then global, each
// group ordered by record name for stable output.
func flattenThresholds(dto *ThresholdRulesDto) []thresholdRow {
	rows := make([]thresholdRow, 0, len(dto.CustomThresholds)+len(dto.GlobalThresholds))
	add := func(scope string, ts []Threshold) {
		sorted := make([]Threshold, len(ts))
		copy(sorted, ts)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Record < sorted[j].Record })
		for _, t := range sorted {
			rows = append(rows, thresholdRow{scope: scope, record: t.Record, expr: t.Expr, active: t.Active, labels: t.Labels})
		}
	}
	add("custom", dto.CustomThresholds)
	add("global", dto.GlobalThresholds)
	return rows
}

// encodeThresholds renders the resolved format from the shape that format
// needs: the flattened rows for the tables, and the unflattened DTO for the
// machine formats, whose custom/global split is the documented wire shape.
//
// ADR-002 rejected a row extractor on cmdio.Table for now, so the choice lives
// in the command. This is the second copy of that switch (the first is
// instrumentation's output.EncodeList) — the trigger the ADR named for moving
// it onto the shared type.
func encodeThresholds(opts *cmdio.Options, w io.Writer, dto *ThresholdRulesDto) error {
	codec, err := opts.Codec()
	if err != nil {
		return err
	}

	switch string(codec.Format()) {
	case cmdio.FormatTable, cmdio.FormatWide:
		return codec.Encode(w, flattenThresholds(dto))
	default:
		return opts.Encode(w, dto)
	}
}

// thresholdTable declares the threshold columns for both the narrow and wide
// renderings — LABELS is the only wide-only column (ADR-002).
func thresholdTable() cmdio.Table[thresholdRow] {
	return cmdio.Table[thresholdRow]{
		Columns: []cmdio.Column[thresholdRow]{
			{Header: "SCOPE", Content: func(r thresholdRow) string { return r.scope }},
			{Header: "RECORD", Content: func(r thresholdRow) string { return r.record }},
			{Header: "ACTIVE", Content: func(r thresholdRow) string { return strconv.FormatBool(r.active) }},
			{Header: "EXPR", Content: func(r thresholdRow) string { return compactExpr(r.expr, exprWidth) }},
			{Header: "LABELS", Visible: cmdio.WideOnly, Content: func(r thresholdRow) string {
				return renderLabels(r.labels)
			}},
		},
	}
}

// exprWidth clips the rendered EXPR column. Real global thresholds are
// multi-line PromQL (clamp_max/quantile_over_time blocks running to hundreds of
// characters), which would otherwise break the table layout outright.
const exprWidth = 60

// compactExpr collapses a threshold expression onto one line and clips it to
// width. PromQL whitespace is not significant, so the collapse is lossless for
// display; -o json and -o yaml carry the untouched expression.
func compactExpr(expr string, width int) string {
	oneLine := strings.Join(strings.Fields(expr), " ")
	if width <= 0 || utf8.RuneCountInString(oneLine) <= width {
		return oneLine
	}
	return string([]rune(oneLine)[:width-1]) + "…"
}

// renderLabels renders a label map as a stable, comma-separated key=value string.
func renderLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+labels[k])
	}
	return strings.Join(pairs, ",")
}
