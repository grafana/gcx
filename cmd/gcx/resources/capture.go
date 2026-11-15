package resources

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
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

// Validate checks the destination before any network I/O. Write checks it
// again, because the directory can change while the capture runs.
func (opts *captureOpts) Validate() error {
	if strings.TrimSpace(opts.Path) == "" {
		return &fail.UsageError{Message: "--path is required, for example: gcx resources capture -p ./before"}
	}
	dest, err := filepath.Abs(opts.Path)
	if err != nil {
		return &fail.UsageError{Message: fmt.Sprintf("invalid --path %q: %v", opts.Path, err)}
	}
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(dest, cwd); err == nil && (rel == "." || !strings.HasPrefix(rel, "..")) {
			return &fail.UsageError{Message: fmt.Sprintf("--path %q is the current directory or contains it; the capture replaces its destination, so choose a new directory, for example: -p ./capture", opts.Path)}
		}
	}
	switch info, err := os.Stat(dest); {
	case err == nil && !info.IsDir():
		return &fail.UsageError{Message: fmt.Sprintf("--path %q is not a directory; choose a new directory", opts.Path)}
	case err == nil:
		entries, err := os.ReadDir(dest)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return &fail.UsageError{Message: fmt.Sprintf("--path %q is not empty; a capture never merges with or overwrites earlier files, so choose a new directory or remove this one", opts.Path)}
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
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
resource types; resource names and other types are usage errors. Data sources
are read one at a time, because their list endpoint omits configuration.

The capture directory holds one normalized JSON file per resource and a
manifest.json that records the stack, the selected scope, the coverage of each
type, and an inventory of the files. The manifest is written last and the
directory is moved into place only when it is complete, so an interrupted
capture never looks finished. The destination must not exist or must be empty.

Coverage is reported per type. A type that is unavailable, fails to read, or is
truncated is recorded as incomplete and the command exits with code 4; the
capture is still written.

Grafana hides resources that the caller cannot read, without an error. The
capture records who took it, and checks that the caller can read every
resource of each type. A type that the caller can read only partly is
incomplete. 'gcx resources diff' rejects captures taken by different callers,
so take both captures with the same context. An administrative context is
the simplest way to get complete reads.

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
			agent.AnnotationLLMHint:   "Capture before and after a workflow with the same types, then run gcx resources diff BEFORE AFTER. Receipt summary counts resource types; per-type resource counts are in files[].count",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()

			cfg, current, err := configOpts.LoadGrafanaConfigWithContext(ctx)
			if err != nil {
				return err
			}
			contextName := configOpts.EffectiveContext()
			if current != nil && current.Name != "" {
				contextName = current.Name
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

			access, err := observation.ReadAccess(ctx, cfg)
			if err != nil {
				detailed := gcxerrors.DetailedError{
					Summary: "Cannot identify the caller",
					Details: "A capture records who took it, because permissions decide which resources a caller can see. gcx could not read the caller identity from /api/user.",
					Parent:  err,
				}
				if status, ok := errors.AsType[*observation.StatusError](err); ok && (status.Status == http.StatusUnauthorized || status.Status == http.StatusForbidden) {
					exit := gcxerrors.ExitAuthFailure
					detailed.ExitCode = &exit
					detailed.Suggestions = []string{"Check the credentials of the current context: gcx config check"}
				}
				return detailed
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
					Context:   contextName,
				},
				Selectors:  args,
				Selections: selections,
				Puller:     puller,
				Access:     access,
				GCXVersion: version.Get(),
			})
			if err != nil {
				return err
			}

			if err := observation.Write(capture, opts.Path); err != nil {
				if errors.Is(err, observation.ErrDestinationNotEmpty) {
					exit := gcxerrors.ExitUsageError
					return gcxerrors.DetailedError{
						Summary:  "Capture destination is not empty",
						Details:  "A capture never merges with or overwrites earlier files. Choose a new --path or remove the existing directory.",
						Parent:   err,
						ExitCode: &exit,
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

	// The receipt counts resource types: a type is the unit of coverage.
	// The resource counts are in each file entry.
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
	types := len(capture.Manifest.Types)
	receipt.Summary = cmdio.MutationSummary{Succeeded: types - len(incomplete), Failed: len(incomplete)}

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
		msg := fmt.Sprintf("%d of %d resource type(s) captured incompletely", len(incomplete), types)
		cmdio.EmitWarn(cmd.ErrOrStderr(), msg)
		return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, &gcxerrors.PartialFailureError{Total: types, Failed: len(incomplete), Op: "capture"})
	}
	return nil
}
