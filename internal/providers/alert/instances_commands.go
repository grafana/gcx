package alert

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// AlertInstanceRecord is a flattened alert instance with parent rule/group context.
type AlertInstanceRecord struct {
	RuleUID     string            `json:"ruleUid"`
	RuleName    string            `json:"ruleName"`
	GroupName   string            `json:"groupName"`
	FolderUID   string            `json:"folderUid,omitempty"`
	State       string            `json:"state"`
	ActiveAt    string            `json:"activeAt,omitempty"`
	Value       any               `json:"value,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

func instancesCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "instances",
		Aliases: []string{"alerts"},
		Short:   "Manage alert instances.",
	}
	cmd.AddCommand(newInstancesListCommand(loader))
	return cmd
}

type instancesListOpts struct {
	IO         cmdio.Options
	RuleUID    string
	GroupName  string
	FolderUID  string
	State      string
	Datasource string
	Name       string
}

func (o *instancesListOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	return validateAlertState(o.State)
}

func (o *instancesListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, InstancesTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.RuleUID, "rule", "", "Filter by rule UID")
	flags.StringVar(&o.GroupName, "group", "", "Filter by group name")
	flags.StringVar(&o.FolderUID, "folder", "", "Filter by folder UID")
	flags.StringVar(&o.State, "state", "", "Filter by alert instance state (firing, pending, inactive)")
	flags.StringVar(&o.Datasource, "datasource", "", "Datasource UID to query (default: Grafana-managed rules)")
	flags.StringVar(&o.Name, "name", "", "Filter by rule name (regex, e.g. 'Tempo.*')")
}

func newInstancesListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &instancesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List alert instances.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
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
				RuleUID:    opts.RuleUID,
				GroupName:  opts.GroupName,
				FolderUID:  opts.FolderUID,
				State:      opts.State,
				Datasource: opts.Datasource,
			})
			if err != nil {
				return err
			}

			instances := collectAlertInstances(resp.Data.Groups)

			if opts.Name != "" {
				re, err := regexp.Compile("(?i)" + opts.Name)
				if err != nil {
					return fmt.Errorf("invalid --name regex %q: %w", opts.Name, err)
				}
				instances = filterInstancesByName(instances, re)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), instances)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func InstancesTable() cmdio.Table[AlertInstanceRecord] {
	return cmdio.Table[AlertInstanceRecord]{Columns: []cmdio.Column[AlertInstanceRecord]{
		{Header: "RULE_UID", Content: func(r AlertInstanceRecord) string { return r.RuleUID }},
		{Header: "RULE", Content: func(r AlertInstanceRecord) string { return r.RuleName }},
		{Header: "GROUP", Visible: cmdio.WideOnly, Content: func(r AlertInstanceRecord) string { return r.GroupName }},
		{Header: "FOLDER", Visible: cmdio.WideOnly, Content: func(r AlertInstanceRecord) string { return cmdio.OrDash(r.FolderUID) }},
		{Header: "STATE", Content: func(r AlertInstanceRecord) string { return r.State }},
		{Header: "ACTIVE_AT", Content: func(r AlertInstanceRecord) string { return cmdio.OrDash(r.ActiveAt) }},
		{Header: "VALUE", Content: func(r AlertInstanceRecord) string { return dashForNil(r.Value) }},
		{Header: "LABELS", Content: func(r AlertInstanceRecord) string { return formatLabels(r.Labels) }},
	}}
}

func filterInstancesByName(instances []AlertInstanceRecord, re *regexp.Regexp) []AlertInstanceRecord {
	filtered := make([]AlertInstanceRecord, 0, len(instances))
	for _, inst := range instances {
		if re.MatchString(inst.RuleName) {
			filtered = append(filtered, inst)
		}
	}
	return filtered
}

func collectAlertInstances(groups []RuleGroup) []AlertInstanceRecord {
	var instances []AlertInstanceRecord
	for _, g := range groups {
		for _, r := range g.Rules {
			for _, a := range r.Alerts {
				state := a.State
				if state == "" {
					state = r.State
				}

				instances = append(instances, AlertInstanceRecord{
					RuleUID:     r.UID,
					RuleName:    r.Name,
					GroupName:   g.Name,
					FolderUID:   g.FolderUID,
					State:       state,
					ActiveAt:    a.ActiveAt,
					Value:       a.Value,
					Labels:      a.Labels,
					Annotations: a.Annotations,
				})
			}
		}
	}
	return instances
}

func validateAlertState(state string) error {
	if state == "" {
		return nil
	}

	validStates := map[string]bool{
		StateFiring:   true,
		StatePending:  true,
		StateInactive: true,
	}
	if !validStates[state] {
		return fmt.Errorf("invalid state %q: must be one of firing, pending, inactive", state)
	}
	return nil
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+labels[k])
	}
	return strings.Join(pairs, ", ")
}

func dashForNil(v any) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprint(v)
}
