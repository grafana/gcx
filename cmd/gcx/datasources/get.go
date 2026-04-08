package datasources

import (
	"fmt"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	dsclient "github.com/grafana/gcx/internal/datasources"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type getOpts struct {
	IO cmdio.Options
}

func (opts *getOpts) setup(flags *pflag.FlagSet) {
	opts.IO.DefaultFormat("yaml")
	opts.IO.BindFlags(flags)
}

func (opts *getOpts) Validate() error {
	return opts.IO.Validate()
}

func getCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &getOpts{}

	cmd := &cobra.Command{
		Use:   "get UID",
		Short: "Get details of a specific datasource",
		Long:  "Get detailed information about a specific datasource by its UID.",
		Args:  cobra.ExactArgs(1),
		Example: `
	# Get datasource details
	gcx datasources get my-prometheus

	# Output as JSON
	gcx datasources get my-prometheus -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			uid := args[0]

			restCfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			dsClient, err := dsclient.NewClient(restCfg)
			if err != nil {
				return err
			}

			ds, err := dsClient.GetByUID(ctx, uid)
			if err != nil {
				return fmt.Errorf("failed to get datasource: %w", err)
			}

			info := &datasourceDetail{
				UID:       ds.UID,
				Name:      ds.Name,
				Type:      ds.Type,
				URL:       ds.URL,
				Access:    ds.Access,
				Default:   ds.IsDefault,
				ReadOnly:  ds.ReadOnly,
				Database:  ds.Database,
				BasicAuth: ds.BasicAuth,
				WithCreds: ds.WithCredentials,
				JSONData:  ds.JSONData,
			}

			return opts.IO.Encode(cmd.OutOrStdout(), info)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

type datasourceDetail struct {
	UID       string `json:"uid" yaml:"uid"`
	Name      string `json:"name" yaml:"name"`
	Type      string `json:"type" yaml:"type"`
	URL       string `json:"url" yaml:"url"`
	Access    string `json:"access" yaml:"access"`
	Default   bool   `json:"default" yaml:"default"`
	ReadOnly  bool   `json:"readOnly" yaml:"readOnly"`
	Database  string `json:"database,omitempty" yaml:"database,omitempty"`
	BasicAuth bool   `json:"basicAuth" yaml:"basicAuth"`
	WithCreds bool   `json:"withCredentials" yaml:"withCredentials"`
	JSONData  any    `json:"jsonData,omitempty" yaml:"jsonData,omitempty"`
}
