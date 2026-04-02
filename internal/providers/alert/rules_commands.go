package alert

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
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

			if opts.State != "" {
				validStates := map[string]bool{StateFiring: true, StatePending: true, StateInactive: true}
				if !validStates[opts.State] {
					return fmt.Errorf("invalid state %q: must be one of firing, pending, inactive", opts.State)
				}
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
					for _, r := range g.Rules {
						if opts.State == "" || r.State == opts.State {
							rules = append(rules, r)
						}
					}
				}
				return codec.Encode(cmd.OutOrStdout(), rules)
			}

			if opts.State != "" {
				for i := range resp.Data.Groups {
					var filtered []RuleStatus
					for _, r := range resp.Data.Groups[i].Rules {
						if r.State == opts.State {
							filtered = append(filtered, r)
						}
					}
					resp.Data.Groups[i].Rules = filtered
				}
			}

			// Filter out groups with no rules to avoid empty groups in JSON/YAML output.
			var nonEmpty []RuleGroup
			for _, g := range resp.Data.Groups {
				if len(g.Rules) > 0 {
					nonEmpty = append(nonEmpty, g)
				}
			}
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

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	if c.Wide {
		fmt.Fprintln(tw, "UID\tNAME\tSTATE\tHEALTH\tLAST_EVAL\tEVAL_TIME\tPAUSED\tFOLDER")
	} else {
		fmt.Fprintln(tw, "UID\tNAME\tSTATE\tHEALTH\tPAUSED")
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
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.UID, r.Name, r.State, r.Health, lastEval, evalTime, paused, r.FolderUID)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.UID, r.Name, r.State, r.Health, paused)
		}
	}

	return tw.Flush()
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

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "UID\tNAME\tSTATE\tHEALTH\tPAUSED")

	paused := "no"
	if rule.IsPaused {
		paused = "yes"
	}
	fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", rule.UID, rule.Name, rule.State, rule.Health, paused)

	return tw.Flush()
}

func (c *RuleDetailTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}
