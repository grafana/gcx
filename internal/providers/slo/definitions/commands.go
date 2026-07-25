package definitions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// Commands returns the definitions command group with CRUD subcommands.
func Commands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "definitions",
		Short:   "Manage SLO definitions.",
		Aliases: []string{"def", "defs"},
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newPushCommand(loader),
		newPullCommand(loader),
		newDeleteCommand(loader),
		newStatusCommand(loader),
		newTimelineCommand(loader),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

type listOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &sloTableCodec{})
	o.IO.RegisterCustomCodec("wide", &sloTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.Int64Var(&o.Limit, "limit", 0, "Maximum number of items to return after fetch (0 for all; use a positive value to trim output only)")
}

func newListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List SLO definitions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, cfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, opts.Limit)
			if err != nil {
				return err
			}

			// Extract Slo from TypedObject
			slos := make([]Slo, len(typedObjs))
			for i := range typedObjs {
				slos[i] = typedObjs[i].Spec
			}

			// Table codec operates on raw []Slo for direct field access.
			// Other formats (yaml/json) convert to K8s envelope Resources
			// for consistency with get/pull and round-trip support.
			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), slos)
			}

			var objs []unstructured.Unstructured
			for _, slo := range slos {
				res, err := ToResource(slo, cfg.Namespace)
				if err != nil {
					return fmt.Errorf("failed to convert SLO %s to resource: %w", slo.UUID, err)
				}
				objs = append(objs, res.ToUnstructured())
			}

			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// sloTableCodec renders SLOs as a tabular table.
type sloTableCodec struct {
	Wide bool
}

func (c *sloTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *sloTableCodec) Encode(w io.Writer, v any) error {
	slos, ok := v.([]Slo)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Slo")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("UUID", "NAME", "TARGET", "WINDOW", "STATUS", "DESCRIPTION")
	} else {
		t = style.NewTable("UUID", "NAME", "TARGET", "WINDOW", "STATUS")
	}

	for _, slo := range slos {
		target := "-"
		window := "-"
		if len(slo.Objectives) > 0 {
			target = fmt.Sprintf("%.2f%%", slo.Objectives[0].Value*100)
			window = slo.Objectives[0].Window
		}

		status := "-"
		if slo.ReadOnly != nil && slo.ReadOnly.Status != nil {
			status = slo.ReadOnly.Status.Type
		}

		if c.Wide {
			t.Row(slo.UUID, slo.Name, target, window, status, slo.Description)
		} else {
			t.Row(slo.UUID, slo.Name, target, window, status)
		}
	}

	return t.Render(w)
}

func (c *sloTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get UUID",
		Short: "Get a single SLO definition.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			uuid := args[0]

			crud, cfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, uuid)
			if err != nil {
				return err
			}

			slo := typedObj.Spec
			res, err := ToResource(slo, cfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert SLO to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// pull command
// ---------------------------------------------------------------------------

type pullOpts struct {
	OutputDir string
}

func (o *pullOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.OutputDir, "output-dir", "d", ".", "Directory to write SLO definition files to")
}

func newPullCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &pullOpts{}
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull SLO definitions to disk.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			crud, cfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, 0)
			if err != nil {
				return err
			}

			outputDir := filepath.Join(opts.OutputDir, "SLO")
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
			}

			codec := format.NewYAMLCodec()

			for _, typedObj := range typedObjs {
				slo := typedObj.Spec
				res, err := ToResource(slo, cfg.Namespace)
				if err != nil {
					return fmt.Errorf("failed to convert SLO %s to resource: %w", slo.UUID, err)
				}

				filePath := filepath.Join(outputDir, slo.UUID+".yaml")
				f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					return fmt.Errorf("failed to open file %s: %w", filePath, err)
				}

				obj := res.ToUnstructured()
				if err := codec.Encode(f, &obj); err != nil {
					f.Close()
					return fmt.Errorf("failed to write SLO %s: %w", slo.UUID, err)
				}
				f.Close()
			}

			// The real output is files on disk — stdout carries the terminal
			// receipt: one bounded entry for the kind directory, not one per
			// file. Agent mode gets the receipt as a single JSON document;
			// the human line stays byte-identical.
			receipt := cmdio.NewArtifactReceipt("pulled", "yaml")
			receipt.Dir = outputDir
			receipt.Summary = cmdio.MutationSummary{Succeeded: len(typedObjs)}
			if len(typedObjs) > 0 {
				receipt.Files = append(receipt.Files, cmdio.ArtifactFile{
					Path:  outputDir,
					Kind:  "SLO",
					Count: len(typedObjs),
				})
			}
			return cmdio.EmitArtifactResult(cmd.OutOrStdout(), receipt, func(w io.Writer) error {
				cmdio.Success(w, "Pulled %d SLO definitions to %s/", len(typedObjs), outputDir)
				return nil
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// push command
// ---------------------------------------------------------------------------

type pushOpts struct {
	IO     cmdio.Options
	DryRun bool
}

func (o *pushOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview changes without making them")
	// The push result flows through the codec system: the default text codec
	// reproduces the historical per-file lines byte-for-byte; agent mode and
	// explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &pushResultCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

// pushItemResult is one successfully processed file in a push batch.
type pushItemResult struct {
	Action string `json:"action" yaml:"action"` // created | updated | dry-run
	Name   string `json:"name" yaml:"name"`
	UUID   string `json:"uuid,omitempty" yaml:"uuid,omitempty"`
	File   string `json:"file,omitempty" yaml:"file,omitempty"`
}

// pushBatchResult is the finite result document for `slo definitions push`
// (and its mirror in the reports package). It is bespoke rather than a
// cmdio.BatchMutation because per-item outcomes carry information a consumer
// cannot recover otherwise (the server-assigned UUID of created SLOs), so the
// shape carries its own discriminators.
type pushBatchResult struct {
	Type          string                  `json:"type" yaml:"type"`
	SchemaVersion string                  `json:"schema_version" yaml:"schema_version"`
	Action        string                  `json:"action" yaml:"action"`
	DryRun        bool                    `json:"dry_run,omitempty" yaml:"dry_run,omitempty"`
	Summary       cmdio.MutationSummary   `json:"summary" yaml:"summary"`
	Items         []pushItemResult        `json:"items" yaml:"items"`
	Failures      []cmdio.MutationFailure `json:"failures" yaml:"failures"`
}

func newPushBatchResult(dryRun bool) pushBatchResult {
	return pushBatchResult{
		Type:          "gcx.slo.push_batch",
		SchemaVersion: "1",
		Action:        "pushed",
		DryRun:        dryRun,
		Items:         []pushItemResult{},
		Failures:      []cmdio.MutationFailure{},
	}
}

// pushResultCodec is the human "text" codec for pushBatchResult values: it
// renders exactly the per-file lines push has always printed. Failures are
// not rendered here — they stay on stderr as diagnostics, matching the
// pre-codec behavior where failures never reached stdout.
type pushResultCodec struct{}

func (c *pushResultCodec) Format() format.Format { return "text" }

func (c *pushResultCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *pushResultCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(pushBatchResult)
	if !ok {
		return errors.New("invalid data type for push result codec: expected pushBatchResult")
	}
	for _, item := range result.Items {
		switch item.Action {
		case "dry-run":
			cmdio.Info(w, "[dry-run] Would push SLO %q (uuid=%s)", item.Name, item.UUID)
		case "created":
			cmdio.Success(w, "Created %s (uuid=%s)", item.Name, item.UUID)
		case "updated":
			cmdio.Success(w, "Updated %s", item.Name)
		}
	}
	return nil
}

// emitPartialResult writes the completed result document (which enumerates
// the failure) to stdout and returns the exit-4 sentinel. Call only after at
// least one target succeeded — the cause additionally goes to stderr because
// reportError writes nothing more for an EmittedError.
func emitPartialResult(cmd *cobra.Command, io *cmdio.Options, result any, cause error) error {
	cmdio.Error(cmd.ErrOrStderr(), "%v", cause)
	if err := io.Encode(cmd.OutOrStdout(), result); err != nil {
		return err
	}
	return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, cause)
}

func newPushCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &pushOpts{}
	cmd := &cobra.Command{
		Use:   "push FILE...",
		Short: "Push SLO definitions from files.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			yamlCodec := format.NewYAMLCodec()

			result := newPushBatchResult(opts.DryRun)

			// fail finalizes the batch after a mid-loop failure. The loop
			// stops at the first error (as it always has): with no prior
			// successes the standard error path owns the output; with a
			// partial result the document goes to stdout and the sentinel
			// carries exit 4.
			fail := func(target cmdio.MutationTarget, remaining int, cause error) error {
				result.Summary.Failed++
				result.Summary.Skipped = remaining
				result.Failures = append(result.Failures, cmdio.MutationFailure{Target: target, Error: cause.Error()})
				if result.Summary.Succeeded == 0 {
					return cause
				}
				return emitPartialResult(cmd, &opts.IO, result, cause)
			}

			for i, filePath := range args {
				slo, err := readSloFile(yamlCodec, filePath)
				if err != nil {
					return fail(cmdio.MutationTarget{Kind: "SLO", Name: filePath}, len(args)-i-1, err)
				}

				if opts.DryRun {
					result.Items = append(result.Items, pushItemResult{Action: "dry-run", Name: slo.Name, UUID: slo.UUID, File: filePath})
					result.Summary.Succeeded++
					continue
				}

				item, err := upsertSLO(ctx, crud, slo)
				if err != nil {
					return fail(cmdio.MutationTarget{Kind: "SLO", Name: slo.Name, UID: slo.UUID}, len(args)-i-1, err)
				}
				item.File = filePath
				result.Items = append(result.Items, item)
				result.Summary.Succeeded++
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// readSloFile reads one YAML manifest from disk and converts it to an Slo.
func readSloFile(yamlCodec format.Codec, filePath string) (*Slo, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Decode YAML into an unstructured object
	var obj unstructured.Unstructured
	if err := yamlCodec.Decode(strings.NewReader(string(data)), &obj); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", filePath, err)
	}

	res, err := resources.FromUnstructured(&obj)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource from %s: %w", filePath, err)
	}

	slo, err := FromResource(res)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource to SLO from %s: %w", filePath, err)
	}
	return slo, nil
}

// upsertSLO creates or updates an SLO depending on whether it already exists.
// If slo.UUID is set, it checks the server; a 404 means create, otherwise update.
// If slo.UUID is empty, it always creates. It returns the item outcome — the
// caller owns rendering, so the same result feeds every output format.
func upsertSLO(ctx context.Context, crud *adapter.TypedCRUD[Slo], slo *Slo) (pushItemResult, error) {
	if slo.UUID == "" {
		// Wrap in TypedObject for create
		typedObj := &adapter.TypedObject[Slo]{
			Spec: *slo,
		}
		created, err := crud.Create(ctx, typedObj)
		if err != nil {
			return pushItemResult{}, fmt.Errorf("failed to create SLO %s: %w", slo.Name, err)
		}
		return pushItemResult{Action: "created", Name: slo.Name, UUID: created.Spec.UUID}, nil
	}

	_, getErr := crud.Get(ctx, slo.UUID)
	switch {
	case getErr == nil:
		// SLO exists — update.
		typedObj := &adapter.TypedObject[Slo]{
			Spec: *slo,
		}
		typedObj.SetName(slo.UUID)
		if _, err := crud.Update(ctx, slo.UUID, typedObj); err != nil {
			return pushItemResult{}, fmt.Errorf("failed to update SLO %s: %w", slo.UUID, err)
		}
		return pushItemResult{Action: "updated", Name: slo.Name, UUID: slo.UUID}, nil

	case errors.Is(getErr, ErrNotFound):
		// SLO not found — create.
		typedObj := &adapter.TypedObject[Slo]{
			Spec: *slo,
		}
		created, err := crud.Create(ctx, typedObj)
		if err != nil {
			return pushItemResult{}, fmt.Errorf("failed to create SLO %s: %w", slo.Name, err)
		}
		return pushItemResult{Action: "created", Name: slo.Name, UUID: created.Spec.UUID}, nil

	default:
		// Any other error (auth, network, server) — propagate.
		return pushItemResult{}, fmt.Errorf("failed to check SLO %s: %w", slo.UUID, getErr)
	}
}

// ---------------------------------------------------------------------------
// delete command
// ---------------------------------------------------------------------------

type deleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	// The delete result flows through the codec system: the default text
	// codec reproduces the historical per-target lines byte-for-byte; agent
	// mode and explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &deleteResultCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

// deleteBatchResult is the finite result document for `slo definitions
// delete` (and its mirror in the reports package). Bespoke rather than a
// cmdio.BatchMutation because the historical human output enumerates each
// deleted target, so the result must carry them.
type deleteBatchResult struct {
	Type          string                  `json:"type" yaml:"type"`
	SchemaVersion string                  `json:"schema_version" yaml:"schema_version"`
	Action        string                  `json:"action" yaml:"action"`
	Summary       cmdio.MutationSummary   `json:"summary" yaml:"summary"`
	Deleted       []string                `json:"deleted" yaml:"deleted"`
	Failures      []cmdio.MutationFailure `json:"failures" yaml:"failures"`
}

func newDeleteBatchResult() deleteBatchResult {
	return deleteBatchResult{
		Type:          "gcx.slo.delete_batch",
		SchemaVersion: "1",
		Action:        "deleted",
		Deleted:       []string{},
		Failures:      []cmdio.MutationFailure{},
	}
}

// deleteResultCodec is the human "text" codec for deleteBatchResult values:
// exactly the per-target lines delete has always printed. Failures stay on
// stderr as diagnostics, matching the pre-codec behavior.
type deleteResultCodec struct{}

func (c *deleteResultCodec) Format() format.Format { return "text" }

func (c *deleteResultCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *deleteResultCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(deleteBatchResult)
	if !ok {
		return errors.New("invalid data type for delete result codec: expected deleteBatchResult")
	}
	for _, uuid := range result.Deleted {
		cmdio.Success(w, "Deleted %s", uuid)
	}
	return nil
}

func newDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete UUID...",
		Short: "Delete SLO definitions.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// The prompt and the decline note are diagnostics — stderr keeps
			// them out of the stdout result document.
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete %d SLO definition(s)?", len(args)))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			result := newDeleteBatchResult()
			for i, uuid := range args {
				if err := crud.Delete(ctx, uuid); err != nil {
					cause := fmt.Errorf("failed to delete SLO %s: %w", uuid, err)
					result.Summary.Failed++
					result.Summary.Skipped = len(args) - i - 1
					result.Failures = append(result.Failures, cmdio.MutationFailure{
						Target: cmdio.MutationTarget{Kind: "SLO", UID: uuid},
						Error:  cause.Error(),
					})
					if result.Summary.Succeeded == 0 {
						return cause
					}
					return emitPartialResult(cmd, &opts.IO, result, cause)
				}
				result.Deleted = append(result.Deleted, uuid)
				result.Summary.Succeeded++
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
