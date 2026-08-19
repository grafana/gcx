package watch_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/server/watch"
)

func newTestWatcher(t *testing.T) *watch.Watcher {
	t.Helper()

	w, err := watch.NewWatcher(context.Background(), func(string) {})
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}

	return w
}

func writeTree(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	for _, sub := range []string{"a", "b", "b/c"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	return root
}

func TestAddWatchesDirectoryTree(t *testing.T) {
	w := newTestWatcher(t)

	if err := w.Add(context.Background(), writeTree(t)); err != nil {
		t.Fatalf("Add: %v", err)
	}
}

func TestAddStopsOnCancelledContext(t *testing.T) {
	w := newTestWatcher(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := w.Add(ctx, writeTree(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestAddRejectsNonDirectory(t *testing.T) {
	w := newTestWatcher(t)

	file := filepath.Join(t.TempDir(), "resource.json")
	if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := w.Add(context.Background(), file); err == nil {
		t.Fatal("expected error for non-directory path, got nil")
	}
}
