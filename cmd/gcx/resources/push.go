package resources

import (
	"errors"
	"fmt"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/gcx/internal/resources/process"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type pushOpts struct {
	Paths             []string
	MaxConcurrent     int
	OnError           OnErrorMode
	DryRun            bool
	OmitManagerFields bool
	IncludeManaged    bool
}

func (opts *pushOpts) setup(flags *pflag.FlagSet) {
	flags.StringSliceVarP(&opts.Paths, "path", "p", []string{defaultResourcesPath}, "Paths on disk from which to read the resources to push")
	flags.IntVar(&opts.MaxConcurrent, "max-concurrent", 10, "Maximum number of concurrent operations")
	bindOnErrorFlag(flags, &opts.OnError)
	flags.BoolVar(&opts.DryRun, "dry-run", opts.DryRun, "If set, the push operation will be simulated, without actually creating or updating any resources")
	flags.BoolVar(&opts.OmitManagerFields, "omit-manager-fields", opts.OmitManagerFields, "If set, the manager fields will not be appended to the resources")
	flags.BoolVar(&opts.IncludeManaged, "include-managed", opts.IncludeManaged, "If set, resources managed by other tools will be included in the push operation")
}

func (opts *pushOpts) Validate() error {
	if len(opts.Paths) == 0 {
		return errors.New("at least one path is required")
	}

	if opts.MaxConcurrent < 1 {
		return errors.New("max-concurrent must be greater than zero")
	}

	return opts.OnError.Validate()
}

func pushCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &pushOpts{}

	cmd := &cobra.Command{
		Use:   "push [RESOURCE_SELECTOR]...",
		Args:  cobra.ArbitraryArgs,
		Short: "Push resources to Grafana",
		Long:  "Push resources to Grafana using a specific format. See examples below for more details.",
		Example: `
	# Everything:

	gcx resources push

	# All instances for a given kind(s):

	gcx resources push dashboards
	gcx resources push dashboards folders

	# Single resource kind, one or more resource instances:

	gcx resources push dashboards/foo
	gcx resources push dashboards/foo,bar

	# Single resource kind, long kind format:

	gcx resources push dashboard.dashboards/foo
	gcx resources push dashboard.dashboards/foo,bar

	# Single resource kind, long kind format with version:

	gcx resources push dashboards.v1alpha1.dashboard.grafana.app/foo
	gcx resources push dashboards.v1alpha1.dashboard.grafana.app/foo,bar

	# Multiple resource kinds, one or more resource instances:

	gcx resources push dashboards/foo folders/qux
	gcx resources push dashboards/foo,bar folders/qux,quux

	# Multiple resource kinds, long kind format:

	gcx resources push dashboard.dashboards/foo folder.folders/qux
	gcx resources push dashboard.dashboards/foo,bar folder.folders/qux,quux

	# Multiple resource kinds, long kind format with version:

	gcx resources push dashboards.v1alpha1.dashboard.grafana.app/foo folders.v1alpha1.folder.grafana.app/qux

	# Provider-backed resource types (SLO, Synthetic Monitoring, Alerting):

	gcx resources push slo -p ./slo-defs/
	gcx resources push checks -p ./checks/
	gcx resources push rules -p ./rules/

	# Mixed push: native and provider resources from the same directory
	# (types auto-detected from apiVersion/kind in YAML files):

	gcx resources push -p ./resources/`,
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

			procs := []remote.Processor{
				// Override namespace to match the target context.
				// This ensures resources are pushed to the current context's namespace
				// regardless of the namespace stored in the resource files.
				process.NewNamespaceOverrider(cfg.Namespace),
			}
			if !opts.OmitManagerFields {
				procs = append(procs, &process.ManagerFieldsAppender{})
			}

			req := remote.PushRequest{
				Resources:      resourcesList,
				MaxConcurrency: opts.MaxConcurrent,
				StopOnError:    opts.OnError.StopOnError(),
				DryRun:         opts.DryRun,
				Processors:     procs,
				IncludeManaged: opts.IncludeManaged,
			}

			var summary *remote.OperationSummary
			err = style.RunWithSpinner(cmd.ErrOrStderr(), "Pushing resources…", func() error {
				var pushErr error
				summary, pushErr = pusher.Push(ctx, req)
				return pushErr
			})
			if err != nil {
				return err
			}

			style.RenderSummary(cmd.OutOrStdout(), "pushed", summary.SuccessCount(), summary.FailedCount(), 0)

			if opts.OnError.FailOnErrors() && summary.FailedCount() > 0 {
				return fmt.Errorf("%d resource(s) failed to push", summary.FailedCount())
			}

			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}
