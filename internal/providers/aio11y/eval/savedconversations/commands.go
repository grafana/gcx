package savedconversations

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromCommand(cmd, loader)
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

type listOpts struct {
	IO     cmdio.Options
	Source string
	Limit  int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Source, "source", "", "Filter by source (telemetry or manual)")
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of saved conversations to return (0 for no limit)")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved conversations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.List(cmd.Context(), opts.Source, int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
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
		Use:   "get <saved-id>",
		Short: "Get a single saved conversation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			sc, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), sc)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
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

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			sc, err := client.Save(cmd.Context(), &SaveRequest{
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

type deleteOpts struct {
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Force, "force", "f", false, "Skip confirmation prompt")
}

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete SAVED-ID...",
		Short: "Delete saved conversations.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !opts.Force {
				fmt.Fprintf(cmd.ErrOrStderr(), "Delete %d saved conversation(s)? [y/N] ", len(args))
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("reading confirmation: %w", err)
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					cmdio.Info(cmd.ErrOrStderr(), "Aborted.")
					return nil
				}
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			for _, id := range args {
				if err := client.Delete(cmd.Context(), id); err != nil {
					return fmt.Errorf("deleting saved conversation %s: %w", id, err)
				}
				cmdio.Success(cmd.ErrOrStderr(), "Deleted saved conversation %s", id)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- collections (reverse lookup) ---

type collectionsOpts struct {
	IO cmdio.Options
}

func (o *collectionsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &CollectionsTableCodec{})
	o.IO.RegisterCustomCodec("wide", &CollectionsTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newCollectionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &collectionsOpts{}
	cmd := &cobra.Command{
		Use:   "collections <saved-id>",
		Short: "List collections that contain a saved conversation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			items, err := client.ListCollections(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table codecs ---

// TableCodec renders []SavedConversation rows.
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
	items, ok := v.([]SavedConversation)
	if !ok {
		return errors.New("invalid data type for table codec: expected []SavedConversation")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("SAVED ID", "NAME", "CONVERSATION", "SOURCE", "GENS", "SAVED BY", "CREATED AT")
	} else {
		t = style.NewTable("SAVED ID", "NAME", "CONVERSATION", "SOURCE", "GENS")
	}

	for _, sc := range items {
		name := aio11yhttp.Truncate(sc.Name, 40)
		source := sc.Source
		if source == "" {
			source = "-"
		}
		gens := strconv.Itoa(sc.GenerationCount)
		if c.Wide {
			savedBy := sc.SavedBy
			if savedBy == "" {
				savedBy = "-"
			}
			t.Row(sc.SavedID, name, sc.ConversationID, source, gens, savedBy, aio11yhttp.FormatTime(sc.CreatedAt))
		} else {
			t.Row(sc.SavedID, name, sc.ConversationID, source, gens)
		}
	}
	return t.Render(w)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// CollectionsTableCodec renders []CollectionRef rows for the reverse-lookup
// `saved-conversations collections <saved-id>` command.
type CollectionsTableCodec struct {
	Wide bool
}

func (c *CollectionsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *CollectionsTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]CollectionRef)
	if !ok {
		return errors.New("invalid data type for table codec: expected []CollectionRef")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("COLLECTION ID", "NAME", "MEMBERS", "DESCRIPTION", "CREATED BY", "CREATED AT")
	} else {
		t = style.NewTable("COLLECTION ID", "NAME", "MEMBERS")
	}

	for _, cref := range items {
		members := strconv.Itoa(cref.MemberCount)
		if c.Wide {
			desc := aio11yhttp.Truncate(cref.Description, 40)
			createdBy := cref.CreatedBy
			if createdBy == "" {
				createdBy = "-"
			}
			t.Row(cref.CollectionID, cref.Name, members, desc, createdBy, aio11yhttp.FormatTime(cref.CreatedAt))
		} else {
			t.Row(cref.CollectionID, cref.Name, members)
		}
	}
	return t.Render(w)
}

func (c *CollectionsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
