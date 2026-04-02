package traces

import (
	"errors"
	"fmt"
	"io"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type tagsOpts struct {
	IO         cmdio.Options
	Datasource string
	Scope      string
	Query      string
}

func (opts *tagsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &tempoTagsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	flags.StringVar(&opts.Scope, "scope", "", "Tag scope filter (resource, span, event, link, instrumentation)")
	flags.StringVarP(&opts.Query, "query", "q", "", "TraceQL query to filter tags")
}

func (opts *tagsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return tempo.ValidateTagScope(opts.Scope)
}

func tagsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &tagsOpts{}

	cmd := &cobra.Command{
		Use:   "tags",
		Short: "List trace tag names",
		Long:  "List all trace tag names from a Tempo datasource, optionally filtered by scope and TraceQL query.",
		Example: `
  # List all tags (use datasource UID, not name)
  gcx traces tags -d <datasource-uid>

  # List tags for a specific scope
  gcx traces tags -d <datasource-uid> --scope resource

  # Filter tags with a TraceQL query
  gcx traces tags -d <datasource-uid> -q '{ span.http.status_code >= 500 }'

  # Output as JSON
  gcx traces tags -d <datasource-uid> -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID := opts.Datasource
			if datasourceUID == "" {
				fullCfg, err := loader.LoadFullConfig(ctx)
				if err != nil {
					return err
				}
				datasourceUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "tempo")
			}
			if datasourceUID == "" {
				return errors.New("datasource UID is required: use -d flag or set datasources.tempo in config")
			}

			client, err := tempo.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Tags(ctx, datasourceUID, tempo.TagsRequest{
				Scope: opts.Scope,
				Query: opts.Query,
			})
			if err != nil {
				return fmt.Errorf("failed to get tags: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return tempo.FormatTagsTable(cmd.OutOrStdout(), resp)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type tempoTagsTableCodec struct{}

func (c *tempoTagsTableCodec) Format() format.Format {
	return "table"
}

func (c *tempoTagsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*tempo.TagsResponse)
	if !ok {
		return errors.New("invalid data type for tempo tags table codec")
	}

	return tempo.FormatTagsTable(w, resp)
}

func (c *tempoTagsTableCodec) Decode(io.Reader, any) error {
	return errors.New("tempo tags table codec does not support decoding")
}
