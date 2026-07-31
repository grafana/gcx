package reports

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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

// Commands returns the reports command group with CRUD subcommands.
func Commands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "reports",
		Short:   "Manage SLO reports.",
		Aliases: []string{"report"},
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
	o.IO.RegisterCustomCodec("table", &reportTableCodec{})
	o.IO.RegisterCustomCodec("wide", &reportTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for all)")
}

func newListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List SLO reports.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			rpts, err := client.List(ctx)
			if err != nil {
				return err
			}

			rpts = adapter.TruncateSlice(rpts, opts.Limit)

			// Table codec operates on raw []Report for direct field access.
			// Other formats (yaml/json) convert to K8s envelope Resources
			// for consistency with get/pull and round-trip support.
			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), rpts)
			}

			var objs []unstructured.Unstructured
			for _, report := range rpts {
				res, err := ToResource(report, restCfg.Namespace)
				if err != nil {
					return fmt.Errorf("failed to convert report %s to resource: %w", report.UUID, err)
				}
				objs = append(objs, res.ToUnstructured())
			}

			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// reportTableCodec renders reports as a tabular table.
type reportTableCodec struct {
	Wide bool
}

func (c *reportTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *reportTableCodec) Encode(w io.Writer, v any) error {
	rpts, ok := v.([]Report)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Report")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("UUID", "NAME", "TIME_SPAN", "SLOS", "SLO_UUIDS")
	} else {
		t = style.NewTable("UUID", "NAME", "TIME_SPAN", "SLOS")
	}

	for _, report := range rpts {
		timeSpan := mapTimeSpan(report.TimeSpan)
		sloCount := len(report.ReportDefinition.Slos)

		if c.Wide {
			sloUUIDs := make([]string, 0, sloCount)
			for _, s := range report.ReportDefinition.Slos {
				sloUUIDs = append(sloUUIDs, s.SloUUID)
			}
			t.Row(report.UUID, report.Name, timeSpan, strconv.Itoa(sloCount), strings.Join(sloUUIDs, ","))
		} else {
			t.Row(report.UUID, report.Name, timeSpan, strconv.Itoa(sloCount))
		}
	}

	return t.Render(w)
}

func (c *reportTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// mapTimeSpan converts API timeSpan values to human-readable labels.
func mapTimeSpan(timeSpan string) string {
	switch timeSpan {
	case "weeklySundayToSunday":
		return "weekly"
	case "calendarMonth":
		return "monthly"
	case "calendarYear":
		return "yearly"
	default:
		return timeSpan
	}
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
		Short: "Get a single SLO report.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			uuid := args[0]

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			report, err := client.Get(ctx, uuid)
			if err != nil {
				return err
			}

			res, err := ToResource(*report, restCfg.Namespace)
			if err != nil {
				return fmt.Errorf("failed to convert report to resource: %w", err)
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
	flags.StringVarP(&o.OutputDir, "output-dir", "d", ".", "Directory to write SLO report files to")
}

func newPullCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &pullOpts{}
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull SLO reports to disk.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			rpts, err := client.List(ctx)
			if err != nil {
				return err
			}

			outputDir := filepath.Join(opts.OutputDir, "Report")
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
			}

			codec := format.NewYAMLCodec()

			for _, report := range rpts {
				res, err := ToResource(report, restCfg.Namespace)
				if err != nil {
					return fmt.Errorf("failed to convert report %s to resource: %w", report.UUID, err)
				}

				filePath := filepath.Join(outputDir, report.UUID+".yaml")
				f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					return fmt.Errorf("failed to open file %s: %w", filePath, err)
				}

				obj := res.ToUnstructured()
				if err := codec.Encode(f, &obj); err != nil {
					f.Close()
					return fmt.Errorf("failed to write report %s: %w", report.UUID, err)
				}
				f.Close()
			}

			// The real output is files on disk — stdout carries the terminal
			// receipt: one bounded entry for the kind directory, not one per
			// file. Agent mode gets the receipt as a single JSON document;
			// the human line stays byte-identical.
			receipt := cmdio.NewArtifactReceipt("pulled", "yaml")
			receipt.Dir = outputDir
			receipt.Summary = cmdio.MutationSummary{Succeeded: len(rpts)}
			if len(rpts) > 0 {
				receipt.Files = append(receipt.Files, cmdio.ArtifactFile{
					Path:  outputDir,
					Kind:  "Report",
					Count: len(rpts),
				})
			}
			return cmdio.EmitArtifactResult(cmd.OutOrStdout(), receipt, func(w io.Writer) error {
				cmdio.Success(w, "Pulled %d SLO reports to %s/", len(rpts), outputDir)
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

// pushBatchResult is the finite result document for `slo reports push`. It is
// bespoke rather than a cmdio.BatchMutation because per-item outcomes carry
// information a consumer cannot recover otherwise (the server-assigned UUID
// of created reports), so the shape carries its own discriminators.
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
			cmdio.Info(w, "[dry-run] Would push report %q (uuid=%s)", item.Name, item.UUID)
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
		Short: "Push SLO reports from files.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
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
				report, err := readReportFile(yamlCodec, filePath)
				if err != nil {
					return fail(cmdio.MutationTarget{Kind: "Report", Name: filePath}, len(args)-i-1, err)
				}

				if opts.DryRun {
					result.Items = append(result.Items, pushItemResult{Action: "dry-run", Name: report.Name, UUID: report.UUID, File: filePath})
					result.Summary.Succeeded++
					continue
				}

				item, err := upsertReport(ctx, client, report)
				if err != nil {
					return fail(cmdio.MutationTarget{Kind: "Report", Name: report.Name, UID: report.UUID}, len(args)-i-1, err)
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

// readReportFile reads one YAML manifest from disk and converts it to a Report.
func readReportFile(yamlCodec format.Codec, filePath string) (*Report, error) {
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

	report, err := FromResource(res)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource to report from %s: %w", filePath, err)
	}
	return report, nil
}

// upsertReport creates or updates a report depending on whether it already exists.
// If report.UUID is set, it checks the server; a 404 means create, otherwise update.
// If report.UUID is empty, it always creates. It returns the item outcome — the
// caller owns rendering, so the same result feeds every output format.
func upsertReport(ctx context.Context, client *Client, report *Report) (pushItemResult, error) {
	if report.UUID == "" {
		resp, err := client.Create(ctx, report)
		if err != nil {
			return pushItemResult{}, fmt.Errorf("failed to create report %s: %w", report.Name, err)
		}
		return pushItemResult{Action: "created", Name: report.Name, UUID: resp.UUID}, nil
	}

	_, getErr := client.Get(ctx, report.UUID)
	switch {
	case getErr == nil:
		// Report exists — update.
		if err := client.Update(ctx, report.UUID, report); err != nil {
			return pushItemResult{}, fmt.Errorf("failed to update report %s: %w", report.UUID, err)
		}
		return pushItemResult{Action: "updated", Name: report.Name, UUID: report.UUID}, nil

	case errors.Is(getErr, ErrNotFound):
		// Report not found — create.
		resp, err := client.Create(ctx, report)
		if err != nil {
			return pushItemResult{}, fmt.Errorf("failed to create report %s: %w", report.Name, err)
		}
		return pushItemResult{Action: "created", Name: report.Name, UUID: resp.UUID}, nil

	default:
		// Any other error (auth, network, server) — propagate.
		return pushItemResult{}, fmt.Errorf("failed to check report %s: %w", report.UUID, getErr)
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

// deleteBatchResult is the finite result document for `slo reports delete`.
// Bespoke rather than a cmdio.BatchMutation because the historical human
// output enumerates each deleted target, so the result must carry them.
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
		Short: "Delete SLO reports.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// The prompt and the decline note are diagnostics — stderr keeps
			// them out of the stdout result document.
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete %d report(s)?", len(args)))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			result := newDeleteBatchResult()
			for i, uuid := range args {
				if err := client.Delete(ctx, uuid); err != nil {
					cause := fmt.Errorf("failed to delete report %s: %w", uuid, err)
					result.Summary.Failed++
					result.Summary.Skipped = len(args) - i - 1
					result.Failures = append(result.Failures, cmdio.MutationFailure{
						Target: cmdio.MutationTarget{Kind: "Report", UID: uuid},
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
