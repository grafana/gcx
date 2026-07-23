package pyroscope

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type pyroscopeLabelsOpts struct {
	dsquery.TimeRangeOpts

	IO         cmdio.Options
	Datasource string
	Label      string
}

func (opts *pyroscopeLabelsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &pyroscopeLabelsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
	flags.StringVarP(&opts.Label, "label", "l", "", "Get values for this label (omit to list all labels)")
	opts.SetupTimeFlags(flags)
}

func (opts *pyroscopeLabelsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return opts.ValidateTimeRange()
}

func LabelsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pyroscopeLabelsOpts{}

	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List labels or label values",
		Long:  "List all labels or get values for a specific label from a Pyroscope datasource.",
		Example: `
	# List all labels (use datasource UID, not name)
	gcx datasources pyroscope labels -d UID

	# Get values for a specific label
	gcx datasources pyroscope labels -d UID --label service_name

	# Search a wider window than the default last hour
	gcx datasources pyroscope labels -d UID --since 24h

	# Output as JSON
	gcx datasources pyroscope labels -d UID -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pyroscope")
			if err != nil {
				return err
			}

			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			start, end, err := opts.ParseTimeRange(time.Now())
			if err != nil {
				return err
			}

			if opts.Label != "" {
				resp, err := client.LabelValues(ctx, datasourceUID, pyroscope.LabelValuesRequest{
					Name:  opts.Label,
					Start: start,
					End:   end,
				})
				if err != nil {
					return fmt.Errorf("failed to get label values: %w", err)
				}

				if len(resp.Names) == 0 {
					emitEmptyWindowHint(cmd.ErrOrStderr(), fmt.Sprintf("values for label %q", opts.Label), start, end, opts.IsRange())
				}
				if opts.IO.OutputFormat == "table" {
					return pyroscope.FormatLabelsTable(cmd.OutOrStdout(), resp.Names)
				}
				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}

			resp, err := client.LabelNames(ctx, datasourceUID, pyroscope.LabelNamesRequest{
				Start: start,
				End:   end,
			})
			if err != nil {
				return fmt.Errorf("failed to get labels: %w", err)
			}

			if len(resp.Names) == 0 {
				emitEmptyWindowHint(cmd.ErrOrStderr(), "labels", start, end, opts.IsRange())
			}
			if opts.IO.OutputFormat == "table" {
				return pyroscope.FormatLabelsTable(cmd.OutOrStdout(), resp.Names)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources pyroscope labels -d UID --since 1h -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type pyroscopeLabelsTableCodec struct{}

func (c *pyroscopeLabelsTableCodec) Format() format.Format {
	return "table"
}

func (c *pyroscopeLabelsTableCodec) Encode(w io.Writer, data any) error {
	switch v := data.(type) {
	case *pyroscope.LabelNamesResponse:
		return pyroscope.FormatLabelsTable(w, v.Names)
	case *pyroscope.LabelValuesResponse:
		return pyroscope.FormatLabelsTable(w, v.Names)
	default:
		return errors.New("invalid data type for pyroscope labels table codec")
	}
}

func (c *pyroscopeLabelsTableCodec) Decode(io.Reader, any) error {
	return errors.New("pyroscope labels table codec does not support decoding")
}
