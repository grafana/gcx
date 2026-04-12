package definitions

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
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

	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for all)")
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

			cmdio.Success(cmd.OutOrStdout(), "Pulled %d SLO definitions to %s/", len(typedObjs), outputDir)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// push command
// ---------------------------------------------------------------------------

type pushOpts struct {
	DryRun bool
}

func (o *pushOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview changes without making them")
}

func newPushCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &pushOpts{}
	cmd := &cobra.Command{
		Use:   "push FILE...",
		Short: "Push SLO definitions from files.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			yamlCodec := format.NewYAMLCodec()

			for _, filePath := range args {
				data, err := os.ReadFile(filePath)
				if err != nil {
					return fmt.Errorf("failed to read file %s: %w", filePath, err)
				}

				// Decode YAML into an unstructured object
				var obj unstructured.Unstructured
				if err := yamlCodec.Decode(strings.NewReader(string(data)), &obj); err != nil {
					return fmt.Errorf("failed to parse %s: %w", filePath, err)
				}

				res, err := resources.FromUnstructured(&obj)
				if err != nil {
					return fmt.Errorf("failed to build resource from %s: %w", filePath, err)
				}

				slo, err := FromResource(res)
				if err != nil {
					return fmt.Errorf("failed to convert resource to SLO from %s: %w", filePath, err)
				}

				if opts.DryRun {
					cmdio.Info(cmd.OutOrStdout(), "[dry-run] Would push SLO %q (uuid=%s)", slo.Name, slo.UUID)
					continue
				}

				if err := upsertSLO(ctx, cmd, crud, slo); err != nil {
					return err
				}
			}

			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// upsertSLO creates or updates an SLO depending on whether it already exists.
// If slo.UUID is set, it checks the server; a 404 means create, otherwise update.
// If slo.UUID is empty, it always creates.
func upsertSLO(ctx context.Context, cmd *cobra.Command, crud *adapter.TypedCRUD[Slo], slo *Slo) error {
	if slo.UUID == "" {
		// Wrap in TypedObject for create
		typedObj := &adapter.TypedObject[Slo]{
			Spec: *slo,
		}
		created, err := crud.Create(ctx, typedObj)
		if err != nil {
			return fmt.Errorf("failed to create SLO %s: %w", slo.Name, err)
		}
		cmdio.Success(cmd.OutOrStdout(), "Created %s (uuid=%s)", slo.Name, created.Spec.UUID)
		return nil
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
			return fmt.Errorf("failed to update SLO %s: %w", slo.UUID, err)
		}
		cmdio.Success(cmd.OutOrStdout(), "Updated %s", slo.Name)
		return nil

	case errors.Is(getErr, ErrNotFound):
		// SLO not found — create.
		typedObj := &adapter.TypedObject[Slo]{
			Spec: *slo,
		}
		created, err := crud.Create(ctx, typedObj)
		if err != nil {
			return fmt.Errorf("failed to create SLO %s: %w", slo.Name, err)
		}
		cmdio.Success(cmd.OutOrStdout(), "Created %s (uuid=%s)", slo.Name, created.Spec.UUID)
		return nil

	default:
		// Any other error (auth, network, server) — propagate.
		return fmt.Errorf("failed to check SLO %s: %w", slo.UUID, getErr)
	}
}

// ---------------------------------------------------------------------------
// delete command
// ---------------------------------------------------------------------------

type deleteOpts struct {
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Force, "force", "f", false, "Skip confirmation prompt")
}

func newDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete UUID...",
		Short: "Delete SLO definitions.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if !opts.Force {
				fmt.Fprintf(cmd.OutOrStdout(), "Delete %d SLO definition(s)? [y/N] ", len(args))
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("failed to read confirmation: %w", err)
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

			for _, uuid := range args {
				if err := crud.Delete(ctx, uuid); err != nil {
					return fmt.Errorf("failed to delete SLO %s: %w", uuid, err)
				}
				cmdio.Success(cmd.OutOrStdout(), "Deleted %s", uuid)
			}

			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
