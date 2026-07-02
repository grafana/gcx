package alert

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/crudcmd"
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
	crudcmd.ListOpts

	GroupName string
	FolderUID string
	State     string
}

func (o *rulesListOpts) setup(flags *pflag.FlagSet) {
	o.Setup(flags, "table", 50, "", &RulesTableCodec{}, &RulesTableCodec{Wide: true})
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

// RulesTableCodec renders alert rules as a tabular table.
type RulesTableCodec struct {
	Wide bool
}

func (c *RulesTableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *RulesTableCodec) Encode(w io.Writer, v any) error {
	if c.Wide {
		return crudcmd.EncodeTable(w, v, "RuleStatus",
			[]string{"UID", "NAME", "STATE", "HEALTH", "LAST_EVAL", "EVAL_TIME", "PAUSED", "FOLDER"},
			func(t *style.TableBuilder, r RuleStatus) {
				paused := "no"
				if r.IsPaused {
					paused = "yes"
				}
				lastEval := r.LastEvaluation
				if lastEval == "0001-01-01T00:00:00Z" {
					lastEval = "never"
				}
				evalTime := fmt.Sprintf("%.3fs", r.EvaluationTime)
				t.Row(r.UID, r.Name, r.State, r.Health, lastEval, evalTime, paused, r.FolderUID)
			})
	}
	return crudcmd.EncodeTable(w, v, "RuleStatus", []string{"UID", "NAME", "STATE", "HEALTH", "PAUSED"}, func(t *style.TableBuilder, r RuleStatus) {
		paused := "no"
		if r.IsPaused {
			paused = "yes"
		}
		t.Row(r.UID, r.Name, r.State, r.Health, paused)
	})
}

func (c *RulesTableCodec) Decode(io.Reader, any) error {
	return crudcmd.ErrTableDecode
}

func newRulesGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*RuleStatus]{
		Use:        "get UID",
		Short:      "Get a single alert rule.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "table",
		Codecs:     []format.Codec{&RuleDetailTableCodec{}},
		Fetch: func(ctx context.Context, args []string) (*RuleStatus, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			return client.GetRule(ctx, args[0])
		},
	})
}

// RuleDetailTableCodec renders a single rule as a table row.
type RuleDetailTableCodec struct{}

func (c *RuleDetailTableCodec) Format() format.Format { return "table" }

func (c *RuleDetailTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeItem(w, v, "RuleStatus", []string{"UID", "NAME", "STATE", "HEALTH", "PAUSED"}, func(t *style.TableBuilder, rule RuleStatus) {
		paused := "no"
		if rule.IsPaused {
			paused = "yes"
		}
		t.Row(rule.UID, rule.Name, rule.State, rule.Health, paused)
	})
}

func (c *RuleDetailTableCodec) Decode(io.Reader, any) error {
	return crudcmd.ErrTableDecode
}
