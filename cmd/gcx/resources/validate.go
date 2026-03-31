package resources

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type validateOpts struct {
	IO cmdio.Options

	Paths         []string
	MaxConcurrent int
	OnError       OnErrorMode
}

func (opts *validateOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("text", &validationTableCodec{})
	opts.IO.DefaultFormat("text")

	opts.IO.BindFlags(flags)

	flags.StringSliceVarP(&opts.Paths, "path", "p", []string{defaultResourcesPath}, "Paths on disk from which to read the resources.")
	flags.IntVar(&opts.MaxConcurrent, "max-concurrent", 10, "Maximum number of concurrent operations")
	bindOnErrorFlag(flags, &opts.OnError)
}

func (opts *validateOpts) Validate() error {
	if len(opts.Paths) == 0 {
		return errors.New("at least one path is required")
	}

	if opts.MaxConcurrent < 1 {
		return errors.New("max-concurrent must be greater than zero")
	}

	return opts.OnError.Validate()
}

func validateCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &validateOpts{}

	cmd := &cobra.Command{
		Use:   "validate [RESOURCE_SELECTOR]...",
		Args:  cobra.ArbitraryArgs,
		Short: "Validate local resources against a Grafana instance",
		Long:  `Validate local resource files against a remote Grafana instance. Requires a live connection to Grafana for server-side validation. Reads resources from disk and reports validation errors per resource.`,
		Example: `
	# Validate all resources in the default directory
	gcx resources validate

	# Validate a single resource kind
	gcx resources validate dashboards

	# Validate a multiple resource kinds
	gcx resources validate dashboards folders

	# Displaying validation results as YAML
	gcx resources validate -o yaml

	# Displaying validation results as JSON
	gcx resources validate -o json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if err := opts.Validate(); err != nil {
				return err
			}

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			sels, err := resources.ParseSelectors(args)
			if err != nil {
				return err
			}

			reg, err := discovery.NewDefaultRegistry(ctx, cfg)
			if err != nil {
				return err
			}

			filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{
				Selectors: sels,
			})
			if err != nil {
				return err
			}

			reader := local.FSReader{
				Decoders:           format.Codecs(),
				MaxConcurrentReads: opts.MaxConcurrent,
				StopOnError:        opts.OnError.StopOnError(),
			}

			resourcesList := resources.NewResources()

			if err := reader.Read(ctx, resourcesList, filters, opts.Paths); err != nil {
				return err
			}

			pusher, err := remote.NewDefaultPusher(ctx, cfg)
			if err != nil {
				return err
			}

			req := remote.PushRequest{
				Resources:        resourcesList,
				MaxConcurrency:   opts.MaxConcurrent,
				StopOnError:      opts.OnError.StopOnError(),
				DryRun:           true,
				NoPushFailureLog: true,
			}

			summary, err := pusher.Push(ctx, req)
			if err != nil {
				return err
			}

			if summary.FailedCount() == 0 && opts.IO.OutputFormat == "text" {
				cmdio.Success(cmd.OutOrStdout(), "No errors found.")
				return nil
			}

			if opts.IO.OutputFormat == "text" {
				if err := opts.IO.Encode(cmd.OutOrStdout(), summary); err != nil {
					return err
				}
			} else {
				printableSummary := struct {
					Failures []map[string]string `json:"failures" yaml:"failures"`
				}{
					Failures: make([]map[string]string, 0),
				}

				for _, failure := range summary.Failures() {
					file := ""
					if failure.Resource != nil {
						file = failure.Resource.SourcePath()
					}
					printableSummary.Failures = append(printableSummary.Failures, map[string]string{
						"file":  file,
						"error": failure.Error.Error(),
					})
				}

				if err := opts.IO.Encode(cmd.OutOrStdout(), printableSummary); err != nil {
					return err
				}
			}

			if opts.OnError.FailOnErrors() && summary.FailedCount() > 0 {
				return fmt.Errorf("%d resource(s) failed to validate", summary.FailedCount())
			}

			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type validationTableCodec struct{}

func (c *validationTableCodec) Format() format.Format {
	return "text"
}

func (c *validationTableCodec) Encode(output io.Writer, input any) error {
	//nolint:forcetypeassert
	summary := input.(*remote.OperationSummary)

	tab := tabwriter.NewWriter(output, 0, 4, 2, ' ', tabwriter.TabIndent|tabwriter.DiscardEmptyColumns)

	fmt.Fprintf(tab, "FILE\tERROR\n")
	for _, failure := range summary.Failures() {
		file := ""
		if failure.Resource != nil {
			file = failure.Resource.SourcePath()
		}
		fmt.Fprintf(tab, "%s\t%s\n", file, failure.Error)
	}

	return tab.Flush()
}

func (c *validationTableCodec) Decode(io.Reader, any) error {
	return errors.New("codec does not support decoding")
}
