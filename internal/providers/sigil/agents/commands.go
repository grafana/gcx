package agents

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := sigilhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the agents command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Query Sigil agent catalog.",
	}

	cmd.AddCommand(
		newShowCommand(loader),
		newVersionsCommand(loader),
	)
	return cmd
}

// --- show (list + lookup) ---

type showOpts struct {
	IO      cmdio.Options
	Limit   int
	Version string
}

func (o *showOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ListTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 100, "Maximum number of agents to return")
	flags.StringVar(&o.Version, "version", "", "Specific effective version to look up")
}

func newShowCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &showOpts{}
	cmd := &cobra.Command{
		Use:   "show [agent-name]",
		Short: "Show agents or a single agent detail.",
		Long: `Show agents. Without a name, lists agents (use --limit to control count).
With a name, shows the full agent definition (use --version for a specific version).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			if len(args) == 1 {
				if !cmd.Flags().Changed("output") && !cmd.Flags().Changed("json") {
					opts.IO.OutputFormat = "yaml"
				}
				detail, err := client.Lookup(cmd.Context(), args[0], opts.Version)
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), detail)
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

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "NAME\tVERSIONS\tGENERATIONS\tTOOLS\tTOKENS\tFIRST SEEN\tLAST SEEN")
	} else {
		fmt.Fprintln(tw, "NAME\tVERSIONS\tGENERATIONS\tTOOLS\tLAST SEEN")
	}

	for _, a := range agents {
		lastSeen := sigilhttp.FormatTime(a.LatestSeenAt)
		if c.Wide {
			firstSeen := sigilhttp.FormatTime(a.FirstSeenAt)
			fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\t%s\n",
				a.AgentName, a.VersionCount, a.GenerationCount, a.ToolCount, a.TokenEstimate.Total, firstSeen, lastSeen)
		} else {
			fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%s\n",
				a.AgentName, a.VersionCount, a.GenerationCount, a.ToolCount, lastSeen)
		}
	}
	return tw.Flush()
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

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "VERSION\tGENERATIONS\tTOOLS\tTOKENS\tFIRST SEEN\tLAST SEEN")

	for _, ver := range versions {
		hash := ver.EffectiveVersion
		if len(hash) > 15 {
			hash = hash[:15] + "..."
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%s\t%s\n",
			hash, ver.GenerationCount, ver.ToolCount, ver.TokenEstimate.Total,
			sigilhttp.FormatTime(ver.FirstSeenAt), sigilhttp.FormatTime(ver.LastSeenAt))
	}
	return tw.Flush()
}

func (c *VersionsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
