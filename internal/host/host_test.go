package host_test

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestSandboxRefusesHostAccess(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing")
	require.NoError(t, host.WriteFile(t.Context(), existing, []byte("secret"), 0o600))

	ctx := host.WithSandbox(t.Context(), &host.Sandbox{})

	_, err := host.ReadFile(ctx, existing)
	require.ErrorIs(t, err, host.ErrUnavailable)
	require.ErrorIs(t, err, fs.ErrNotExist, "sandboxed reads look like an empty filesystem")

	require.ErrorIs(t, host.WriteFile(ctx, filepath.Join(dir, "new"), nil, 0o600), host.ErrUnavailable)
	require.ErrorIs(t, host.RenameNoReplace(ctx, existing, filepath.Join(dir, "renamed")), host.ErrUnavailable)
	require.FileExists(t, existing)
	_, err = host.Stat(ctx, filepath.Join(dir, "new"))
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = fs.ReadFile(host.DirFS(ctx, dir), "existing")
	require.ErrorIs(t, err, host.ErrUnavailable)

	_, err = host.Command(ctx, "true")
	require.ErrorIs(t, err, host.ErrUnavailable)
	_, err = host.Listen(ctx, "tcp", "127.0.0.1:0")
	require.ErrorIs(t, err, host.ErrUnavailable)
	_, err = host.UserHomeDir(ctx)
	require.ErrorIs(t, err, host.ErrUnavailable)
	_, err = host.StdinFile(ctx)
	require.ErrorIs(t, err, host.ErrUnavailable)
	_, err = host.EvalSymlinks(ctx, existing)
	require.ErrorIs(t, err, host.ErrUnavailable)
	_, err = host.Glob(ctx, filepath.Join(dir, "*"))
	require.ErrorIs(t, err, host.ErrUnavailable)
	_, err = host.OpenKeyring(ctx)
	require.ErrorIs(t, err, host.ErrUnavailable)
	_, err = host.NewWatcher(ctx)
	require.ErrorIs(t, err, host.ErrUnavailable)
	host.IgnoreSignals(ctx, os.Interrupt) // must not touch the process disposition
	require.ErrorIs(t, host.CaptureStdout(ctx, io.Discard, func() { t.Fatal("must not run inside a sandbox") }), host.ErrUnavailable)

	// Discovery still works inside a sandbox, cached in memory instead of on disk.
	discovery, err := host.NewCachedDiscoveryClientForConfig(ctx, &rest.Config{Host: "https://example.invalid"}, filepath.Join(dir, "discovery"), "", time.Minute)
	require.NoError(t, err)
	assert.NotNil(t, discovery)
	assert.NoDirExists(t, filepath.Join(dir, "discovery"))

	var walked error
	_ = host.WalkDir(ctx, dir, func(_ string, _ fs.DirEntry, err error) error {
		walked = err
		return nil
	})
	require.ErrorIs(t, walked, host.ErrUnavailable)
}

func TestSandboxServesEnvStdioAndArgs(t *testing.T) {
	t.Setenv("GCX_HOST_TEST", "process")

	var stdout, stderr bytes.Buffer
	ctx := host.WithSandbox(t.Context(), &host.Sandbox{
		Args:   []string{"gcx", "slo", "list"},
		Env:    map[string]string{"B": "2", "A": "1"},
		Stdin:  bytes.NewBufferString("input"),
		Stdout: &stdout,
		Stderr: &stderr,
	})

	_, ok := host.LookupEnv(ctx, "GCX_HOST_TEST")
	assert.False(t, ok, "process env must not leak into a sandbox")
	assert.Equal(t, "1", host.Getenv(ctx, "A"))
	assert.Equal(t, []string{"A=1", "B=2"}, host.Environ(ctx))
	assert.Equal(t, []string{"gcx", "slo", "list"}, host.Args(ctx))

	in, err := io.ReadAll(host.Stdin(ctx))
	require.NoError(t, err)
	assert.Equal(t, "input", string(in))

	_, _ = io.WriteString(host.Stdout(ctx), "out")
	_, _ = io.WriteString(host.Stderr(ctx), "err")
	assert.Equal(t, "out", stdout.String())
	assert.Equal(t, "err", stderr.String())

	empty := host.WithSandbox(t.Context(), &host.Sandbox{})
	in, err = io.ReadAll(host.Stdin(empty))
	require.NoError(t, err)
	assert.Empty(t, in)
	_, err = io.WriteString(host.Stdout(empty), "dropped")
	require.NoError(t, err)
}

func TestNoSandboxUsesProcess(t *testing.T) {
	t.Setenv("GCX_HOST_TEST", "process")
	assert.Equal(t, "process", host.Getenv(t.Context(), "GCX_HOST_TEST"))
	assert.False(t, host.Sandboxed(t.Context()))

	_, err := host.ReadFile(t.Context(), filepath.Join(t.TempDir(), "missing"))
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.NotErrorIs(t, err, host.ErrUnavailable)

	dir := t.TempDir()
	from, to := filepath.Join(dir, "from"), filepath.Join(dir, "to")
	require.NoError(t, host.Mkdir(t.Context(), from, 0o700))
	require.NoError(t, host.Mkdir(t.Context(), to, 0o700))
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		require.Error(t, host.RenameNoReplace(t.Context(), from, to), "must not replace an existing directory")
	}
	require.NoError(t, host.RenameNoReplace(t.Context(), from, filepath.Join(dir, "moved")))
}

func TestAbs(t *testing.T) {
	ctx := host.WithSandbox(t.Context(), &host.Sandbox{})

	abs, err := host.Abs(ctx, "/a/b/../c")
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean("/a/c"), abs)

	_, err = host.Abs(ctx, "relative")
	require.ErrorIs(t, err, host.ErrUnavailable, "a sandbox has no working directory")

	abs, err = host.Abs(t.Context(), "relative")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(abs))
}
