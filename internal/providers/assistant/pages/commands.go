// Package pages exposes Grafana Assistant Pages commands: list and read
// published pages, pull a page's source project, and push a changed project
// back as the page's next version.
package pages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	clientpages "github.com/grafana/gcx/internal/assistant/pages"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const experimental = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions. It needs Pages and Pages import and export enabled on the stack."

func markExperimental(cmd *cobra.Command) {
	cmd.Short = "[experimental] " + cmd.Short
	cmd.Long = experimental + "\n\n" + cmd.Long
	cmd.Annotations = map[string]string{agent.AnnotationStability: agent.StabilityExperimental}
}

// Commands returns the experimental Pages subtree.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pages",
		Short: "Read and update Grafana Assistant Pages.",
		Long:  "Pages are React apps the Grafana Assistant builds and publishes in the page catalog. List and get read published pages. Pull writes a page's source project to a directory; edit it there, then push builds it on the stack and publishes it as the page's next version.",
		Example: "  gcx assistant pages list --section mine\n" +
			"  gcx assistant pages pull 3f2b8c1e-7a4d-4e9b-9c6a-5d1e2f3a4b5c --dir ./page\n" +
			"  gcx assistant pages push --dir ./page --dry-run\n" +
			"  gcx assistant pages push --dir ./page",
	}
	markExperimental(cmd)
	cmd.AddCommand(newListCommand(loader), newGetCommand(loader), newPullCommand(loader), newPushCommand(loader))
	return cmd
}

// session is a configured Pages client and the stack's Grafana URL.
type session struct {
	client     *clientpages.Client
	grafanaURL string
}

func newSession(cmd *cobra.Command, loader *providers.ConfigLoader) (*session, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	grafanaURL := cfg.GrafanaURL
	if grafanaURL == "" {
		grafanaURL = cfg.Host
	}
	return &session{client: clientpages.NewClient(base), grafanaURL: strings.TrimRight(grafanaURL, "/")}, nil
}

func (s *session) pageURL(id string) string {
	return s.grafanaURL + "/a/grafana-assistant-app/pages/" + id
}

func bindIO(opts *cmdio.Options, flags *pflag.FlagSet) {
	opts.RegisterCustomCodec("text", &textCodec{})
	opts.DefaultFormat("text")
	opts.BindFlags(flags)
}

// ---------------------------------------------------------------- list

type listOpts struct {
	IO      cmdio.Options
	Section string
	Search  string
	Limit   int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	bindIO(&o.IO, flags)
	flags.StringVar(&o.Section, "section", "mine", "Which pages to list: mine (your pages), discover (folder pages), starred, or unlisted")
	flags.StringVar(&o.Search, "search", "", "Only pages matching this text")
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of pages to return")
}

func (o *listOpts) Validate() error {
	switch o.Section {
	case "mine", "discover", "starred", "unlisted":
	default:
		return fmt.Errorf("--section must be mine, discover, starred or unlisted, not %q", o.Section)
	}
	if o.Limit < 1 {
		return errors.New("--limit must be at least 1")
	}
	return o.IO.Validate()
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List published Assistant Pages.",
		Long:  "List pages in the page catalog that your identity can open, newest first. Use get PAGE for one page's details and pull PAGE for its source.",
		Args:  cobra.NoArgs,
		Example: "  gcx assistant pages list\n" +
			"  gcx assistant pages list --section discover --search radar\n" +
			"  gcx assistant pages list -o json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			s, err := newSession(cmd, loader)
			if err != nil {
				return err
			}
			entries, more, err := listAll(cmd.Context(), s.client, opts)
			if err != nil {
				return err
			}
			if more {
				cmdio.Warning(cmd.ErrOrStderr(), "Showing the first %d pages; raise --limit or narrow with --search to see the rest.", len(entries))
			}
			return opts.IO.Encode(cmd.OutOrStdout(), entries)
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

// listAll follows the catalog's cursor until it has limit pages. more
// reports that the catalog holds pages past the limit.
func listAll(ctx context.Context, client *clientpages.Client, opts *listOpts) ([]clientpages.Entry, bool, error) {
	entries := []clientpages.Entry{}
	cursor := ""
	for {
		batch := min(opts.Limit-len(entries), 100)
		page, next, err := client.List(ctx, clientpages.ListOptions{Section: opts.Section, Search: opts.Search, Limit: batch, Cursor: cursor})
		if err != nil {
			return nil, false, err
		}
		entries = append(entries, page...)
		if next == "" {
			return entries, false, nil
		}
		if len(entries) >= opts.Limit {
			return entries[:opts.Limit], true, nil
		}
		cursor = next
	}
}

// ---------------------------------------------------------------- get

type getOpts struct {
	IO cmdio.Options
	ID string
}

func (o *getOpts) setup(flags *pflag.FlagSet) { bindIO(&o.IO, flags) }

func (o *getOpts) Validate() error {
	if strings.TrimSpace(o.ID) == "" {
		return errors.New("PAGE must be a page id; find one with gcx assistant pages list")
	}
	return o.IO.Validate()
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get PAGE",
		Short: "Get a published Assistant Page.",
		Long:  "Read one published page's details: title, version, revision, folder and sharing. PAGE is the page id from list or from the page's URL.",
		Args:  cobra.ExactArgs(1),
		Example: "  gcx assistant pages get 3f2b8c1e-7a4d-4e9b-9c6a-5d1e2f3a4b5c\n" +
			"  gcx assistant pages get 3f2b8c1e-7a4d-4e9b-9c6a-5d1e2f3a4b5c -o json",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ID = pageID(args[0])
			if err := opts.Validate(); err != nil {
				return err
			}
			s, err := newSession(cmd, loader)
			if err != nil {
				return err
			}
			entry, err := s.client.Get(cmd.Context(), opts.ID)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), getResult{Entry: *entry, URL: s.pageURL(entry.ID)})
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

type getResult struct {
	clientpages.Entry

	URL string `json:"url"`
}

// pageID accepts a page id or a page URL.
func pageID(ref string) string {
	ref = strings.TrimSpace(ref)
	if i := strings.Index(ref, "/pages/"); i >= 0 {
		ref = ref[i+len("/pages/"):]
		if j := strings.IndexAny(ref, "/?#"); j >= 0 {
			ref = ref[:j]
		}
	}
	// Catalog URLs may carry a slug before the id: <slug>-<uuid>.
	if len(ref) > 36 && ref[len(ref)-37] == '-' {
		ref = ref[len(ref)-36:]
	}
	return ref
}

// ---------------------------------------------------------------- pull

type pullOpts struct {
	IO    cmdio.Options
	ID    string
	Dir   string
	Force bool
}

func (o *pullOpts) setup(flags *pflag.FlagSet) {
	bindIO(&o.IO, flags)
	flags.StringVarP(&o.Dir, "dir", "d", "", "Directory to write the page's source to (default: ./<page id>)")
	flags.BoolVar(&o.Force, "force", false, "Write into a non-empty directory that holds no pull of this page")
}

func (o *pullOpts) Validate() error {
	if strings.TrimSpace(o.ID) == "" {
		return errors.New("PAGE must be a page id; find one with gcx assistant pages list")
	}
	return o.IO.Validate()
}

func newPullCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pullOpts{}
	cmd := &cobra.Command{
		Use:   "pull PAGE",
		Short: "Write a published page's source project to a directory.",
		Long: "Export the page's current published version: its React/TypeScript project (package.json, src/, pages.bindings.json and the bundled SDK) and speaker notes. " +
			"The directory also gets " + clientpages.MetaFile + ", which records the page and version so push can update it. " +
			"Pulling again into the same directory replaces the files and removes ones the page no longer has.",
		Args: cobra.ExactArgs(1),
		Example: "  gcx assistant pages pull 3f2b8c1e-7a4d-4e9b-9c6a-5d1e2f3a4b5c --dir ./page\n" +
			"  gcx assistant pages pull https://mystack.grafana.net/a/grafana-assistant-app/pages/3f2b8c1e-7a4d-4e9b-9c6a-5d1e2f3a4b5c",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ID = pageID(args[0])
			if err := opts.Validate(); err != nil {
				return err
			}
			if opts.Dir == "" {
				opts.Dir = opts.ID
			}
			s, err := newSession(cmd, loader)
			if err != nil {
				return err
			}
			entry, err := s.client.Get(cmd.Context(), opts.ID)
			if err != nil {
				return err
			}
			export, err := s.client.Export(cmd.Context(), entry.ID, entry.RevisionID)
			if err != nil {
				return err
			}
			meta, err := clientpages.WriteProject(opts.Dir, entry, export, opts.Force)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), pullResult{ID: entry.ID, Title: entry.Title, Version: entry.Version, RevisionID: entry.RevisionID, Dir: opts.Dir, Files: len(meta.Files), URL: s.pageURL(entry.ID)})
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

type pullResult struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Version    int    `json:"version"`
	RevisionID string `json:"revisionId"`
	Dir        string `json:"dir"`
	Files      int    `json:"files"`
	URL        string `json:"url"`
}

// ---------------------------------------------------------------- push

type pushOpts struct {
	IO     cmdio.Options
	Dir    string
	DryRun bool
}

func (o *pushOpts) setup(flags *pflag.FlagSet) {
	bindIO(&o.IO, flags)
	flags.StringVarP(&o.Dir, "dir", "d", ".", "Directory a previous pull wrote")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Build the source on the stack without publishing it; a compile error fails the command")
}

func (o *pushOpts) Validate() error {
	if strings.TrimSpace(o.Dir) == "" {
		return errors.New("--dir must name the directory a pull wrote")
	}
	return o.IO.Validate()
}

func newPushCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pushOpts{}
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Build a pulled page's source on the stack and publish it as the page's next version.",
		Long: "Send the directory's whole project to the page it was pulled from. The stack builds it in a new Assistant conversation, as the Assistant builds pages; a compile error fails the command with the compiler output and publishes nothing. " +
			"On success the build becomes the page's next version, with its title, folder and sharing unchanged, and " + clientpages.MetaFile + " records it, so you can push again. " +
			"Push refuses when someone published a newer version after your pull: pull again and reapply your changes. Earlier versions stay in the page's history. " +
			"With --dry-run the build is kept in that conversation and nothing is published.",
		Args: cobra.NoArgs,
		Example: "  gcx assistant pages push --dir ./page --dry-run\n" +
			"  gcx assistant pages push --dir ./page -o json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			s, err := newSession(cmd, loader)
			if err != nil {
				return err
			}
			result, err := runPush(cmd.Context(), s, opts)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	markExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

type pushResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	ChatID    string `json:"chatId"`
	PageID    string `json:"pageId"`
	BuildID   string `json:"buildId"`
	Published bool   `json:"published"`
	Version   int    `json:"version"`
	URL       string `json:"url"`
}

// rebuildAttempts covers a sandbox's cold start, whose first build can fail
// with a server error.
const rebuildAttempts = 3

func runPush(ctx context.Context, s *session, opts *pushOpts) (*pushResult, error) {
	meta, err := clientpages.ReadMeta(opts.Dir)
	if err != nil {
		return nil, err
	}
	source, err := clientpages.ReadProject(opts.Dir)
	if err != nil {
		return nil, err
	}
	entry, err := s.client.Get(ctx, meta.ID)
	if err != nil {
		return nil, err
	}
	if entry.Version != meta.Version {
		return nil, fmt.Errorf("page %s is at version %d, but %s was pulled at version %d; pull it again into another directory, reapply your changes and push", entry.ID, entry.Version, opts.Dir, meta.Version)
	}
	chatID, err := s.client.CreateChat(ctx, "gcx: edit "+entry.Title)
	if err != nil {
		return nil, err
	}
	page, err := s.client.EditFromSource(ctx, entry.ID, chatID, clientpages.Export{Source: source, SpeakerNotes: meta.SpeakerNotes})
	if err != nil {
		return nil, err
	}
	page, err = rebuild(ctx, s.client, chatID, page.ID)
	if err != nil {
		return nil, err
	}
	if page.Error != "" {
		return nil, fmt.Errorf("the page did not build; fix the source and push again:\n%s", page.Error)
	}
	if page.CandidateID == "" {
		return nil, errors.New("the stack built the page but returned no build to publish; push again")
	}
	result := &pushResult{ID: entry.ID, Title: entry.Title, ChatID: chatID, PageID: page.ID, BuildID: page.CandidateID, Version: entry.Version, URL: s.pageURL(entry.ID)}
	if opts.DryRun {
		return result, nil
	}
	page, err = s.client.Accept(ctx, chatID, page.ID, page.CandidateID)
	if err != nil {
		return nil, err
	}
	published, err := s.client.Publish(ctx, entry, page.ID, page.ReleaseID)
	if err != nil {
		return nil, err
	}
	result.Published, result.Version, result.BuildID = true, published.Version, page.ReleaseID
	meta.Version, meta.RevisionID = published.Version, published.RevisionID
	if err := clientpages.UpdateMeta(opts.Dir, meta, source); err != nil {
		return nil, fmt.Errorf("published version %d, but could not record it in %s: %w", published.Version, opts.Dir, err)
	}
	return result, nil
}

func rebuild(ctx context.Context, client *clientpages.Client, chatID, pageID string) (*clientpages.Page, error) {
	var lastErr error
	for attempt := range rebuildAttempts {
		page, err := client.Rebuild(ctx, chatID, pageID)
		if err == nil {
			return page, nil
		}
		var status *gcxerrors.HTTPStatusError
		if !errors.As(err, &status) || status.Status < http.StatusInternalServerError {
			return nil, err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
	return nil, lastErr
}

// ---------------------------------------------------------------- text output

type textCodec struct{}

func (c *textCodec) Format() format.Format { return "text" }

func (c *textCodec) Decode(io.Reader, any) error {
	return errors.New("text format does not support decoding")
}

func (c *textCodec) Encode(dst io.Writer, value any) error {
	switch v := value.(type) {
	case []clientpages.Entry:
		table := style.NewTable("ID", "TITLE", "VERSION", "SHARING", "UPDATED")
		for _, e := range v {
			table.Row(e.ID, e.Title, strconv.Itoa(e.Version), sharing(e), assistanthttp.FormatTime(e.UpdatedAt))
		}
		return table.Render(dst)
	case getResult:
		table := style.NewTable("FIELD", "VALUE")
		table.Row("ID", v.ID)
		table.Row("TITLE", v.Title)
		table.Row("VERSION", strconv.Itoa(v.Version))
		table.Row("REVISION", v.RevisionID)
		table.Row("SHARING", sharing(v.Entry))
		table.Row("UPDATED", assistanthttp.FormatTime(v.UpdatedAt))
		table.Row("URL", v.URL)
		return table.Render(dst)
	case pullResult:
		_, err := fmt.Fprintf(dst, "Pulled %q version %d (%d files) into %s\n%s\n", v.Title, v.Version, v.Files, v.Dir, v.URL)
		return err
	case *pushResult:
		if !v.Published {
			_, err := fmt.Fprintf(dst, "Built %q in conversation %s; nothing published (--dry-run)\n", v.Title, v.ChatID)
			return err
		}
		_, err := fmt.Fprintf(dst, "Published %q version %d\n%s\n", v.Title, v.Version, v.URL)
		return err
	}
	return fmt.Errorf("text format cannot encode %T", value)
}

func sharing(e clientpages.Entry) string {
	var parts []string
	switch {
	case e.FolderUID != "":
		parts = append(parts, "folder "+e.FolderUID)
	case e.Unlisted:
		parts = append(parts, "unlisted")
	}
	if e.LinkSharing {
		parts = append(parts, "link")
	}
	if n := len(e.SharedWith); n > 0 {
		parts = append(parts, fmt.Sprintf("%d people", n))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}
