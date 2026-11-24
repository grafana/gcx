package pages

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Build statuses a push reports.
const (
	// StatusBuilt: files were sent and the page built a new version.
	StatusBuilt = "built"
	// StatusSaved: files were sent, but none changes the page's build, so
	// its version stays.
	StatusSaved = "saved"
	// StatusFailed: files were sent and the build failed.
	StatusFailed = "failed"
	// StatusUnchanged: nothing changed since the last push or pull.
	StatusUnchanged = "unchanged"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Draft is a draft page and where it opens.
type Draft struct {
	ChatID    string `json:"chatId"`
	PageID    string `json:"pageId"`
	Title     string `json:"title"`
	ReleaseID string `json:"releaseId"`
	URL       string `json:"url"`
	Dir       string `json:"dir"`
	Files     int    `json:"files"`
	// Publication is the published page publishing this draft updates, and
	// PublishedURL where it opens.
	Publication  *Publication `json:"publication,omitempty"`
	PublishedURL string       `json:"publishedUrl,omitempty"`
	// Omitted is the saved agent guidance left out of a copy of another
	// person's page, such as its AGENTS.md and the SDK's Markdown docs.
	Omitted []string `json:"omitted,omitempty"`
}

// PushResult reports a push.
type PushResult struct {
	Status    string   `json:"status"`
	URL       string   `json:"url"`
	ReleaseID string   `json:"releaseId,omitempty"`
	Written   []string `json:"written"`
	Deleted   []string `json:"deleted"`
	// Ignored are local files outside the paths a page saves.
	Ignored []string `json:"ignored,omitempty"`
	// Errors are the type, build or startup errors of a failed build.
	Errors string `json:"errors,omitempty"`
}

// Create starts a draft page from the Pages template in a new conversation,
// builds it, and mirrors its project into dir, which must be missing or
// empty.
func (c *Client) Create(ctx context.Context, dir, title, style string) (*Draft, error) {
	if err := checkEmpty(dir); err != nil {
		return nil, err
	}
	chatID, err := c.CreateChat(ctx, title)
	if err != nil {
		return nil, err
	}
	if _, err := c.StartSandbox(ctx, chatID); err != nil {
		return nil, err
	}
	result, err := c.CreatePage(ctx, chatID, Slug(title), title, style)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 || len(result.Pages) == 0 {
		return nil, fmt.Errorf("the sandbox could not create the page: %s", strings.TrimSpace(result.Output+"\n"+result.Error))
	}
	page := result.Pages[len(result.Pages)-1]
	page.ChatID = chatID
	if page.CandidateID != "" && page.CandidateID != page.ReleaseID {
		accepted, err := c.Accept(ctx, chatID, page.ID, page.CandidateID)
		if err != nil {
			return nil, err
		}
		page = *accepted
	}
	if page.ReleaseID == "" {
		return nil, fmt.Errorf("the starter page did not build: %s", strings.TrimSpace(page.Error+"\n"+result.Output))
	}
	return c.mirror(ctx, dir, page, nil)
}

// Pull copies a published page into a new conversation, as Edit does in
// Grafana, and mirrors its project into dir, which must be missing or empty;
// an empty dir is a folder named after the page. Publishing the copy makes it
// the page's next version.
func (c *Client) Pull(ctx context.Context, dir, ref string) (*Draft, error) {
	if dir != "" {
		if err := checkEmpty(dir); err != nil {
			return nil, err
		}
	}
	entry, err := c.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		dir = Slug(entry.Title)
		if err := checkEmpty(dir); err != nil {
			return nil, err
		}
	}
	chatID, err := c.CreateChat(ctx, entry.Title)
	if err != nil {
		return nil, err
	}
	page, err := c.Edit(ctx, entry.ID, chatID)
	if err != nil {
		return nil, err
	}
	page.ChatID = chatID
	return c.mirror(ctx, dir, *page, &Publication{ID: entry.ID, Version: entry.Version})
}

// Refresh mirrors the draft's current release into its directory again, for
// changes made in Grafana. It refuses while dir has unpushed changes unless
// force is set.
func (c *Client) Refresh(ctx context.Context, dir string, link *Link, force bool) (*Draft, error) {
	if !force {
		diff, err := Changes(dir, link)
		if err != nil {
			return nil, err
		}
		if !diff.Empty() {
			return nil, fmt.Errorf("%s has %d unpushed change(s); push them first, or pass --force to discard them", dir, len(diff.Changed)+len(diff.Deleted))
		}
	}
	pages, err := c.Pages(ctx, link.ChatID)
	if err != nil {
		return nil, err
	}
	for _, page := range pages {
		if page.ID == link.PageID {
			page.ChatID = link.ChatID
			return c.mirror(ctx, dir, page, link.Publication)
		}
	}
	return nil, fmt.Errorf("the conversation no longer has page %s (%s)", link.PageID, link.Title)
}

func (c *Client) mirror(ctx context.Context, dir string, page Page, publication *Publication) (*Draft, error) {
	if page.ReleaseID == "" {
		return nil, fmt.Errorf("page %s has no working version to download yet", page.Title)
	}
	source, omitted, err := c.Source(ctx, page.ChatID, page.ID, page.ReleaseID)
	if err != nil {
		return nil, err
	}
	link, err := WriteProject(dir, Link{
		Grafana: c.host, ChatID: page.ChatID, PageID: page.ID, Project: page.ProjectPath,
		Title: page.Title, ReleaseID: page.ReleaseID, Publication: publication,
	}, source)
	if err != nil {
		return nil, err
	}
	d := c.draft(dir, link)
	d.Omitted = omitted
	return d, nil
}

func (c *Client) draft(dir string, link *Link) *Draft {
	d := &Draft{
		ChatID: link.ChatID, PageID: link.PageID, Title: link.Title, ReleaseID: link.ReleaseID,
		URL: c.DraftURL(link.ChatID, link.PageID), Dir: dir, Files: len(link.Files), Publication: link.Publication,
	}
	if link.Publication != nil {
		d.PublishedURL = c.PageURL(link.Publication.ID)
	}
	return d
}

// Push writes dir's changes into the draft's sandbox, which builds the page,
// and makes a build that compiles the draft's version. A failed build keeps
// the previous version and reports its errors; the files stay written, so
// the next push sends only the fix.
func (c *Client) Push(ctx context.Context, dir string, link *Link) (*PushResult, error) {
	diff, err := Changes(dir, link)
	if err != nil {
		return nil, err
	}
	out := &PushResult{Status: StatusUnchanged, URL: c.DraftURL(link.ChatID, link.PageID), ReleaseID: link.ReleaseID, Written: []string{}, Deleted: []string{}, Ignored: diff.Ignored}
	if diff.Empty() {
		return out, nil
	}
	var last *WorkspaceResult
	save := func() error { return WriteLink(dir, link) }
	for _, change := range diff.Changed {
		result, err := c.WriteFile(ctx, link.ChatID, link.Project, change.Path, change.Content)
		if err != nil {
			return nil, errors.Join(err, save())
		}
		if result.ExitCode != 0 {
			return nil, errors.Join(fmt.Errorf("write %s: %s", change.Path, strings.TrimSpace(result.Output)), save())
		}
		link.Files[change.Path] = digest(change.Content)
		out.Written = append(out.Written, change.Path)
		last = result
	}
	if len(diff.Deleted) > 0 {
		result, err := c.DeleteFiles(ctx, link.ChatID, link.Project, diff.Deleted)
		if err != nil {
			return nil, errors.Join(err, save())
		}
		if result.ExitCode != 0 {
			return nil, errors.Join(fmt.Errorf("delete files: %s", strings.TrimSpace(result.Output)), save())
		}
		for _, name := range diff.Deleted {
			delete(link.Files, name)
		}
		out.Deleted = diff.Deleted
		last = result
	}
	page, project := findPage(last, link)
	switch {
	case project != nil && project.Status == StatusFailed:
		out.Status, out.Errors = StatusFailed, project.Error
	case page != nil && page.Error != "" && page.CandidateID == "":
		out.Status, out.Errors = StatusFailed, page.Error
	case page != nil && page.CandidateID != "" && page.CandidateID != page.ReleaseID:
		accepted, err := c.Accept(ctx, link.ChatID, link.PageID, page.CandidateID)
		if err != nil {
			return nil, errors.Join(err, save())
		}
		link.ReleaseID = accepted.ReleaseID
		out.Status = StatusBuilt
	default:
		out.Status = StatusSaved
	}
	out.ReleaseID = link.ReleaseID
	return out, save()
}

// Snapshot captures the draft's latest build on the server and returns the
// image's bytes and file extension.
func (c *Client) Snapshot(ctx context.Context, link *Link, opts ScreenshotOptions) (*Screenshot, []byte, string, error) {
	shot, err := c.Screenshot(ctx, link.ChatID, link.PageID, opts)
	if err != nil {
		return nil, nil, "", err
	}
	data, ext, err := decodeImage(shot.Image)
	if err != nil {
		return nil, nil, "", err
	}
	return shot, data, ext, nil
}

// PublishDraft publishes the draft's version: as the next version of its
// publication when it has one, with that publication's details unless meta
// overrides them, or else as a new page with meta.
func (c *Client) PublishDraft(ctx context.Context, dir string, link *Link, meta Metadata, sharing *Sharing) (*Entry, error) {
	req := PublishRequest{PageID: link.PageID, ReleaseID: link.ReleaseID, Metadata: meta, Sharing: sharing}
	if link.Publication != nil {
		current, err := c.Get(ctx, link.Publication.ID)
		if err != nil {
			return nil, err
		}
		if current.Version != link.Publication.Version {
			return nil, fmt.Errorf("page %s is at version %d, and this draft was pulled at version %d; pull it into a new directory, reapply your changes, push and publish", current.Title, current.Version, link.Publication.Version)
		}
		req.ID, req.Version = current.ID, current.Version
		req.Metadata = mergeMetadata(*current, meta)
	}
	entry, err := c.Publish(ctx, req)
	if err != nil {
		return nil, err
	}
	link.Publication = &Publication{ID: entry.ID, Version: entry.Version}
	return entry, WriteLink(dir, link)
}

// mergeMetadata keeps a publication's details where meta leaves them empty.
func mergeMetadata(current Entry, meta Metadata) Metadata {
	merged := Metadata{
		Title: current.Title, Description: current.Description, Category: current.Category,
		Tags: current.Tags, FolderUID: current.FolderUID, Unlisted: current.Unlisted,
	}
	if meta.Title != "" {
		merged.Title = meta.Title
	}
	if meta.Description != "" {
		merged.Description = meta.Description
	}
	if meta.Category != "" {
		merged.Category = meta.Category
	}
	if meta.Tags != nil {
		merged.Tags = meta.Tags
	}
	return merged
}

func findPage(result *WorkspaceResult, link *Link) (*Page, *Project) {
	if result == nil {
		return nil, nil
	}
	var page *Page
	for i := range result.Pages {
		if result.Pages[i].ID == link.PageID {
			page = &result.Pages[i]
		}
	}
	var project *Project
	for i := range result.Projects {
		p := &result.Projects[i]
		if p.UID == link.PageID || strings.TrimSuffix(strings.TrimPrefix(p.Directory, "/workspace/pages/"), "/") == link.Project {
			project = p
		}
	}
	return page, project
}

func decodeImage(dataURL string) ([]byte, string, error) {
	meta, payload, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(meta, "data:image/") || !strings.HasSuffix(meta, ";base64") {
		return nil, "", errors.New("the capture is not an image data URL")
	}
	ext := "png"
	if strings.HasPrefix(meta, "data:image/jpeg") {
		ext = "jpg"
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", fmt.Errorf("decode the capture: %w", err)
	}
	return data, ext, nil
}

// Slug is a project folder name for title.
func Slug(title string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 48 {
		s = strings.TrimRight(s[:48], "-")
	}
	if s == "" {
		return "page"
	}
	return s
}

// checkEmpty accepts a missing or empty directory.
func checkEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty; use a new or empty directory", dir)
	}
	return nil
}
