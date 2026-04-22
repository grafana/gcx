package alert

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/limit"
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
		Short: "Manage alert rules.",
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
}

func (o *rulesListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &RulesTableCodec{})
	o.IO.RegisterCustomCodec("wide", &RulesTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.GroupName, "group", "", "Filter by group name")
	flags.StringVar(&o.FolderUID, "folder", "", "Filter by folder UID")
	flags.StringVar(&o.State, "state", "", "Filter by rule state (firing, pending, inactive)")
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
				resolvedLimit := limit.Resolve(ctx, 50)
				rules = adapter.TruncateSlice(rules, resolvedLimit)
				return codec.Encode(cmd.OutOrStdout(), rules)
			}

			// Filter out groups with no rules to avoid empty groups in JSON/YAML output.
			resolvedLimit := limit.Resolve(ctx, 50)
			var nonEmpty []RuleGroup
			for _, g := range resp.Data.Groups {
				if len(g.Rules) > 0 {
					nonEmpty = append(nonEmpty, g)
				}
			}
			nonEmpty = adapter.TruncateSlice(nonEmpty, resolvedLimit)
			return opts.IO.Encode(cmd.OutOrStdout(), nonEmpty)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// RulesTableCodec renders alert rules as a tabular table.
type RulesTableCodec struct {
	Wide bool
}

func (c *RulesTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *RulesTableCodec) Encode(w io.Writer, v any) error {
	rules, ok := v.([]RuleStatus)
	if !ok {
		return errors.New("invalid data type for table codec: expected []RuleStatus")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("UID", "NAME", "STATE", "HEALTH", "LAST_EVAL", "EVAL_TIME", "PAUSED", "FOLDER")
	} else {
		t = style.NewTable("UID", "NAME", "STATE", "HEALTH", "PAUSED")
	}

	for _, r := range rules {
		paused := "no"
		if r.IsPaused {
			paused = "yes"
		}

		if c.Wide {
			lastEval := r.LastEvaluation
			if lastEval == "0001-01-01T00:00:00Z" {
				lastEval = "never"
			}
			evalTime := fmt.Sprintf("%.3fs", r.EvaluationTime)
			t.Row(r.UID, r.Name, r.State, r.Health, lastEval, evalTime, paused, r.FolderUID)
		} else {
			t.Row(r.UID, r.Name, r.State, r.Health, paused)
		}
	}

	return t.Render(w)
}

func (c *RulesTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
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
