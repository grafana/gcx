package collections

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/commandutil"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/savedconversations"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromCommand(cmd, loader)
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
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
		newListConversationsCommand(loader),
		newAddConversationsCommand(loader),
		newRemoveConversationCommand(loader),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of collections to return (0 for no limit)")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List collections.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, opts.Limit)
			if err != nil {
				return err
			}

			specs := make([]Collection, len(typedObjs))
			for i := range typedObjs {
				specs[i] = typedObjs[i].Spec
			}

			if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
				return opts.IO.Encode(cmd.OutOrStdout(), specs)
			}

			objs := make([]unstructured.Unstructured, 0, len(specs))
			for _, spec := range specs {
				u, err := crud.ToUnstructured(spec)
				if err != nil {
					return err
				}
				objs = append(objs, u)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <collection-id>",
		Short: "Get a single collection.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return err
			}

			u, err := crud.ToUnstructured(typedObj.Spec)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), &u)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
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
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var col Collection
	if err := json.Unmarshal(data, &col); err != nil {
		var yamlCol Collection
		if yamlErr := yaml.Unmarshal(data, &yamlCol); yamlErr != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, yamlErr)
		}
		col = yamlCol
	}
	if strings.TrimSpace(col.Name) == "" {
		return nil, fmt.Errorf("parsing %s: name is required", path)
	}
	return &col, nil
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new collection.",
		Example: `  # Create with inline flags.
  gcx agento11y collections create --name "Regression suite" --description "Nightly regression"

  # Create from a YAML file (either raw {name,description} or a typed resource envelope).
  gcx agento11y collections create -f collection.yaml`,
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
			crud, _, err := NewTypedCRUD(ctx, loader)
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
			crud, client, err := newCRUDAndClient(ctx, loader)
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

type deleteOpts struct {
	IO    cmdio.Options
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	// The delete result is a BatchMutation document through the codec
	// system: the human text default stays silent (per-id receipts go to
	// stderr, as they always have); agent mode and explicit -o json/yaml
	// get the structured document.
	o.IO.RegisterCustomCodec("text", commandutil.SilentTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete COLLECTION-ID...",
		Short: "Delete collections.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete %d collection(s)?", len(args)))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			ctx := cmd.Context()
			crud, _, err := NewTypedCRUD(ctx, loader)
			if err != nil {
				return err
			}

			return runDelete(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, args, func(id string) error {
				return crud.Delete(ctx, id)
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runDelete performs the delete loop and writes the result document. Split
// from RunE so the output contract is testable without a live plugin API.
func runDelete(stdout, stderr io.Writer, opts *deleteOpts, ids []string, del func(id string) error) error {
	return commandutil.RunBatchDelete(stdout, stderr, &opts.IO,
		"collection", "Deleted collection %s", "deleting collection %s", ids, del)
}

// --- membership commands ---
//
// Collection membership is addressed by the parent collection's ID, so the
// three commands are operation-subject compounds directly under `collections`
// (list-conversations / add-conversations / remove-conversation) rather than
// a nested noun group.

type membersListOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *membersListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &savedconversations.TableCodec{})
	o.IO.RegisterCustomCodec("wide", &savedconversations.TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of saved conversations to return (0 for no limit)")
}

func newListConversationsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &membersListOpts{}
	cmd := &cobra.Command{
		Use:   "list-conversations <collection-id>",
		Short: "List saved conversations belonging to a collection.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.ListMembers(cmd.Context(), args[0], int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// membershipResult is the finite result of a collection membership mutation
// (add-conversations / remove-conversation). The cmdio mutation family cannot
// represent a relation — the acted-on pair of a collection and its member
// saved-conversation ids — so this bespoke shape carries the required
// type/schema_version discriminators itself.
type membershipResult struct {
	Type          string   `json:"type" yaml:"type"`
	SchemaVersion string   `json:"schema_version" yaml:"schema_version"`
	Action        string   `json:"action" yaml:"action"`
	CollectionID  string   `json:"collection_id" yaml:"collection_id"`
	SavedIDs      []string `json:"saved_ids" yaml:"saved_ids"`
}

// newMembershipResult returns a membershipResult with the discriminators set.
func newMembershipResult(action, collectionID string, savedIDs []string) membershipResult {
	return membershipResult{
		Type:          "gcx.aio11y.collection_membership",
		SchemaVersion: "1",
		Action:        action,
		CollectionID:  collectionID,
		SavedIDs:      savedIDs,
	}
}

type membershipOpts struct {
	IO cmdio.Options
}

func (o *membershipOpts) setup(flags *pflag.FlagSet) {
	// The membership result is a document through the codec system: the
	// human text default stays silent (the receipt goes to stderr, as it
	// always has); agent mode and explicit -o json/yaml get the structured
	// document.
	o.IO.RegisterCustomCodec("text", commandutil.SilentTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newAddConversationsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &membershipOpts{}
	cmd := &cobra.Command{
		Use:   "add-conversations <collection-id> <saved-id>...",
		Short: "Add one or more saved conversations to a collection.",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			collectionID := args[0]
			savedIDs := args[1:]
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			if err := client.AddMembers(cmd.Context(), collectionID, savedIDs); err != nil {
				return err
			}
			return emitAddConversationsReceipt(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, collectionID, savedIDs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// emitAddConversationsReceipt writes the stderr receipt and the stdout result
// document for a completed add-conversations call. Split from RunE so the
// output contract is testable without a live plugin API.
func emitAddConversationsReceipt(stdout, stderr io.Writer, opts *membershipOpts, collectionID string, savedIDs []string) error {
	cmdio.Success(stderr, "Added %d conversation(s) to collection %s", len(savedIDs), collectionID)
	return opts.IO.Encode(stdout, newMembershipResult("added", collectionID, savedIDs))
}

func newRemoveConversationCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &membershipOpts{}
	cmd := &cobra.Command{
		Use:   "remove-conversation <collection-id> <saved-id>",
		Short: "Remove a single saved conversation from a collection.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			collectionID := args[0]
			savedID := args[1]
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			if err := client.RemoveMember(cmd.Context(), collectionID, savedID); err != nil {
				return err
			}
			return emitRemoveConversationReceipt(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, collectionID, savedID)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// emitRemoveConversationReceipt writes the stderr receipt and the stdout
// result document for a completed remove-conversation call. Split from RunE
// so the output contract is testable without a live plugin API.
func emitRemoveConversationReceipt(stdout, stderr io.Writer, opts *membershipOpts, collectionID, savedID string) error {
	cmdio.Success(stderr, "Removed %s from collection %s", savedID, collectionID)
	return opts.IO.Encode(stdout, newMembershipResult("removed", collectionID, []string{savedID}))
}

// --- table codecs ---

// TableCodec renders []Collection rows.
type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]Collection)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Collection")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "MEMBERS", "DESCRIPTION", "CREATED BY", "CREATED AT", "UPDATED AT")
	} else {
		t = style.NewTable("ID", "NAME", "MEMBERS", "DESCRIPTION")
	}

	for _, col := range items {
		desc := aio11yhttp.Truncate(col.Description, 40)
		members := strconv.Itoa(col.MemberCount)
		if c.Wide {
			createdBy := col.CreatedBy
			if createdBy == "" {
				createdBy = "-"
			}
			t.Row(col.CollectionID, col.Name, members, desc, createdBy, aio11yhttp.FormatTime(col.CreatedAt), aio11yhttp.FormatTime(col.UpdatedAt))
		} else {
			t.Row(col.CollectionID, col.Name, members, desc)
		}
	}
	return t.Render(w)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
