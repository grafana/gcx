package traces

import (
	"errors"
	"fmt"
	"io"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type labelsOpts struct {
	IO         cmdio.Options
	Datasource string
	Label      string
	Scope      string
	Query      string
}

func (opts *labelsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &tempoLabelsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	flags.StringVarP(&opts.Label, "label", "l", "", "Get values for this label (omit to list all labels)")
	flags.StringVar(&opts.Scope, "scope", "", "Tag scope filter (resource, span, event, link, instrumentation)")
	flags.StringVarP(&opts.Query, "query", "q", "", "TraceQL query to filter labels")
}

func (opts *labelsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return tempo.ValidateTagScope(opts.Scope)
}

// labelsCmd returns the `labels` subcommand for Tempo tag/label discovery.
// It also registers `tags` as a non-deprecated alias.
func labelsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &labelsOpts{}

	cmd := &cobra.Command{
		Use:     "labels",
		Aliases: []string{"tags"},
		Short:   "List trace labels or label values",
		Long: `List all trace labels or get values for a specific label from a Tempo datasource.

When -l/--label is provided, returns values for that label.
When -l is omitted, returns all label names.

Datasource is resolved from -d flag or datasources.tempo in your context.`,
		Example: `
  # List all labels
  gcx traces labels -d <datasource-uid>

  # Get values for a specific label
  gcx traces labels -d <datasource-uid> -l service.name

  # Using the tags alias
  gcx traces tags -d <datasource-uid> -l service.name

  # Filter by scope
  gcx traces labels -d <datasource-uid> -l service.name --scope span

  # Filter with a TraceQL query
  gcx traces labels -d <datasource-uid> -q '{ span.http.status_code >= 500 }'

  # Output as JSON
  gcx traces labels -d <datasource-uid> -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err == nil {
				cfgCtx = fullCfg.GetCurrentContext()
			}

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "tempo")
			if err != nil {
				return err
			}

			client, err := tempo.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			// When -l is set, get values for that label; otherwise list all labels.
			if opts.Label != "" {
				resp, err := client.TagValues(ctx, datasourceUID, tempo.TagValuesRequest{
					Tag:   opts.Label,
					Scope: opts.Scope,
					Query: opts.Query,
				})
				if err != nil {
					return fmt.Errorf("failed to get label values: %w", err)
				}

				if opts.IO.OutputFormat == "table" {
					return tempo.FormatTagValuesTable(cmd.OutOrStdout(), resp)
				}

				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}

			resp, err := client.Tags(ctx, datasourceUID, tempo.TagsRequest{
				Scope: opts.Scope,
				Query: opts.Query,
			})
			if err != nil {
				return fmt.Errorf("failed to get labels: %w", err)
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

// tempoLabelsTableCodec renders either TagsResponse or TagValuesResponse as a table.
type tempoLabelsTableCodec struct{}

func (c *tempoLabelsTableCodec) Format() format.Format {
	return "table"
}

func (c *tempoLabelsTableCodec) Encode(w io.Writer, data any) error {
	switch resp := data.(type) {
	case *tempo.TagsResponse:
		return tempo.FormatTagsTable(w, resp)
	case *tempo.TagValuesResponse:
		return tempo.FormatTagValuesTable(w, resp)
	default:
		return errors.New("invalid data type for tempo labels table codec")
	}
}

func (c *tempoLabelsTableCodec) Decode(io.Reader, any) error {
	return errors.New("tempo labels table codec does not support decoding")
}
