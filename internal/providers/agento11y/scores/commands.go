package scores

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// scoreListMeta builds finalized §15.4 truncation metadata for a score list
// page: nil when the page is complete, otherwise a ListMeta carrying the
// safety-cap flag (when safetyCap bounded the fetch) and an argv-derived
// continuation command. safetyCap is 0 for sources with no client-side cap.
func scoreListMeta(returned, limit int, hasMore bool, safetyCap int) *cmdio.ListMeta {
	return cmdio.AttachListMeta(cmdio.PagedListMeta(returned, limit, hasMore, safetyCap), os.Args)
}

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := agento11yhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// --- list-scores (generation) ---

type listOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of scores to return")
}

// NewListScoresCommand returns the `list-scores` leaf command, mounted under
// `gcx agento11y generations`. Scores are addressed by the parent generation's
// ID, so the command is an operation-subject compound under generations; the
// command lives in this package alongside the scores client and table codec.
func NewListScoresCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list-scores <generation-id>",
		Short: "List evaluation scores for a generation.",
		Long:  `List evaluation scores produced by online rules for a generation.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			scores, err := client.ListByGeneration(cmd.Context(), args[0], opts.Limit)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), scores)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- list-scores (rule) ---

// ruleScoresEnvelope wraps rule score rows in the list envelope so stdout is
// `{"items": [...]}` — never a bare array — and truncation metadata can ride
// in-band in list_meta (output.md §15.2/§15.6). The table/wide codec recognizes
// this shape; json/yaml/agents codecs serialize it verbatim and `--json list`
// discovery follows the items key.
type ruleScoresEnvelope struct {
	Items    []Score         `json:"items" yaml:"items"`
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

type listRuleOpts struct {
	IO          cmdio.Options
	Limit       int
	EvaluatorID string
	Passed      bool
	From        string
	To          string
	AgentNames  []string
	Models      []string
	Providers   []string
	ScoreValues []string
	MinValue    float64
	MaxValue    float64
	SortBy      string
	SortDir     string
}

func (o *listRuleOpts) setup(flags *pflag.FlagSet) {
	// GenMeta only affects the wide layout (AGENT/MODEL columns), so the plain
	// table codec is left at its default.
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true, GenMeta: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	// A client-side safety cap bounds --limit 0, so the standard "0 means all"
	// binder does not apply; disclose the cap in the help instead.
	flags.IntVar(&o.Limit, "limit", ruleScoreDefaultLimit, fmt.Sprintf("Maximum number of scores to return. 0, or a value above %d, returns up to the %d-row safety cap; narrow with filters to see beyond it", scoreHardCap, scoreHardCap))
	flags.StringVar(&o.EvaluatorID, "evaluator-id", "", "Filter by evaluator ID")
	flags.BoolVar(&o.Passed, "passed", false, "Filter by pass/fail (omit for all scores)")
	flags.StringVar(&o.From, "from", "", "Inclusive lower bound on created_at (RFC3339)")
	flags.StringVar(&o.To, "to", "", "Exclusive upper bound on created_at (RFC3339)")
	flags.StringArrayVar(&o.AgentNames, "agent-name", nil, "Filter by exact agent name, case-sensitive (repeat to OR; not comma-split)")
	flags.StringArrayVar(&o.Models, "model", nil, "Filter by exact generation model, case-sensitive (repeat to OR; not comma-split)")
	flags.StringArrayVar(&o.Providers, "provider", nil, "Filter by exact generation provider, case-sensitive (repeat to OR; not comma-split)")
	flags.StringArrayVar(&o.ScoreValues, "score-value", nil, "Filter by exact score value, case-sensitive (repeat to OR; not comma-split, max 20)")
	flags.Float64Var(&o.MinValue, "min-value", 0, "Inclusive lower bound on numeric score value (omit for no lower bound)")
	flags.Float64Var(&o.MaxValue, "max-value", 0, "Inclusive upper bound on numeric score value (omit for no upper bound)")
	flags.StringVar(&o.SortBy, "sort-by", "created_at", "Sort field: created_at or value")
	flags.StringVar(&o.SortDir, "sort-dir", "desc", "Sort direction: asc or desc")
	// Different filter flags combine with AND; repeats of one flag OR together.
}

// Validate checks IO options and the request options. It is the single
// validation site: buildRuleScoreQuery trusts its input, so every filter error
// is reported here with flag names. scoreOpts is passed in (rather than rebuilt)
// so RunE builds once and reuses the same value for validation and the request;
// flags is needed to tell an explicit empty value from an omitted flag.
func (o *listRuleOpts) Validate(flags *pflag.FlagSet, scoreOpts ListScoresOptions) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if err := o.validateNonEmptyFilters(flags); err != nil {
		return err
	}
	return validateScoreOptions(scoreOpts)
}

// validateNonEmptyFilters rejects filters set to an empty value, so an unset
// shell variable (--evaluator-id "") fails loudly instead of silently widening
// the query scope. sort-by/sort-dir are deliberately excluded: they are not
// filters (omitting them applies a default, not "skip"), and validateScoreOptions
// already reports empty with the allowed-values message.
func (o *listRuleOpts) validateNonEmptyFilters(flags *pflag.FlagSet) error {
	scalars := []struct {
		name, val string
	}{
		{"evaluator-id", o.EvaluatorID},
		{"from", o.From},
		{"to", o.To},
	}
	for _, s := range scalars {
		if flags.Changed(s.name) && s.val == "" {
			return fmt.Errorf("--%s was set to an empty value; omit the flag to skip this filter", s.name)
		}
	}
	repeatables := []struct {
		name string
		vals []string
	}{
		{"agent-name", o.AgentNames},
		{"model", o.Models},
		{"provider", o.Providers},
		{"score-value", o.ScoreValues},
	}
	for _, s := range repeatables {
		if slices.Contains(s.vals, "") {
			return fmt.Errorf("--%s contains an empty value; remove the empty entry", s.name)
		}
	}
	return nil
}

// checkFinite rejects NaN/±Inf for an optional numeric bound. nil (omitted) is
// fine.
func checkFinite(flag string, v *float64) error {
	if v == nil {
		return nil
	}
	if math.IsNaN(*v) || math.IsInf(*v, 0) {
		return fmt.Errorf("%s must be a finite number (got %v)", flag, *v)
	}
	return nil
}

// validateScoreOptions checks the score-list filters, reporting errors with CLI
// flag names.
func validateScoreOptions(opts ListScoresOptions) error {
	if opts.Limit < 0 {
		return fmt.Errorf("--limit must be >= 0 (got %d)", opts.Limit)
	}
	if len(opts.ScoreValues) > ruleScoreMaxValues {
		return fmt.Errorf(
			"--score-value supports at most %d entries (got %d); narrow the filter or split the request",
			ruleScoreMaxValues, len(opts.ScoreValues),
		)
	}
	// pflag's float64 flag is strconv.ParseFloat, which accepts NaN/Inf; reject
	// them here so they never reach the wire (NaN comparisons are all false, so
	// the min<=max guard below would pass a NaN through untouched).
	if err := checkFinite("--min-value", opts.MinValue); err != nil {
		return err
	}
	if err := checkFinite("--max-value", opts.MaxValue); err != nil {
		return err
	}
	if opts.MinValue != nil && opts.MaxValue != nil && *opts.MinValue > *opts.MaxValue {
		return fmt.Errorf("--min-value (%v) must be <= --max-value (%v)", *opts.MinValue, *opts.MaxValue)
	}
	if err := validateScoreTimeBounds(opts.From, opts.To); err != nil {
		return err
	}
	switch opts.SortBy {
	case "created_at", "value":
	default:
		return fmt.Errorf("--sort-by must be \"created_at\" or \"value\" (got %q)", opts.SortBy)
	}
	switch opts.SortDir {
	case "asc", "desc":
	default:
		return fmt.Errorf("--sort-dir must be \"asc\" or \"desc\" (got %q)", opts.SortDir)
	}
	return nil
}

// validateScoreTimeBounds checks optional RFC3339 time bounds. Either bound may
// be omitted; when both are set, from must be before to. Each string is parsed
// once.
func validateScoreTimeBounds(from, to string) error {
	var fromT, toT time.Time
	if from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return fmt.Errorf("invalid --from value: %w", err)
		}
		fromT = t
	}
	if to != "" {
		t, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return fmt.Errorf("invalid --to value: %w", err)
		}
		toT = t
	}
	if from != "" && to != "" && !fromT.Before(toT) {
		return errors.New("--from must be before --to")
	}
	return nil
}

func (o *listRuleOpts) toOptions(flags *pflag.FlagSet) ListScoresOptions {
	opts := ListScoresOptions{
		Limit:       o.Limit,
		EvaluatorID: o.EvaluatorID,
		From:        o.From,
		To:          o.To,
		AgentNames:  o.AgentNames,
		Models:      o.Models,
		Providers:   o.Providers,
		ScoreValues: o.ScoreValues,
		SortBy:      o.SortBy,
		SortDir:     o.SortDir,
	}
	if flags.Changed("passed") {
		passed := o.Passed
		opts.Passed = &passed
	}
	if flags.Changed("min-value") {
		minVal := o.MinValue
		opts.MinValue = &minVal
	}
	if flags.Changed("max-value") {
		maxVal := o.MaxValue
		opts.MaxValue = &maxVal
	}
	return opts
}

// NewListRuleScoresCommand returns the `list-scores` leaf command mounted under
// `gcx agento11y rules`. Scores are addressed by the parent rule's ID.
func NewListRuleScoresCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listRuleOpts{}
	cmd := &cobra.Command{
		Use:   "list-scores <rule-id>",
		Short: "List online evaluation scores for a rule.",
		Long: `List online evaluation score rows for a rule.

Each row may include an LLM-judge explanation in the payload. The default table
shows score key, value, pass/fail, evaluator, and timestamp only; use -o wide
(truncated explanation column) or -o json for full explanation text.

Use --passed=false to focus on failing scores for failure theme analysis.
Filter by evaluator, time range, agent, model, or provider as needed.`,
		Example: `  # Recent scores (summary table; no explanations).
  gcx agento11y rules list-scores <rule-id>

  # Failing scores with explanations.
  gcx agento11y rules list-scores <rule-id> --passed=false -o json

  # Wide table with truncated explanation column.
  gcx agento11y rules list-scores <rule-id> --passed=false -o wide

  # Scoped to one evaluator and time window.
  gcx agento11y rules list-scores <rule-id> --evaluator-id <id> --from 2026-04-01T00:00:00Z --to 2026-04-02T00:00:00Z -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleID := strings.TrimSpace(args[0])
			if ruleID == "" {
				return errors.New("rule ID must not be empty")
			}
			scoreOpts := opts.toOptions(cmd.Flags())
			if err := opts.Validate(cmd.Flags(), scoreOpts); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			scores, hasMore, err := client.ListByRule(cmd.Context(), ruleID, scoreOpts)
			if err != nil {
				return err
			}
			// New command: use the list envelope so truncation rides in-band in
			// list_meta, and report the client-side safety cap (ScoreHardCap).
			meta := scoreListMeta(len(scores), opts.Limit, hasMore, ScoreHardCap())
			if err := opts.IO.Encode(cmd.OutOrStdout(), ruleScoresEnvelope{Items: scores, ListMeta: meta}); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table codec ---

// TableCodec renders scores as a text table. Wide adds the VERSION/RULE/
// EXPLANATION columns. GenMeta adds AGENT/MODEL columns, but only under Wide
// (the plain table ignores it); it is set only for the rule endpoint, which
// populates those fields — the generation endpoint leaves them empty.
type TableCodec struct {
	Wide    bool
	GenMeta bool
}

func (c *TableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TableCodec) Encode(w io.Writer, v any) error {
	// Accept both the bare slice (generation list-scores) and the rule
	// list-scores envelope, so the codec renders the rows either way.
	var items []Score
	switch t := v.(type) {
	case []Score:
		items = t
	case ruleScoresEnvelope:
		items = t.Items
	default:
		return errors.New("invalid data type for table codec: expected []Score or ruleScoresEnvelope")
	}

	var t *style.TableBuilder
	switch {
	case c.Wide && c.GenMeta:
		t = style.NewTable("SCORE KEY", "TYPE", "VALUE", "PASSED", "EVALUATOR", "VERSION", "RULE", "AGENT", "MODEL", "EXPLANATION", "CREATED AT")
	case c.Wide:
		t = style.NewTable("SCORE KEY", "TYPE", "VALUE", "PASSED", "EVALUATOR", "VERSION", "RULE", "EXPLANATION", "CREATED AT")
	default:
		t = style.NewTable("SCORE KEY", "VALUE", "PASSED", "EVALUATOR", "CREATED AT")
	}

	for _, s := range items {
		passed := scorePassedDisplay(s.Passed)
		switch {
		case c.Wide && c.GenMeta:
			t.Row(s.ScoreKey, s.ScoreType, s.Value.Display(), passed,
				s.EvaluatorID, s.EvaluatorVersion, dashOrEmpty(s.RuleID), dashOrEmpty(s.AgentName), dashOrEmpty(s.GenModel),
				agento11yhttp.Truncate(s.Explanation, 80), agento11yhttp.FormatTime(s.CreatedAt))
		case c.Wide:
			t.Row(s.ScoreKey, s.ScoreType, s.Value.Display(), passed,
				s.EvaluatorID, s.EvaluatorVersion, dashOrEmpty(s.RuleID),
				agento11yhttp.Truncate(s.Explanation, 80), agento11yhttp.FormatTime(s.CreatedAt))
		default:
			t.Row(s.ScoreKey, s.Value.Display(), passed,
				s.EvaluatorID, agento11yhttp.FormatTime(s.CreatedAt))
		}
	}
	return t.Render(w)
}

// dashOrEmpty renders an empty cell value as "-".
func dashOrEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// scorePassedDisplay renders the tri-state pass/fail as yes/no/-.
func scorePassedDisplay(passed *bool) string {
	switch {
	case passed == nil:
		return "-"
	case *passed:
		return "yes"
	default:
		return "no"
	}
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
