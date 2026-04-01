package checks

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
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
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &checkTableCodec{})
	o.IO.RegisterCustomCodec("wide", &checkWideTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringArrayVar(&o.Labels, "label", nil, "Filter by label key=value (repeatable, e.g. --label env=prod)")
	flags.StringVar(&o.JobPattern, "job", "", "Filter by job name glob pattern (e.g. --job 'shopk8s-*')")
}

func newListCommand(loader smcfg.Loader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Synthetic Monitoring checks.",
		Example: `  # List all checks.
  gcx synth checks list

  # Filter by job glob.
  gcx synth checks list --job 'shopk8s-*'

  # Filter by label.
  gcx synth checks list --label env=prod`,
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

			typedObjs, err := crud.List(ctx)
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
			return codec.Encode(cmd.OutOrStdout(), objs)
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

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tJOB\tTARGET\tTYPE")

	for _, c := range checkList {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			checkDisplayName(c), c.Job, c.Target, c.Settings.CheckType())
	}

	return tw.Flush()
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

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tJOB\tTARGET\tTYPE\tENABLED\tFREQ\tTIMEOUT\tPROBES")

	for _, c := range checkList {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%v\t%ds\t%ds\t%d\n",
			checkDisplayName(c), c.Job, c.Target, c.Settings.CheckType(), c.Enabled,
			c.Frequency/1000, c.Timeout/1000, len(c.Probes))
	}

	return tw.Flush()
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
		Example: `  # Get check by resource name (from 'gcx synth checks list').
  gcx synth checks get grafana-instance-health-5594

  # Get check by numeric ID.
  gcx synth checks get 5594

  # Get check with current execution status.
  gcx synth checks get grafana-instance-health-5594 --show-status`,
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

			if codec.Format() == "table" || codec.Format() == "wide" {
				// Query status before rendering so we can merge it into the table.
				var info checkStatusInfo
				if opts.ShowStatus {
					var err error
					info, err = queryCheckStatus(ctx, loader, c.Job, c.Target)
					if err != nil {
						cmdio.Warning(cmd.OutOrStdout(), "could not retrieve execution status: %v", err)
					}
				}
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
			return codec.Encode(cmd.OutOrStdout(), &obj)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// create
// ---------------------------------------------------------------------------

type createOpts struct {
	File            string
	ShowStatus      bool
	ValidateTargets bool
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the check manifest (YAML)")
	flags.BoolVar(&o.ShowStatus, "show-status", false, "Query and display check status after creation")
	flags.BoolVar(&o.ValidateTargets, "validate-targets", false, "Pre-flight HTTP HEAD request for HTTP check targets (warning only)")
}

func (o *createOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func newCreateCommand(loader smcfg.StatusLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Synthetic Monitoring check from a file.",
		Example: `  # Create a check from a YAML file.
  gcx synth checks create -f check.yaml

  # Create and show resulting status.
  gcx synth checks create -f check.yaml --show-status

  # Validate HTTP target before creating.
  gcx synth checks create -f check.yaml --validate-targets`,
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

			// Warn if all probes are offline.
			if AllProbesOffline(spec.Probes, probeOnlineMap) {
				cmdio.Warning(cmd.OutOrStdout(), "all probes for check %q are offline — results will report NODATA", spec.Job)
			}

			// Optional HTTP target pre-flight.
			if opts.ValidateTargets {
				if err := ValidateHTTPTarget(spec.Settings.CheckType(), spec.Target, 5*time.Second); err != nil {
					cmdio.Warning(cmd.OutOrStdout(), "target validation: %v", err)
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
			cmdio.Success(cmd.OutOrStdout(), "Created check %q (id=%d)", spec.Job, created.Spec.checkID)

			// Write back the slug-id composite name so subsequent updates use the correct resource name.
			if err := updateNameInFile(opts.File, created.Spec.name); err != nil {
				cmdio.Warning(cmd.OutOrStdout(), "check created but could not update %s: %v", opts.File, err)
			}

			if opts.ShowStatus {
				info, err := queryCheckStatus(ctx, loader, spec.Job, spec.Target)
				if err != nil {
					cmdio.Warning(cmd.OutOrStdout(), "could not retrieve check status: %v", err)
				} else {
					cmdio.Info(cmd.OutOrStdout(), "status: %s", info.Status)
				}
			}

			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

type updateOpts struct {
	File            string
	ShowStatus      bool
	ValidateTargets bool
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the check manifest (YAML)")
	flags.BoolVar(&o.ShowStatus, "show-status", false, "Query and display the previous check status after update")
	flags.BoolVar(&o.ValidateTargets, "validate-targets", false, "Pre-flight HTTP HEAD request for HTTP check targets (warning only)")
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required")
	}
	return nil
}

func newUpdateCommand(loader smcfg.StatusLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a Synthetic Monitoring check from a file.",
		Example: `  # Update a check using its resource name.
  gcx synth checks update web-check-1234 -f check.yaml

  # Update and show previous status.
  gcx synth checks update web-check-1234 -f check.yaml --show-status`,
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
				return fmt.Errorf("could not extract numeric check ID from name %q — use the resource name from 'gcx synth checks list'", name)
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

			// Warn if all probes are offline.
			if AllProbesOffline(spec.Probes, probeOnlineMap) {
				cmdio.Warning(cmd.OutOrStdout(), "all probes for check %q are offline — results will report NODATA", spec.Job)
			}

			// Optional HTTP target pre-flight.
			if opts.ValidateTargets {
				if err := ValidateHTTPTarget(spec.Settings.CheckType(), spec.Target, 5*time.Second); err != nil {
					cmdio.Warning(cmd.OutOrStdout(), "target validation: %v", err)
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

			if opts.ShowStatus {
				// Query status before the update so we can report "previous status".
				prevInfo, err := queryCheckStatus(ctx, loader, spec.Job, spec.Target)
				if err != nil {
					// Non-fatal — proceed with the update regardless.
					cmdio.Warning(cmd.OutOrStdout(), "could not retrieve previous status: %v", err)
				}

				if _, err := crud.Update(ctx, name, typedObj); err != nil {
					return fmt.Errorf("updating check %d: %w", checkID, err)
				}

				if prevInfo.Status != "" {
					cmdio.Success(cmd.OutOrStdout(), "Updated check %q (id=%d) — previous status: %s", spec.Job, checkID, prevInfo.Status)
				} else {
					cmdio.Success(cmd.OutOrStdout(), "Updated check %q (id=%d)", spec.Job, checkID)
				}
				return nil
			}

			if _, err := crud.Update(ctx, name, typedObj); err != nil {
				return fmt.Errorf("updating check %d: %w", checkID, err)
			}
			cmdio.Success(cmd.OutOrStdout(), "Updated check %q (id=%d)", spec.Job, checkID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

type deleteOpts struct {
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Force, "force", "f", false, "Skip confirmation prompt")
}

func newDeleteCommand(loader smcfg.Loader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete NAME...",
		Short: "Delete Synthetic Monitoring checks.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if !opts.Force {
				fmt.Fprintf(cmd.OutOrStdout(), "Delete %d check(s)? [y/N] ", len(args))
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					cmdio.Info(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			for _, name := range args {
				// Accepts both slug-id names (grafana-instance-health-5594) and bare numeric IDs (5594).
				// DeleteFn extracts the numeric ID via extractIDFromSlug.
				if err := crud.Delete(ctx, name); err != nil {
					return fmt.Errorf("deleting check %s: %w", name, err)
				}
				cmdio.Success(cmd.OutOrStdout(), "Deleted check %s", name)
			}
			return nil
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
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	header := "NAME\tJOB\tTARGET\tTYPE"
	row := fmt.Sprintf("%s\t%s\t%s\t%s",
		checkDisplayName(c), c.Job, c.Target, c.Settings.CheckType())

	if wide {
		header += "\tENABLED\tFREQ\tTIMEOUT\tPROBES"
		row += fmt.Sprintf("\t%v\t%ds\t%ds\t%d",
			c.Enabled, c.Frequency/1000, c.Timeout/1000, len(c.Probes))
	}

	if info.Status != "" {
		successStr := "--"
		if info.Success != nil {
			successStr = fmt.Sprintf("%.2f%%", *info.Success*100)
		}
		header += "\tSUCCESS\tSTATUS"
		row += fmt.Sprintf("\t%s\t%s", successStr, info.Status)
	}

	fmt.Fprintln(tw, header)
	fmt.Fprintln(tw, row)

	return tw.Flush()
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
	count := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "---" {
			count++
			if count > 1 {
				return true
			}
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
