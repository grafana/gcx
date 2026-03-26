package checks

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/grafana/grafanactl/internal/format"
	cmdio "github.com/grafana/grafanactl/internal/output"
	"github.com/grafana/grafanactl/internal/providers/synth/smcfg"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
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
		newPushCommand(loader),
		newPullCommand(loader),
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
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &checkTableCodec{})
	o.IO.RegisterCustomCodec("wide", &checkWideTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListCommand(loader smcfg.Loader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Synthetic Monitoring checks.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

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

			// Extract checkResource from TypedObject
			checkResources := make([]checkResource, len(typedObjs))
			for i := range typedObjs {
				checkResources[i] = typedObjs[i].Spec
			}

			// Convert checkResources to Check for table codecs
			checkList := make([]Check, len(checkResources))
			for i, cr := range checkResources {
				checkList[i] = Check{
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
					Probes:           []int64{}, // Probe IDs not available from checkResource
				}
			}

			if codec.Format() == "table" || codec.Format() == "wide" {
				return codec.Encode(cmd.OutOrStdout(), checkList)
			}

			// For yaml/json output, marshal typed objects to unstructured
			var objs []unstructured.Unstructured
			for _, typedObj := range typedObjs {
				// Marshal to JSON to ensure proper K8s envelope structure
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
	fmt.Fprintln(tw, "ID\tJOB\tTARGET\tTYPE")

	for _, c := range checkList {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n",
			c.ID, c.Job, c.Target, c.Settings.CheckType())
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
	fmt.Fprintln(tw, "ID\tJOB\tTARGET\tTYPE\tENABLED\tFREQ\tTIMEOUT\tPROBES")

	for _, c := range checkList {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%v\t%ds\t%ds\t%d\n",
			c.ID, c.Job, c.Target, c.Settings.CheckType(), c.Enabled,
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
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &checkTableCodec{})
	o.IO.RegisterCustomCodec("wide", &checkWideTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader smcfg.Loader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get ID",
		Short: "Get a single Synthetic Monitoring check.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Try to parse as a numeric ID for compatibility
			_, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid check ID %q: must be a number or name", args[0])
			}

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, args[0])
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
				return codec.Encode(cmd.OutOrStdout(), []Check{c})
			}

			// For yaml/json, use the typed object
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
// pull
// ---------------------------------------------------------------------------

type pullOpts struct {
	OutputDir string
}

func (o *pullOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.OutputDir, "output", "d", ".", "Directory to write check YAML files to")
}

func newPullCommand(loader smcfg.Loader) *cobra.Command {
	opts := &pullOpts{}
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull Synthetic Monitoring checks to disk.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx)
			if err != nil {
				return err
			}

			outputDir := filepath.Join(opts.OutputDir, "checks")
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("creating output directory %s: %w", outputDir, err)
			}

			yamlCodec := format.NewYAMLCodec()

			for _, typedObj := range typedObjs {
				// Convert to unstructured for YAML encoding
				objData, err := json.Marshal(typedObj)
				if err != nil {
					return fmt.Errorf("marshaling typed object: %w", err)
				}
				var obj unstructured.Unstructured
				if err := json.Unmarshal(objData, &obj); err != nil {
					return fmt.Errorf("unmarshaling to unstructured: %w", err)
				}

				cr := typedObj.Spec
				filePath := filepath.Join(outputDir, strconv.FormatInt(cr.checkID, 10)+".yaml")
				f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
				if err != nil {
					return fmt.Errorf("opening file %s: %w", filePath, err)
				}

				if err := yamlCodec.Encode(f, &obj); err != nil {
					f.Close()
					return fmt.Errorf("writing check %d: %w", cr.checkID, err)
				}
				f.Close()
			}

			cmdio.Success(cmd.OutOrStdout(), "Pulled %d checks to %s/", len(typedObjs), outputDir)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// push
// ---------------------------------------------------------------------------

type pushOpts struct {
	DryRun bool
}

func (o *pushOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview changes without applying them")
}

func newPushCommand(loader smcfg.Loader) *cobra.Command {
	opts := &pushOpts{}
	cmd := &cobra.Command{
		Use:   "push FILE...",
		Short: "Push Synthetic Monitoring checks from files.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			crud, namespace, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			yamlCodec := format.NewYAMLCodec()

			for _, filePath := range args {
				data, err := os.ReadFile(filePath)
				if err != nil {
					return fmt.Errorf("reading %s: %w", filePath, err)
				}

				var obj unstructured.Unstructured
				if err := yamlCodec.Decode(strings.NewReader(string(data)), &obj); err != nil {
					return fmt.Errorf("parsing %s: %w", filePath, err)
				}

				res, err := resources.FromUnstructured(&obj)
				if err != nil {
					return fmt.Errorf("building resource from %s: %w", filePath, err)
				}

				spec, id, err := FromResource(res)
				if err != nil {
					return fmt.Errorf("converting resource from %s: %w", filePath, err)
				}

				// Set namespace from context if missing.
				if obj.GetNamespace() == "" {
					obj.SetNamespace(namespace)
				}

				// Reconstruct checkResource from spec and ID
				cr := checkResource{
					CheckSpec: *spec,
					name:      "",
					checkID:   id,
				}
				if id != 0 {
					cr.name = slugifyJob(spec.Job) + "-" + strconv.FormatInt(id, 10)
				} else {
					cr.name = slugifyJob(spec.Job)
				}

				if opts.DryRun {
					action := "create"
					if id > 0 {
						action = "update"
					}
					cmdio.Info(cmd.OutOrStdout(), "[dry-run] Would %s check %q (id=%d)", action, spec.Job, id)
					continue
				}

				// Create typed object for CRUD operations
				typedObj := &adapter.TypedObject[checkResource]{
					Spec: cr,
				}
				typedObj.SetName(cr.name)
				typedObj.SetNamespace(namespace)

				if id == 0 {
					created, err := crud.Create(ctx, typedObj)
					if err != nil {
						return fmt.Errorf("creating check %q: %w", spec.Job, err)
					}
					cmdio.Success(cmd.OutOrStdout(), "Created check %q (id=%d)", spec.Job, created.Spec.checkID)

					// Update the local YAML file with the server-assigned ID.
					if err := updateNameInFile(filePath, strconv.FormatInt(created.Spec.checkID, 10)); err != nil {
						cmdio.Warning(cmd.OutOrStdout(), "Check created but could not update %s: %v", filePath, err)
					}
				} else {
					if _, err := crud.Update(ctx, cr.name, typedObj); err != nil {
						return fmt.Errorf("updating check %d: %w", id, err)
					}
					cmdio.Success(cmd.OutOrStdout(), "Updated check %q (id=%d)", spec.Job, id)
				}
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// updateNameInFile rewrites metadata.name in a YAML file to newName.
// This is used after a create to persist the server-assigned numeric ID.
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
		Use:   "delete ID...",
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

			for _, arg := range args {
				// Try as numeric ID first, otherwise use as name
				id, err := strconv.ParseInt(arg, 10, 64)
				var name string
				if err == nil && id > 0 {
					name = slugifyJob("") + "-" + strconv.FormatInt(id, 10)
				} else {
					name = arg
				}

				if err := crud.Delete(ctx, name); err != nil {
					return fmt.Errorf("deleting check %s: %w", arg, err)
				}
				cmdio.Success(cmd.OutOrStdout(), "Deleted check %s", arg)
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
