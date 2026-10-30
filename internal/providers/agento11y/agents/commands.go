package agents

import (
	"strconv"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := agento11yhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the agents command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Query Agent Observability agent catalog.",
	}

	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newVersionsCommand(loader),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, ListTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of agents to return")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List agents.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			agents, err := client.List(cmd.Context(), opts.Limit)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), agents)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO      cmdio.Options
	Version string
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Version, "version", "", "Specific effective version to look up")
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <agent-name>",
		Short: "Get a single agent definition.",
		Long:  `Get the full agent definition. Use --version for a specific version.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			detail, err := client.Lookup(cmd.Context(), args[0], opts.Version)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), detail)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- versions ---

type versionsOpts struct {
	IO cmdio.Options
}

func (o *versionsOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, VersionsTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newVersionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &versionsOpts{}
	cmd := &cobra.Command{
		Use:   "list-versions <agent-name>",
		Short: "List version history for an agent.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			versions, err := client.Versions(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), versions)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- list table codec ---

func ListTable() cmdio.Table[Agent] {
	return cmdio.Table[Agent]{Columns: []cmdio.Column[Agent]{
		{Header: "NAME", Content: func(r Agent) string { return r.AgentName }},
		{Header: "VERSIONS", Content: func(r Agent) string { return strconv.Itoa(r.VersionCount) }},
		{Header: "GENERATIONS", Content: func(r Agent) string { return strconv.FormatInt(r.GenerationCount, 10) }},
		{Header: "TOOLS", Content: func(r Agent) string { return strconv.Itoa(r.ToolCount) }},
		{Header: "TOKENS", Visible: cmdio.WideOnly, Content: func(r Agent) string { return strconv.Itoa(r.TokenEstimate.Total) }},
		{Header: "FIRST SEEN", Visible: cmdio.WideOnly, Content: func(r Agent) string { return agento11yhttp.FormatTime(r.FirstSeenAt) }},
		{Header: "LAST SEEN", Content: func(r Agent) string { return agento11yhttp.FormatTime(r.LatestSeenAt) }},
	}}
}

// --- versions table codec ---

func VersionsTable() cmdio.Table[AgentVersion] {
	return cmdio.Table[AgentVersion]{Columns: []cmdio.Column[AgentVersion]{
		{Header: "VERSION", Content: func(r AgentVersion) string { return r.EffectiveVersion }},
		{Header: "GENERATIONS", Content: func(r AgentVersion) string { return strconv.FormatInt(r.GenerationCount, 10) }},
		{Header: "TOOLS", Content: func(r AgentVersion) string { return strconv.Itoa(r.ToolCount) }},
		{Header: "TOKENS", Content: func(r AgentVersion) string { return strconv.Itoa(r.TokenEstimate.Total) }},
		{Header: "FIRST SEEN", Content: func(r AgentVersion) string { return agento11yhttp.FormatTime(r.FirstSeenAt) }},
		{Header: "LAST SEEN", Content: func(r AgentVersion) string { return agento11yhttp.FormatTime(r.LastSeenAt) }},
	}}
}
