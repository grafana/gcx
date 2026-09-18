package definitions

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/slo/api"
	"github.com/grafana/gcx/internal/providers/slo/transfer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the definitions command group with CRUD subcommands.
func Commands(loader providers.GrafanaConfigLoader) *cobra.Command {
	resource := providers.BindGrafanaResource(loader, SloResource())
	cmd := &cobra.Command{
		Use:     "definitions",
		Short:   "Manage SLO definitions.",
		Aliases: []string{"def", "defs"},
	}
	cmd.AddCommand(
		newListCommand(resource),
		newGetCommand(resource),
		transfer.PushCommand(resource, "SLO", "slos."+api.Version+"."+api.Group),
		transfer.PullCommand(resource, "SLO definitions", "slos."+api.Version+"."+api.Group),
		newDeleteCommand(resource),
		newStatusCommand(resource),
		newTimelineCommand(resource),
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
	cmdio.RegisterTable(&o.IO, sloTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.Int64Var(&o.Limit, "limit", 0, "Maximum number of items to return after fetch (0 for all; use a positive value to trim output only)")
}

func newListCommand(resource providers.BoundResource[Slo]) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List SLO definitions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			crud, _, err := resource.Load(ctx)
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
				obj, err := crud.ToUnstructured(slo)
				if err != nil {
					return fmt.Errorf("failed to convert SLO %s to resource: %w", slo.UUID, err)
				}
				objs = append(objs, obj)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func sloTable() cmdio.Table[Slo] {
	return cmdio.Table[Slo]{Columns: []cmdio.Column[Slo]{
		{Header: "UUID", Content: func(s Slo) string { return s.UUID }},
		{Header: "NAME", Content: func(s Slo) string { return s.Name }},
		{Header: "TARGET", Content: func(s Slo) string {
			if len(s.Objectives) == 0 {
				return "-"
			}
			return fmt.Sprintf("%.2f%%", s.Objectives[0].Value*100)
		}},
		{Header: "WINDOW", Content: func(s Slo) string {
			if len(s.Objectives) == 0 {
				return "-"
			}
			return s.Objectives[0].Window
		}},
		{Header: "STATUS", Content: func(s Slo) string {
			if s.ReadOnly == nil || s.ReadOnly.Status == nil {
				return "-"
			}
			return s.ReadOnly.Status.Type
		}},
		{Header: "DESCRIPTION", Visible: cmdio.WideOnly, Content: func(s Slo) string { return s.Description }},
	}}
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

func newGetCommand(resource providers.BoundResource[Slo]) *cobra.Command {
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

			crud, _, err := resource.Load(ctx)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, uuid)
			if err != nil {
				return err
			}

			slo := typedObj.Spec
			obj, err := crud.ToUnstructured(slo)
			if err != nil {
				return fmt.Errorf("failed to convert SLO to resource: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
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

func newDeleteCommand(resource providers.BoundResource[Slo]) *cobra.Command {
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

			crud, _, err := resource.Load(ctx)
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
