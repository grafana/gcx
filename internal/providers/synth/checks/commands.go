package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Commands returns the checks command group with CRUD subcommands.
func Commands(loader smcfg.StatusLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "checks",
		Short:   "Manage Synthetic Monitoring checks.",
		Aliases: []string{"check"},
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
		newStatusCommand(loader),
		newTimelineCommand(loader),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

type listOpts struct {
	IO         cmdio.Options
	Labels     []string
	JobPattern string
	Limit      int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &checkTableCodec{})
	o.IO.RegisterCustomCodec("wide", &checkWideTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringArrayVar(&o.Labels, "label", nil, "Filter by label key=value (repeatable, e.g. --label env=prod)")
	flags.StringVar(&o.JobPattern, "job", "", "Filter by job name glob pattern (e.g. --job 'shopk8s-*')")
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for all)")
}

func newListCommand(loader smcfg.Loader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Synthetic Monitoring checks.",
		Example: `  # List all checks.
  gcx synthetic-monitoring checks list

  # Filter by job glob.
  gcx synthetic-monitoring checks list --job 'shopk8s-*'

  # Filter by label.
  gcx synthetic-monitoring checks list --label env=prod`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			labelMap, err := ParseLabelFlags(opts.Labels)
			if err != nil {
				return err
			}
			filter := &CheckFilter{Labels: labelMap, JobPattern: opts.JobPattern}
			if err := filter.Validate(); err != nil {
				return err
			}

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, opts.Limit)
			if err != nil {
				return err
			}

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
			}

			// Build Check list for table codecs, applying filters.
			checkList := make([]Check, 0, len(typedObjs))
			for i := range typedObjs {
				cr := typedObjs[i].Spec
				c := Check{
					ID:               cr.checkID,
					Job:              cr.Job,
					Target:           cr.Target,
					Frequency:        cr.Frequency,
					Offset:           cr.Offset,
					Timeout:          cr.Timeout,
					Enabled:          cr.Enabled,
					Labels:           cr.Labels,
					Settings:         cr.Settings,
					BasicMetricsOnly: cr.BasicMetricsOnly,
					AlertSensitivity: cr.AlertSensitivity,
					Probes:           []int64{},
				}
				if filter.MatchCheck(c) {
					checkList = append(checkList, c)
				}
			}

			if codec.Format() == "table" || codec.Format() == "wide" {
				return codec.Encode(cmd.OutOrStdout(), checkList)
			}

			// For yaml/json output, marshal typed objects that pass the filter.
			var objs []unstructured.Unstructured
			for _, typedObj := range typedObjs {
				cr := typedObj.Spec
				c := Check{
					ID:     cr.checkID,
					Job:    cr.Job,
					Target: cr.Target,
					Labels: cr.Labels,
				}
				if !filter.MatchCheck(c) {
					continue
				}
				objData, err := json.Marshal(typedObj)
				if err != nil {
					return fmt.Errorf("marshaling typed object: %w", err)
				}
				var obj unstructured.Unstructured
				if err := json.Unmarshal(objData, &obj); err != nil {
					return fmt.Errorf("unmarshaling to unstructured: %w", err)
				}
				objs = append(objs, obj)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type checkTableCodec struct{}

func (c *checkTableCodec) Format() format.Format { return "table" }

func (c *checkTableCodec) Encode(w io.Writer, v any) error {
	checkList, ok := v.([]Check)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Check")
	}

	t := style.NewTable("NAME", "JOB", "TARGET", "TYPE")

	for _, c := range checkList {
		t.Row(checkDisplayName(c), c.Job, c.Target, c.Settings.CheckType())
	}

	return t.Render(w)
}

func (c *checkTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}

type checkWideTableCodec struct{}

func (c *checkWideTableCodec) Format() format.Format { return "wide" }

func (c *checkWideTableCodec) Encode(w io.Writer, v any) error {
	checkList, ok := v.([]Check)
	if !ok {
		return errors.New("invalid data type for wide codec: expected []Check")
	}

	t := style.NewTable("NAME", "JOB", "TARGET", "TYPE", "ENABLED", "FREQ", "TIMEOUT", "PROBES")

	for _, c := range checkList {
		t.Row(checkDisplayName(c), c.Job, c.Target, c.Settings.CheckType(),
			strconv.FormatBool(c.Enabled),
			fmt.Sprintf("%ds", c.Frequency/1000),
			fmt.Sprintf("%ds", c.Timeout/1000),
			strconv.Itoa(len(c.Probes)))
	}

	return t.Render(w)
}

func (c *checkWideTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("wide format does not support decoding")
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

type getOpts struct {
	IO         cmdio.Options
	ShowStatus bool
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &checkTableCodec{})
	o.IO.RegisterCustomCodec("wide", &checkWideTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.BoolVar(&o.ShowStatus, "show-status", false, "Query and display the check's current execution status from Prometheus")
}

func newGetCommand(loader smcfg.StatusLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Get a single Synthetic Monitoring check.",
		Example: `  # Get check by resource name (from 'gcx synthetic-monitoring checks list').
  gcx synthetic-monitoring checks get grafana-instance-health-5594

  # Get check by numeric ID.
  gcx synthetic-monitoring checks get 5594

  # Get check with current execution status.
  gcx synthetic-monitoring checks get grafana-instance-health-5594 --show-status`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Accept both slug-id names and bare numeric IDs.
			name := args[0]
			if _, ok := extractIDFromSlug(name); !ok {
				return fmt.Errorf("invalid check name %q: must be a resource name (e.g. grafana-instance-health-5594) or numeric ID", name)
			}

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, name)
			if err != nil {
				return err
			}

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
			}

			cr := typedObj.Spec
			c := Check{
				ID:               cr.checkID,
				Job:              cr.Job,
				Target:           cr.Target,
				Frequency:        cr.Frequency,
				Offset:           cr.Offset,
				Timeout:          cr.Timeout,
				Enabled:          cr.Enabled,
				Labels:           cr.Labels,
				Settings:         cr.Settings,
				BasicMetricsOnly: cr.BasicMetricsOnly,
				AlertSensitivity: cr.AlertSensitivity,
				Probes:           []int64{},
			}

			// Pattern 13: --show-status always fetches the execution status,
			// regardless of the output format — codecs control display, not
			// data acquisition. A fetch failure is a diagnostic, not the
			// result: stderr keeps it out of the stdout document/table.
			var info checkStatusInfo
			if opts.ShowStatus {
				info, err = queryCheckStatus(ctx, loader, c.Job, c.Target, c.AlertSensitivity)
				if err != nil {
					cmdio.Warning(cmd.ErrOrStderr(), "could not retrieve execution status: %v", err)
				}
			}

			if codec.Format() == "table" || codec.Format() == "wide" {
				return encodeGetTable(cmd.OutOrStdout(), c, info, codec.Format() == "wide")
			}

			// For yaml/json, use the typed object.
			objData, err := json.Marshal(typedObj)
			if err != nil {
				return fmt.Errorf("marshaling typed object: %w", err)
			}
			var obj unstructured.Unstructured
			if err := json.Unmarshal(objData, &obj); err != nil {
				return fmt.Errorf("unmarshaling to unstructured: %w", err)
			}
			// Merge the fetched status into the structured output as an
			// optional top-level status member carrying the same data the
			// table's STATUS/SUCCESS columns render. Absent when
			// --show-status was not set or the status fetch failed.
			if info.Status != "" {
				status := map[string]any{"status": info.Status}
				if info.Success != nil {
					status["success"] = *info.Success
				}
				obj.Object["status"] = status
			}
			return opts.IO.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// create
// ---------------------------------------------------------------------------

type createOpts struct {
	IO              cmdio.Options
	File            string
	ShowStatus      bool
	ValidateTargets bool
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the check manifest (YAML)")
	flags.BoolVar(&o.ShowStatus, "show-status", false, "Query and display check status after creation")
	flags.BoolVar(&o.ValidateTargets, "validate-targets", false, "Pre-flight HTTP HEAD request for HTTP check targets (warning only)")
	// The create result flows through the codec system: the default text
	// codec reproduces the historical lines byte-for-byte; agent mode and
	// explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &checkCreateCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *createOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

// checkCreateResult is the finite result document for `checks create`. It is
// bespoke because the server-assigned check ID and slug-id resource name only
// surface here (plus the file rewrite), so the shape carries its own
// discriminators.
type checkCreateResult struct {
	Type          string `json:"type" yaml:"type"`
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Action        string `json:"action" yaml:"action"`
	Job           string `json:"job" yaml:"job"`
	ID            int64  `json:"id" yaml:"id"`
	// Name is the slug-id resource name used by get/update/delete.
	Name string `json:"name" yaml:"name"`
	// Status is the post-create execution status (--show-status only).
	Status string `json:"status,omitempty" yaml:"status,omitempty"`
}

// checkCreateCodec is the human "text" codec for checkCreateResult values:
// exactly the lines create has always printed on stdout.
type checkCreateCodec struct{}

func (c *checkCreateCodec) Format() format.Format { return "text" }

func (c *checkCreateCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *checkCreateCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(checkCreateResult)
	if !ok {
		return errors.New("invalid data type for check create codec: expected checkCreateResult")
	}
	cmdio.Success(w, "Created check %q (id=%d)", r.Job, r.ID)
	if r.Status != "" {
		cmdio.Info(w, "status: %s", r.Status)
	}
	return nil
}

func newCreateCommand(loader smcfg.StatusLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Synthetic Monitoring check from a file.",
		Long: `Create a Synthetic Monitoring check from a file.

Note: checks incur Grafana Cloud usage — each test execution is billed, and
check results are stored as metrics and logs, which count toward your metrics
and logs usage. See ` + docs.SyntheticMonitoringInvoice + `.`,
		Example: `  # Create a check from a YAML file.
  gcx synthetic-monitoring checks create -f check.yaml

  # Create and show resulting status.
  gcx synthetic-monitoring checks create -f check.yaml --show-status

  # Validate HTTP target before creating.
  gcx synthetic-monitoring checks create -f check.yaml --validate-targets`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Fetch probe info for validation and offline probe warning.
			probeIDMap, probeOnlineMap, err := FetchProbeInfo(ctx, loader)
			if err != nil {
				return err
			}

			spec, err := readCheckSpec(opts.File)
			if err != nil {
				return err
			}

			// Client-side validation before hitting the API.
			if errs := ValidateCheckSpec(spec, probeIDMap); len(errs) > 0 {
				return fmt.Errorf("check validation failed:\n  - %s", strings.Join(errs, "\n  - "))
			}

			// Warn if all probes are offline. Advisory diagnostics go to
			// stderr — stdout is reserved for the result document.
			if AllProbesOffline(spec.Probes, probeOnlineMap) {
				cmdio.Warning(cmd.ErrOrStderr(), "all probes for check %q are offline — results will report NODATA", spec.Job)
			}

			// Optional HTTP target pre-flight.
			if opts.ValidateTargets {
				if err := ValidateHTTPTarget(spec.Settings.CheckType(), spec.Target, 5*time.Second); err != nil {
					cmdio.Warning(cmd.ErrOrStderr(), "target validation: %v", err)
				}
			}

			crud, namespace, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			cr := checkResource{
				CheckSpec: *spec,
				name:      slugifyJob(spec.Job),
				checkID:   0,
			}
			typedObj := &adapter.TypedObject[checkResource]{Spec: cr}
			typedObj.SetName(cr.name)
			typedObj.SetNamespace(namespace)

			created, err := crud.Create(ctx, typedObj)
			if err != nil {
				return fmt.Errorf("creating check %q: %w", spec.Job, err)
			}

			result := checkCreateResult{
				Type:          "gcx.synth.check_create",
				SchemaVersion: "1",
				Action:        "created",
				Job:           spec.Job,
				ID:            created.Spec.checkID,
				Name:          created.Spec.name,
			}

			// Write back the slug-id composite name so subsequent updates use the correct resource name.
			if err := updateNameInFile(opts.File, created.Spec.name); err != nil {
				cmdio.Warning(cmd.ErrOrStderr(), "check created but could not update %s: %v", opts.File, err)
			}

			if opts.ShowStatus {
				info, err := queryCheckStatus(ctx, loader, spec.Job, spec.Target, spec.AlertSensitivity)
				if err != nil {
					cmdio.Warning(cmd.ErrOrStderr(), "could not retrieve check status: %v", err)
				} else {
					result.Status = info.Status
				}
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

type updateOpts struct {
	IO              cmdio.Options
	File            string
	ShowStatus      bool
	ValidateTargets bool
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the check manifest (YAML)")
	flags.BoolVar(&o.ShowStatus, "show-status", false, "Query and display the previous check status after update")
	flags.BoolVar(&o.ValidateTargets, "validate-targets", false, "Pre-flight HTTP HEAD request for HTTP check targets (warning only)")
	// The update result flows through the codec system: the default text
	// codec reproduces the historical line byte-for-byte; agent mode and
	// explicit -o json/yaml get the structured document.
	o.IO.RegisterCustomCodec("text", &checkUpdateCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return o.IO.Validate()
}

// checkUpdateResult is the finite result document for `checks update`.
type checkUpdateResult struct {
	Type          string `json:"type" yaml:"type"`
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Action        string `json:"action" yaml:"action"`
	Job           string `json:"job" yaml:"job"`
	ID            int64  `json:"id" yaml:"id"`
	// Name is the slug-id resource name used by get/update/delete.
	Name string `json:"name" yaml:"name"`
	// PreviousStatus is the pre-update execution status (--show-status only).
	PreviousStatus string `json:"previous_status,omitempty" yaml:"previous_status,omitempty"`
}

// checkUpdateCodec is the human "text" codec for checkUpdateResult values:
// exactly the line update has always printed on stdout.
type checkUpdateCodec struct{}

func (c *checkUpdateCodec) Format() format.Format { return "text" }

func (c *checkUpdateCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *checkUpdateCodec) Encode(w io.Writer, v any) error {
	r, ok := v.(checkUpdateResult)
	if !ok {
		return errors.New("invalid data type for check update codec: expected checkUpdateResult")
	}
	if r.PreviousStatus != "" {
		cmdio.Success(w, "Updated check %q (id=%d) — previous status: %s", r.Job, r.ID, r.PreviousStatus)
	} else {
		cmdio.Success(w, "Updated check %q (id=%d)", r.Job, r.ID)
	}
	return nil
}

func newUpdateCommand(loader smcfg.StatusLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a Synthetic Monitoring check from a file.",
		Long: `Update a Synthetic Monitoring check from a file.

Note: frequency and probe changes affect billable usage — each test execution
is billed, and check results are stored as metrics and logs, which count
toward your metrics and logs usage. See ` + docs.SyntheticMonitoringInvoice + `.`,
		Example: `  # Update a check using its resource name.
  gcx synthetic-monitoring checks update web-check-1234 -f check.yaml

  # Update and show previous status.
  gcx synthetic-monitoring checks update web-check-1234 -f check.yaml --show-status`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			name := args[0]

			// Extract numeric ID from the resource name (e.g. "web-check-1234" → 1234).
			checkID, ok := extractIDFromSlug(name)
			if !ok || checkID == 0 {
				return fmt.Errorf("could not extract numeric check ID from name %q — use the resource name from 'gcx synthetic-monitoring checks list'", name)
			}

			// Fetch probe info for validation and offline probe warning.
			probeIDMap, probeOnlineMap, err := FetchProbeInfo(ctx, loader)
			if err != nil {
				return err
			}

			spec, err := readCheckSpec(opts.File)
			if err != nil {
				return err
			}

			// Client-side validation before hitting the API.
			if errs := ValidateCheckSpec(spec, probeIDMap); len(errs) > 0 {
				return fmt.Errorf("check validation failed:\n  - %s", strings.Join(errs, "\n  - "))
			}

			// Warn if all probes are offline. Advisory diagnostics go to
			// stderr — stdout is reserved for the result document.
			if AllProbesOffline(spec.Probes, probeOnlineMap) {
				cmdio.Warning(cmd.ErrOrStderr(), "all probes for check %q are offline — results will report NODATA", spec.Job)
			}

			// Optional HTTP target pre-flight.
			if opts.ValidateTargets {
				if err := ValidateHTTPTarget(spec.Settings.CheckType(), spec.Target, 5*time.Second); err != nil {
					cmdio.Warning(cmd.ErrOrStderr(), "target validation: %v", err)
				}
			}

			crud, namespace, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			cr := checkResource{
				CheckSpec: *spec,
				name:      name,
				checkID:   checkID,
			}
			typedObj := &adapter.TypedObject[checkResource]{Spec: cr}
			typedObj.SetName(name)
			typedObj.SetNamespace(namespace)

			// Query the previous status before the update so it reflects the
			// pre-update alertSensitivity threshold.
			var prevStatus string
			if opts.ShowStatus {
				prevSensitivity := existingSensitivity(ctx, loader, checkID, spec.AlertSensitivity)
				prevInfo, err := queryCheckStatus(ctx, loader, spec.Job, spec.Target, prevSensitivity)
				if err != nil {
					cmdio.Warning(cmd.ErrOrStderr(), "could not retrieve previous status: %v", err)
				}
				prevStatus = prevInfo.Status
			}

			if _, err := crud.Update(ctx, name, typedObj); err != nil {
				return fmt.Errorf("updating check %d: %w", checkID, err)
			}

			result := checkUpdateResult{
				Type:           "gcx.synth.check_update",
				SchemaVersion:  "1",
				Action:         "updated",
				Job:            spec.Job,
				ID:             checkID,
				Name:           name,
				PreviousStatus: prevStatus,
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// existingSensitivity fetches the current alertSensitivity for a check so that
// "previous status" is evaluated against the old threshold, not the new spec's.
// Falls back to fallback if the fetch fails for any reason.
func existingSensitivity(ctx context.Context, loader smcfg.Loader, checkID int64, fallback string) string {
	restCfg, uid, _, err := loader.LoadSMProxyConfig(ctx)
	if err != nil {
		return fallback
	}
	client, err := NewClient(restCfg, uid, loader)
	if err != nil {
		return fallback
	}
	existing, err := client.Get(ctx, checkID)
	if err != nil {
		return fallback
	}
	return existing.AlertSensitivity
}

// ---------------------------------------------------------------------------
// delete
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

// deleteBatchResult is the finite result document for `checks delete` (and
// its mirror in the probes package). Bespoke rather than a
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
		Type:          "gcx.synth.delete_batch",
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
	for _, name := range result.Deleted {
		cmdio.Success(w, "Deleted check %s", name)
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

func newDeleteCommand(loader smcfg.Loader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete NAME...",
		Short: "Delete Synthetic Monitoring checks.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// The prompt and the decline note are diagnostics — stderr keeps
			// them out of the stdout result document.
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete %d check(s)?", len(args)))
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
			for i, name := range args {
				// Accepts both slug-id names (grafana-instance-health-5594) and bare numeric IDs (5594).
				// DeleteFn extracts the numeric ID via extractIDFromSlug.
				if err := crud.Delete(ctx, name); err != nil {
					cause := fmt.Errorf("deleting check %s: %w", name, err)
					result.Summary.Failed++
					result.Summary.Skipped = len(args) - i - 1
					result.Failures = append(result.Failures, cmdio.MutationFailure{
						Target: cmdio.MutationTarget{Kind: "Check", Name: name},
						Error:  cause.Error(),
					})
					if result.Summary.Succeeded == 0 {
						return cause
					}
					return emitPartialResult(cmd, &opts.IO, result, cause)
				}
				result.Deleted = append(result.Deleted, name)
				result.Summary.Succeeded++
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// encodeGetTable renders a single check as a table row, appending SUCCESS and STATUS
// columns when status info is available (non-empty Status).
func encodeGetTable(w io.Writer, c Check, info checkStatusInfo, wide bool) error {
	headers := []string{"NAME", "JOB", "TARGET", "TYPE"}
	row := []string{checkDisplayName(c), c.Job, c.Target, c.Settings.CheckType()}

	if wide {
		headers = append(headers, "ENABLED", "FREQ", "TIMEOUT", "PROBES")
		row = append(row,
			strconv.FormatBool(c.Enabled),
			fmt.Sprintf("%ds", c.Frequency/1000),
			fmt.Sprintf("%ds", c.Timeout/1000),
			strconv.Itoa(len(c.Probes)))
	}

	if info.Status != "" {
		successStr := "--"
		if info.Success != nil {
			successStr = fmt.Sprintf("%.2f%%", *info.Success*100)
		}
		headers = append(headers, "SUCCESS", "STATUS")
		row = append(row, successStr, info.Status)
	}

	t := style.NewTable(headers...)
	t.Row(row...)

	return t.Render(w)
}

// checkDisplayName computes the user-facing "slug-id" resource name from a Check.
// This is the name the user passes to get, update, and delete commands.
func checkDisplayName(c Check) string {
	name := slugifyJob(c.Job)
	if c.ID != 0 {
		name += "-" + strconv.FormatInt(c.ID, 10)
	}
	return name
}

// readCheckSpec reads and parses a single-document check YAML file into a CheckSpec.
// Returns an error if the file contains multiple YAML documents (use "gcx resources push" for batch).
func readCheckSpec(filePath string) (*CheckSpec, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filePath, err)
	}

	// Reject multi-document YAML — create/update operate on a single check.
	if hasMultipleDocuments(data) {
		return nil, fmt.Errorf("%s contains multiple YAML documents — create/update operate on a single check; use 'gcx resources push checks' for batch operations", filePath)
	}

	var obj unstructured.Unstructured
	if err := format.NewYAMLCodec().Decode(strings.NewReader(string(data)), &obj); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filePath, err)
	}

	res, err := resources.FromUnstructured(&obj)
	if err != nil {
		return nil, fmt.Errorf("building resource from %s: %w", filePath, err)
	}

	spec, _, err := FromResource(res)
	if err != nil {
		return nil, fmt.Errorf("converting resource from %s: %w", filePath, err)
	}

	return spec, nil
}

// hasMultipleDocuments checks if YAML data contains more than one document
// by looking for "---" document separators on their own line.
func hasMultipleDocuments(data []byte) bool {
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "---" {
			return true
		}
	}
	return false
}

// updateNameInFile rewrites metadata.name in a YAML file to newName.
// This is used after a create to persist the server-assigned resource name.
func updateNameInFile(filePath, newName string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	inMetadata := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "metadata:" {
			inMetadata = true
			continue
		}
		if inMetadata {
			if strings.HasPrefix(trimmed, "name:") {
				lines[i] = strings.Replace(line, trimmed, "name: "+strconv.Quote(newName), 1)
				break
			}
			// Stop searching if we leave the metadata block (new top-level key).
			if len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				break
			}
		}
	}

	return os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0600)
}
