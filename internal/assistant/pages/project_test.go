package pages_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/assistant/pages"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSavedPath(t *testing.T) {
	for name, want := range map[string]bool{
		"page.json":                           true,
		"pages.bindings.json":                 true,
		"pages.design.json":                   true,
		"src/App.tsx":                         true,
		"src/assets/logo.svg":                 true,
		"packages/pages-sdk/docs/queries.md":  true,
		".agents/skills/page-design/SKILL.md": true,
		"assets/data.csv":                     true,
		"public/robots.txt":                   true,
		"README.md":                           false,
		"notes.txt":                           false,
		"srcfoo/App.tsx":                      false,
		"src/.env":                            false,
		"src/node_modules/x.js":               false,
		"dist/page.json":                      false,
		".pages/source.json":                  false,
		".gcx-page.json":                      false,
		"../src/App.tsx":                      false,
		"/src/App.tsx":                        false,
	} {
		assert.Equal(t, want, pages.SavedPath(name), name)
	}
}

func TestWriteProjectReplacesAPreviousMirrorOfTheSamePage(t *testing.T) {
	dir := t.TempDir()
	link := pages.Link{ChatID: "chat", PageID: "page", Project: "p", Title: "Page", ReleaseID: "r1"}
	_, err := pages.WriteProject(dir, link, &pages.Source{Files: map[string][]byte{"src/App.tsx": []byte("a"), "src/Old.tsx": []byte("old")}})
	require.NoError(t, err)

	link.ReleaseID = "r2"
	written, err := pages.WriteProject(dir, link, &pages.Source{Files: map[string][]byte{"src/App.tsx": []byte("b")}})
	require.NoError(t, err)

	assert.Equal(t, "r2", written.ReleaseID)
	data, err := os.ReadFile(filepath.Join(dir, "src/App.tsx"))
	require.NoError(t, err)
	assert.Equal(t, "b", string(data))
	assert.NoFileExists(t, filepath.Join(dir, "src/Old.tsx"), "files the page dropped are removed")
}

func TestWriteProjectGuardsTheDirectory(t *testing.T) {
	source := &pages.Source{Files: map[string][]byte{"src/App.tsx": []byte("a")}}

	other := t.TempDir()
	_, err := pages.WriteProject(other, pages.Link{ChatID: "c", PageID: "one", Project: "p", Title: "One"}, source)
	require.NoError(t, err)
	_, err = pages.WriteProject(other, pages.Link{ChatID: "c", PageID: "two", Project: "p", Title: "Two"}, source)
	require.ErrorContains(t, err, "holds another page")

	busy := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(busy, "mine.txt"), nil, 0o600))
	_, err = pages.WriteProject(busy, pages.Link{ChatID: "c", PageID: "one", Project: "p"}, source)
	require.ErrorContains(t, err, "is not empty")

	for _, name := range []string{"../escape.tsx", "/etc/passwd", "src/../../x", ".gcx-page.json", `src\App.tsx`} {
		_, err := pages.WriteProject(t.TempDir(), pages.Link{ChatID: "c", PageID: "one", Project: "p"}, &pages.Source{Files: map[string][]byte{name: []byte("x")}})
		require.ErrorContains(t, err, "unsafe page source path", name)
	}
}

func TestChangesRejectsFilesGcxCannotPush(t *testing.T) {
	dir := t.TempDir()
	link, err := pages.WriteProject(dir, pages.Link{ChatID: "c", PageID: "p", Project: "p"}, &pages.Source{Files: map[string][]byte{"src/App.tsx": []byte("a")}})
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src/assets"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src/assets/logo.png"), []byte{0x89, 'P', 'N', 'G', 0}, 0o600))
	_, err = pages.Changes(dir, link)
	require.ErrorContains(t, err, "src/assets/logo.png cannot be pushed")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "src/assets/logo.png"), []byte(strings.Repeat("x", 121<<10)), 0o600))
	_, err = pages.Changes(dir, link)
	require.ErrorContains(t, err, "at most 120 KiB")
}

func TestChangesSkipsDependenciesAndBuildOutput(t *testing.T) {
	dir := t.TempDir()
	link, err := pages.WriteProject(dir, pages.Link{ChatID: "c", PageID: "p", Project: "p"}, &pages.Source{Files: map[string][]byte{"src/App.tsx": []byte("a")}})
	require.NoError(t, err)
	for _, name := range []string{"node_modules/react/index.js", "dist/page.json", ".git/HEAD"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte{0}, 0o600))
	}

	diff, err := pages.Changes(dir, link)
	require.NoError(t, err)
	assert.True(t, diff.Empty())
	assert.Empty(t, diff.Ignored)
}

func TestSlug(t *testing.T) {
	assert.Equal(t, "checkout-health", pages.Slug("Checkout health"))
	assert.Equal(t, "q3-review-eu-us", pages.Slug("  Q3 review: EU/US!  "))
	assert.Equal(t, "page", pages.Slug("🚀"))
	assert.LessOrEqual(t, len(pages.Slug(strings.Repeat("long title ", 20))), 48)
}
