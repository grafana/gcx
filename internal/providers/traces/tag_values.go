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

type tagValuesOpts struct {
	IO         cmdio.Options
	Datasource string
	Scope      string
	Query      string
}

func (opts *tagValuesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &tempoTagValuesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	flags.StringVar(&opts.Scope, "scope", "", "Tag scope filter (resource, span, event, link, instrumentation)")
	flags.StringVarP(&opts.Query, "query", "q", "", "TraceQL query to filter tag values")
}

func (opts *tagValuesOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return tempo.ValidateTagScope(opts.Scope)
}

func tagValuesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &tagValuesOpts{}

	cmd := &cobra.Command{
		Use:   "tag-values TAG",
		Short: "List values for a trace tag",
		Long:  "List values for a specific trace tag from a Tempo datasource, optionally filtered by scope and TraceQL query.",
		Example: `
  # List values for a tag (use datasource UID, not name)
  gcx traces tag-values service.name -d <datasource-uid>

  # Filter by scope
  gcx traces tag-values service.name -d <datasource-uid> --scope resource

  # Filter with a TraceQL query
  gcx traces tag-values http.status_code -d <datasource-uid> -q '{ span.http.method = "GET" }'

  # Output as JSON
  gcx traces tag-values service.name -d <datasource-uid> -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			resp, err := client.TagValues(ctx, datasourceUID, tempo.TagValuesRequest{
				Tag:   args[0],
				Scope: opts.Scope,
				Query: opts.Query,
			})
			if err != nil {
				return fmt.Errorf("failed to get tag values: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return tempo.FormatTagValuesTable(cmd.OutOrStdout(), resp)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type tempoTagValuesTableCodec struct{}

func (c *tempoTagValuesTableCodec) Format() format.Format {
	return "table"
}

func (c *tempoTagValuesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*tempo.TagValuesResponse)
	if !ok {
		return errors.New("invalid data type for tempo tag values table codec")
	}

	return tempo.FormatTagValuesTable(w, resp)
}

func (c *tempoTagValuesTableCodec) Decode(io.Reader, any) error {
	return errors.New("tempo tag values table codec does not support decoding")
}
