// Package pages provides gcx pages: find published Grafana Pages, and write
// a page as a local React project that the stack builds and hosts.
package pages

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

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

var sections = []string{"discover", "mine", "starred", "unlisted"} //nolint:gochecknoglobals // Closed vocabulary for --section.

// Commands returns the experimental gcx pages tree.
func Commands() *cobra.Command {
	loader := &providers.ConfigLoader{}
	cmd := &cobra.Command{
		Use:   "pages",
		Short: "Write, build and publish Grafana Pages.",
		Long: `A Grafana Page is a live web page on Grafana data, such as a report, a
dashboard-like view or a slideshow: a small React and TypeScript project
that the stack builds in a sandbox and serves in Grafana, where its queries
run as each viewer. Create or pull a page into a local directory, edit its
files, push to build it, snapshot to see it, and publish it to share it.

Requires Grafana Pages on the stack and a Grafana Assistant that accepts
gcx's Pages commands. These commands write files and build pages without
running Grafana Assistant.`,
		Example: `  gcx pages create --title "Checkout health" --dir ./checkout-health
  gcx pages push --dir ./checkout-health
  gcx pages snapshot --dir ./checkout-health
  gcx pages publish --dir ./checkout-health --folder ops`,
	}
	agent.MarkExperimental(cmd)
	loader.BindFlags(cmd.PersistentFlags())
	cmd.AddCommand(newListCommand(loader), newGetCommand(loader), newCreateCommand(loader), newPullCommand(loader), newPushCommand(loader), newSnapshotCommand(loader), newPublishCommand(loader))
	return cmd
}

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*clientpages.Client, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return clientpages.NewClient(base, cfg.GrafanaURL), nil
}

// textCodec renders a command's result for people; JSON and YAML encode the
// same value.
type textCodec struct {
	render func(io.Writer, any) error
}

func (c textCodec) Format() format.Format { return "text" }
func (c textCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}
func (c textCodec) Encode(w io.Writer, value any) error { return c.render(w, value) }

func bindIO(opts *cmdio.Options, flags *pflag.FlagSet, render func(io.Writer, any) error) {
	opts.RegisterCustomCodec("text", textCodec{render: render})
	opts.DefaultFormat("text")
	opts.BindFlags(flags)
}

type listOpts struct {
	IO      cmdio.Options
	Section string
	Search  string
	Limit   int
}

// maxListed bounds a list without --limit, so --limit 0 cannot page through
// an unbounded catalog.
const maxListed = 1000

func (o *listOpts) setup(flags *pflag.FlagSet) {
	bindIO(&o.IO, flags, renderList)
	flags.StringVar(&o.Section, "section", "", "Which pages: "+strings.Join(sections, ", ")+". Default lists every page you can find")
	flags.StringVar(&o.Search, "search", "", "Only pages whose title or description match")
	o.IO.BindListLimit(flags, &o.Limit, "pages", 30)
}

func (o *listOpts) Validate() error {
	if o.Section != "" && !slices.Contains(sections, o.Section) {
		return fmt.Errorf("--section must be one of %s", strings.Join(sections, ", "))
	}
	if o.Limit < 0 {
		return errors.New("--limit must not be negative")
	}
	return o.IO.Validate()
}

type listResult struct {
	Items    []clientpages.Entry `json:"items"`
	ListMeta *cmdio.ListMeta     `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

// ListItemsKey keeps list_meta when --json selects item fields.
func (listResult) ListItemsKey() string { return "items" }

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List published Grafana Pages.",
		Long:  "List the published pages in the page catalog that you can open, newest first. Use --section mine for your own pages. Draft pages that live only in a conversation are not listed.",
		Args:  cobra.NoArgs,
		Example: `  gcx pages list --section mine
  gcx pages list --search checkout -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			limit := opts.Limit
			if limit == 0 || limit > maxListed {
				limit = maxListed
			}
			result := listResult{Items: []clientpages.Entry{}}
			cursor := ""
			var more bool
			for {
				batch, next, err := client.List(cmd.Context(), clientpages.ListOptions{Section: opts.Section, Search: opts.Search, Limit: min(100, limit-len(result.Items)), Cursor: cursor})
				if err != nil {
					return err
				}
				result.Items = append(result.Items, batch...)
				more = next != ""
				if !more || len(result.Items) >= limit {
					break
				}
				cursor = next
			}
			if len(result.Items) > limit {
				result.Items, more = result.Items[:limit], true
			}
			result.ListMeta = cmdio.AttachListMeta(cmdio.PagedListMeta(len(result.Items), opts.Limit, more, maxListed), os.Args)
			if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), result.ListMeta)
			return nil
		},
	}
	agent.MarkExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func renderList(w io.Writer, value any) error {
	result, ok := value.(listResult)
	if !ok {
		return errors.New("invalid data type for pages list")
	}
	if len(result.Items) == 0 {
		_, err := fmt.Fprintln(w, "No pages found.")
		return err
	}
	tb := style.NewTable("ID", "TITLE", "CATEGORY", "VERSION", "UPDATED", "URL")
	for _, p := range result.Items {
		tb.Row(p.ID, p.Title, p.Category, strconv.Itoa(p.Version), assistanthttp.FormatTime(p.UpdatedAt), p.URL)
	}
	return tb.Render(w)
}

type getOpts struct {
	IO     cmdio.Options
	Source bool
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	bindIO(&o.IO, flags, renderContent)
	flags.BoolVar(&o.Source, "source", false, "Include the page's source text (bindings and src/, without the SDK)")
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get PAGE",
		Short: "Get a published Grafana Page and its queries.",
		Long:  "Read a published page by ID or link: its details, its named queries (the bindings it runs as each viewer) and, with --source, its source text. To change the page, pull it into a directory instead.",
		Args:  cobra.ExactArgs(1),
		Example: `  gcx pages get checkout-health-0a1b2c3d4e5f
  gcx pages get https://example.grafana.net/a/grafana-assistant-app/pages/checkout-health-0a1b2c3d4e5f --source -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			content, err := client.Content(cmd.Context(), args[0], opts.Source)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), content)
		},
	}
	agent.MarkExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func renderContent(w io.Writer, value any) error {
	c, ok := value.(*clientpages.Content)
	if !ok {
		return errors.New("invalid data type for page content")
	}
	audience := "folder " + c.Page.FolderUID
	if c.Page.Unlisted {
		audience = "unlisted"
	}
	fmt.Fprintf(w, "%s (version %d, %s, %s)\n%s\n", c.Page.Title, c.Page.Version, c.Page.Category, audience, c.Page.URL)
	if c.Page.Description != "" {
		fmt.Fprintf(w, "\n%s\n", c.Page.Description)
	}
	fmt.Fprintf(w, "\nQueries (%d):\n", len(c.Queries))
	for _, q := range c.Queries {
		fmt.Fprintf(w, "  %s: %s (datasource %s)\n", q.Name, q.Title, q.DatasourceUID)
	}
	if len(c.Files) > 0 {
		names := make([]string, 0, len(c.Files))
		for name := range c.Files {
			names = append(names, name)
		}
		slices.Sort(names)
		fmt.Fprintf(w, "\nSource files (%d): %s\n", len(names), strings.Join(names, ", "))
	}
	return nil
}

type createOpts struct {
	IO    cmdio.Options
	Title string
	Dir   string
	Style string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	bindIO(&o.IO, flags, renderDraft)
	flags.StringVar(&o.Title, "title", "", "The page's title (required)")
	flags.StringVar(&o.Dir, "dir", "", "Directory to write the page's project into; must be new or empty. Default: a folder named after the title")
	flags.StringVar(&o.Style, "style", "", "A style pack from the SDK, such as keynote, newsroom or grafana-native")
}

func (o *createOpts) Validate() error {
	o.Title = strings.TrimSpace(o.Title)
	if o.Title == "" || len([]rune(o.Title)) > 256 {
		return errors.New("--title is required and must be at most 256 characters; try --title \"Checkout health\"")
	}
	return o.IO.Validate()
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Start a Grafana Page as a local project.",
		Long: `Start a draft page from the Pages template in a new conversation on the
stack, build the starter, and write its whole project (page.json,
pages.bindings.json, src/, and the Pages SDK with its docs under
packages/pages-sdk/) into a local directory. Edit the files, then push.

The draft is private to you until you publish it. No Assistant turn runs.`,
		Args: cobra.NoArgs,
		Example: `  gcx pages create --title "Checkout health"
  gcx pages create --title "Q3 review" --dir ./q3-review --style keynote`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			dir := opts.Dir
			if dir == "" {
				dir = clientpages.Slug(opts.Title)
			}
			draft, err := client.Create(cmd.Context(), dir, opts.Title, opts.Style)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), draft)
		},
	}
	agent.MarkExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func renderDraft(w io.Writer, value any) error {
	d, ok := value.(*clientpages.Draft)
	if !ok {
		return errors.New("invalid data type for draft page")
	}
	fmt.Fprintf(w, "%s: %d files in %s\nDraft: %s\n", d.Title, d.Files, d.Dir, d.URL)
	if d.Publication != nil {
		fmt.Fprintf(w, "Publishing updates %s (pulled at version %d).\n", d.PublishedURL, d.Publication.Version)
	}
	if len(d.Omitted) > 0 {
		fmt.Fprintf(w, "Left out %d file(s) of its author's agent guidance and SDK docs, which another person may have written; the stack builds with the template's.\n", len(d.Omitted))
	}
	fmt.Fprintf(w, "\nRead %s, edit src/App.tsx and pages.bindings.json, then: gcx pages push --dir %s\n", filepath.Join(d.Dir, "packages/pages-sdk/README.md"), d.Dir)
	return nil
}

type pullOpts struct {
	IO    cmdio.Options
	Dir   string
	Force bool
}

func (o *pullOpts) setup(flags *pflag.FlagSet) {
	bindIO(&o.IO, flags, renderDraft)
	flags.StringVar(&o.Dir, "dir", "", "Directory to write the project into. With PAGE, it must be new or empty (default: a folder named after the page); without PAGE, the project to refresh (default: .)")
	flags.BoolVar(&o.Force, "force", false, "Without PAGE, discard unpushed local changes")
}

func newPullCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pullOpts{}
	cmd := &cobra.Command{
		Use:   "pull [PAGE]",
		Short: "Pull a Grafana Page's project into a local directory.",
		Long: `With PAGE (an ID or link), copy a published page into a new conversation,
as Edit does in Grafana, and write its project into a new directory.
Publishing from that directory makes your changes the page's next version.

Without PAGE, download the draft's current version into its directory
again, for changes made to it in Grafana, such as by Grafana Assistant.`,
		Args: cobra.MaximumNArgs(1),
		Example: `  gcx pages pull checkout-health-0a1b2c3d4e5f --dir ./checkout-health
  gcx pages pull --dir ./checkout-health`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if len(args) == 1 && opts.Force {
				return errors.New("--force applies only when refreshing a directory without PAGE")
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			var draft *clientpages.Draft
			if len(args) == 0 {
				draft, err = refresh(cmd, client, opts)
			} else {
				draft, err = client.Pull(cmd.Context(), opts.Dir, args[0])
			}
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), draft)
		},
	}
	agent.MarkExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func refresh(cmd *cobra.Command, client *clientpages.Client, opts *pullOpts) (*clientpages.Draft, error) {
	dir := opts.Dir
	if dir == "" {
		dir = "."
	}
	link, err := clientpages.ReadLink(dir)
	if err != nil {
		return nil, err
	}
	return client.Refresh(cmd.Context(), dir, link, opts.Force)
}

type dirOpts struct {
	IO  cmdio.Options
	Dir string
}

func (o *dirOpts) setup(flags *pflag.FlagSet, render func(io.Writer, any) error) {
	bindIO(&o.IO, flags, render)
	flags.StringVar(&o.Dir, "dir", ".", "The page's project directory")
}

func newPushCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &dirOpts{}
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Build a Grafana Page from its local project.",
		Long: `Write the files that changed in a page's project directory into its
sandbox on the stack, which type-checks and builds the page. A build that
compiles becomes the draft's version, which Grafana shows and snapshot
captures; a failed build returns the compiler's errors and keeps the
previous version.

Push changes only your draft. Publish shares it.`,
		Args: cobra.NoArgs,
		Example: `  gcx pages push --dir ./checkout-health
  gcx pages push -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			link, err := clientpages.ReadLink(opts.Dir)
			if err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			result, err := client.Push(cmd.Context(), opts.Dir, link)
			if err != nil {
				return err
			}
			if result.Status == clientpages.StatusFailed {
				return gcxerrors.DetailedError{
					Summary: "The page did not build",
					Details: result.Errors,
					Suggestions: []string{
						"Fix the files and lines the errors name, then run gcx pages push --dir " + opts.Dir + " again; only the changed files are sent",
						"The draft keeps showing its previous version until a build compiles",
					},
				}
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	agent.MarkExperimental(cmd)
	opts.setup(cmd.Flags(), renderPush)
	return cmd
}

func renderPush(w io.Writer, value any) error {
	r, ok := value.(*clientpages.PushResult)
	if !ok {
		return errors.New("invalid data type for push result")
	}
	switch r.Status {
	case clientpages.StatusUnchanged:
		fmt.Fprintf(w, "Nothing to build: no files changed since the last push.\n")
	case clientpages.StatusSaved:
		fmt.Fprintf(w, "Saved: %d file(s) written, %d deleted. None of them changes the page's build, so its version stays.\n", len(r.Written), len(r.Deleted))
	default:
		fmt.Fprintf(w, "Built: %d file(s) written, %d deleted.\n", len(r.Written), len(r.Deleted))
	}
	if len(r.Ignored) > 0 {
		fmt.Fprintf(w, "Not saved with the page (outside its saved paths): %s\n", strings.Join(r.Ignored, ", "))
	}
	fmt.Fprintf(w, "Draft: %s\n", r.URL)
	return nil
}

type snapshotOpts struct {
	dirOpts

	OutputDir string
	Width     int
	Theme     string
	Clicks    []string
	ScrollTo  string
}

type snapshotReceipt struct {
	File string `json:"file"`
	// URL is the draft the image shows.
	URL       string `json:"url"`
	ReleaseID string `json:"releaseId"`
	Candidate bool   `json:"candidate"`
	Failed    bool   `json:"failed"`
}

func (o *snapshotOpts) setup(flags *pflag.FlagSet) {
	o.dirOpts.setup(flags, renderSnapshot)
	flags.StringVar(&o.OutputDir, "output-dir", ".", "Directory to write the image into")
	flags.IntVar(&o.Width, "width", 1200, "Page width in CSS pixels, 320 to 2400")
	flags.StringVar(&o.Theme, "theme", "", "light or dark. Default: the renderer's default (dark)")
	flags.StringArrayVar(&o.Clicks, "click", nil, "CSS selector to click before the capture, such as a tab; repeat for several, in order")
	flags.StringVar(&o.ScrollTo, "scroll-to", "", "CSS selector to scroll to before the capture")
}

func (o *snapshotOpts) Validate() error {
	if o.Width < 320 || o.Width > 2400 {
		return errors.New("--width must be between 320 and 2400")
	}
	if o.Theme != "" && o.Theme != "light" && o.Theme != "dark" {
		return errors.New("--theme must be light or dark")
	}
	if len(o.Clicks) > 16 {
		return errors.New("--click can be repeated at most 16 times")
	}
	return o.IO.Validate()
}

func newSnapshotCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &snapshotOpts{}
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Render a Grafana Page's latest build as an image.",
		Long: `Render the draft's latest build on the stack's server, with live data, and
write the image (PNG or JPEG, at most 1568 pixels on its longer side) into
--output-dir. Look at the image after each meaningful push: a build that
compiles can still render wrongly. A page that failed to render shows why
in the image and fails the command.`,
		Args: cobra.NoArgs,
		Example: `  gcx pages snapshot --dir ./checkout-health
  gcx pages snapshot --dir ./checkout-health --theme light --width 800
  gcx pages snapshot --dir ./q3-review --click '[aria-label="Next slide"]'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			link, err := clientpages.ReadLink(opts.Dir)
			if err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			shot, data, ext, err := client.Snapshot(cmd.Context(), link, clientpages.ScreenshotOptions{Width: opts.Width, ColorMode: opts.Theme, Clicks: opts.Clicks, ScrollTo: opts.ScrollTo})
			if err != nil {
				return err
			}
			if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
				return fmt.Errorf("create output directory: %w", err)
			}
			file := filepath.Join(opts.OutputDir, clientpages.Slug(link.Project)+"."+ext)
			if err := os.WriteFile(file, data, 0o600); err != nil {
				return err
			}
			receipt := snapshotReceipt{File: file, URL: client.DraftURL(link.ChatID, link.PageID), ReleaseID: shot.ReleaseID, Candidate: shot.Candidate, Failed: shot.Failed}
			if shot.Failed {
				return gcxerrors.DetailedError{
					Summary:     "The page failed to render",
					Details:     "The image shows the reason: " + file,
					Suggestions: []string{"Read the image, fix the cause, push and snapshot again"},
				}
			}
			return opts.IO.Encode(cmd.OutOrStdout(), receipt)
		},
	}
	agent.MarkExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func renderSnapshot(w io.Writer, value any) error {
	r, ok := value.(snapshotReceipt)
	if !ok {
		return errors.New("invalid data type for snapshot receipt")
	}
	_, err := fmt.Fprintf(w, "Wrote %s\nDraft: %s\n", r.File, r.URL)
	return err
}

type publishOpts struct {
	dirOpts

	Title       string
	Description string
	Category    string
	Tags        []string
	Folder      string
	Unlisted    bool
	LinkSharing bool
}

func (o *publishOpts) setup(flags *pflag.FlagSet) {
	o.dirOpts.setup(flags, renderPublished)
	flags.StringVar(&o.Title, "title", "", "Title in the catalog. Default: the page's title, or the publication's")
	flags.StringVar(&o.Description, "description", "", "What the page shows, at most 2000 characters")
	flags.StringVar(&o.Category, "category", "", "One of "+strings.Join(clientpages.Categories, ", ")+". Default for a new page: other")
	flags.StringSliceVar(&o.Tags, "tag", nil, "Tag the page; repeat or separate with commas, at most 12")
	flags.StringVar(&o.Folder, "folder", "", "First publish only: the folder UID that shares the page with everyone who can view the folder")
	flags.BoolVar(&o.Unlisted, "unlisted", false, "First publish only: share with no folder; only you, and with --link-sharing anyone with the link, can open it")
	flags.BoolVar(&o.LinkSharing, "link-sharing", false, "With --unlisted: anyone in the organization who has the link can open the page")
}

func (o *publishOpts) Validate() error {
	if o.Category != "" && !slices.Contains(clientpages.Categories, o.Category) {
		return fmt.Errorf("--category must be one of %s", strings.Join(clientpages.Categories, ", "))
	}
	if len([]rune(o.Description)) > 2000 {
		return errors.New("--description must be at most 2000 characters")
	}
	if len(o.Tags) > 12 {
		return errors.New("--tag can name at most 12 tags")
	}
	if o.Folder != "" && o.Unlisted {
		return errors.New("--folder and --unlisted are mutually exclusive")
	}
	if o.LinkSharing && !o.Unlisted {
		return errors.New("--link-sharing requires --unlisted")
	}
	return o.IO.Validate()
}

// publication is what to publish: for a new page, its details and audience
// from the flags; for a published page, only the details the flags change.
func (o *publishOpts) publication(link *clientpages.Link) (clientpages.Metadata, *clientpages.Sharing, error) {
	meta := clientpages.Metadata{Title: o.Title, Description: o.Description, Category: o.Category, Tags: o.Tags}
	if link.Publication != nil {
		if o.Folder != "" || o.Unlisted || o.LinkSharing {
			return meta, nil, errors.New("this page is already published; change its folder or sharing in Grafana, then publish without --folder, --unlisted or --link-sharing")
		}
		return meta, nil, nil
	}
	if o.Folder == "" && !o.Unlisted {
		return meta, nil, errors.New("a new page needs --folder FOLDER_UID, or --unlisted (with --link-sharing to share its link)")
	}
	if meta.Title == "" {
		meta.Title = link.Title
	}
	if meta.Category == "" {
		meta.Category = "other"
	}
	meta.FolderUID, meta.Unlisted = o.Folder, o.Unlisted
	if !o.Unlisted {
		return meta, nil, nil
	}
	return meta, &clientpages.Sharing{Users: []clientpages.Share{}, LinkSharing: o.LinkSharing}, nil
}

func newPublishCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &publishOpts{}
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a Grafana Page's draft to the page catalog.",
		Long: `Publish the draft's current version, the last push that built. A page
pulled from the catalog, or published before, becomes that publication's
next version, keeping its folder and sharing; earlier versions stay in its
history. A new page needs --folder, or --unlisted.

Publishing shares the page with other people. Publish only when the user
asked for that. It needs a user identity (gcx login), not a service account.`,
		Args: cobra.NoArgs,
		Example: `  gcx pages publish --dir ./checkout-health --folder ops-reports --category report
  gcx pages publish --dir ./q3-review --unlisted --link-sharing
  gcx pages publish --dir ./checkout-health`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			link, err := clientpages.ReadLink(opts.Dir)
			if err != nil {
				return err
			}
			diff, err := clientpages.Changes(opts.Dir, link)
			if err != nil {
				return err
			}
			if !diff.Empty() {
				cmdio.Warning(cmd.ErrOrStderr(), "%d local change(s) are not pushed; publishing the last version that built. Run gcx pages push first to include them.", len(diff.Changed)+len(diff.Deleted))
			}
			meta, sharing, err := opts.publication(link)
			if err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			entry, err := client.PublishDraft(cmd.Context(), opts.Dir, link, meta, sharing)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), entry)
		},
	}
	agent.MarkExperimental(cmd)
	opts.setup(cmd.Flags())
	return cmd
}

func renderPublished(w io.Writer, value any) error {
	e, ok := value.(*clientpages.Entry)
	if !ok {
		return errors.New("invalid data type for published page")
	}
	_, err := fmt.Fprintf(w, "Published %s as version %d.\n%s\n", e.Title, e.Version, e.URL)
	return err
}
