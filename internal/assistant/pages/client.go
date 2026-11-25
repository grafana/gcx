// Package pages is a client for Grafana Pages, the live React pages Grafana
// Assistant hosts: published pages in the page catalog, and the draft pages
// conversations build in their sandboxes.
package pages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/gcxerrors"
)

const (
	catalogPath = "/api/v1/page-catalog"
	chatsPath   = "/api/v1/chats"

	// ClientHeader names gcx to the Pages API as a first-party client, with
	// the version of the API its commands are built against. The Pages API
	// refuses gcx requests without it, such as gcx api.
	ClientHeader = "X-Grafana-Pages-Client"
	// ClientVersion is the Pages API version these commands speak.
	ClientVersion = "gcx/1"

	// chatOrigin marks conversations gcx opens for a page.
	chatOrigin = "grafana-assistant-app/pages"

	// sandboxTimeout is the longest a sandbox operation may run, in seconds.
	sandboxTimeout = 120
	// notStarted is how a sandbox operation answers before the sandbox starts.
	notStarted = "Sandbox is not started"
	// recovered opens the answer to an operation that found its sandbox gone:
	// the Assistant restarts it from the saved sources, and says whether the
	// operation itself did not run.
	recovered = "The workspace was restarted from saved sources on a new VM"
	skipped   = "did not run"
	// maxAttempts bounds how often one sandbox operation is sent: once, again
	// after starting the sandbox, and again after a restart skipped it.
	maxAttempts = 3
)

var (
	// ErrRefused reports an Assistant that does not accept gcx's Pages commands.
	ErrRefused = errors.New("this Grafana Assistant does not accept gcx's Pages commands yet")
	// ErrUnavailable reports a stack without Pages.
	ErrUnavailable = errors.New("grafana Pages is not enabled on this stack")
)

// Entry is a published page as the catalog lists it.
type Entry struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Tags        []string  `json:"tags"`
	FolderUID   string    `json:"folderUid"`
	Unlisted    bool      `json:"unlisted,omitempty"`
	LinkSharing bool      `json:"linkSharing,omitempty"`
	SharedWith  []Share   `json:"sharedWith,omitempty"`
	RevisionID  string    `json:"revisionId"`
	Version     int       `json:"version"`
	Published   bool      `json:"published"`
	ChatID      string    `json:"chatId,omitempty"`
	Views       int       `json:"views"`
	UpdatedAt   time.Time `json:"updatedAt"`
	UpdatedBy   string    `json:"updatedBy,omitempty"`
	// URL is where the page opens in Grafana. gcx fills it in.
	URL string `json:"url,omitempty"`
}

// Share is a person an unlisted page is shared with.
type Share struct {
	UserID int64  `json:"userId"`
	Login  string `json:"login"`
	Role   string `json:"role,omitempty"`
}

// Query is one of a published page's named queries (bindings).
type Query struct {
	Name          string         `json:"name"`
	Title         string         `json:"title,omitempty"`
	DatasourceUID string         `json:"datasourceUid"`
	Query         map[string]any `json:"query"`
}

// Content is a published page with its named queries, and its source text
// when asked for.
type Content struct {
	Page         Entry             `json:"page"`
	Queries      []Query           `json:"queries"`
	Files        map[string]string `json:"files,omitempty"`
	OmittedFiles []string          `json:"omittedFiles,omitempty"`
}

// Page is a draft page in a conversation.
type Page struct {
	ID          string `json:"id"`
	ChatID      string `json:"chatId"`
	Title       string `json:"title"`
	Revision    int    `json:"revision"`
	ReleaseID   string `json:"releaseId,omitempty"`
	CandidateID string `json:"candidateId,omitempty"`
	Error       string `json:"error,omitempty"`
	ProjectPath string `json:"projectPath,omitempty"`
	EditOfID    string `json:"editOfId,omitempty"`
}

// Project is the sandbox's report on one page project after an operation.
type Project struct {
	UID       string `json:"uid,omitempty"`
	Directory string `json:"directory"`
	Title     string `json:"title,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

// WorkspaceResult is what a sandbox operation reports.
type WorkspaceResult struct {
	Output   string    `json:"output"`
	Error    string    `json:"error,omitempty"`
	ExitCode int       `json:"exitCode"`
	Pages    []Page    `json:"pages"`
	Projects []Project `json:"projects"`
}

// Source is a page's project: paths relative to the project root and their
// bytes, which travel base64-encoded.
type Source struct {
	Version int               `json:"version"`
	Files   map[string][]byte `json:"files"`
}

// Screenshot is a server capture of a page's latest build.
type Screenshot struct {
	ReleaseID string `json:"releaseId"`
	Candidate bool   `json:"candidate"`
	Failed    bool   `json:"failed"`
	Accepted  bool   `json:"accepted"`
	// Image is a PNG or JPEG data URL.
	Image string `json:"image"`
}

// ScreenshotOptions shape a capture.
type ScreenshotOptions struct {
	Width     int      `json:"width,omitempty"`
	ColorMode string   `json:"colorMode,omitempty"`
	Clicks    []string `json:"clicks,omitempty"`
	ScrollTo  string   `json:"scrollTo,omitempty"`
}

// Metadata describes a publication.
type Metadata struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	FolderUID   string   `json:"folderUid"`
	Unlisted    bool     `json:"unlisted,omitempty"`
}

// Sharing is who can open an unlisted page besides its author.
type Sharing struct {
	Users       []Share `json:"users"`
	LinkSharing bool    `json:"linkSharing"`
}

// PublishRequest publishes a draft page's release. ID and Version name the
// publication to update; leave them empty for a first publish.
type PublishRequest struct {
	PageID    string   `json:"pageId"`
	ReleaseID string   `json:"releaseId"`
	ID        string   `json:"id"`
	Version   int      `json:"version"`
	Metadata  Metadata `json:"metadata"`
	Sharing   *Sharing `json:"sharing,omitempty"`
}

// ListBatch is one batch of a catalog listing.
type ListBatch struct {
	Pages []Entry
	// NextCursor continues the listing; empty when it is complete.
	NextCursor string
	// Degraded reports that the Grafana Page API failed, so the batch leaves
	// out the pages shared in folders.
	Degraded bool
}

// ListOptions narrows a catalog listing.
type ListOptions struct {
	// Section is discover, mine, starred or unlisted.
	Section string
	Search  string
	Limit   int
	Cursor  string
}

// Categories are the publication categories the catalog accepts.
var Categories = []string{"overview", "investigation", "incident", "report", "story", "presentation", "architecture", "exploration", "other"} //nolint:gochecknoglobals // Closed vocabulary shared by flag validation and help.

// Client calls the Pages API.
type Client struct {
	base *assistanthttp.Client
	host string
}

// NewClient wraps an Assistant HTTP client. host is the Grafana URL, used to
// build links to pages.
func NewClient(base *assistanthttp.Client, host string) *Client {
	return &Client{base: base, host: strings.TrimSuffix(host, "/")}
}

// PageURL is where a published page opens in Grafana.
func (c *Client) PageURL(id string) string {
	return c.host + "/a/grafana-assistant-app/pages/" + url.PathEscape(id)
}

// DraftURL is where a conversation's draft page opens in Grafana.
func (c *Client) DraftURL(chatID, pageID string) string {
	return c.host + "/a/grafana-assistant-app/pages/new/" + url.PathEscape(chatID) + "?page=" + url.QueryEscape(pageID)
}

// List returns one batch of catalog pages.
func (c *Client) List(ctx context.Context, opts ListOptions) (*ListBatch, error) {
	q := url.Values{}
	if opts.Section != "" {
		q.Set("section", opts.Section)
	}
	if opts.Search != "" {
		q.Set("search", opts.Search)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	path := catalogPath
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out struct {
		Pages      []Entry `json:"pages"`
		NextCursor string  `json:"nextCursor"`
		Degraded   bool    `json:"degraded"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out, "list pages"); err != nil {
		return nil, err
	}
	if out.Pages == nil {
		out.Pages = []Entry{}
	}
	for i := range out.Pages {
		out.Pages[i].URL = c.PageURL(out.Pages[i].ID)
	}
	return &ListBatch{Pages: out.Pages, NextCursor: out.NextCursor, Degraded: out.Degraded}, nil
}

// Get returns one published page by ID or link.
func (c *Client) Get(ctx context.Context, ref string) (*Entry, error) {
	var out struct {
		Page Entry `json:"page"`
	}
	if err := c.do(ctx, http.MethodGet, catalogPath+"/"+url.PathEscape(ref), nil, &out, "get page"); err != nil {
		return nil, err
	}
	out.Page.URL = c.PageURL(out.Page.ID)
	return &out.Page, nil
}

// Content returns a published page with its named queries, and its source
// text without the SDK when source is set.
func (c *Client) Content(ctx context.Context, ref string, source bool) (*Content, error) {
	path := catalogPath + "/" + url.PathEscape(ref) + "/content"
	if source {
		path += "?source=true"
	}
	var out Content
	if err := c.do(ctx, http.MethodGet, path, nil, &out, "read page"); err != nil {
		return nil, err
	}
	if out.Queries == nil {
		out.Queries = []Query{}
	}
	out.Page.URL = c.PageURL(out.Page.ID)
	return &out, nil
}

// CreateChat opens a new conversation for a page and returns its ID.
func (c *Client) CreateChat(ctx context.Context, title string) (string, error) {
	req := map[string]any{"name": title, "nameAutogenerated": false, "origin": chatOrigin, "engine": "aisdk"}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, chatsPath, req, &out, "create conversation"); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("create conversation: the response has no conversation id")
	}
	return out.ID, nil
}

// Edit copies a published page into an empty conversation, as Edit does in
// Grafana. Publishing the copy updates the publication.
func (c *Client) Edit(ctx context.Context, ref, chatID string) (*Page, error) {
	return c.pageRequest(ctx, http.MethodPost, catalogPath+"/"+url.PathEscape(ref)+"/edit", map[string]any{"chatId": chatID}, "edit page")
}

// Pages lists a conversation's draft pages.
func (c *Client) Pages(ctx context.Context, chatID string) ([]Page, error) {
	var out struct {
		Pages []Page `json:"pages"`
	}
	if err := c.do(ctx, http.MethodGet, chatsPath+"/"+url.PathEscape(chatID)+"/pages", nil, &out, "list conversation pages"); err != nil {
		return nil, err
	}
	return out.Pages, nil
}

// StartSandbox prepares a conversation's sandbox, restoring its pages.
func (c *Client) StartSandbox(ctx context.Context, chatID string) (*WorkspaceResult, error) {
	return c.workspace(ctx, chatID, map[string]any{"start": true}, "start sandbox")
}

// CreatePage starts a page project from the Pages template in the
// conversation's sandbox and builds it.
func (c *Client) CreatePage(ctx context.Context, chatID, directory, title, style string) (*WorkspaceResult, error) {
	create := map[string]any{"directory": directory, "name": title}
	if style != "" {
		create["style"] = style
	}
	return c.workspaceStarted(ctx, chatID, map[string]any{"createPage": create}, "create page")
}

// WriteFile replaces a file of a page project in the sandbox, which builds
// the project. project is the page's projectPath; name is relative to it.
func (c *Client) WriteFile(ctx context.Context, chatID, project, name string, content []byte) (*WorkspaceResult, error) {
	file := map[string]any{"tool": "sandbox_write", "path": "/workspace/pages/" + project + "/" + name, "content": string(content)}
	return c.workspaceStarted(ctx, chatID, map[string]any{"file": file}, "write "+name)
}

// DeleteFiles removes files of a page project from the sandbox in one patch,
// which builds the project.
func (c *Client) DeleteFiles(ctx context.Context, chatID, project string, names []string) (*WorkspaceResult, error) {
	var patch strings.Builder
	patch.WriteString("*** Begin Patch\n")
	for _, name := range names {
		// Patch paths are relative to /workspace.
		patch.WriteString("*** Delete File: pages/" + project + "/" + name + "\n")
	}
	patch.WriteString("*** End Patch\n")
	file := map[string]any{"tool": "sandbox_patch", "patch": patch.String()}
	return c.workspaceStarted(ctx, chatID, map[string]any{"file": file}, "delete files")
}

// Accept makes a built candidate the page's release.
func (c *Client) Accept(ctx context.Context, chatID, pageID, candidate string) (*Page, error) {
	return c.pageRequest(ctx, http.MethodPost, c.draftPath(chatID, pageID)+"/accept", map[string]any{"candidate": candidate}, "accept build")
}

// Source returns the project a version of a draft page was built from, and
// the saved agent guidance the Assistant left out of it: a copy of another
// person's page comes without their AGENTS.md, skills and SDK Markdown.
func (c *Client) Source(ctx context.Context, chatID, pageID, releaseID string) (*Source, []string, error) {
	var out struct {
		Source       Source   `json:"source"`
		OmittedFiles []string `json:"omittedFiles"`
	}
	err := c.do(ctx, http.MethodGet, c.draftPath(chatID, pageID)+"/releases/"+url.PathEscape(releaseID)+"/source", nil, &out, "read page source")
	// An Assistant without the route answers with the router's plain 404,
	// not a Pages error document.
	var status *gcxerrors.HTTPStatusError
	if errors.As(err, &status) && status.Status == http.StatusNotFound && !strings.Contains(status.ServerMessage, `"name"`) {
		return nil, nil, fmt.Errorf("%w: it cannot download a page's project (%w)", ErrRefused, err)
	}
	if err != nil {
		return nil, nil, err
	}
	return &out.Source, out.OmittedFiles, nil
}

// Screenshot captures a draft page's latest build on the server.
func (c *Client) Screenshot(ctx context.Context, chatID, pageID string, opts ScreenshotOptions) (*Screenshot, error) {
	var out Screenshot
	if err := c.do(ctx, http.MethodPost, c.draftPath(chatID, pageID)+"/screenshot", opts, &out, "capture page"); err != nil {
		return nil, err
	}
	return &out, nil
}

// Publish publishes a draft page's release to the catalog.
func (c *Client) Publish(ctx context.Context, req PublishRequest) (*Entry, error) {
	if req.ID == "" {
		req.ID = "00000000-0000-0000-0000-000000000000"
	}
	if req.Metadata.Tags == nil {
		req.Metadata.Tags = []string{}
	}
	var out struct {
		Page Entry `json:"page"`
	}
	if err := c.do(ctx, http.MethodPost, catalogPath, req, &out, "publish page"); err != nil {
		return nil, err
	}
	out.Page.URL = c.PageURL(out.Page.ID)
	return &out.Page, nil
}

func (c *Client) draftPath(chatID, pageID string) string {
	return chatsPath + "/" + url.PathEscape(chatID) + "/pages/" + url.PathEscape(pageID)
}

// workspaceStarted runs a sandbox operation until it has run: it starts the
// sandbox when it has not started, and sends the operation again when the
// Assistant restarted a lost sandbox instead of running it. An operation that
// reports an error fails.
func (c *Client) workspaceStarted(ctx context.Context, chatID string, op map[string]any, name string) (*WorkspaceResult, error) {
	for attempt := 1; ; attempt++ {
		result, err := c.workspace(ctx, chatID, op, name)
		if err != nil {
			return nil, err
		}
		retry := false
		switch {
		case strings.HasPrefix(result.Output, notStarted):
			if _, err := c.StartSandbox(ctx, chatID); err != nil {
				return nil, err
			}
			retry = true
		case strings.HasPrefix(result.Output, recovered) && strings.Contains(firstParagraph(result.Output), skipped):
			retry = true
		case result.Error != "":
			return nil, fmt.Errorf("%s: %s", name, strings.TrimSpace(result.Error))
		}
		if !retry {
			return result, nil
		}
		if attempt == maxAttempts {
			return nil, fmt.Errorf("%s did not run after %d attempts; the sandbox kept restarting: %s", name, attempt, strings.TrimSpace(firstParagraph(result.Output)))
		}
	}
}

// firstParagraph is the text before the first blank line, where the
// Assistant puts its notice about the operation.
func firstParagraph(text string) string {
	first, _, _ := strings.Cut(text, "\n\n")
	return first
}

func (c *Client) workspace(ctx context.Context, chatID string, op map[string]any, name string) (*WorkspaceResult, error) {
	op["timeout"] = sandboxTimeout
	var out WorkspaceResult
	if err := c.do(ctx, http.MethodPost, chatsPath+"/"+url.PathEscape(chatID)+"/sandbox/commands", op, &out, name); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) pageRequest(ctx context.Context, method, path string, req any, op string) (*Page, error) {
	var out struct {
		Page Page `json:"page"`
	}
	if err := c.do(ctx, method, path, req, &out, op); err != nil {
		return nil, err
	}
	return &out.Page, nil
}

// do sends one request with the Pages client header and decodes the data of
// the {"data": …} envelope into out.
func (c *Client) do(ctx context.Context, method, path string, req, out any, op string) error {
	var body io.Reader
	if req != nil {
		data, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal %s request: %w", op, err)
		}
		body = bytes.NewReader(data)
	}
	resp, err := c.base.DoRequestWithHeaders(ctx, method, path, body, map[string]string{ClientHeader: ClientVersion})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return classify(fmt.Errorf("%s: %w", op, assistanthttp.HandleErrorResponse(resp)))
	}
	envelope := struct {
		Data any `json:"data"`
	}{Data: out}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode %s response: %w", op, err)
	}
	return nil
}

// classify adds recovery advice to the API errors a caller can act on.
func classify(err error) error {
	var status *gcxerrors.HTTPStatusError
	if !errors.As(err, &status) {
		return err
	}
	switch {
	case status.Status == http.StatusForbidden && strings.Contains(status.ServerMessage, "gcx"):
		return fmt.Errorf("%w: %w", ErrRefused, err)
	case status.Status == http.StatusNotFound && strings.Contains(status.ServerMessage, "disabled"):
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	case status.Status == http.StatusConflict && strings.Contains(status.ServerMessage, "page-changed"):
		return fmt.Errorf("%w (someone published a newer version; pull the page again, reapply your changes, push and publish)", err)
	case status.Status == http.StatusUnauthorized:
		return fmt.Errorf("%w (publishing and editing published pages need a user identity; sign in with gcx login rather than a service account token)", err)
	case status.Status == http.StatusNotFound:
		return fmt.Errorf("%w (check the page ID with gcx pages catalog list)", err)
	case status.Status == http.StatusForbidden:
		return fmt.Errorf("%w (your identity cannot read or change this page; ask its author, or check your Pages permissions)", err)
	}
	return err
}
