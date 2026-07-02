package collections

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/savedconversations"
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newClient(ctx context.Context, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromContext(ctx, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the collections command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collections",
		Short: "Manage named groups of saved conversations.",
	}
	cmd.AddCommand(
		newListCommand(),
		newGetCommand(),
		newCreateCommand(),
		newUpdateCommand(loader),
		newDeleteCommand(),
		newConversationsCommand(loader),
	)
	return cmd
}

// --- list ---

func newListCommand() *cobra.Command {
	return crudcmd.NewTypedListCommand(crudcmd.TypedListConfig[Collection]{
		Use:          "list",
		Short:        "List collections.",
		DefaultFmt:   "table",
		LimitDefault: 50,
		LimitUsage:   "Maximum number of collections to return (0 for no limit)",
		Codecs:       []format.Codec{&TableCodec{}, &TableCodec{Wide: true}},
		Noun:         "collection",
		NewCRUD:      NewTypedCRUD,
		ToResource: func(crud *adapter.TypedCRUD[Collection], item Collection) (unstructured.Unstructured, error) {
			return crud.ToUnstructured(item)
		},
	})
}

// --- get ---

func newGetCommand() *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*unstructured.Unstructured]{
		Use:        "get <collection-id>",
		Short:      "Get a single collection.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "yaml",
		Fetch: func(ctx context.Context, args []string) (*unstructured.Unstructured, error) {
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return nil, err
			}
			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return nil, err
			}
			u, err := crud.ToUnstructured(typedObj.Spec)
			if err != nil {
				return nil, err
			}
			return &u, nil
		},
	})
}

// --- create ---

type createOpts struct {
	IO          cmdio.Options
	File        string
	Name        string
	Description string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.File, "filename", "f", "", "File containing the collection create payload (use - for stdin)")
	flags.StringVar(&o.Name, "name", "", "Collection name (required if --filename is not given)")
	flags.StringVar(&o.Description, "description", "", "Collection description")
}

func (o *createOpts) Validate() error {
	if o.File == "" && strings.TrimSpace(o.Name) == "" {
		return errors.New("either --filename/-f or --name is required")
	}
	if o.File != "" && (o.Name != "" || o.Description != "") {
		return errors.New("--filename/-f is mutually exclusive with --name and --description")
	}
	return o.IO.Validate()
}

// readCollectionFile reads a Collection from a JSON or YAML file. For
// envelope-shaped YAMLs use `gcx resources push`.
func readCollectionFile(path string, stdin io.Reader) (*Collection, error) {
	col, err := crudcmd.ReadJSONOrYAMLFile[Collection](path, stdin)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(col.Name) == "" {
		return nil, fmt.Errorf("parsing %s: name is required", path)
	}
	return col, nil
}

func newCreateCommand() *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new collection.",
		Example: `  # Create with inline flags.
  gcx aio11y collections create --name "Regression suite" --description "Nightly regression"

  # Create from a YAML file (either raw {name,description} or a typed resource envelope).
  gcx aio11y collections create -f collection.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			var spec Collection
			if opts.File != "" {
				col, err := readCollectionFile(opts.File, cmd.InOrStdin())
				if err != nil {
					return err
				}
				spec = *col
			} else {
				spec = Collection{Name: opts.Name, Description: opts.Description}
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			created, err := crud.Create(ctx, &adapter.TypedObject[Collection]{Spec: spec})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Collection %s created", created.Spec.CollectionID)
			return opts.IO.Encode(cmd.OutOrStdout(), created.Spec)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- update ---

type updateOpts struct {
	IO          cmdio.Options
	Name        string
	Description string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Name, "name", "", "New collection name")
	flags.StringVar(&o.Description, "description", "", "New collection description")
}

// newUpdateCommand sends a true partial PATCH; TypedCRUD's full-spec UpdateFn
// cannot express field-presence semantics, so the request goes through the
// underlying Client. The response is rendered via TypedCRUD.ToUnstructured so
// JSON/YAML output matches `gcx resources get`.
func newUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <collection-id>",
		Short: "Patch a collection's name and/or description.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			req := &UpdateRequest{}
			if cmd.Flags().Changed("name") {
				name := opts.Name
				req.Name = &name
			}
			if cmd.Flags().Changed("description") {
				desc := opts.Description
				req.Description = &desc
			}
			if req.Name == nil && req.Description == nil {
				return errors.New("at least one of --name or --description is required")
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return err
			}

			client, err := newClient(ctx, loader)
			if err != nil {
				return err
			}
			updated, err := client.Update(ctx, args[0], req)
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Collection %s updated", updated.CollectionID)

			u, err := crud.ToUnstructured(*updated)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), &u)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- delete ---

func newDeleteCommand() *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete COLLECTION-ID...",
		Short: "Delete collections.",
		Args:  cobra.MinimumNArgs(1),
		Out:   func(cmd *cobra.Command) io.Writer { return cmd.ErrOrStderr() },
		Confirm: func(args []string) string {
			return fmt.Sprintf("Delete %d collection(s)?", len(args))
		},
		NewDelete: func(ctx context.Context) (func(string) error, error) {
			crud, _, err := NewTypedCRUD(ctx)
			if err != nil {
				return nil, err
			}
			return func(id string) error { return crud.Delete(ctx, id) }, nil
		},
		Success: func(id string) string { return "Deleted collection " + id },
	})
}

// --- conversations subgroup ---

func newConversationsCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conversations",
		Short: "Manage saved conversations belonging to a collection.",
	}
	cmd.AddCommand(
		newConversationsListCommand(loader),
		newConversationsAddCommand(loader),
		newConversationsRemoveCommand(loader),
	)
	return cmd
}

func newConversationsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &crudcmd.ListOpts{}
	cmd := &cobra.Command{
		Use:   "list <collection-id>",
		Short: "List saved conversations belonging to a collection.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newClient(ctx, loader)
			if err != nil {
				return err
			}
			items, err := client.ListMembers(ctx, args[0], int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.Setup(cmd.Flags(), "table", 50, "Maximum number of saved conversations to return (0 for no limit)",
		&savedconversations.TableCodec{}, &savedconversations.TableCodec{Wide: true})
	return cmd
}

func newConversationsAddCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <collection-id> <saved-id>...",
		Short: "Add one or more saved conversations to a collection.",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			collectionID := args[0]
			savedIDs := args[1:]
			ctx := cmd.Context()
			client, err := newClient(ctx, loader)
			if err != nil {
				return err
			}
			if err := client.AddMembers(ctx, collectionID, savedIDs); err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Added %d conversation(s) to collection %s", len(savedIDs), collectionID)
			return nil
		},
	}
	return cmd
}

func newConversationsRemoveCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <collection-id> <saved-id>",
		Short: "Remove a single saved conversation from a collection.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			collectionID := args[0]
			savedID := args[1]
			ctx := cmd.Context()
			client, err := newClient(ctx, loader)
			if err != nil {
				return err
			}
			if err := client.RemoveMember(ctx, collectionID, savedID); err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Removed %s from collection %s", savedID, collectionID)
			return nil
		},
	}
	return cmd
}

// --- table codecs ---

// TableCodec renders []Collection rows.
type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *TableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, col Collection) {
		desc := aio11yhttp.Truncate(col.Description, 40)
		members := strconv.Itoa(col.MemberCount)

		if !c.Wide {
			t.Row(col.CollectionID, col.Name, members, desc)
			return
		}

		createdBy := col.CreatedBy
		if createdBy == "" {
			createdBy = "-"
		}
		t.Row(col.CollectionID, col.Name, members, desc, createdBy, aio11yhttp.FormatTime(col.CreatedAt), aio11yhttp.FormatTime(col.UpdatedAt))
	}

	if c.Wide {
		return crudcmd.EncodeTable(w, v, "Collection", []string{"ID", "NAME", "MEMBERS", "DESCRIPTION", "CREATED BY", "CREATED AT", "UPDATED AT"}, row)
	}
	return crudcmd.EncodeTable(w, v, "Collection", []string{"ID", "NAME", "MEMBERS", "DESCRIPTION"}, row)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}
