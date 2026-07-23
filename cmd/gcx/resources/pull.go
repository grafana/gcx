package resources

import (
	"errors"
	"io"
	"path/filepath"
	"sort"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
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

	// flags is the bound flag set, kept so Validate can reject flags that
	// cannot round-trip as on-disk resource files (--json, --jq).
	flags *pflag.FlagSet
}

func (opts *pullOpts) setup(flags *pflag.FlagSet) {
	// OutputFormat doubles as the on-disk file extension and the encoder for
	// pulled resources — pin the default so agent mode does not flip it to
	// the agents codec (which would write `<name>.agents` files, with
	// spill-summary envelopes instead of content for large resources).
	// An explicit -o json|yaml from the user still wins.
	opts.IO.PinDefaultFormat("json")

	// Validate rejects -o agents (below), so never advertise it in the
	// format menu (usage string and unknown-format error listings).
	opts.IO.HideFormat("agents")

	// Bind all the flags
	opts.IO.BindFlags(flags)
	opts.flags = flags

	// Validate rejects every use of --json/--jq (below), so hide both from
	// help — advertising flags the command always refuses is the same
	// dishonesty as advertising the rejected agents format.
	_ = flags.MarkHidden("json")
	_ = flags.MarkHidden("jq")

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
	// The pull-specific typed rejections run BEFORE the shared IO.Validate:
	// the shared errors are untyped (exit 1 today, repo-wide), so a mixed
	// invocation like `pull -o yaml --json x` must hit the typed exit-2
	// rejection below, matching what a solo `--json x` gets.
	//
	// The agents codec is a display codec: it writes compact JSON and, above
	// the spill threshold, a spill-summary envelope instead of the payload.
	// Neither belongs in a resource file on disk. UsageError classifies the
	// rejection as invalid usage (exit 2, DESIGN.md taxonomy).
	if opts.IO.OutputFormat == "agents" {
		return &fail.UsageError{Message: "output format 'agents' cannot be used with pull: it writes display envelopes, not resource content. Use -o json or -o yaml"}
	}

	// --json (field selection/discovery) and --jq (transformation) shape the
	// encoded document, but pull writes resource files that push reads back —
	// a field-selected or jq-transformed document is not the resource and
	// cannot round-trip. pull encodes via opts.IO.Codec() directly, which
	// would silently ignore both flags; reject upfront instead, mirroring
	// the edit rejections.
	if opts.flags != nil {
		if f := opts.flags.Lookup("json"); f != nil && f.Changed {
			return &fail.UsageError{Message: "--json cannot be used with pull: field-selected output cannot round-trip as a resource file. Use -o json or -o yaml"}
		}
		if f := opts.flags.Lookup("jq"); f != nil && f.Changed {
			return &fail.UsageError{Message: "--jq cannot be used with pull: transformed output cannot round-trip as a resource file. Use -o json or -o yaml"}
		}
	}

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

			// Collect written files per kind directory for the receipt:
			// individual paths would make the receipt grow with the pull
			// size, so it stays one bounded entry per kind.
			type dirGroup struct {
				kind  string
				count int
			}
			written := map[string]*dirGroup{}
			var writeFailures []cmdio.MutationFailure
			writer := local.FSWriter{
				Path:        opts.Path,
				Namer:       local.GroupResourcesByKind(opts.IO.OutputFormat, local.PluralsFromFilters(res.Filters)),
				Encoder:     codec,
				StopOnError: opts.OnError.StopOnError(),
				OnWritten: func(path string, resource *resources.Resource) {
					dir := filepath.Dir(path)
					if written[dir] == nil {
						written[dir] = &dirGroup{kind: resource.Kind()}
					}
					written[dir].count++
				},
				// A fetched resource whose file write fails is a failed pull:
				// without this the receipt would count it as succeeded and the
				// command would exit 0 (the writer only logs skipped writes).
				OnWriteError: func(resource *resources.Resource, err error) {
					target := cmdio.MutationTarget{}
					if resource != nil {
						target.Kind = resource.Kind()
						target.Name = resource.Name()
					}
					writeFailures = append(writeFailures, cmdio.MutationFailure{Target: target, Error: err.Error()})
				},
			}

			if err := writer.Write(ctx, &res.Resources); err != nil {
				return err
			}

			pullSummary := res.PullSummary

			receipt := cmdio.NewArtifactReceipt("pulled", opts.IO.OutputFormat)
			receipt.Dir = opts.Path
			receipt.Summary = cmdio.MutationSummary{
				// Succeeded means "on disk": fetched resources whose write
				// failed move from succeeded to failed.
				Succeeded: pullSummary.SuccessCount() - len(writeFailures),
				Failed:    pullSummary.FailedCount() + len(writeFailures),
				Skipped:   pullSummary.SkippedCount(),
			}
			dirs := make([]string, 0, len(written))
			for dir := range written {
				dirs = append(dirs, dir)
			}
			sort.Strings(dirs)
			for _, dir := range dirs {
				receipt.Files = append(receipt.Files, cmdio.ArtifactFile{
					Path:  dir,
					Kind:  written[dir].kind,
					Count: written[dir].count,
				})
			}
			for _, failure := range pullSummary.Failures() {
				target := cmdio.MutationTarget{}
				if failure.Resource != nil {
					target.Kind = failure.Resource.Kind()
					target.Name = failure.Resource.Name()
				}
				msg := ""
				if failure.Error != nil {
					msg = failure.Error.Error()
				}
				receipt.Failures = append(receipt.Failures, cmdio.MutationFailure{Target: target, Error: msg})
			}
			receipt.Failures = append(receipt.Failures, writeFailures...)

			emitErr := cmdio.EmitArtifactResult(cmd.OutOrStdout(), receipt, func(w io.Writer) error {
				// Same line format as always, but the counts include file-
				// write failures — previously a failed write still printed
				// "0 errors" while the details went only to the warn log.
				succeeded, failed := receipt.Summary.Succeeded, receipt.Summary.Failed
				printer := cmdio.Success
				if failed != 0 {
					printer = cmdio.Warning
					if succeeded == 0 {
						printer = cmdio.Error
					}
				}

				if skipped := pullSummary.SkippedCount(); skipped > 0 {
					printer(w, "%d resources pulled, %d errors (%d resource types skipped — not listable)", succeeded, failed, skipped)
				} else {
					printer(w, "%d resources pulled, %d errors", succeeded, failed)
				}
				return nil
			})
			if emitErr != nil {
				return emitErr
			}

			totalFailed := pullSummary.FailedCount() + len(writeFailures)
			if opts.OnError.FailOnErrors() && totalFailed > 0 {
				// The receipt (with enumerated failures, including file-write
				// failures) is already on stdout — the typed stderr diagnostic
				// + EmittedError carry exit 4 without a second error document.
				return partialBatchFailure(cmd.ErrOrStderr(), "pull",
					pullSummary.SuccessCount()+pullSummary.FailedCount(), totalFailed)
			}

			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}
