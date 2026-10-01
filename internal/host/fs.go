package host

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
)

// ReadFile mirrors [os.ReadFile].
func ReadFile(ctx context.Context, name string) ([]byte, error) {
	if Sandboxed(ctx) {
		return nil, pathErr("open", name)
	}
	return os.ReadFile(name)
}

// WriteFile mirrors [os.WriteFile].
func WriteFile(ctx context.Context, name string, data []byte, perm fs.FileMode) error {
	if Sandboxed(ctx) {
		return pathErr("open", name)
	}
	return os.WriteFile(name, data, perm)
}

// Open mirrors [os.Open].
func Open(ctx context.Context, name string) (*os.File, error) {
	if Sandboxed(ctx) {
		return nil, pathErr("open", name)
	}
	return os.Open(name)
}

// OpenFile mirrors [os.OpenFile].
func OpenFile(ctx context.Context, name string, flag int, perm fs.FileMode) (*os.File, error) {
	if Sandboxed(ctx) {
		return nil, pathErr("open", name)
	}
	return os.OpenFile(name, flag, perm)
}

// Create mirrors [os.Create].
func Create(ctx context.Context, name string) (*os.File, error) {
	if Sandboxed(ctx) {
		return nil, pathErr("open", name)
	}
	return os.Create(name)
}

// CreateTemp mirrors [os.CreateTemp].
func CreateTemp(ctx context.Context, dir, pattern string) (*os.File, error) {
	if Sandboxed(ctx) {
		return nil, pathErr("createtemp", filepath.Join(dir, pattern))
	}
	return os.CreateTemp(dir, pattern)
}

// MkdirTemp mirrors [os.MkdirTemp].
func MkdirTemp(ctx context.Context, dir, pattern string) (string, error) {
	if Sandboxed(ctx) {
		return "", pathErr("mkdirtemp", filepath.Join(dir, pattern))
	}
	return os.MkdirTemp(dir, pattern)
}

// Mkdir mirrors [os.Mkdir].
func Mkdir(ctx context.Context, name string, perm fs.FileMode) error {
	if Sandboxed(ctx) {
		return pathErr("mkdir", name)
	}
	return os.Mkdir(name, perm)
}

// MkdirAll mirrors [os.MkdirAll].
func MkdirAll(ctx context.Context, path string, perm fs.FileMode) error {
	if Sandboxed(ctx) {
		return pathErr("mkdir", path)
	}
	return os.MkdirAll(path, perm)
}

// Remove mirrors [os.Remove].
func Remove(ctx context.Context, name string) error {
	if Sandboxed(ctx) {
		return pathErr("remove", name)
	}
	return os.Remove(name)
}

// RemoveAll mirrors [os.RemoveAll].
func RemoveAll(ctx context.Context, path string) error {
	if Sandboxed(ctx) {
		return pathErr("removeall", path)
	}
	return os.RemoveAll(path)
}

// Rename mirrors [os.Rename].
func Rename(ctx context.Context, oldpath, newpath string) error {
	if Sandboxed(ctx) {
		return pathErr("rename", oldpath)
	}
	return os.Rename(oldpath, newpath)
}

// Link mirrors [os.Link].
func Link(ctx context.Context, oldname, newname string) error {
	if Sandboxed(ctx) {
		return pathErr("link", oldname)
	}
	return os.Link(oldname, newname)
}

// Chmod mirrors [os.Chmod].
func Chmod(ctx context.Context, name string, mode fs.FileMode) error {
	if Sandboxed(ctx) {
		return pathErr("chmod", name)
	}
	return os.Chmod(name, mode)
}

// Stat mirrors [os.Stat].
func Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if Sandboxed(ctx) {
		return nil, pathErr("stat", name)
	}
	return os.Stat(name)
}

// Lstat mirrors [os.Lstat].
func Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if Sandboxed(ctx) {
		return nil, pathErr("lstat", name)
	}
	return os.Lstat(name)
}

// ReadDir mirrors [os.ReadDir].
func ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if Sandboxed(ctx) {
		return nil, pathErr("open", name)
	}
	return os.ReadDir(name)
}

// WalkDir mirrors [filepath.WalkDir].
func WalkDir(ctx context.Context, root string, fn fs.WalkDirFunc) error {
	if Sandboxed(ctx) {
		return fn(root, nil, pathErr("lstat", root))
	}
	return filepath.WalkDir(root, fn)
}

// DirFS mirrors [os.DirFS]. Inside a sandbox every operation on the returned
// filesystem fails.
func DirFS(ctx context.Context, dir string) fs.FS {
	if Sandboxed(ctx) {
		return sandboxFS{}
	}
	return os.DirFS(dir)
}

type sandboxFS struct{}

func (sandboxFS) Open(name string) (fs.File, error) {
	return nil, pathErr("open", name)
}

// Well-known directories.

// Getwd mirrors [os.Getwd].
func Getwd(ctx context.Context) (string, error) {
	if Sandboxed(ctx) {
		return "", opErr("getwd")
	}
	return os.Getwd()
}

// UserHomeDir mirrors [os.UserHomeDir].
func UserHomeDir(ctx context.Context) (string, error) {
	if Sandboxed(ctx) {
		return "", opErr("user home directory")
	}
	return os.UserHomeDir()
}

// UserConfigDir mirrors [os.UserConfigDir].
func UserConfigDir(ctx context.Context) (string, error) {
	if Sandboxed(ctx) {
		return "", opErr("user config directory")
	}
	return os.UserConfigDir()
}

// UserCacheDir mirrors [os.UserCacheDir].
func UserCacheDir(ctx context.Context) (string, error) {
	if Sandboxed(ctx) {
		return "", opErr("user cache directory")
	}
	return os.UserCacheDir()
}

// TempDir mirrors [os.TempDir]. Inside a sandbox it returns an error, since
// there is nowhere to put temporary files.
func TempDir(ctx context.Context) (string, error) {
	if Sandboxed(ctx) {
		return "", opErr("temp directory")
	}
	return os.TempDir(), nil
}

// Hostname mirrors [os.Hostname].
func Hostname(ctx context.Context) (string, error) {
	if Sandboxed(ctx) {
		return "", opErr("hostname")
	}
	return os.Hostname()
}

// EvalSymlinks mirrors [filepath.EvalSymlinks].
func EvalSymlinks(ctx context.Context, path string) (string, error) {
	if Sandboxed(ctx) {
		return "", pathErr("lstat", path)
	}
	return filepath.EvalSymlinks(path)
}

// Glob mirrors [filepath.Glob].
func Glob(ctx context.Context, pattern string) ([]string, error) {
	if Sandboxed(ctx) {
		return nil, pathErr("glob", pattern)
	}
	return filepath.Glob(pattern)
}

// RenameNoReplace renames oldpath to newpath, failing if newpath already
// exists. Linux, macOS and Windows enforce this atomically; elsewhere it falls
// back to [os.Rename] and callers must check newpath is absent first.
func RenameNoReplace(ctx context.Context, oldpath, newpath string) error {
	if Sandboxed(ctx) {
		return pathErr("rename", oldpath)
	}
	return renameNoReplace(oldpath, newpath)
}

// Abs mirrors [filepath.Abs]. A sandbox has no working directory, so relative
// paths fail there; absolute paths are only cleaned.
func Abs(ctx context.Context, path string) (string, error) {
	if Sandboxed(ctx) {
		if filepath.IsAbs(path) {
			return filepath.Clean(path), nil
		}
		return "", pathErr("abs", path)
	}
	return filepath.Abs(path)
}
