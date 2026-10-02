package dynamicobservability

import (
	"context"
	"sort"
	"strconv"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/retry"
)

type commandOpts struct{ IO cmdio.Options }

func (o *commandOpts) setup(flags *pflag.FlagSet, table cmdio.Table[rulesetStatus]) {
	cmdio.RegisterTable(&o.IO, table)
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *commandOpts) setupAgents(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, agentTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *commandOpts) setupMutation(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, mutationTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *commandOpts) Validate() error { return o.IO.Validate() }

func rulesetTable() cmdio.Table[rulesetStatus] {
	return cmdio.Table[rulesetStatus]{Columns: []cmdio.Column[rulesetStatus]{
		{Header: "NAME", Content: func(r rulesetStatus) string { return r.Name }},
		{Header: "PHASE", Content: func(r rulesetStatus) string { return r.Phase }},
		{Header: "CLUSTER", Content: func(r rulesetStatus) string { return r.Cluster }},
		{Header: "NAMESPACE", Content: func(r rulesetStatus) string { return r.Namespace }},
		{Header: "ATTACHED", Content: func(r rulesetStatus) string { return strconv.Itoa(r.AttachedTargets) }},
		{Header: "ERRORS", Content: func(r rulesetStatus) string { return strconv.Itoa(r.ErrorTargets) }},
	}}
}

func agentTable() cmdio.Table[agentStatus] {
	return cmdio.Table[agentStatus]{Columns: []cmdio.Column[agentStatus]{
		{Header: "NAME", Content: func(a agentStatus) string { return a.Name }},
		{Header: "CLUSTER", Content: func(a agentStatus) string { return a.Cluster }},
		{Header: "NODE", Content: func(a agentStatus) string { return a.NodeName }},
		{Header: "CONNECTION", Content: func(a agentStatus) string { return a.Connection }},
		{Header: "HEALTH", Content: func(a agentStatus) string { return a.Health }},
		{Header: "ATTACHMENTS", Content: func(a agentStatus) string { return strconv.Itoa(a.ActiveAttachments) }},
	}}
}

func mutationTable() cmdio.Table[cmdio.SingleMutation] {
	return cmdio.Table[cmdio.SingleMutation]{Columns: []cmdio.Column[cmdio.SingleMutation]{
		{Header: "NAME", Content: func(m cmdio.SingleMutation) string { return m.Target.Name }},
		{Header: "ACTION", Content: func(m cmdio.SingleMutation) string { return m.Action }},
		{Header: "CHANGED", Content: func(m cmdio.SingleMutation) string { return strconv.FormatBool(m.Changed != nil && *m.Changed) }},
	}}
}

func newRulesetListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &commandOpts{}
	cmd := experimental("list", "List rulesets with current attachment status")
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		api, err := newAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		probes, err := list[probe](cmd.Context(), api.client, api.probes)
		if err != nil {
			return err
		}
		now := time.Now()
		rows := make([]rulesetStatus, 0, len(probes))
		for _, p := range probes {
			rows = append(rows, probeStatus(p, now))
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		return opts.IO.Encode(cmd.OutOrStdout(), rows)
	}
	opts.setup(cmd.Flags(), rulesetTable())
	return cmd
}

func newRulesetStatusCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &commandOpts{}
	cmd := experimental("status <name>", "Show a ruleset's node and target attachment status")
	cmd.Args = cobra.ExactArgs(1)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		api, err := newAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		obj, err := api.client.Get(cmd.Context(), api.probes, args[0], metav1.GetOptions{})
		if err != nil {
			return err
		}
		p, err := decode[probe](obj)
		if err != nil {
			return err
		}
		row := probeStatus(p, time.Now())
		if opts.IO.OutputFormat == "table" {
			return opts.IO.Encode(cmd.OutOrStdout(), []rulesetStatus{row})
		}
		return opts.IO.Encode(cmd.OutOrStdout(), row)
	}
	opts.setup(cmd.Flags(), rulesetTable())
	return cmd
}

func newAgentListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &commandOpts{}
	cmd := experimental("list", "List node agents with heartbeat and readiness")
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		api, err := newAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		agents, err := list[nodeAgent](cmd.Context(), api.client, api.agents)
		if err != nil {
			return err
		}
		now := time.Now()
		rows := make([]agentStatus, 0, len(agents))
		for _, a := range agents {
			rows = append(rows, agentHealth(a, now))
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		return opts.IO.Encode(cmd.OutOrStdout(), rows)
	}
	opts.setupAgents(cmd.Flags())
	return cmd
}

func newAgentStatusCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &commandOpts{}
	cmd := experimental("status <name>", "Show one agent's heartbeat and readiness")
	cmd.Args = cobra.ExactArgs(1)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		api, err := newAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		obj, err := api.client.Get(cmd.Context(), api.agents, args[0], metav1.GetOptions{})
		if err != nil {
			return err
		}
		a, err := decode[nodeAgent](obj)
		if err != nil {
			return err
		}
		row := agentHealth(a, time.Now())
		if opts.IO.OutputFormat == "table" {
			return opts.IO.Encode(cmd.OutOrStdout(), []agentStatus{row})
		}
		return opts.IO.Encode(cmd.OutOrStdout(), row)
	}
	opts.setupAgents(cmd.Flags())
	return cmd
}

func newPauseCommand(loader *providers.ConfigLoader, paused bool) *cobra.Command {
	opts := &commandOpts{}
	action := "resume"
	if paused {
		action = "pause"
	}
	cmd := experimental(action+" <name>", action+" a ruleset")
	cmd.Args = cobra.ExactArgs(1)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		api, err := newAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		changed := false
		changed, err = setPaused(cmd.Context(), api.client, api.probes, args[0], paused)
		if err != nil {
			return err
		}
		result := cmdio.NewSingleMutation(action, cmdio.MutationTarget{Kind: "DynamicProbe", Name: args[0]})
		result.Changed = &changed
		if opts.IO.OutputFormat == "table" {
			return opts.IO.Encode(cmd.OutOrStdout(), []cmdio.SingleMutation{result})
		}
		return opts.IO.Encode(cmd.OutOrStdout(), result)
	}
	opts.setupMutation(cmd.Flags())
	return cmd
}

func setPaused(ctx context.Context, client resourceClient, desc resources.Descriptor, name string, paused bool) (bool, error) {
	changed := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		obj, err := client.Get(ctx, desc, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		current, _, err := unstructured.NestedBool(obj.Object, "spec", "paused")
		if err != nil {
			return err
		}
		if current == paused {
			changed = false
			return nil
		}
		if err := unstructured.SetNestedField(obj.Object, paused, "spec", "paused"); err != nil {
			return err
		}
		_, err = client.Update(ctx, desc, obj, metav1.UpdateOptions{})
		if err == nil {
			changed = true
		}
		return err
	})
	return changed, err
}
