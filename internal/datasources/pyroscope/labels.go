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
	Expr       string
}

func (opts *pyroscopeLabelsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &pyroscopeLabelsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
	flags.StringVarP(&opts.Label, "label", "l", "", "Get values for this label (omit to list all labels)")
	flags.StringVar(&opts.Expr, "expr", "", "Label selector to scope the results (alternative to positional argument)")
	opts.SetupTimeFlags(flags)
}

func (opts *pyroscopeLabelsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return opts.ValidateTimeRange()
}

// resolveExpr resolves the optional label selector from the positional
// argument or --expr. Unlike query commands, no selector is a valid input.
func (opts *pyroscopeLabelsOpts) resolveExpr(args []string) (string, error) {
	if opts.Expr != "" && len(args) > 0 {
		return "", errors.New("provide the selector as a positional argument or via --expr, not both")
	}
	if opts.Expr != "" {
		return opts.Expr, nil
	}
	if len(args) > 0 {
		return args[0], nil
	}
	return "", nil
}

func LabelsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pyroscopeLabelsOpts{}

	cmd := &cobra.Command{
		Use:   "labels [EXPR]",
		Short: "List labels or label values",
		Long: `List all labels or get values for a specific label from a Pyroscope datasource.

EXPR is an optional label selector (e.g., '{service_name="frontend"}') that
scopes the results to matching series.`,
		Example: `
	# List all labels (use datasource UID, not name)
	gcx datasources pyroscope labels -d UID

	# Get values for a specific label
	gcx datasources pyroscope labels -d UID --label service_name

	# Labels present on series matching a selector
	gcx datasources pyroscope labels -d UID '{service_name="frontend"}'

	# Values of a label, scoped to a selector
	gcx datasources pyroscope labels -d UID '{namespace="prod"}' -l service_name

	# Search a wider window than the default last hour
	gcx datasources pyroscope labels -d UID --since 24h

	# Output as JSON
	gcx datasources pyroscope labels -d UID -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			expr, err := opts.resolveExpr(args)
			if err != nil {
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

			var matchers []string
			if expr != "" {
				matchers = []string{expr}
			}

			if opts.Label != "" {
				resp, err := client.LabelValues(ctx, datasourceUID, pyroscope.LabelValuesRequest{
					Name:     opts.Label,
					Matchers: matchers,
					Start:    start,
					End:      end,
				})
				if err != nil {
					return fmt.Errorf("failed to get label values: %w", err)
				}

				if len(resp.Names) == 0 {
					emitEmptyWindowHint(cmd.ErrOrStderr(), scopedSubject(fmt.Sprintf("values for label %q", opts.Label), expr), start, end, opts.IsRange())
				}
				if opts.IO.OutputFormat == "table" {
					return pyroscope.FormatLabelsTable(cmd.OutOrStdout(), resp.Names)
				}
				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}

			resp, err := client.LabelNames(ctx, datasourceUID, pyroscope.LabelNamesRequest{
				Matchers: matchers,
				Start:    start,
				End:      end,
			})
			if err != nil {
				return fmt.Errorf("failed to get labels: %w", err)
			}

			if len(resp.Names) == 0 {
				emitEmptyWindowHint(cmd.ErrOrStderr(), scopedSubject("labels", expr), start, end, opts.IsRange())
			}
			if opts.IO.OutputFormat == "table" {
				return pyroscope.FormatLabelsTable(cmd.OutOrStdout(), resp.Names)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources pyroscope labels -d UID '{service_name=\"frontend\"}' --since 1h -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

// scopedSubject appends the selector to an empty-window hint subject when the
// request was selector-scoped.
func scopedSubject(subject, expr string) string {
	if expr == "" {
		return subject
	}
	return subject + " matching " + expr
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
