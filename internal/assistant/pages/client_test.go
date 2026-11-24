package pages_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/pages"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const base = "/api/plugins/grafana-assistant-app/resources/api/v1"

// fakeAssistant is enough of the Pages API for gcx's commands: one
// conversation whose sandbox holds one page project and builds it on every
// write. A file containing BROKEN fails to compile.
type fakeAssistant struct {
	t        *testing.T
	mu       sync.Mutex
	started  bool
	files    map[string][]byte
	release  string
	builds   int
	requests []string
	writes   []string
	patches  []string
	accepted []string
	publish  map[string]any
	catalog  pages.Entry
	refuse   string
}

const (
	chatID  = "11111111-1111-1111-1111-111111111111"
	pageID  = "22222222-2222-2222-2222-222222222222"
	project = "checkout-health"
)

func newFake(t *testing.T) (*fakeAssistant, *pages.Client) {
	t.Helper()
	f := &fakeAssistant{t: t, catalog: pages.Entry{ID: "pub-1", Title: "Checkout health", Category: "report", FolderUID: "ops", Version: 3, Tags: []string{"checkout"}}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	baseClient, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "stacks-1"})
	require.NoError(t, err)
	return f, pages.NewClient(baseClient, "https://example.grafana.net")
}

func (f *fakeAssistant) respond(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	assert.NoError(f.t, json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data}))
}

func (f *fakeAssistant) page(candidate string, buildError string) map[string]any {
	return map[string]any{"id": pageID, "chatId": chatID, "title": "Checkout health", "releaseId": f.release, "candidateId": candidate, "error": buildError, "projectPath": project}
}

// build is what the sandbox reports after a change to the project.
func (f *fakeAssistant) build() map[string]any {
	for name, data := range f.files {
		if strings.Contains(string(data), "BROKEN") {
			errs := "src/" + filepath.Base(name) + "(1,1): error TS2304: Cannot find name 'BROKEN'."
			return map[string]any{"output": "", "exitCode": 0, "pages": []any{f.page("", errs)}, "projects": []any{map[string]any{"uid": pageID, "directory": project, "status": "failed", "error": errs}}}
		}
	}
	f.builds++
	candidate := fmt.Sprintf("33333333-3333-3333-3333-%012d", f.builds)
	return map[string]any{"output": "", "exitCode": 0, "pages": []any{f.page(candidate, "")}, "projects": []any{map[string]any{"uid": pageID, "directory": project, "status": "compiled"}}}
}

func (f *fakeAssistant) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(f.t, pages.ClientVersion, r.Header.Get(pages.ClientHeader), "every Pages request names gcx's Pages client")
	path := strings.TrimPrefix(r.URL.Path, base)
	f.requests = append(f.requests, r.Method+" "+path)
	if f.refuse != "" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(f.refuse))
		return
	}
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	draft := "/chats/" + chatID + "/pages/" + pageID
	switch {
	case r.Method == http.MethodPost && path == "/chats":
		assert.Equal(f.t, "grafana-assistant-app/pages", body["origin"])
		f.respond(w, map[string]any{"id": chatID})
	case r.Method == http.MethodPost && path == "/chats/"+chatID+"/sandbox/commands":
		f.workspace(w, body)
	case r.Method == http.MethodPost && path == draft+"/accept":
		f.release = str(body["candidate"])
		f.accepted = append(f.accepted, f.release)
		f.respond(w, map[string]any{"page": f.page("", "")})
	case r.Method == http.MethodGet && path == draft+"/releases/"+f.release+"/source":
		files := map[string][]byte{}
		for name, data := range f.files {
			files[strings.TrimPrefix(name, "/workspace/pages/"+project+"/")] = data
		}
		f.respond(w, map[string]any{"source": map[string]any{"version": 1, "files": files}})
	case r.Method == http.MethodGet && path == "/chats/"+chatID+"/pages":
		f.respond(w, map[string]any{"pages": []any{f.page("", "")}})
	case r.Method == http.MethodPost && path == draft+"/screenshot":
		f.respond(w, map[string]any{"releaseId": f.release, "image": "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG"))})
	case r.Method == http.MethodGet && path == "/page-catalog/pub-1":
		f.respond(w, map[string]any{"page": f.catalog})
	case r.Method == http.MethodPost && path == "/page-catalog/pub-1/edit":
		f.files = map[string][]byte{"/workspace/pages/" + project + "/src/App.tsx": []byte("export const App = () => null;\n")}
		f.release = "44444444-4444-4444-4444-444444444444"
		f.respond(w, map[string]any{"page": f.page("", "")})
	case r.Method == http.MethodPost && path == "/page-catalog":
		f.publish = body
		published := f.catalog
		if body["id"] != "pub-1" {
			published = pages.Entry{ID: pageID, Title: str(object(body["metadata"])["title"])}
		}
		published.Version++
		f.respond(w, map[string]any{"page": published})
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeAssistant) workspace(w http.ResponseWriter, body map[string]any) {
	assert.EqualValues(f.t, 120, body["timeout"])
	switch {
	case body["start"] == true:
		f.started = true
		f.respond(w, map[string]any{"output": "started", "pages": []any{}, "projects": []any{}})
	case !f.started:
		f.respond(w, map[string]any{"output": "Sandbox is not started. Call start_sandbox first, then retry this operation.", "pages": []any{}, "projects": []any{}})
	case body["createPage"] != nil:
		create := object(body["createPage"])
		assert.Equal(f.t, project, create["directory"])
		assert.Equal(f.t, "Checkout health", create["name"])
		root := "/workspace/pages/" + project + "/"
		f.files = map[string][]byte{
			root + "page.json":                    []byte(`{"apiVersion":"grafana.ai/v0alpha1","kind":"Page","metadata":{"name":"Checkout health","uid":"` + pageID + `"}}`),
			root + "pages.bindings.json":          []byte("{}\n"),
			root + "src/main.tsx":                 []byte("import { App } from './App';\n"),
			root + "src/App.tsx":                  []byte("export const App = () => null;\n"),
			root + "packages/pages-sdk/README.md": []byte("# Pages SDK\n"),
		}
		f.respond(w, f.build())
	case body["file"] != nil:
		file := object(body["file"])
		switch file["tool"] {
		case "sandbox_write":
			name := str(file["path"])
			f.writes = append(f.writes, strings.TrimPrefix(name, "/workspace/pages/"+project+"/"))
			f.files[name] = []byte(str(file["content"]))
		case "sandbox_patch":
			patch := str(file["patch"])
			f.patches = append(f.patches, patch)
			for line := range strings.SplitSeq(patch, "\n") {
				if name, ok := strings.CutPrefix(line, "*** Delete File: "); ok {
					delete(f.files, "/workspace/"+name)
				}
			}
		}
		f.respond(w, f.build())
	default:
		f.t.Errorf("unexpected sandbox operation %v", body)
	}
}

func TestCreateMirrorsTheStarterAndLinksTheDraft(t *testing.T) {
	f, client := newFake(t)
	dir := filepath.Join(t.TempDir(), "checkout-health")

	draft, err := client.Create(context.Background(), dir, "Checkout health", "")
	require.NoError(t, err)

	assert.Equal(t, chatID, draft.ChatID)
	assert.Equal(t, pageID, draft.PageID)
	assert.Equal(t, "https://example.grafana.net/a/grafana-assistant-app/pages/new/"+chatID+"?page="+pageID, draft.URL)
	assert.Equal(t, 5, draft.Files)
	assert.Len(t, f.accepted, 1, "the starter's build is accepted, so the draft has a version to download")
	sdk, err := os.ReadFile(filepath.Join(dir, "packages/pages-sdk/README.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Pages SDK\n", string(sdk))
	link, err := pages.ReadLink(dir)
	require.NoError(t, err)
	assert.Equal(t, project, link.Project)
	assert.Equal(t, f.release, link.ReleaseID)
	assert.Nil(t, link.Publication)
	assert.Empty(t, draft.PublishedURL, "a new draft has no published page yet")
}

func TestCreateRefusesANonEmptyDirectoryBeforeCallingTheStack(t *testing.T) {
	f, client := newFake(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("mine"), 0o600))

	_, err := client.Create(context.Background(), dir, "Checkout health", "")
	require.ErrorContains(t, err, "is not empty")
	assert.Empty(t, f.requests)
}

func TestPushWritesChangesInBuildOrderAndAcceptsTheBuild(t *testing.T) {
	f, client := newFake(t)
	dir := filepath.Join(t.TempDir(), "page")
	_, err := client.Create(context.Background(), dir, "Checkout health", "")
	require.NoError(t, err)
	write(t, dir, "src/App.tsx", "import { Chart } from './Chart';\nexport const App = () => <Chart />;\n")
	write(t, dir, "src/Chart.tsx", "export const Chart = () => null;\n")
	write(t, dir, "pages.bindings.json", `{"requests":{"title":"Requests","datasourceUid":"prom","query":{"expr":"up"}}}`)
	write(t, dir, "notes.txt", "not part of the page")
	link, err := pages.ReadLink(dir)
	require.NoError(t, err)

	result, err := client.Push(context.Background(), dir, link)
	require.NoError(t, err)

	assert.Equal(t, pages.StatusBuilt, result.Status)
	assert.Equal(t, []string{"pages.bindings.json", "src/Chart.tsx", "src/App.tsx"}, f.writes, "data, then modules, then the entry point")
	assert.Equal(t, []string{"notes.txt"}, result.Ignored)
	assert.Equal(t, f.release, result.ReleaseID)
	assert.Len(t, f.accepted, 2)

	again, err := client.Push(context.Background(), dir, mustLink(t, dir))
	require.NoError(t, err)
	assert.Equal(t, pages.StatusUnchanged, again.Status)
	assert.Len(t, f.writes, 3, "an unchanged project sends nothing")
}

func TestPushReportsAFailedBuildAndSendsOnlyTheFixNext(t *testing.T) {
	f, client := newFake(t)
	dir := filepath.Join(t.TempDir(), "page")
	_, err := client.Create(context.Background(), dir, "Checkout health", "")
	require.NoError(t, err)
	release := f.release
	write(t, dir, "src/App.tsx", "export const App = () => BROKEN;\n")
	write(t, dir, "src/Chart.tsx", "export const Chart = () => null;\n")

	result, err := client.Push(context.Background(), dir, mustLink(t, dir))
	require.NoError(t, err)
	assert.Equal(t, pages.StatusFailed, result.Status)
	assert.Contains(t, result.Errors, "error TS2304")
	assert.Equal(t, release, mustLink(t, dir).ReleaseID, "a failed build keeps the previous version")

	write(t, dir, "src/App.tsx", "export const App = () => null;\n")
	fixed, err := client.Push(context.Background(), dir, mustLink(t, dir))
	require.NoError(t, err)
	assert.Equal(t, pages.StatusBuilt, fixed.Status)
	assert.Equal(t, []string{"src/App.tsx"}, fixed.Written)
}

func TestPushDeletesRemovedFilesInOnePatch(t *testing.T) {
	f, client := newFake(t)
	dir := filepath.Join(t.TempDir(), "page")
	_, err := client.Create(context.Background(), dir, "Checkout health", "")
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "packages/pages-sdk/README.md")))

	result, err := client.Push(context.Background(), dir, mustLink(t, dir))
	require.NoError(t, err)
	assert.Equal(t, []string{"packages/pages-sdk/README.md"}, result.Deleted)
	require.Len(t, f.patches, 1)
	assert.Equal(t, "*** Begin Patch\n*** Delete File: pages/"+project+"/packages/pages-sdk/README.md\n*** End Patch\n", f.patches[0])
	assert.NotContains(t, mustLink(t, dir).Files, "packages/pages-sdk/README.md")
}

func TestPushStartsAStoppedSandbox(t *testing.T) {
	f, client := newFake(t)
	dir := filepath.Join(t.TempDir(), "page")
	_, err := client.Create(context.Background(), dir, "Checkout health", "")
	require.NoError(t, err)
	f.started = false
	write(t, dir, "src/App.tsx", "export const App = () => 'hi';\n")

	result, err := client.Push(context.Background(), dir, mustLink(t, dir))
	require.NoError(t, err)
	assert.Equal(t, pages.StatusBuilt, result.Status)
	assert.True(t, f.started)
}

func TestPullCopiesThePublishedPageAndPublishUpdatesIt(t *testing.T) {
	f, client := newFake(t)
	dir := filepath.Join(t.TempDir(), "page")

	draft, err := client.Pull(context.Background(), dir, "pub-1")
	require.NoError(t, err)
	assert.Equal(t, &pages.Publication{ID: "pub-1", Version: 3}, draft.Publication)
	assert.Equal(t, "https://example.grafana.net/a/grafana-assistant-app/pages/new/"+chatID+"?page="+pageID, draft.URL)
	assert.Equal(t, "https://example.grafana.net/a/grafana-assistant-app/pages/pub-1", draft.PublishedURL)

	entry, err := client.PublishDraft(context.Background(), dir, mustLink(t, dir), pages.Metadata{Description: "Errors and latency"}, nil)
	require.NoError(t, err)
	assert.Equal(t, 4, entry.Version)
	assert.Equal(t, "https://example.grafana.net/a/grafana-assistant-app/pages/pub-1", entry.URL)
	assert.Equal(t, "pub-1", f.publish["id"])
	assert.EqualValues(t, 3, f.publish["version"])
	meta := object(f.publish["metadata"])
	assert.Equal(t, "Checkout health", meta["title"], "details the flags leave out are kept")
	assert.Equal(t, "Errors and latency", meta["description"])
	assert.Equal(t, "ops", meta["folderUid"])
	assert.Equal(t, &pages.Publication{ID: "pub-1", Version: 4}, mustLink(t, dir).Publication)
}

func TestPublishRefusesAStaleDraft(t *testing.T) {
	f, client := newFake(t)
	dir := filepath.Join(t.TempDir(), "page")
	_, err := client.Pull(context.Background(), dir, "pub-1")
	require.NoError(t, err)
	f.catalog.Version = 5

	_, err = client.PublishDraft(context.Background(), dir, mustLink(t, dir), pages.Metadata{}, nil)
	require.ErrorContains(t, err, "is at version 5, and this draft was pulled at version 3")
	assert.Nil(t, f.publish)
}

func TestFirstPublishCreatesAPublication(t *testing.T) {
	f, client := newFake(t)
	dir := filepath.Join(t.TempDir(), "page")
	_, err := client.Create(context.Background(), dir, "Checkout health", "")
	require.NoError(t, err)

	_, err = client.PublishDraft(context.Background(), dir, mustLink(t, dir), pages.Metadata{Title: "Checkout health", Category: "report", Unlisted: true}, &pages.Sharing{Users: []pages.Share{}, LinkSharing: true})
	require.NoError(t, err)
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", f.publish["id"])
	assert.Equal(t, f.release, f.publish["releaseId"])
	assert.Equal(t, map[string]any{"users": []any{}, "linkSharing": true}, f.publish["sharing"])
}

func TestSnapshotDecodesTheImage(t *testing.T) {
	_, client := newFake(t)
	dir := filepath.Join(t.TempDir(), "page")
	_, err := client.Create(context.Background(), dir, "Checkout health", "")
	require.NoError(t, err)

	shot, data, ext, err := client.Snapshot(context.Background(), mustLink(t, dir), pages.ScreenshotOptions{Width: 800, ColorMode: "light"})
	require.NoError(t, err)
	assert.Equal(t, "png", ext)
	assert.Equal(t, []byte("\x89PNG"), data)
	assert.False(t, shot.Failed)
}

func TestErrorsNameWhatToDo(t *testing.T) {
	for name, tt := range map[string]struct {
		body string
		want error
	}{
		"assistant refuses gcx": {`{"name":"FORBIDDEN","message":"Pages can't be used with gcx. Open Pages in Grafana instead."}`, pages.ErrRefused},
		"older pages client":    {`{"name":"FORBIDDEN","message":"This Grafana Assistant does not support this gcx release's Pages commands (gcx/1)."}`, pages.ErrRefused},
	} {
		t.Run(name, func(t *testing.T) {
			f, client := newFake(t)
			f.refuse = tt.body
			_, _, err := client.List(context.Background(), pages.ListOptions{Section: "mine", Limit: 1})
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestUnavailablePages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"name":"NOT_FOUND","message":"Pages catalog not found: disabled"}`))
	}))
	t.Cleanup(server.Close)
	baseClient, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
	require.NoError(t, err)
	_, _, err = pages.NewClient(baseClient, server.URL).List(context.Background(), pages.ListOptions{})
	require.ErrorIs(t, err, pages.ErrUnavailable)
}

// str and object read decoded JSON values; a missing or mistyped value is
// zero, which the assertions then report.
func str(v any) string {
	s, _ := v.(string)
	return s
}

func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func mustLink(t *testing.T, dir string) *pages.Link {
	t.Helper()
	link, err := pages.ReadLink(dir)
	require.NoError(t, err)
	return link
}

func TestSourceOnAnAssistantWithoutTheRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(server.Close)
	baseClient, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
	require.NoError(t, err)
	_, err = pages.NewClient(baseClient, server.URL).Source(context.Background(), chatID, pageID, "release")
	require.ErrorIs(t, err, pages.ErrRefused)
	require.ErrorContains(t, err, "cannot download a page's project")
}
