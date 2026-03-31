package config

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
)

type configPathEntry struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Priority int    `json:"priority"`
	Modified string `json:"modified"`
}

func pathCmd(configOpts *Options) *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "path",
		Short: "Show loaded config file paths",
		Long:  "Display all config files that contribute to the merged configuration, ordered by priority.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configOpts.loadConfigTolerantLayered(cmd.Context())
			if err != nil {
				return err
			}

			sources := cfg.Sources
			if len(sources) == 0 {
				cmd.Println("No config files found.")
				return nil
			}

			entries := make([]configPathEntry, 0, len(sources))
			for _, src := range sources {
				entries = append(entries, configPathEntry{
					Path:     src.Path,
					Type:     src.Type,
					Priority: src.Priority(),
					Modified: src.ModTime.Format(time.DateTime),
				})
			}

			// Reverse for display: highest priority (lowest number) first.
			for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
				entries[i], entries[j] = entries[j], entries[i]
			}

			// JSON/YAML: use io codec.
			if outputFormat == "json" || outputFormat == "yaml" {
				ioOpts := &cmdio.Options{OutputFormat: outputFormat}
				return ioOpts.Encode(cmd.OutOrStdout(), entries)
			}

			// Default: table output.
			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', tabwriter.TabIndent|tabwriter.DiscardEmptyColumns)
			fmt.Fprintf(tab, "PRIORITY\tTYPE\tPATH\tMODIFIED\n")
			for _, e := range entries {
				fmt.Fprintf(tab, "%d\t%s\t%s\t%s\n", e.Priority, e.Type, e.Path, e.Modified)
			}
			return tab.Flush()
		},
	}

	defaultFormat := "table"
	if agent.IsAgentMode() {
		defaultFormat = "json"
	}
	cmd.Flags().StringVarP(&outputFormat, "output", "o", defaultFormat, "Output format. One of: json, table, yaml")

	return cmd
}
