package alert

import (
	"errors"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// groupsCommands returns the groups command group.
func groupsCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "Manage alert rule groups.",
	}
	cmd.AddCommand(
		newGroupsListCommand(loader),
		newGroupsGetCommand(loader),
		newGroupsStatusCommand(loader),
	)
	return cmd
}

type groupsListOpts struct {
	IO cmdio.Options
}

func (o *groupsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &GroupsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
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

			return opts.IO.Encode(cmd.OutOrStdout(), groups)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// GroupsTableCodec renders alert rule groups as a tabular table.
type GroupsTableCodec struct{}

func (c *GroupsTableCodec) Format() format.Format { return "table" }

func (c *GroupsTableCodec) Encode(w io.Writer, v any) error {
	groups, ok := v.([]RuleGroup)
	if !ok {
		return errors.New("invalid data type for table codec: expected []RuleGroup")
	}

	t := style.NewTable("NAME", "FOLDER", "RULES", "INTERVAL")
	for _, g := range groups {
		// Interval is in seconds per the Prometheus/Grafana ruler API contract.
		t.Row(g.Name, g.FolderUID, strconv.Itoa(len(g.Rules)), strconv.Itoa(g.Interval)+"s")
	}
	return t.Render(w)
}

func (c *GroupsTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
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
	o.IO.RegisterCustomCodec("table", &GroupsStatusTableCodec{})
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

			if len(groups) == 0 {
				cmdio.Info(cmd.OutOrStdout(), "No alert rule groups found.")
				return nil
			}

			return opts.IO.Encode(cmd.OutOrStdout(), groups)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// GroupsStatusTableCodec renders alert rule group status summaries as a tabular table.
type GroupsStatusTableCodec struct{}

func (c *GroupsStatusTableCodec) Format() format.Format { return "table" }

func (c *GroupsStatusTableCodec) Encode(w io.Writer, v any) error {
	groups, ok := v.([]RuleGroup)
	if !ok {
		return errors.New("invalid data type for status table codec: expected []RuleGroup")
	}

	t := style.NewTable("GROUP", "RULES", "FIRING", "PENDING", "INACTIVE", "LAST_EVAL")
	for _, g := range groups {
		firing, pending, inactive := 0, 0, 0
		for _, r := range g.Rules {
			switch r.State {
			case StateFiring:
				firing++
			case StatePending:
				pending++
			case StateInactive:
				inactive++
			default:
				// The Grafana alerting API only returns firing/pending/inactive,
				// but count unexpected states as inactive defensively.
				inactive++
			}
		}
		lastEval := g.LastEvaluation
		if lastEval == "0001-01-01T00:00:00Z" {
			lastEval = "never"
		}
		t.Row(g.Name, strconv.Itoa(len(g.Rules)), strconv.Itoa(firing), strconv.Itoa(pending), strconv.Itoa(inactive), lastEval)
	}
	return t.Render(w)
}

func (c *GroupsStatusTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("status table codec does not support decoding")
}
