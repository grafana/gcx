package resources

import (
	"errors"
	"io"
	"path/filepath"
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/observation"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/grafana/gcx/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const experimentalObservationPreamble = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions."

type captureOpts struct {
	Path string
}

func (opts *captureOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&opts.Path, "path", "p", "", "Directory to write the capture to; it must not exist or must be empty")
}

func (opts *captureOpts) Validate() error {
	if opts.Path == "" {
		return &fail.UsageError{Message: "--path is required"}
	}
	return nil
}

func captureCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &captureOpts{}

	cmd := &cobra.Command{
		Use:   "capture [RESOURCE_TYPE]...",
		Short: "[experimental] Capture selected resource types for a later diff",
		Long: experimentalObservationPreamble + `

Capture records the observable state of selected resource types on the current
stack, so that a later capture of the same stack can be compared with
'gcx resources diff'. It is read-only.

Supported resource types: ` + strings.Join(observation.SupportedTypes(), ", ") + `.
Without arguments, every supported type is captured. Arguments select whole
resource types; resource names are not accepted.

The capture directory holds one normalized JSON file per resource and a
manifest.json that records the stack, the selected scope, the coverage of each
type, and an inventory of the files. The manifest is written last and the
directory is moved into place only when it is complete, so an interrupted
capture never looks finished. The destination must not exist or must be empty.

Coverage is reported per type. A type that is unavailable, fails to read, or is
truncated is recorded as incomplete and the command exits with code 4; the
capture is still written. Use an administrative context: ordinary permissions
can hide resources, and admin access is not verified.

Captures can contain sensitive configuration and query text. Files are
readable by the owner only. Secret values are never captured: only the names of
configured secrets are recorded.`,
		Example: `
	# Capture every supported type
	gcx resources capture -p ./before

	# Capture dashboards and folders only
	gcx resources capture dashboards folders -p ./before`,
		Args: cobra.ArbitraryArgs,
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "Capture before and after a workflow with the same types, then run gcx resources diff BEFORE AFTER",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			reg, err := discovery.NewDefaultRegistry(ctx, cfg)
			if err != nil {
				return err
			}

			selections, err := observation.ResolveSelections(reg, args, cmd.ErrOrStderr())
			if unsupported, ok := errors.AsType[*observation.UnsupportedSelectorError](err); ok {
				return &fail.UsageError{Message: unsupported.Error()}
			}
			if err != nil {
				return err
			}

			puller, err := remote.NewDefaultPullerWithRegistry(cfg, reg)
			if err != nil {
				return err
			}

			server := cfg.GrafanaURL
			if server == "" {
				server = cfg.Host
			}

			capture, err := observation.Collect(ctx, observation.CollectRequest{
				Stack: observation.StackIdentity{
					Server:    observation.ServerIdentity(server),
					Namespace: cfg.Namespace,
					Context:   configOpts.EffectiveContext(),
				},
				Selectors:  args,
				Selections: selections,
				Puller:     puller,
				GCXVersion: version.Get(),
			})
			if err != nil {
				return err
			}

			if err := observation.Write(capture, opts.Path); err != nil {
				if errors.Is(err, observation.ErrDestinationNotEmpty) {
					return gcxerrors.DetailedError{
						Summary: "Capture destination is not empty",
						Details: "A capture never merges with or overwrites earlier files. Choose a new --path or remove the existing directory.",
						Parent:  err,
					}
				}
				return err
			}

			return emitCaptureReceipt(cmd, capture, opts.Path)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

func emitCaptureReceipt(cmd *cobra.Command, capture *observation.Capture, path string) error {
	receipt := cmdio.NewArtifactReceipt("captured", "json")
	receipt.Dir = path
	receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: filepath.Join(path, observation.ManifestFile)})

	total := 0
	var incomplete []observation.TypeCoverage
	for _, cov := range capture.Manifest.Types {
		total += cov.Count
		if cov.Count > 0 {
			receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: filepath.Join(path, cov.Type), Kind: cov.Kind, Count: cov.Count})
		}
		if !cov.Complete() {
			incomplete = append(incomplete, cov)
			receipt.Failures = append(receipt.Failures, cmdio.MutationFailure{
				Target: cmdio.MutationTarget{Kind: cov.Type},
				Error:  cov.Status + ": " + strings.Join(cov.Reasons, "; "),
			})
		}
	}
	receipt.Summary = cmdio.MutationSummary{Succeeded: total, Failed: len(incomplete)}

	emitErr := cmdio.EmitArtifactResult(cmd.OutOrStdout(), receipt, func(w io.Writer) error {
		printer := cmdio.Success
		if len(incomplete) > 0 {
			printer = cmdio.Warning
		}
		printer(w, "%d resources captured across %d types to %s", total, len(capture.Manifest.Types), path)
		for _, cov := range incomplete {
			cmdio.Warning(w, "%s is %s: %s", cov.Type, cov.Status, strings.Join(cov.Reasons, "; "))
		}
		return nil
	})
	if emitErr != nil {
		return emitErr
	}

	if len(incomplete) > 0 {
		return partialBatchFailure(cmd.ErrOrStderr(), "capture", len(capture.Manifest.Types), len(incomplete))
	}
	return nil
}
