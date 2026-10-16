package alert

import (
	"errors"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// groupsCommands returns the groups command group.
func groupsCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "Inspect alert rule groups and their evaluation status.",
		Long: `Inspect Grafana-managed alert rule groups.

These commands are read-only. To modify the rules in a group, use the
resources commands: gcx resources pull/push alertrules.`,
	}
	cmd.AddCommand(
		newGroupsListCommand(loader),
		newGroupsGetCommand(loader),
		newGroupsStatusCommand(loader),
	)
	return cmd
}

type groupsListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *groupsListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, GroupsTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for unlimited)")
}

func newGroupsListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &groupsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List alert rule groups.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
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

			groups, err := client.ListGroups(ctx)
			if err != nil {
				return err
			}

			groups = adapter.TruncateSlice(groups, opts.Limit)

			return opts.IO.Encode(cmd.OutOrStdout(), groups)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func GroupsTable() cmdio.Table[RuleGroup] {
	return cmdio.Table[RuleGroup]{Columns: []cmdio.Column[RuleGroup]{
		{Header: "NAME", Content: func(r RuleGroup) string { return r.Name }},
		{Header: "FOLDER", Content: func(r RuleGroup) string { return r.FolderUID }},
		{Header: "RULES", Content: func(r RuleGroup) string { return strconv.Itoa(len(r.Rules)) }},
		// Interval is in seconds per the Prometheus/Grafana ruler API contract.
		{Header: "INTERVAL", Content: func(r RuleGroup) string { return strconv.Itoa(r.Interval) + "s" }},
	}}
}

type groupsGetOpts struct {
	IO cmdio.Options
}

func (o *groupsGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &GroupRulesTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

//nolint:dupl // Similar structure to rules get command is intentional
func newGroupsGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &groupsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get a single alert rule group.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			name := args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			group, err := client.GetGroup(ctx, name)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), group)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// GroupRulesTableCodec renders a group's rules as a table.
type GroupRulesTableCodec struct{}

func (c *GroupRulesTableCodec) Format() format.Format { return "table" }

func (c *GroupRulesTableCodec) Encode(w io.Writer, v any) error {
	group, ok := v.(*RuleGroup)
	if !ok {
		return errors.New("invalid data type for table codec: expected *RuleGroup")
	}

	t := style.NewTable("UID", "NAME", "STATE", "HEALTH", "PAUSED")
	for _, r := range group.Rules {
		paused := "no"
		if r.IsPaused {
			paused = "yes"
		}
		t.Row(r.UID, r.Name, r.State, r.Health, paused)
	}
	return t.Render(w)
}

func (c *GroupRulesTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}

type groupsStatusOpts struct {
	IO cmdio.Options
}

func (o *groupsStatusOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, GroupsStatusTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGroupsStatusCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &groupsStatusOpts{}
	cmd := &cobra.Command{
		Use:   "status [NAME]",
		Short: "Show alert rule group status.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
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

			var groups []RuleGroup
			if len(args) == 1 {
				group, err := client.GetGroup(ctx, args[0])
				if err != nil {
					return err
				}
				groups = []RuleGroup{*group}
			} else {
				groups, err = client.ListGroups(ctx)
				if err != nil {
					return err
				}
			}

			if groups == nil {
				// Normalize so machine formats (json/yaml/agents) encode an
				// empty list ([]), not null. The human empty-state line is
				// owned by the table codec below — the command itself never
				// writes prose to stdout.
				groups = []RuleGroup{}
			}

			return opts.IO.Encode(cmd.OutOrStdout(), groups)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func GroupsStatusTable() cmdio.Table[RuleGroup] {
	return cmdio.Table[RuleGroup]{
		Columns: []cmdio.Column[RuleGroup]{
			{Header: "GROUP", Content: func(g RuleGroup) string { return g.Name }},
			{Header: "RULES", Content: func(g RuleGroup) string { return strconv.Itoa(len(g.Rules)) }},
			{Header: "FIRING", Content: func(g RuleGroup) string { return groupStateCount(g, StateFiring) }},
			{Header: "PENDING", Content: func(g RuleGroup) string { return groupStateCount(g, StatePending) }},
			{Header: "INACTIVE", Content: func(g RuleGroup) string { return groupStateCount(g, StateInactive) }},
			{Header: "LAST_EVAL", Content: func(g RuleGroup) string {
				if g.LastEvaluation == "0001-01-01T00:00:00Z" {
					return "never"
				}
				return g.LastEvaluation
			}},
		},
		// Keep prose in the table codec so JSON, YAML and agents still encode [].
		Empty: func(w io.Writer) error {
			cmdio.Info(w, "No alert rule groups found.")
			return nil
		},
	}
}

func groupStateCount(g RuleGroup, state string) string {
	count := 0
	for _, r := range g.Rules {
		ruleState := r.State
		// Preserve the status summary's treatment of unexpected states as inactive.
		if ruleState != StateFiring && ruleState != StatePending {
			ruleState = StateInactive
		}
		if ruleState == state {
			count++
		}
	}
	return strconv.Itoa(count)
}
