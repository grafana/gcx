package resources

import (
	"errors"
	"fmt"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/gcx/internal/resources/process"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	defaultResourcesPath = "./resources"
)

type pullOpts struct {
	IO             cmdio.Options
	OnError        OnErrorMode
	IncludeManaged bool
	Path           string
}

func (opts *pullOpts) setup(flags *pflag.FlagSet) {
	// Bind all the flags
	opts.IO.BindFlags(flags)

	bindOnErrorFlag(flags, &opts.OnError)
	flags.StringVarP(&opts.Path, "path", "p", defaultResourcesPath, "Path on disk in which the resources will be written")
	flags.BoolVar(
		&opts.IncludeManaged,
		"include-managed",
		opts.IncludeManaged,
		"Include resources managed by tools other than gcx",
	)
}

func (opts *pullOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}

	if opts.Path == "" {
		return errors.New("--path is required")
	}

	return opts.OnError.Validate()
}

func pullCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &pullOpts{}

	cmd := &cobra.Command{
		Use:   "pull [RESOURCE_SELECTOR]...",
		Args:  cobra.ArbitraryArgs,
		Short: "Pull resources from Grafana",
		Long:  "Pull resources from Grafana using a specific format. See examples below for more details.",
		Example: `
	# Everything:

	gcx resources pull

	# All instances for a given kind(s):

	gcx resources pull dashboards
	gcx resources pull dashboards folders

	# Single resource kind, one or more resource instances:

	gcx resources pull dashboards/foo
	gcx resources pull dashboards/foo,bar

	# Single resource kind, long kind format:

	gcx resources pull dashboard.dashboards/foo
	gcx resources pull dashboard.dashboards/foo,bar

	# Single resource kind, long kind format with version:

	gcx resources pull dashboards.v1alpha1.dashboard.grafana.app/foo
	gcx resources pull dashboards.v1alpha1.dashboard.grafana.app/foo,bar

	# Multiple resource kinds, one or more resource instances:

	gcx resources pull dashboards/foo folders/qux
	gcx resources pull dashboards/foo,bar folders/qux,quux

	# Multiple resource kinds, long kind format:

	gcx resources pull dashboard.dashboards/foo folder.folders/qux
	gcx resources pull dashboard.dashboards/foo,bar folder.folders/qux,quux

	# Multiple resource kinds, long kind format with version:

	gcx resources pull dashboards.v1alpha1.dashboard.grafana.app/foo folders.v1alpha1.folder.grafana.app/qux

	# Provider-backed resource types (SLO, Synthetic Monitoring, Alerting):

	gcx resources pull slo -p ./slo-defs/
	gcx resources pull checks -p ./checks/
	gcx resources pull rules -p ./rules/`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if err := opts.Validate(); err != nil {
				return err
			}

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
			}

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			res, err := FetchResources(ctx, FetchRequest{
				Config: cfg,
				// Strip server fields from the resources.
				// This includes fields like `resourceVersion`, `uid`, etc.
				Processors: []remote.Processor{
					&process.ServerFieldsStripper{},
				},
				ExcludeManaged: !opts.IncludeManaged,
				StopOnError:    opts.OnError.StopOnError(),
			}, args)
			if err != nil {
				return err
			}

			writer := local.FSWriter{
				Path:        opts.Path,
				Namer:       local.GroupResourcesByKind(opts.IO.OutputFormat, local.PluralsFromFilters(res.Filters)),
				Encoder:     codec,
				StopOnError: opts.OnError.StopOnError(),
			}

			if err := writer.Write(ctx, &res.Resources); err != nil {
				return err
			}

			pullSummary := res.PullSummary

			printer := cmdio.Success
			if pullSummary.FailedCount() != 0 {
				printer = cmdio.Warning
				if pullSummary.SuccessCount() == 0 {
					printer = cmdio.Error
				}
			}

			if skipped := pullSummary.SkippedCount(); skipped > 0 {
				printer(cmd.OutOrStdout(), "%d resources pulled, %d errors (%d resource types skipped — not listable)", pullSummary.SuccessCount(), pullSummary.FailedCount(), skipped)
			} else {
				printer(cmd.OutOrStdout(), "%d resources pulled, %d errors", pullSummary.SuccessCount(), pullSummary.FailedCount())
			}

			if opts.OnError.FailOnErrors() && pullSummary.FailedCount() > 0 {
				return fmt.Errorf("%d resource(s) failed to pull", pullSummary.FailedCount())
			}

			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}
