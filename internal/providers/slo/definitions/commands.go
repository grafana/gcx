package definitions

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
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

func newListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &crudcmd.ListOpts{}
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
	opts.Setup(cmd.Flags(), "table", 0,
		"Maximum number of items to return after fetch (0 for all; use a positive value to trim output only)",
		&sloTableCodec{}, &sloTableCodec{Wide: true})
	return cmd
}

// sloTableCodec renders SLOs as a tabular table.
type sloTableCodec struct {
	Wide bool
}

func (c *sloTableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *sloTableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, slo Slo) {
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
			return
		}
		t.Row(slo.UUID, slo.Name, target, window, status)
	}

	if c.Wide {
		return crudcmd.EncodeTable(w, v, "Slo", []string{"UUID", "NAME", "TARGET", "WINDOW", "STATUS", "DESCRIPTION"}, row)
	}
	return crudcmd.EncodeTable(w, v, "Slo", []string{"UUID", "NAME", "TARGET", "WINDOW", "STATUS"}, row)
}

func (c *sloTableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

func newGetCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*unstructured.Unstructured]{
		Use:        "get UUID",
		Short:      "Get a single SLO definition.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "yaml",
		Fetch: func(ctx context.Context, args []string) (*unstructured.Unstructured, error) {
			crud, cfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return nil, err
			}

			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return nil, err
			}

			res, err := ToResource(typedObj.Spec, cfg.Namespace)
			if err != nil {
				return nil, fmt.Errorf("failed to convert SLO to resource: %w", err)
			}

			obj := res.ToUnstructured()
			return &obj, nil
		},
	})
}

// ---------------------------------------------------------------------------
// pull command
// ---------------------------------------------------------------------------

func newPullCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewPullCommand(crudcmd.PullConfig[Slo]{
		Use:         "pull",
		Short:       "Pull SLO definitions to disk.",
		OutputUsage: "Directory to write SLO definition files to",
		SubDir:      "SLO",
		Noun:        "SLO definitions",
		Fetch: func(ctx context.Context) ([]Slo, string, error) {
			crud, cfg, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return nil, "", err
			}
			typedObjs, err := crud.List(ctx, 0)
			if err != nil {
				return nil, "", err
			}
			slos := make([]Slo, len(typedObjs))
			for i := range typedObjs {
				slos[i] = typedObjs[i].Spec
			}
			return slos, cfg.Namespace, nil
		},
		ToResource: ToResource,
		ID:         func(slo Slo) string { return slo.UUID },
	})
}

// ---------------------------------------------------------------------------
// push command
// ---------------------------------------------------------------------------

func newPushCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewPushCommand(crudcmd.PushConfig[Slo]{
		Use:          "push FILE...",
		Short:        "Push SLO definitions from files.",
		FromResource: FromResource,
		NewUpsert: func(ctx context.Context) (func(cmd *cobra.Command, item *Slo) error, error) {
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return nil, err
			}
			return func(cmd *cobra.Command, slo *Slo) error {
				return crudcmd.Upsert(ctx, *slo, sloUpsertConfig(cmd, crud))
			}, nil
		},
		Name: func(s Slo) string { return s.Name },
		ID:   func(s Slo) string { return s.UUID },
	})
}

// sloUpsertConfig adapts TypedCRUD[Slo] to the generic create-or-update-by-
// probing-404 flow shared with other push commands.
func sloUpsertConfig(cmd *cobra.Command, crud *adapter.TypedCRUD[Slo]) crudcmd.UpsertConfig[Slo] {
	return crudcmd.UpsertConfig[Slo]{
		HasID: func(slo Slo) bool { return slo.UUID != "" },
		ID:    func(slo Slo) string { return slo.UUID },
		Name:  func(slo Slo) string { return slo.Name },
		Get: func(ctx context.Context, id string) error {
			_, err := crud.Get(ctx, id)
			return err
		},
		IsNotFound: func(err error) bool { return errors.Is(err, ErrNotFound) },
		Create: func(ctx context.Context, slo Slo) (Slo, error) {
			created, err := crud.Create(ctx, &adapter.TypedObject[Slo]{Spec: slo})
			if err != nil {
				return Slo{}, err
			}
			return created.Spec, nil
		},
		Update: func(ctx context.Context, id string, slo Slo) error {
			typedObj := &adapter.TypedObject[Slo]{Spec: slo}
			typedObj.SetName(id)
			_, err := crud.Update(ctx, id, typedObj)
			return err
		},
		OnCreated: func(created Slo) {
			cmdio.Success(cmd.OutOrStdout(), "Created %s (uuid=%s)", created.Name, created.UUID)
		},
		OnUpdated: func(slo Slo) {
			cmdio.Success(cmd.OutOrStdout(), "Updated %s", slo.Name)
		},
	}
}

// ---------------------------------------------------------------------------
// delete command
// ---------------------------------------------------------------------------

func newDeleteCommand(loader GrafanaConfigLoader) *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete UUID...",
		Short: "Delete SLO definitions.",
		Args:  cobra.MinimumNArgs(1),
		Confirm: func(args []string) string {
			return fmt.Sprintf("Delete %d SLO definition(s)?", len(args))
		},
		NewDelete: func(ctx context.Context) (func(string) error, error) {
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return nil, err
			}
			return func(uuid string) error { return crud.Delete(ctx, uuid) }, nil
		},
		Success: func(uuid string) string { return "Deleted " + uuid },
	})
}
