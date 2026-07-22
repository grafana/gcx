package experiments

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the experiments command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "experiments",
		Short: "Manage eval experiment runs.",
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newCancelCommand(loader),
		newScoresCommand(loader),
		newReportCommand(loader),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of experiments to return (0 for no limit)")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List experiments.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.List(cmd.Context(), int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <run-id>",
		Short: "Get a single experiment by run ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			exp, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), exp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- create ---

type createOpts struct {
	IO   cmdio.Options
	File string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the experiment create payload (use - for stdin)")
}

func (o *createOpts) Validate() error {
	if strings.TrimSpace(o.File) == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

// readExperimentFile reads an Experiment from a JSON or YAML file. The
// format is picked from the file extension when known (.json, .yaml, .yml)
// so that a typo in a JSON file surfaces a JSON error rather than a
// confusing YAML one. For stdin or unknown extensions, JSON is tried first
// and YAML is used as a fallback.
func readExperimentFile(path string, stdin io.Reader) (*Experiment, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var exp Experiment
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if err := json.Unmarshal(data, &exp); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &exp); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
	default:
		jsonErr := json.Unmarshal(data, &exp)
		if jsonErr != nil {
			var yamlExp Experiment
			if yamlErr := yaml.Unmarshal(data, &yamlExp); yamlErr != nil {
				return nil, fmt.Errorf("parsing %s as JSON or YAML: %w", path, errors.Join(jsonErr, yamlErr))
			}
			exp = yamlExp
		}
	}
	if strings.TrimSpace(exp.Name) == "" {
		return nil, fmt.Errorf("parsing %s: name is required", path)
	}
	return &exp, nil
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new experiment from a JSON or YAML file.",
		Example: `  # Create from a YAML file.
  gcx agento11y experiments create -f experiment.yaml

  # Create from stdin.
  cat experiment.json | gcx agento11y experiments create -f -`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			exp, err := readExperimentFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			created, err := client.Create(cmd.Context(), exp)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Experiment %s created", created.RunID)
			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- update ---

type updateOpts struct {
	IO          cmdio.Options
	Name        string
	Description string
	Tags        []string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "New experiment name")
	flags.StringVar(&o.Description, "description", "", "New experiment description; pass an empty string to clear")
	flags.StringSliceVar(&o.Tags, "tag", nil, "Experiment tag (repeatable or comma-separated; replaces all tags)")
}

// newUpdateCommand sends a true partial PATCH using pointer fields gated by
// cmd.Flags().Changed(...). Only fields the user explicitly sets are sent on the
// wire. Tags replace the full tag set when --tag is present; pass --tag "" to
// clear tags. Status and error are intentionally not exposed — they are
// server-managed lifecycle fields; use `cancel` for the one user-driven transition.
func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <run-id>",
		Short: "Patch an experiment's mutable fields.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			req := &UpdateRequest{}
			if cmd.Flags().Changed("name") {
				name := opts.Name
				req.Name = &name
			}
			if cmd.Flags().Changed("description") {
				description := opts.Description
				req.Description = &description
			}
			if cmd.Flags().Changed("tag") {
				tags := opts.Tags
				req.Tags = &tags
			}
			if req.Name == nil && req.Description == nil && req.Tags == nil {
				return errors.New("--name, --description, or --tag is required")
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			updated, err := client.Update(cmd.Context(), args[0], req)
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Experiment %s updated", updated.RunID)
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	cmd.InitDefaultHelpFlag()
	flags := cmd.Flags()
	flags.SortFlags = false
	opts.setup(flags)
	return cmd
}

// --- cancel ---

type cancelOpts struct {
	Force bool
}

func (o *cancelOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}

func newCancelCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &cancelOpts{}
	cmd := &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel a running experiment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Cancel experiment %s?", args[0]))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			if err := client.Cancel(cmd.Context(), args[0]); err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Experiment %s canceled", args[0])
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- scores ---

type scoresOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *scoresOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ScoresTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ScoresTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of scores to return (0 for no limit)")
}

func newScoresCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &scoresOpts{}
	cmd := &cobra.Command{
		Use:   "list-scores <run-id>",
		Short: "List scores produced by an experiment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.ListScores(cmd.Context(), args[0], int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- report ---

type reportOpts struct {
	IO cmdio.Options
}

func (o *reportOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &ReportTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newReportCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &reportOpts{}
	cmd := &cobra.Command{
		Use:   "report <run-id>",
		Short: "Fetch the aggregate report for an experiment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			report, err := client.GetReport(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), report)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table codecs ---

// TableCodec renders []Experiment rows.
type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]Experiment)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Experiment")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("RUN-ID", "NAME", "STATUS", "SOURCE", "COLLECTION-ID", "TAGS", "SCORES", "CREATED", "COMPLETED", "DESCRIPTION", "ERROR")
	} else {
		t = style.NewTable("RUN-ID", "NAME", "STATUS", "SOURCE", "COLLECTION-ID", "TAGS", "SCORES", "CREATED")
	}

	for _, exp := range items {
		scores := strconv.Itoa(exp.ScoreCount)
		collection := exp.CollectionID
		if collection == "" {
			collection = "-"
		}
		status := exp.Status
		if status == "" {
			status = "-"
		}
		source := exp.Source
		if source == "" {
			source = "-"
		}
		tags := formatTags(exp.Tags)
		if c.Wide {
			completed := "-"
			if exp.CompletedAt != nil {
				completed = aio11yhttp.FormatTime(*exp.CompletedAt)
			}
			t.Row(exp.RunID, exp.Name, status, source, collection, tags, scores, aio11yhttp.FormatTime(exp.CreatedAt), completed, aio11yhttp.Truncate(exp.Description, 40), aio11yhttp.Truncate(exp.Error, 40))
		} else {
			t.Row(exp.RunID, exp.Name, status, source, collection, tags, scores, aio11yhttp.FormatTime(exp.CreatedAt))
		}
	}
	return t.Render(w)
}

func formatTags(tags []string) string {
	if len(tags) == 0 {
		return "-"
	}
	return strings.Join(tags, ", ")
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ScoresTableCodec renders []ScoreItem rows.
type ScoresTableCodec struct {
	Wide bool
}

func (c *ScoresTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ScoresTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]ScoreItem)
	if !ok {
		return errors.New("invalid data type for scores table codec: expected []ScoreItem")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("SCORE-ID", "EVALUATOR", "KEY", "VALUE", "PASSED", "GENERATION", "EXPLANATION", "CREATED")
	} else {
		t = style.NewTable("SCORE-ID", "EVALUATOR", "KEY", "VALUE", "PASSED", "GENERATION")
	}

	for _, s := range items {
		passed := "-"
		if s.Passed != nil {
			if *s.Passed {
				passed = "true"
			} else {
				passed = "false"
			}
		}
		value := s.Value.Display()
		key := s.ScoreKey
		if key == "" {
			key = "-"
		}
		gen := s.GenerationID
		if gen == "" {
			gen = "-"
		}
		evaluator := s.EvaluatorID
		if evaluator == "" {
			evaluator = "-"
		}
		if c.Wide {
			t.Row(s.ScoreID, evaluator, key, value, passed, gen, aio11yhttp.Truncate(s.Explanation, 40), aio11yhttp.FormatTime(s.CreatedAt))
		} else {
			t.Row(s.ScoreID, evaluator, key, value, passed, gen)
		}
	}
	return t.Render(w)
}

func (c *ScoresTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ReportTextCodec renders an *ExperimentReport (or ExperimentReport) as a
// human-readable summary with per-breakdown totals.
type ReportTextCodec struct{}

func (c *ReportTextCodec) Format() format.Format {
	return "text"
}

func (c *ReportTextCodec) Encode(w io.Writer, v any) error {
	var r *ExperimentReport
	switch val := v.(type) {
	case *ExperimentReport:
		r = val
	case ExperimentReport:
		r = &val
	default:
		return errors.New("invalid data type for report text codec: expected *ExperimentReport")
	}
	if r == nil {
		return errors.New("invalid data type for report text codec: expected *ExperimentReport")
	}

	const labelFmt = "%-15s %s\n"
	if r.Run.RunID != "" {
		fmt.Fprintf(w, labelFmt, "Run:", r.Run.RunID)
	}
	if r.Run.Name != "" {
		fmt.Fprintf(w, labelFmt, "Name:", r.Run.Name)
	}
	if r.Run.Status != "" {
		fmt.Fprintf(w, labelFmt, "Status:", r.Run.Status)
	}
	s := r.Summary
	fmt.Fprintf(w, labelFmt, "Scores:", strconv.Itoa(s.NScores))
	fmt.Fprintf(w, labelFmt, "Conversations:", strconv.Itoa(s.NConversations))
	fmt.Fprintf(w, labelFmt, "Generations:", strconv.Itoa(s.NGenerations))
	if s.NScores > 0 {
		fmt.Fprintf(w, labelFmt, "Pass rate:", fmt.Sprintf("%.2f%%", s.PassRate*100))
		fmt.Fprintf(w, labelFmt, "Mean score:", fmt.Sprintf("%g", s.MeanScore))
	}
	if s.TotalCostUSD > 0 {
		fmt.Fprintf(w, labelFmt, "Cost:", fmt.Sprintf("$%.4f", s.TotalCostUSD))
	}
	if s.TotalTokens > 0 {
		fmt.Fprintf(w, labelFmt, "Tokens:", strconv.FormatInt(s.TotalTokens, 10))
	}

	breakdowns := reportBreakdownRows(r.Breakdowns)
	if len(breakdowns) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Breakdowns:")
		for _, row := range breakdowns {
			b := row.breakdown
			key := b.Key
			if key == "" {
				key = "-"
			}
			fmt.Fprintf(w, "  %s/%s: count=%d", row.group, key, b.Count)
			if b.Count > 0 {
				fmt.Fprintf(w, " pass_rate=%.2f%% mean_score=%g", b.PassRate*100, b.MeanScore)
			}
			if b.TotalCostUSD > 0 {
				fmt.Fprintf(w, " cost=$%.4f", b.TotalCostUSD)
			}
			if b.TotalTokens > 0 {
				fmt.Fprintf(w, " tokens=%d", b.TotalTokens)
			}
			fmt.Fprintln(w)
		}
	}
	return nil
}

type reportBreakdownRow struct {
	group     string
	breakdown ExperimentReportBreakdown
}

func reportBreakdownRows(b ExperimentReportBreakdowns) []reportBreakdownRow {
	rows := []reportBreakdownRow{}
	add := func(group string, items []ExperimentReportBreakdown) {
		for _, item := range items {
			rows = append(rows, reportBreakdownRow{group: group, breakdown: item})
		}
	}
	add("task", b.ByTask)
	add("category", b.ByCategory)
	add("evaluator", b.ByEvaluator)
	add("score_key", b.ByScoreKey)
	add("evaluator_score_key", b.ByEvaluatorScoreKey)
	return rows
}

func (c *ReportTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}
