package datasources

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	dsclient "github.com/grafana/gcx/internal/datasources"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listOpts struct {
	IO   cmdio.Options
	Type string
}

func (opts *listOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &datasourceTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Type, "type", "t", "", "Filter by datasource type (e.g., prometheus, loki)")
}

func (opts *listOpts) Validate() error {
	return opts.IO.Validate()
}

func listCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &listOpts{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all datasources",
		Long:  "List all datasources configured in Grafana.",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "--type prometheus -o json",
		},
		Example: `
	# List all datasources
	gcx datasources list

	# List only Prometheus datasources
	gcx datasources list --type prometheus

	# Output as JSON
	gcx datasources list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			restCfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			dsClient, err := dsclient.NewClient(restCfg)
			if err != nil {
				return err
			}

			datasources, err := dsClient.List(ctx)
			if err != nil {
				return fmt.Errorf("failed to list datasources: %w", err)
			}

			if opts.Type != "" {
				filtered := make([]*datasourceInfo, 0)
				for _, ds := range datasources {
					if strings.EqualFold(ds.Type, opts.Type) {
						filtered = append(filtered, &datasourceInfo{
							UID:      ds.UID,
							Name:     ds.Name,
							Type:     ds.Type,
							URL:      ds.URL,
							Default:  ds.IsDefault,
							ReadOnly: ds.ReadOnly,
						})
					}
				}
				return outputDatasources(cmd, opts, filtered)
			}

			infos := make([]*datasourceInfo, 0, len(datasources))
			for _, ds := range datasources {
				infos = append(infos, &datasourceInfo{
					UID:      ds.UID,
					Name:     ds.Name,
					Type:     ds.Type,
					URL:      ds.URL,
					Default:  ds.IsDefault,
					ReadOnly: ds.ReadOnly,
				})
			}

			return outputDatasources(cmd, opts, infos)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

type datasourceInfo struct {
	UID      string `json:"uid" yaml:"uid"`
	Name     string `json:"name" yaml:"name"`
	Type     string `json:"type" yaml:"type"`
	URL      string `json:"url" yaml:"url"`
	Default  bool   `json:"default" yaml:"default"`
	ReadOnly bool   `json:"readOnly" yaml:"readOnly"`
}

func outputDatasources(cmd *cobra.Command, opts *listOpts, datasources []*datasourceInfo) error {
	if opts.IO.OutputFormat == "table" {
		return opts.IO.Encode(cmd.OutOrStdout(), datasources)
	}

	return opts.IO.Encode(cmd.OutOrStdout(), map[string]any{"datasources": datasources})
}

type datasourceTableCodec struct{}

func (c *datasourceTableCodec) Format() format.Format {
	return "table"
}

func (c *datasourceTableCodec) Encode(w io.Writer, data any) error {
	datasources, ok := data.([]*datasourceInfo)
	if !ok {
		return errors.New("invalid data type for table codec")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "UID\tNAME\tTYPE\tURL\tDEFAULT")

	for _, ds := range datasources {
		defaultStr := ""
		if ds.Default {
			defaultStr = "*"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", ds.UID, ds.Name, ds.Type, ds.URL, defaultStr)
	}

	return tw.Flush()
}

func (c *datasourceTableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}
