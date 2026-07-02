package savedconversations

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
	"github.com/grafana/gcx/internal/providers/crudcmd"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(ctx context.Context, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromContext(ctx, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the saved-conversations command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "saved-conversations",
		Short: "Bookmark live conversations as fixed inputs for evaluation runs.",
	}
	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newSaveCommand(loader),
		newDeleteCommand(loader),
		newCollectionsCommand(loader),
	)
	return cmd
}

// --- list ---

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	var source string
	return crudcmd.NewListCommand(crudcmd.ListConfig[SavedConversation]{
		Use:          "list",
		Short:        "List saved conversations.",
		DefaultFmt:   "table",
		LimitDefault: 50,
		LimitUsage:   "Maximum number of saved conversations to return (0 for no limit)",
		Codecs:       []format.Codec{&TableCodec{}, &TableCodec{Wide: true}},
		ExtraFlags: func(flags *pflag.FlagSet) {
			flags.StringVar(&source, "source", "", "Filter by source (telemetry or manual)")
		},
		Fetch: func(ctx context.Context, limit int64) ([]SavedConversation, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.List(ctx, source, int(limit))
		},
	})
}

// --- get ---

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[*SavedConversation]{
		Use:        "get <saved-id>",
		Short:      "Get a single saved conversation.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "yaml",
		Fetch: func(ctx context.Context, args []string) (*SavedConversation, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.Get(ctx, args[0])
		},
	})
}

// --- save ---

type saveOpts struct {
	IO      cmdio.Options
	SavedID string
	Name    string
	Tags    []string
}

func (o *saveOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.SavedID, "saved-id", "", "Bookmark ID; defaults to saved-<conversation-id>")
	flags.StringVar(&o.Name, "name", "", "Human-readable name for the bookmark (required)")
	flags.StringArrayVar(&o.Tags, "tag", nil, "Tag in key=value form (repeatable)")
}

func (o *saveOpts) Validate() error {
	if strings.TrimSpace(o.Name) == "" {
		return errors.New("--name is required")
	}
	return o.IO.Validate()
}

func parseTags(raw []string) (map[string]string, error) {
	tags := make(map[string]string, len(raw))
	for _, t := range raw {
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --tag %q: expected key=value", t)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			return nil, fmt.Errorf("invalid --tag %q: empty key", t)
		}
		tags[k] = v
	}
	return tags, nil
}

func newSaveCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &saveOpts{}
	cmd := &cobra.Command{
		Use:   "save <conversation-id>",
		Short: "Bookmark an existing live conversation as a saved conversation.",
		Long: `Bookmark a live conversation surfaced by gcx aio11y conversations.
By default the bookmark ID is derived as saved-<conversation-id>, matching the
plugin UI; pass --saved-id to override.`,
		Example: `  # Bookmark with the default saved ID.
  gcx aio11y saved-conversations save conv-123 --name "Regression seed"

  # Bookmark with tags.
  gcx aio11y saved-conversations save conv-123 --name "Regression seed" --tag suite=checkout --tag priority=high`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			tags, err := parseTags(opts.Tags)
			if err != nil {
				return err
			}
			conversationID := args[0]
			savedID := opts.SavedID
			if savedID == "" {
				savedID = "saved-" + conversationID
			}

			ctx := cmd.Context()
			client, err := newClient(ctx, loader)
			if err != nil {
				return err
			}
			sc, err := client.Save(ctx, &SaveRequest{
				SavedID:        savedID,
				ConversationID: conversationID,
				Name:           opts.Name,
				Tags:           tags,
			})
			if err != nil {
				return err
			}
			cmdio.Success(cmd.ErrOrStderr(), "Saved conversation %s", sc.SavedID)
			return opts.IO.Encode(cmd.OutOrStdout(), sc)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- delete ---

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	return crudcmd.NewDeleteCommand(crudcmd.DeleteConfig{
		Use:   "delete SAVED-ID...",
		Short: "Delete saved conversations.",
		Args:  cobra.MinimumNArgs(1),
		Out:   func(cmd *cobra.Command) io.Writer { return cmd.ErrOrStderr() },
		Confirm: func(args []string) string {
			return fmt.Sprintf("Delete %d saved conversation(s)?", len(args))
		},
		NewDelete: func(ctx context.Context) (func(string) error, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return func(id string) error { return client.Delete(ctx, id) }, nil
		},
		Success: func(id string) string { return "Deleted saved conversation " + id },
	})
}

// --- collections (reverse lookup) ---

func newCollectionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	return crudcmd.NewGetCommand(crudcmd.GetConfig[[]CollectionRef]{
		Use:        "collections <saved-id>",
		Short:      "List collections that contain a saved conversation.",
		Args:       cobra.ExactArgs(1),
		DefaultFmt: "table",
		Codecs:     []format.Codec{&CollectionsTableCodec{}, &CollectionsTableCodec{Wide: true}},
		Fetch: func(ctx context.Context, args []string) ([]CollectionRef, error) {
			client, err := newClient(ctx, loader)
			if err != nil {
				return nil, err
			}
			return client.ListCollections(ctx, args[0])
		},
	})
}

// --- table codecs ---

// TableCodec renders []SavedConversation rows.
type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *TableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, sc SavedConversation) {
		name := aio11yhttp.Truncate(sc.Name, 40)
		source := sc.Source
		if source == "" {
			source = "-"
		}
		gens := strconv.Itoa(sc.GenerationCount)

		if !c.Wide {
			t.Row(sc.SavedID, name, sc.ConversationID, source, gens)
			return
		}

		savedBy := sc.SavedBy
		if savedBy == "" {
			savedBy = "-"
		}
		t.Row(sc.SavedID, name, sc.ConversationID, source, gens, savedBy, aio11yhttp.FormatTime(sc.CreatedAt))
	}

	if c.Wide {
		return crudcmd.EncodeTable(w, v, "SavedConversation", []string{"SAVED ID", "NAME", "CONVERSATION", "SOURCE", "GENS", "SAVED BY", "CREATED AT"}, row)
	}
	return crudcmd.EncodeTable(w, v, "SavedConversation", []string{"SAVED ID", "NAME", "CONVERSATION", "SOURCE", "GENS"}, row)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}

// CollectionsTableCodec renders []CollectionRef rows for the reverse-lookup
// `saved-conversations collections <saved-id>` command.
type CollectionsTableCodec struct {
	Wide bool
}

func (c *CollectionsTableCodec) Format() format.Format { return crudcmd.WideFormat(c.Wide) }

func (c *CollectionsTableCodec) Encode(w io.Writer, v any) error {
	row := func(t *style.TableBuilder, cref CollectionRef) {
		members := strconv.Itoa(cref.MemberCount)
		if !c.Wide {
			t.Row(cref.CollectionID, cref.Name, members)
			return
		}
		desc := aio11yhttp.Truncate(cref.Description, 40)
		createdBy := cref.CreatedBy
		if createdBy == "" {
			createdBy = "-"
		}
		t.Row(cref.CollectionID, cref.Name, members, desc, createdBy, aio11yhttp.FormatTime(cref.CreatedAt))
	}

	if c.Wide {
		return crudcmd.EncodeTable(w, v, "CollectionRef", []string{"COLLECTION ID", "NAME", "MEMBERS", "DESCRIPTION", "CREATED BY", "CREATED AT"}, row)
	}
	return crudcmd.EncodeTable(w, v, "CollectionRef", []string{"COLLECTION ID", "NAME", "MEMBERS"}, row)
}

func (c *CollectionsTableCodec) Decode(_ io.Reader, _ any) error {
	return crudcmd.ErrTableDecode
}
