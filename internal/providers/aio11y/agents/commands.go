package agents

import (
	"errors"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the agents command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Query AI Observability agent catalog.",
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
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ListTableCodec{Wide: true})
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
	o.IO.RegisterCustomCodec("table", &VersionsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newVersionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &versionsOpts{}
	cmd := &cobra.Command{
		Use:   "versions <agent-name>",
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

type ListTableCodec struct {
	Wide bool
}

func (c *ListTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ListTableCodec) Encode(w io.Writer, v any) error {
	agents, ok := v.([]Agent)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Agent")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("NAME", "VERSIONS", "GENERATIONS", "TOOLS", "TOKENS", "FIRST SEEN", "LAST SEEN")
	} else {
		t = style.NewTable("NAME", "VERSIONS", "GENERATIONS", "TOOLS", "LAST SEEN")
	}

	for _, a := range agents {
		lastSeen := aio11yhttp.FormatTime(a.LatestSeenAt)
		if c.Wide {
			firstSeen := aio11yhttp.FormatTime(a.FirstSeenAt)
			t.Row(a.AgentName, strconv.Itoa(a.VersionCount), strconv.FormatInt(a.GenerationCount, 10),
				strconv.Itoa(a.ToolCount), strconv.Itoa(a.TokenEstimate.Total), firstSeen, lastSeen)
		} else {
			t.Row(a.AgentName, strconv.Itoa(a.VersionCount), strconv.FormatInt(a.GenerationCount, 10),
				strconv.Itoa(a.ToolCount), lastSeen)
		}
	}
	return t.Render(w)
}

func (c *ListTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- versions table codec ---

type VersionsTableCodec struct{}

func (c *VersionsTableCodec) Format() format.Format { return "table" }

func (c *VersionsTableCodec) Encode(w io.Writer, v any) error {
	versions, ok := v.([]AgentVersion)
	if !ok {
		return errors.New("invalid data type for table codec: expected []AgentVersion")
	}

	t := style.NewTable("VERSION", "GENERATIONS", "TOOLS", "TOKENS", "FIRST SEEN", "LAST SEEN")
	for _, ver := range versions {
		t.Row(ver.EffectiveVersion, strconv.FormatInt(ver.GenerationCount, 10),
			strconv.Itoa(ver.ToolCount), strconv.Itoa(ver.TokenEstimate.Total),
			aio11yhttp.FormatTime(ver.FirstSeenAt), aio11yhttp.FormatTime(ver.LastSeenAt))
	}
	return t.Render(w)
}

func (c *VersionsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
