package datasources

import (
	"errors"
	"fmt"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	dsclient "github.com/grafana/gcx/internal/datasources"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type updateOpts struct {
	IO          cmdio.Options
	File        string
	SecretsFile string
	DryRun      bool
}

func (opts *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.File, "filename", "f", "", "File containing the datasource manifest (use - for stdin)")
	flags.StringVar(&opts.SecretsFile, "secrets-file", "", "File containing secret values to merge into the secure block")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "Preview the change without applying it")
	opts.IO.DefaultFormat("yaml")
	opts.IO.BindFlags(flags)
}

func (opts *updateOpts) Validate() error {
	if opts.File == "" {
		return errors.New("--filename/-f is required")
	}
	return opts.IO.Validate()
}

func updateCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &updateOpts{}

	cmd := &cobra.Command{
		Use:   "update UID",
		Short: "Update a datasource from a manifest file",
		Long: `Update an existing datasource instance from a declarative manifest file.

This is a full replace: fields omitted from the manifest are reset. The current
resourceVersion is fetched and applied automatically (optimistic concurrency).
Secret values are updated via the top-level secure block. update does not
prompt; use --dry-run to preview the change.`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "<uid> -f datasource.yaml",
		},
		Example: `
	# Update a datasource from a YAML manifest
	gcx datasources update my-ds-uid -f sentry.yaml

	# Preview the change
	gcx datasources update my-ds-uid -f sentry.yaml --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			uid := args[0]

			manifest, err := dsclient.ReadManifestFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			manifest.Metadata.Name = uid
			if err := manifest.ResolveSecrets(opts.SecretsFile); err != nil {
				return err
			}

			restCfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			transport, err := dsclient.NewTransport(restCfg)
			if err != nil {
				return err
			}

			// Fetch the current object to confirm it exists and to compute the
			// dry-run diff.
			current, err := transport.GetByUID(ctx, uid)
			if err != nil {
				if dsclient.IsNotFound(err) {
					return fmt.Errorf("datasource %q not found", uid)
				}
				return fmt.Errorf("failed to fetch current datasource %q: %w", uid, err)
			}

			if opts.DryRun {
				summary := dsclient.DiffManifest(dsclient.ManifestFromDatasource(current), manifest)
				cmdio.Info(cmd.ErrOrStderr(), "Dry run — no changes applied.\n%s", summary.Render())
				manifest.Sanitize()
				redactSecrets(manifest)
				return opts.IO.Encode(cmd.OutOrStdout(), manifest)
			}

			ds := manifest.ToDatasource()
			dsclient.WarnIfSecretMissing(ds)

			updated, err := transport.Update(ctx, uid, ds)
			if err != nil {
				return fmt.Errorf("failed to update datasource: %w", err)
			}

			cmdio.Success(cmd.ErrOrStderr(), "Updated datasource %q (uid=%s)", updated.Name, updated.UID)
			return opts.IO.Encode(cmd.OutOrStdout(), dsclient.ManifestFromDatasource(updated))
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}
