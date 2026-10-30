package pages_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/assistant/pages"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func export(files map[string]string) *pages.Export {
	out := &pages.Export{Source: pages.Source{Version: 1, Files: map[string][]byte{}}}
	for name, body := range files {
		out.Source.Files[name] = []byte(body)
	}
	return out
}

func TestPullThenReadRoundTripsTheSource(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "radar")
	entry := &pages.Entry{ID: "page-1", Title: "Radar", Version: 3, RevisionID: "rev-3"}
	exported := export(map[string]string{"package.json": "{}", "src/App.tsx": "export {}", "pages.bindings.json": "{}"})
	exported.SpeakerNotes = map[string]string{"intro": "Say hello"}

	meta, err := pages.WriteProject(dir, entry, exported, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"package.json", "pages.bindings.json", "src/App.tsx"}, meta.Files)

	// Installed and built output and dotfiles are not source.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "node_modules", "react"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "node_modules", "react", "index.js"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o600))

	source, err := pages.ReadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, exported.Source, source)

	read, err := pages.ReadMeta(dir)
	require.NoError(t, err)
	assert.Equal(t, meta, read)
	assert.Equal(t, "Say hello", read.SpeakerNotes["intro"])
}

func TestPullingAgainRemovesFilesThePageNoLongerHas(t *testing.T) {
	dir := t.TempDir()
	entry := &pages.Entry{ID: "page-1", Version: 1}
	_, err := pages.WriteProject(dir, entry, export(map[string]string{"src/Old.tsx": "old", "src/App.tsx": "v1"}), false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("mine"), 0o600))

	entry.Version = 2
	_, err = pages.WriteProject(dir, entry, export(map[string]string{"src/App.tsx": "v2"}), false)
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(dir, "src", "Old.tsx"))
	assert.FileExists(t, filepath.Join(dir, "notes.md"), "files the pull did not write are left alone")
	got, err := os.ReadFile(filepath.Join(dir, "src", "App.tsx"))
	require.NoError(t, err)
	assert.Equal(t, "v2", string(got))
}

func TestPullRefusesADirectoryItDidNotWrite(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o600))
	entry := &pages.Entry{ID: "page-1"}

	_, err := pages.WriteProject(dir, entry, export(map[string]string{"package.json": "{}"}), false)
	require.ErrorContains(t, err, "--force")

	_, err = pages.WriteProject(dir, entry, export(map[string]string{"package.json": "{}"}), true)
	require.NoError(t, err)

	_, err = pages.WriteProject(dir, &pages.Entry{ID: "page-2"}, export(map[string]string{"package.json": "{}"}), false)
	require.ErrorContains(t, err, "holds page page-1")
}

func TestPullRejectsPathsOutsideTheProject(t *testing.T) {
	for _, name := range []string{"../escape.ts", "/etc/passwd", "src/../../x", `src\x.ts`, pages.MetaFile} {
		t.Run(name, func(t *testing.T) {
			_, err := pages.WriteProject(t.TempDir(), &pages.Entry{ID: "p"}, export(map[string]string{name: "x"}), false)
			require.Error(t, err)
		})
	}
}

func TestReadProjectNeedsSource(t *testing.T) {
	_, err := pages.ReadProject(t.TempDir())
	require.ErrorContains(t, err, "no source files")

	_, err = pages.ReadMeta(t.TempDir())
	require.ErrorContains(t, err, "pull the page first")
}
