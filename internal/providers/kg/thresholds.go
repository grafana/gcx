package kg

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
		Short: "Get the whole threshold config.",
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
			return encodeThresholdRowsOrValue(
				&getOpts.IO,
				cmd.OutOrStdout(),
				[]unstructured.Unstructured{obj},
				&obj,
			)
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
			return encodeThresholdRowsOrValue(&listOpts.IO, cmd.OutOrStdout(), flattenThresholds(dto), dto)
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
	o.IO.RegisterCustomCodec(cmdio.FormatTable, &RuleTableCodec{})
	o.IO.RegisterCustomCodec(cmdio.FormatWide, &RuleWideTableCodec{})
	o.IO.DefaultFormat(cmdio.FormatTable)
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

// encodeThresholdRowsOrValue keeps the threshold command's two output shapes
// local to KG: table formats consume flattened rows, while machine formats
// preserve the backend DTO or resource envelope.
func encodeThresholdRowsOrValue[T any](opts *cmdio.Options, w io.Writer, rows []T, value any) error {
	codec, err := opts.Codec()
	if err != nil {
		return err
	}

	switch string(codec.Format()) {
	case cmdio.FormatTable, cmdio.FormatWide, cmdio.FormatText:
		return codec.Encode(w, rows)
	default:
		return opts.Encode(w, value)
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
			{Header: "EXPR", Content: func(r thresholdRow) string { return compactExpr(r.expr) }},
			{Header: "LABELS", Visible: cmdio.WideOnly, Content: func(r thresholdRow) string {
				return renderLabels(r.labels)
			}},
		},
	}
}

// compactExpr folds whitespace outside quoted strings without truncating the
// expression. This keeps one threshold on one table row while preserving
// whitespace that is meaningful inside PromQL string literals.
func compactExpr(expr string) string {
	var b strings.Builder
	b.Grow(len(expr))

	var quote rune
	escaped := false
	pendingSpace := false
	for _, r := range expr {
		if quote != 0 {
			switch r {
			case '\n':
				b.WriteString(`\n`)
				continue
			case '\r':
				b.WriteString(`\r`)
				continue
			case '\t':
				b.WriteString(`\t`)
				continue
			}
			b.WriteRune(r)
			switch {
			case escaped:
				escaped = false
			case r == '\\' && quote != '`':
				escaped = true
			case r == quote:
				quote = 0
			}
			continue
		}

		if unicode.IsSpace(r) {
			pendingSpace = b.Len() > 0
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		if r == '"' || r == '\'' || r == '`' {
			quote = r
		}
		b.WriteRune(r)
	}
	return b.String()
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
		value := labels[k]
		if strings.ContainsAny(value, ",=\r\n\t") {
			value = strconv.Quote(value)
		}
		pairs = append(pairs, k+"="+value)
	}
	return strings.Join(pairs, ",")
}
