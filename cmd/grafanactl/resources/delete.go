package resources

import (
	"context"
	"errors"
	"fmt"

	cmdconfig "github.com/grafana/grafanactl/cmd/grafanactl/config"
	"github.com/grafana/grafanactl/cmd/grafanactl/fail"
	cmdio "github.com/grafana/grafanactl/cmd/grafanactl/io"
	"github.com/grafana/grafanactl/internal/config"
	"github.com/grafana/grafanactl/internal/format"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/discovery"
	"github.com/grafana/grafanactl/internal/resources/local"
	"github.com/grafana/grafanactl/internal/resources/remote"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type deleteOpts struct {
	OnError       OnErrorMode
	Force         bool
	MaxConcurrent int
	DryRun        bool
	Path          []string
	Yes           bool
}

func (opts *deleteOpts) setup(flags *pflag.FlagSet) {
	bindOnErrorFlag(flags, &opts.OnError)
	flags.IntVar(&opts.MaxConcurrent, "max-concurrent", 10, "Maximum number of concurrent operations")
	flags.BoolVar(&opts.Force, "force", opts.Force, "Delete all resources of the specified resource types")
	flags.BoolVar(&opts.DryRun, "dry-run", opts.DryRun, "If set, the delete operation will be simulated")
	flags.StringSliceVarP(&opts.Path, "path", "p", nil, "Path on disk containing the resources to delete")
	flags.BoolVarP(&opts.Yes, "yes", "y", false, "Auto-approve destructive operations (automatically enables --force)")
}

func (opts *deleteOpts) Validate(args []string) error {
	if opts.MaxConcurrent < 1 {
		return errors.New("max-concurrent must be greater than zero")
	}

	if len(args) == 0 && len(opts.Path) == 0 {
		return errors.New("either --path or resource selectors need to be specified")
	}

	return opts.OnError.Validate()
}

func deleteCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &deleteOpts{}

	cmd := &cobra.Command{
		Use:   "delete [RESOURCE_SELECTOR]...",
		Args:  cobra.ArbitraryArgs,
		Short: "Delete resources from Grafana",
		Long:  "Delete resources from Grafana.",
		Example: `
	# Delete a single dashboard
	grafanactl resources delete dashboards/some-dashboard

	# Delete multiple dashboards
	grafanactl resources delete dashboards/some-dashboard,other-dashboard

	# Delete a dashboard and a folder
	grafanactl resources delete dashboards/some-dashboard folders/some-folder

	# Delete every dashboard
	grafanactl resources delete dashboards --force

	# Delete every resource defined in the given directory
	grafanactl resources delete -p ./unwanted-resources/

	# Delete every dashboard defined in the given directory
	grafanactl resources delete -p ./unwanted-resources/ dashboard

	# Delete all dashboards with auto-approval
	grafanactl resources delete dashboards --yes

	# Delete all dashboards using environment variable
	GRAFANACTL_AUTO_APPROVE=1 grafanactl resources delete dashboards
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if err := opts.Validate(args); err != nil {
				return err
			}

			// Apply auto-approval if enabled
			cliOpts, err := config.LoadCLIOptions()
			if err != nil {
				return err
			}

			if (opts.Yes || cliOpts.AutoApprove) && !opts.Force {
				cmdio.Info(cmd.OutOrStdout(), "Auto-approval enabled: automatically setting --force")
				opts.Force = true
			}

			cfg, err := configOpts.LoadRESTConfig(ctx)
			if err != nil {
				return err
			}

			sels, err := resources.ParseSelectors(args)
			if err != nil {
				return err
			}

			if !opts.Force && !sels.HasNamedSelectorsOnly() {
				return fail.DetailedError{
					Summary: "Invalid resource selector",
					Details: "Expected a resource selector targeting named resources only. Example: dashboard/some-dashboard",
					Suggestions: []string{
						"Specify the --force flag to force the deletion.",
					},
				}
			}

			var res resources.Resources

			// Load resources by selectors only
			if len(opts.Path) == 0 {
				fetchRes, err := fetchResources(ctx, fetchRequest{
					Config:      cfg,
					StopOnError: opts.OnError.StopOnError(),
				}, args)
				if err != nil {
					return err
				}

				res = fetchRes.Resources
			} else {
				// Load resources from the filesystem
				res = *resources.NewResources()
				if err := loadResourcesFromDirectories(ctx, cfg, &res, opts, sels); err != nil {
					return err
				}
			}

			if opts.DryRun {
				cmdio.Info(cmd.OutOrStdout(), "Dry-run mode enabled")
			}

			// Delete!
			deleter, err := remote.NewDeleter(ctx, cfg)
			if err != nil {
				return err
			}

			req := remote.DeleteRequest{
				Resources:      &res,
				MaxConcurrency: opts.MaxConcurrent,
				StopOnError:    opts.OnError.StopOnError(),
				DryRun:         opts.DryRun,
			}

			summary, err := deleter.Delete(ctx, req)
			if err != nil {
				if summary != nil {
					cmdio.Warning(cmd.OutOrStdout(), "%d resources deleted, %d errors (aborted)", summary.SuccessCount(), summary.FailedCount())
				}
				return err
			}

			// Reporting time.
			printer := cmdio.Success
			if summary.FailedCount() != 0 {
				printer = cmdio.Warning
				if summary.SuccessCount() == 0 {
					printer = cmdio.Error
				}
			}

			printer(cmd.OutOrStdout(), "%d resources deleted, %d errors", summary.SuccessCount(), summary.FailedCount())

			if opts.OnError.FailOnErrors() && summary.FailedCount() > 0 {
				return fmt.Errorf("%d resource(s) failed to delete", summary.FailedCount())
			}

			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func loadResourcesFromDirectories(ctx context.Context, cfg config.NamespacedRESTConfig, res *resources.Resources, opts *deleteOpts, selectors resources.Selectors) error {
	reg, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return err
	}

	reader := local.FSReader{
		Decoders:           format.Codecs(),
		MaxConcurrentReads: opts.MaxConcurrent,
		StopOnError:        opts.OnError.StopOnError(),
	}

	filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{
		Selectors: selectors,
	})
	if err != nil {
		return err
	}

	return reader.Read(ctx, res, filters, opts.Path)
}
