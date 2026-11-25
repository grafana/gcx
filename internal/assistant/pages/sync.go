package pages

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// errNoDraft reports a pulled directory that has not been pushed: it has no
// build of its own to capture or publish yet.
var errNoDraft = errors.New("this directory has no draft yet: push it first, which builds a draft from the pulled page")

// ownManifest makes page.json name the draft it is written to. A pulled page's
// manifest names the published page, and a draft whose page.json names another
// page loses its project.
func ownManifest(data []byte, pageID string) ([]byte, error) {
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("page.json is not valid JSON: %w", err)
	}
	metadata, _ := manifest["metadata"].(map[string]any)
	if metadata == nil {
		return nil, errors.New(`page.json has no metadata; keep {"apiVersion":"grafana.ai/v0alpha1","kind":"Page","metadata":{"name":"…","uid":"…"}}`)
	}
	if metadata["uid"] == pageID {
		return data, nil
	}
	metadata["uid"] = pageID
	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// LocalPage is a page project in a local directory: the draft it mirrors,
// once it has one, and the published page it updates, if any.
type LocalPage struct {
	ChatID    string `json:"chatId,omitempty"`
	PageID    string `json:"pageId,omitempty"`
	Title     string `json:"title"`
	ReleaseID string `json:"releaseId,omitempty"`
	// URL is where the draft opens, or the published page while there is no
	// draft yet.
	URL   string `json:"url"`
	Dir   string `json:"dir"`
	Files int    `json:"files"`
	// Publication is the published page the directory updates, and
	// PublishedURL where it opens.
	Publication  *Publication `json:"publication,omitempty"`
	PublishedURL string       `json:"publishedUrl,omitempty"`
	// Omitted are files the download left out: for a pulled page, the
	// binary or large files the catalog does not return as text; for a copy
	// of another person's page, their saved agent guidance.
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
	// DraftCreated reports that this push built the directory's draft from
	// its published page, the first push after a pull.
	DraftCreated bool `json:"draftCreated,omitempty"`
}

// Create starts a draft page from the Pages template in a new conversation,
// builds it, and mirrors its project into dir, which must be missing or
// empty.
func (c *Client) Create(ctx context.Context, dir, title, style string) (*LocalPage, error) {
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

// Pull writes a published page's source into dir, which must be missing or
// empty; an empty dir is a folder named after the page. It only reads: the
// catalog returns the page's text source, without the SDK under packages/
// and its stylesheets, which the stack keeps. The first push makes a draft
// from the page, and publishing it makes the page's next version.
func (c *Client) Pull(ctx context.Context, dir, ref string) (*LocalPage, error) {
	if dir != "" {
		if err := checkEmpty(dir); err != nil {
			return nil, err
		}
	}
	content, err := c.Content(ctx, ref, true)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		dir = Slug(content.Page.Title)
		if err := checkEmpty(dir); err != nil {
			return nil, err
		}
	}
	return c.writePublished(dir, content)
}

// writePublished writes a published page's text source into dir.
func (c *Client) writePublished(dir string, content *Content) (*LocalPage, error) {
	files := make(map[string][]byte, len(content.Files))
	for name, text := range content.Files {
		files[name] = []byte(text)
	}
	link, err := WriteProject(dir, Link{
		Grafana: c.host, Title: content.Page.Title,
		Publication: &Publication{ID: content.Page.ID, Version: content.Page.Version},
	}, &Source{Version: 1, Files: files})
	if err != nil {
		return nil, err
	}
	local := c.local(dir, link)
	local.Omitted = content.OmittedFiles
	return local, nil
}

// Refresh downloads the directory's page into it again, for changes made in
// Grafana: the draft's current version, or for a page pulled but not pushed
// yet, its published version. It refuses while dir has unpushed changes
// unless force is set.
func (c *Client) Refresh(ctx context.Context, dir string, link *Link, force bool) (*LocalPage, error) {
	if !force {
		diff, err := Changes(dir, link)
		if err != nil {
			return nil, err
		}
		if !diff.Empty() {
			return nil, fmt.Errorf("%s has %d unpushed change(s); push them first, or pass --force to discard them", dir, len(diff.Changed)+len(diff.Deleted))
		}
	}
	if !link.HasDraft() {
		content, err := c.Content(ctx, link.Publication.ID, true)
		if err != nil {
			return nil, err
		}
		return c.writePublished(dir, content)
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

func (c *Client) mirror(ctx context.Context, dir string, page Page, publication *Publication) (*LocalPage, error) {
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
	local := c.local(dir, link)
	local.Omitted = omitted
	return local, nil
}

func (c *Client) local(dir string, link *Link) *LocalPage {
	local := &LocalPage{
		ChatID: link.ChatID, PageID: link.PageID, Title: link.Title, ReleaseID: link.ReleaseID,
		Dir: dir, Files: len(link.Files), Publication: link.Publication,
	}
	if link.Publication != nil {
		local.PublishedURL = c.PageURL(link.Publication.ID)
		local.URL = local.PublishedURL
	}
	if link.HasDraft() {
		local.URL = c.DraftURL(link.ChatID, link.PageID)
	}
	return local
}

// startDraft makes the draft a pulled directory pushes into: a copy of its
// published page in a new conversation, as Edit does in Grafana. It refuses
// when the page was published again since the pull, so the push cannot
// overwrite that version with changes made to an older one.
func (c *Client) startDraft(ctx context.Context, link *Link) error {
	current, err := c.Get(ctx, link.Publication.ID)
	if err != nil {
		return err
	}
	if current.Version != link.Publication.Version {
		return fmt.Errorf("page %s is at version %d, and this directory was pulled at version %d; pull it into a new directory, reapply your changes and push", current.Title, current.Version, link.Publication.Version)
	}
	chatID, err := c.CreateChat(ctx, current.Title)
	if err != nil {
		return err
	}
	page, err := c.Edit(ctx, current.ID, chatID)
	if err != nil {
		return err
	}
	link.ChatID, link.PageID, link.Project, link.ReleaseID = chatID, page.ID, page.ProjectPath, page.ReleaseID
	return nil
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
	save := func() error { return WriteLink(dir, link) }
	if diff.Empty() {
		if link.PendingCandidate == "" {
			return out, nil
		}
		// An earlier push built a version but stopped before making it the
		// draft's version: finish that.
		if err := c.finishPending(ctx, link); err != nil {
			return nil, errors.Join(err, save())
		}
		out.Status, out.ReleaseID = StatusBuilt, link.ReleaseID
		return out, save()
	}
	if !link.HasDraft() {
		if err := c.startDraft(ctx, link); err != nil {
			return nil, err
		}
		if err := save(); err != nil {
			return nil, err
		}
		out.URL, out.DraftCreated = c.DraftURL(link.ChatID, link.PageID), true
	}
	var last *WorkspaceResult
	for _, change := range diff.Changed {
		content := change.Content
		if change.Path == "page.json" {
			if content, err = ownManifest(content, link.PageID); err != nil {
				return nil, errors.Join(err, save())
			}
		}
		result, err := c.WriteFile(ctx, link.ChatID, link.Project, change.Path, content)
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
		// Record the build first: if accepting it fails, the next push
		// finishes it, though no file has changed by then.
		link.PendingCandidate = page.CandidateID
		accepted, err := c.Accept(ctx, link.ChatID, link.PageID, page.CandidateID)
		if err != nil {
			return nil, errors.Join(err, save())
		}
		link.ReleaseID, link.PendingCandidate = accepted.ReleaseID, ""
		out.Status = StatusBuilt
	default:
		out.Status = StatusSaved
	}
	out.ReleaseID = link.ReleaseID
	return out, save()
}

// finishPending makes the build an earlier push recorded the draft's
// version. It reads the draft first: the build may have become its version
// meanwhile, or been replaced by a newer one.
func (c *Client) finishPending(ctx context.Context, link *Link) error {
	pages, err := c.Pages(ctx, link.ChatID)
	if err != nil {
		return err
	}
	pending := link.PendingCandidate
	for _, page := range pages {
		if page.ID != link.PageID {
			continue
		}
		switch pending {
		case page.ReleaseID:
			link.ReleaseID, link.PendingCandidate = pending, ""
			return nil
		case page.CandidateID:
			accepted, err := c.Accept(ctx, link.ChatID, link.PageID, pending)
			if err != nil {
				return err
			}
			link.ReleaseID, link.PendingCandidate = accepted.ReleaseID, ""
			return nil
		}
		link.PendingCandidate = ""
		return errors.New("the version an earlier push built was replaced by a newer build of the draft; pull the draft with gcx pages pull --dir, or change a file and push again")
	}
	return fmt.Errorf("the conversation no longer has page %s (%s)", link.PageID, link.Title)
}

// Snapshot captures the draft's latest build on the server and returns the
// image's bytes and file extension.
func (c *Client) Snapshot(ctx context.Context, link *Link, opts ScreenshotOptions) (*Screenshot, []byte, string, error) {
	if !link.HasDraft() {
		return nil, nil, "", errNoDraft
	}
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
	if !link.HasDraft() {
		return nil, errNoDraft
	}
	if link.PendingCandidate != "" {
		return nil, fmt.Errorf("the last push built a version that is not the draft's version yet; run gcx pages push --dir %s to finish it, then publish", dir)
	}
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
