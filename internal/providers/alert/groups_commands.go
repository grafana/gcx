package alert

import (
	"context"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
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

func newGroupsListCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewListCommand(crudcmd.ListConfig[RuleGroup]{
		Use:          "list",
		Short:        "List alert rule groups.",
		DefaultFmt:   "table",
		LimitDefault: 50,
		Codecs:       []format.Codec{&GroupsTableCodec{}},
		Fetch: func(ctx context.Context, limit int64) ([]RuleGroup, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			groups, err := client.ListGroups(ctx)
			if err != nil {
				return nil, err
			}
			return adapter.TruncateSlice(groups, limit), nil
		},
	})
}

// GroupsTableCodec renders alert rule groups as a tabular table.
type GroupsTableCodec struct{}

func (c *GroupsTableCodec) Format() format.Format { return "table" }

func (c *GroupsTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeTable(w, v, "RuleGroup", []string{"NAME", "FOLDER", "RULES", "INTERVAL"}, func(t *style.TableBuilder, g RuleGroup) {
		// Interval is in seconds per the Prometheus/Grafana ruler API contract.
		t.Row(g.Name, g.FolderUID, strconv.Itoa(len(g.Rules)), strconv.Itoa(g.Interval)+"s")
	})
}

func (c *GroupsTableCodec) Decode(io.Reader, any) error {
	return crudcmd.ErrTableDecode
}

func newGroupsGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*RuleGroup]{
		Use:        "get NAME",
		Short:      "Get a single alert rule group.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "table",
		Codecs:     []format.Codec{&GroupRulesTableCodec{}},
		Fetch: func(ctx context.Context, args []string) (*RuleGroup, error) {
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return nil, err
			}
			client, err := NewClient(restCfg)
			if err != nil {
				return nil, err
			}
			return client.GetGroup(ctx, args[0])
		},
	})
}

// GroupRulesTableCodec renders a group's rules as a table.
type GroupRulesTableCodec struct{}

func (c *GroupRulesTableCodec) Format() format.Format { return "table" }

func (c *GroupRulesTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeItem(w, v, "RuleGroup", []string{"UID", "NAME", "STATE", "HEALTH", "PAUSED"}, func(t *style.TableBuilder, group RuleGroup) {
		for _, r := range group.Rules {
			paused := "no"
			if r.IsPaused {
				paused = "yes"
			}
			t.Row(r.UID, r.Name, r.State, r.Health, paused)
		}
	})
}

func (c *GroupRulesTableCodec) Decode(io.Reader, any) error {
	return crudcmd.ErrTableDecode
}

func newGroupsStatusCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &crudcmd.GetOpts{}
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
	opts.Setup(cmd.Flags(), "table", &GroupsStatusTableCodec{})
	return cmd
}

// GroupsStatusTableCodec renders alert rule group status summaries as a tabular table.
type GroupsStatusTableCodec struct{}

func (c *GroupsStatusTableCodec) Format() format.Format { return "table" }

func (c *GroupsStatusTableCodec) Encode(w io.Writer, v any) error {
	return crudcmd.EncodeTable(w, v, "RuleGroup", []string{"GROUP", "RULES", "FIRING", "PENDING", "INACTIVE", "LAST_EVAL"}, func(t *style.TableBuilder, g RuleGroup) {
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
	})
}

func (c *GroupsStatusTableCodec) Decode(io.Reader, any) error {
	return crudcmd.ErrTableDecode
}
