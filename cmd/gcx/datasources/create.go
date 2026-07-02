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

type createOpts struct {
	IO          cmdio.Options
	File        string
	SecretsFile string
	DryRun      bool
}

func (opts *createOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.File, "filename", "f", "", "File containing the datasource manifest (use - for stdin)")
	flags.StringVar(&opts.SecretsFile, "secrets-file", "", "File containing secret values to merge into the secure block")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "Render the object that would be created without writing it")
	opts.IO.DefaultFormat("yaml")
	opts.IO.BindFlags(flags)
}

func (opts *createOpts) Validate() error {
	if opts.File == "" {
		return errors.New("--filename/-f is required")
	}
	return opts.IO.Validate()
}

func createCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &createOpts{}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a datasource from a manifest file",
		Long: `Create a datasource instance from a declarative manifest file.

The manifest is a Kubernetes-style envelope. spec.type is the plugin ID
(e.g. grafana-sentry-datasource) and selects the API group. Secret values go
in the top-level secure block via {create: <value>}, {fromEnv: <VAR>}, or
{fromFile: <path>}; they are never returned on read.`,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "-f datasource.yaml",
		},
		Example: `
	# Create a datasource from a YAML manifest
	gcx datasources create -f sentry.yaml

	# Create from stdin
	cat sentry.yaml | gcx datasources create -f -

	# Preview without writing
	gcx datasources create -f sentry.yaml --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			manifest, err := dsclient.ReadManifestFile(opts.File, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if err := manifest.ResolveSecrets(opts.SecretsFile); err != nil {
				return err
			}

			if opts.DryRun {
				summary := dsclient.DiffManifest(nil, manifest)
				cmdio.Info(cmd.ErrOrStderr(), "Dry run — no datasource was created.\n%s", summary.Render())
				manifest.Sanitize()
				redactSecrets(manifest)
				return opts.IO.Encode(cmd.OutOrStdout(), manifest)
			}

			restCfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			transport, err := dsclient.NewTransport(restCfg)
			if err != nil {
				return err
			}

			ds := manifest.ToDatasource()
			dsclient.WarnIfSecretMissing(ds)

			created, err := transport.Create(ctx, ds)
			if err != nil {
				return fmt.Errorf("failed to create datasource: %w", err)
			}

			cmdio.Success(cmd.ErrOrStderr(), "Created datasource %q (uid=%s)", created.Name, created.UID)
			return opts.IO.Encode(cmd.OutOrStdout(), dsclient.ManifestFromDatasource(created))
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags())
	return cmd
}

// redactSecrets removes resolved secret values from a manifest before it is
// printed (e.g. during --dry-run), replacing each entry with a redaction
// marker so the value is never disclosed.
func redactSecrets(m *dsclient.DataSourceManifest) {
	for k, sv := range m.Secure {
		if sv.Remove {
			m.Secure[k] = dsclient.SecureValue{Remove: true}
			continue
		}
		m.Secure[k] = dsclient.SecureValue{Name: "<redacted>"}
	}
}
