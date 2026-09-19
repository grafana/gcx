package alert

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// rulesCommands returns the rules command group.
func rulesCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Inspect alert rule state and health.",
		Long: `Inspect Grafana-managed alert rules.

These commands are read-only: they show evaluation state, health, and timing.
To create, modify, or delete alert rules, use the resources commands:

  gcx resources pull alertrules -p ./rules   # export rules to disk
  gcx resources push -p ./rules              # apply edited rules
  gcx resources delete alertrules/<name>     # delete a rule`,
	}
	cmd.AddCommand(
		newRulesListCommand(loader),
		newRulesGetCommand(loader),
	)
	return cmd
}

type rulesListOpts struct {
	IO        cmdio.Options
	GroupName string
	FolderUID string
	State     string
	Limit     int64
}

func (o *rulesListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, RulesTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.GroupName, "group", "", "Filter by group name")
	flags.StringVar(&o.FolderUID, "folder", "", "Filter by folder UID")
	flags.StringVar(&o.State, "state", "", "Filter by rule state (firing, pending, inactive)")
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for unlimited)")
}

func newRulesListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &rulesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List alert rules.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			if err := validateAlertState(opts.State); err != nil {
				return err
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			resp, err := client.List(ctx, ListOptions{
				GroupName: opts.GroupName,
				FolderUID: opts.FolderUID,
				State:     opts.State,
			})
			if err != nil {
				return err
			}

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
			}

			if codec.Format() == "table" || codec.Format() == "wide" {
				var rules []RuleStatus
				for _, g := range resp.Data.Groups {
					rules = append(rules, g.Rules...)
				}
				rules = adapter.TruncateSlice(rules, opts.Limit)
				return codec.Encode(cmd.OutOrStdout(), rules)
			}

			// Filter out groups with no rules to avoid empty groups in JSON/YAML output.
			var nonEmpty []RuleGroup
			for _, g := range resp.Data.Groups {
				if len(g.Rules) > 0 {
					nonEmpty = append(nonEmpty, g)
				}
			}
			nonEmpty = adapter.TruncateSlice(nonEmpty, opts.Limit)
			return opts.IO.Encode(cmd.OutOrStdout(), nonEmpty)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func RulesTable() cmdio.Table[RuleStatus] {
	return cmdio.Table[RuleStatus]{Columns: []cmdio.Column[RuleStatus]{
		{Header: "UID", Content: func(r RuleStatus) string { return r.UID }},
		{Header: "NAME", Content: func(r RuleStatus) string { return r.Name }},
		{Header: "STATE", Content: func(r RuleStatus) string { return r.State }},
		{Header: "HEALTH", Content: func(r RuleStatus) string { return r.Health }},
		{Header: "LAST_EVAL", Visible: cmdio.WideOnly, Content: func(r RuleStatus) string {
			if r.LastEvaluation == "0001-01-01T00:00:00Z" {
				return "never"
			}
			return r.LastEvaluation
		}},
		{Header: "EVAL_TIME", Visible: cmdio.WideOnly, Content: func(r RuleStatus) string { return fmt.Sprintf("%.3fs", r.EvaluationTime) }},
		{Header: "PAUSED", Content: func(r RuleStatus) string {
			if r.IsPaused {
				return "yes"
			}
			return "no"
		}},
		{Header: "FOLDER", Visible: cmdio.WideOnly, Content: func(r RuleStatus) string { return r.FolderUID }},
	}}
}

type rulesGetOpts struct {
	IO cmdio.Options
}

func (o *rulesGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &RuleDetailTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

//nolint:dupl // Similar structure to groups get command is intentional
func newRulesGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &rulesGetOpts{}
	cmd := &cobra.Command{
		Use:   "get UID",
		Short: "Get a single alert rule.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			uid := args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			rule, err := client.GetRule(ctx, uid)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), rule)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// RuleDetailTableCodec renders a single rule as a table row.
type RuleDetailTableCodec struct{}

func (c *RuleDetailTableCodec) Format() format.Format { return "table" }

func (c *RuleDetailTableCodec) Encode(w io.Writer, v any) error {
	rule, ok := v.(*RuleStatus)
	if !ok {
		return errors.New("invalid data type for table codec: expected *RuleStatus")
	}

	paused := "no"
	if rule.IsPaused {
		paused = "yes"
	}

	t := style.NewTable("UID", "NAME", "STATE", "HEALTH", "PAUSED")
	t.Row(rule.UID, rule.Name, rule.State, rule.Health, paused)
	return t.Render(w)
}

func (c *RuleDetailTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}
