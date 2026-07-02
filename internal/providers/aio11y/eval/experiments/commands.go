package experiments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(ctx context.Context, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromContext(ctx, loader)
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

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	return crudcmd.NewListCommand(crudcmd.ListConfig[Experiment]{
		Use:          "list",
		Short:        "List experiments.",
		DefaultFmt:   "table",
		LimitDefault: 50,
		LimitUsage:   "Maximum number of experiments to return (0 for no limit)",
		Codecs:       []format.Codec{&TableCodec{}, &TableCodec{Wide: true}},
		Fetch: func(ctx context.Context, limit int64) ([]Experiment, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.List(ctx, int(limit))
		},
	})
}

// --- get ---

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*Experiment]{
		Use:        "get <run-id>",
		Short:      "Get a single experiment by run ID.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "yaml",
		Fetch: func(ctx context.Context, args []string) (*Experiment, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.Get(ctx, args[0])
		},
	})
}

// --- create ---

// readExperimentFile reads an Experiment from a JSON or YAML file. The
// format is picked from the file extension when known (.json, .yaml, .yml)
// so that a typo in a JSON file surfaces a JSON error rather than a
// confusing YAML one. For stdin or unknown extensions, JSON is tried first
// and YAML is used as a fallback.
func readExperimentFile(path string, stdin io.Reader) (*Experiment, error) {
	data, err := crudcmd.ReadFile(path, stdin)
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
	return crudcmd.NewCreateCommand(crudcmd.CreateConfig[Experiment]{
		Use:   "create",
		Short: "Create a new experiment from a JSON or YAML file.",
		Example: `  # Create from a YAML file.
  gcx aio11y experiments create -f experiment.yaml

  # Create from stdin.
  cat experiment.json | gcx aio11y experiments create -f -`,
		DefaultFmt:    "json",
		FilenameUsage: "File containing the experiment create payload (use - for stdin)",
		Read:          readExperimentFile,
		Create: func(ctx context.Context, exp Experiment) (Experiment, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return Experiment{}, err
			}
			created, err := client.Create(ctx, &exp)
			if err != nil {
				return Experiment{}, err
			}
			return *created, nil
		},
		OnSuccess: func(cmd *cobra.Command, created Experiment) {
			cmdio.Success(cmd.ErrOrStderr(), "Experiment %s created", created.RunID)
		},
	})
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

			ctx := cmd.Context()
			client, err := newClient(ctx, loader)
			if err != nil {
				return err
			}
			updated, err := client.Update(ctx, args[0], req)
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

			ctx := cmd.Context()
			client, err := newClient(ctx, loader)
			if err != nil {
				return err
			}
			if err := client.Cancel(ctx, args[0]); err != nil {
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

func newScoresCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &crudcmd.ListOpts{}
	cmd := &cobra.Command{
		Use:   "scores <run-id>",
		Short: "List scores produced by an experiment.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, err := newClient(ctx, loader)
			if err != nil {
				return err
			}
			items, err := client.ListScores(ctx, args[0], int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.Setup(cmd.Flags(), "table", 50, "Maximum number of scores to return (0 for no limit)", &ScoresTableCodec{}, &ScoresTableCodec{Wide: true})
	return cmd
}

// --- report ---

func newReportCommand(loader *providers.ConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*ExperimentReport]{
		Use:        "report <run-id>",
		Short:      "Fetch the aggregate report for an experiment.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "text",
		Codecs:     []format.Codec{&ReportTextCodec{}},
		Fetch: func(ctx context.Context, args []string) (*ExperimentReport, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.GetReport(ctx, args[0])
		},
	})
}

// --- table codecs ---

// TableCodec renders []Experiment rows.
type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *TableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, exp Experiment) {
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
		if !c.Wide {
			t.Row(exp.RunID, exp.Name, status, source, collection, tags, scores, aio11yhttp.FormatTime(exp.CreatedAt))
			return
		}
		completed := "-"
		if exp.CompletedAt != nil {
			completed = aio11yhttp.FormatTime(*exp.CompletedAt)
		}
		t.Row(exp.RunID, exp.Name, status, source, collection, tags, scores, aio11yhttp.FormatTime(exp.CreatedAt), completed, aio11yhttp.Truncate(exp.Description, 40), aio11yhttp.Truncate(exp.Error, 40))
	}

	if c.Wide {
		return crudcmd.EncodeTable(w, v, "Experiment", []string{"RUN-ID", "NAME", "STATUS", "SOURCE", "COLLECTION-ID", "TAGS", "SCORES", "CREATED", "COMPLETED", "DESCRIPTION", "ERROR"}, row)
	}
	return crudcmd.EncodeTable(w, v, "Experiment", []string{"RUN-ID", "NAME", "STATUS", "SOURCE", "COLLECTION-ID", "TAGS", "SCORES", "CREATED"}, row)
}

func formatTags(tags []string) string {
	if len(tags) == 0 {
		return "-"
	}
	return strings.Join(tags, ", ")
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}

// ScoresTableCodec renders []ScoreItem rows.
type ScoresTableCodec struct {
	Wide bool
}

func (c *ScoresTableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *ScoresTableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, s ScoreItem) {
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
		if !c.Wide {
			t.Row(s.ScoreID, evaluator, key, value, passed, gen)
			return
		}
		t.Row(s.ScoreID, evaluator, key, value, passed, gen, aio11yhttp.Truncate(s.Explanation, 40), aio11yhttp.FormatTime(s.CreatedAt))
	}

	if c.Wide {
		return crudcmd.EncodeTable(w, v, "ScoreItem", []string{"SCORE-ID", "EVALUATOR", "KEY", "VALUE", "PASSED", "GENERATION", "EXPLANATION", "CREATED"}, row)
	}
	return crudcmd.EncodeTable(w, v, "ScoreItem", []string{"SCORE-ID", "EVALUATOR", "KEY", "VALUE", "PASSED", "GENERATION"}, row)
}

func (c *ScoresTableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
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
