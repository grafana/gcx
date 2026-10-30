//go:build linux

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// cachedRuntime compiles the stdin guest with dir as its cache.
func cachedRuntime(t *testing.T, dir string) *Runtime {
	t.Helper()
	r, err := New(t.Context(), stdinGuest, Config{CacheDir: dir, MemoryLimitBytes: 1 << 20})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

// skipAsRoot skips a test that relies on file modes, which root ignores.
func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads and writes regardless of file modes")
	}
}

// chmodAll sets every directory under dir to dirMode and every file to fileMode,
// and restores both on cleanup so t.TempDir can remove them.
func chmodAll(t *testing.T, dir string, dirMode, fileMode fs.FileMode) {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var dirs []string
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		return root.Chmod(path, fileMode)
	})
	if err != nil {
		t.Fatal(err)
	}
	// Directories last, deepest first, so the walk could still enter them.
	for _, d := range slices.Backward(dirs) {
		if err := root.Chmod(d, dirMode); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, d := range dirs {
			_ = os.Chmod(filepath.Join(dir, d), 0o755)
		}
	})
}

func assertRuns(t *testing.T, r *Runtime) {
	t.Helper()
	if _, err := r.Run(t.Context(), Invocation{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// A warm cache is used, and reports no error.
func TestNewUsesCache(t *testing.T) {
	dir := t.TempDir()
	cachedRuntime(t, dir)
	before := cacheEntries(t, dir)
	r := cachedRuntime(t, dir)
	if err := r.CacheError(); err != nil {
		t.Fatalf("CacheError: %v, want nil", err)
	}
	// A miss would have rewritten the entry.
	if after := cacheEntries(t, dir); !maps.Equal(after, before) {
		t.Fatalf("cache entries changed from %v to %v: New missed", before, after)
	}
	assertRuns(t, r)
}

// cacheEntries returns the modification time of each entry in dir.
func cacheEntries(t *testing.T, dir string) map[string]time.Time {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*", "*"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no cache entries (%v)", err)
	}
	entries := map[string]time.Time{}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		entries[p] = info.ModTime()
	}
	return entries
}

// Errors outside the cache directory, like the guest's root under TMPDIR,
// aren't the cache's.
func TestIsCacheError(t *testing.T) {
	dir := t.TempDir()
	inCache := &fs.PathError{Op: "open", Path: filepath.Join(dir, "wazero-v1", "entry"), Err: fs.ErrPermission}
	outside := &fs.PathError{Op: "mkdirtemp", Path: filepath.Join(os.TempDir(), "gcx-sandbox-root"), Err: fs.ErrPermission}
	sibling := &fs.PathError{Op: "open", Path: dir + "-other/entry", Err: fs.ErrPermission}
	// wazero reports paths in a relative cache directory as absolute.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relEntry := &fs.PathError{Op: "open", Path: filepath.Join(wd, "cache", "wazero-v1", "entry"), Err: fs.ErrPermission}
	for _, tc := range []struct {
		name string
		err  error
		dir  string
		want bool
	}{
		{name: "entry in the cache", err: inCache, dir: dir, want: true},
		{name: "wrapped entry in the cache", err: fmt.Errorf("compiling: %w", inCache), dir: dir, want: true},
		{name: "opening the cache", err: fmt.Errorf("%w: %w", errCacheDir, errors.New("mkdir: permission denied")), dir: dir, want: true},
		{name: "path outside the cache", err: outside, dir: dir, want: false},
		{name: "sibling of the cache", err: sibling, dir: dir, want: false},
		{name: "no cache", err: inCache, dir: "", want: false},
		{name: "entry in a relative cache", err: relEntry, dir: "cache", want: true},
		{name: "path outside a relative cache", err: outside, dir: "cache", want: false},
		{name: "not a path error", err: errors.New("invalid module"), dir: dir, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCacheError(tc.err, tc.dir); got != tc.want {
				t.Fatalf("isCacheError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// A cache its user can't read, like a root-only copy in an image, costs a
// compile rather than failing New, and CacheError says why. That holds for a
// relative CacheDir too, whose errors wazero reports with absolute paths.
func TestNewCompilesWithoutUnreadableCache(t *testing.T) {
	skipAsRoot(t)
	for _, relative := range []bool{false, true} {
		t.Run(fmt.Sprintf("relative=%v", relative), func(t *testing.T) {
			testNewCompilesWithoutUnreadableCache(t, relative)
		})
	}
}

func testNewCompilesWithoutUnreadableCache(t *testing.T, relative bool) {
	t.Helper()
	dir := t.TempDir()
	cachedRuntime(t, dir)
	chmodAll(t, dir, 0o700, 0o000)
	if relative {
		t.Chdir(filepath.Dir(dir))
		dir = filepath.Base(dir)
	}

	r := cachedRuntime(t, dir)
	var pathErr *fs.PathError
	if err := r.CacheError(); !errors.As(err, &pathErr) || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("CacheError: %v, want a permission error", err)
	}
	assertRuns(t, r)
}

// A read-only cache that misses can't take the compiled code, and that costs
// a compile rather than failing New: whether it misses an entry, as for a
// module it wasn't built for, or the whole directory of this wazero version.
func TestNewCompilesWithoutReadOnlyCacheMiss(t *testing.T) {
	skipAsRoot(t)
	for _, tc := range []struct {
		name string
		miss func(t *testing.T, dir string)
	}{
		{name: "entry", miss: func(t *testing.T, dir string) {
			t.Helper()
			cachedRuntime(t, dir)
			entries, err := filepath.Glob(filepath.Join(dir, "*", "*"))
			if err != nil || len(entries) == 0 {
				t.Fatalf("no cache entries (%v)", err)
			}
			for _, e := range entries {
				if err := os.Remove(e); err != nil {
					t.Fatal(err)
				}
			}
		}},
		{name: "version directory", miss: func(*testing.T, string) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.miss(t, dir)
			chmodAll(t, dir, 0o555, 0o444)

			r := cachedRuntime(t, dir)
			if err := r.CacheError(); !errors.Is(err, fs.ErrPermission) && !errors.Is(err, errCacheDir) {
				t.Fatalf("CacheError: %v, want a permission error", err)
			}
			assertRuns(t, r)
		})
	}
}
