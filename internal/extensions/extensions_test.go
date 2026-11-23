package extensions_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/extensions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testExt is testdata/testext built against the extsdk module.
//
//nolint:gochecknoglobals
var testExt string

// The test binary stands in for gcx when an extension runs a command.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_GCX") == "1" {
		os.Exit(fakeGCX())
	}
	dir, err := os.MkdirTemp("", "gcx-testext-")
	if err != nil {
		panic(err)
	}
	testExt = filepath.Join(dir, exe("testext"))
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", testExt, ".")
	cmd.Dir = filepath.Join("testdata", "testext")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build test extension: %v\n%s", err, out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func fakeGCX() int {
	if len(os.Args) > 1 && os.Args[1] == "big" {
		// Larger than any single gRPC message gcx accepts by default.
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), bigOutput))
		return 3
	}
	if len(os.Args) > 1 && os.Args[1] == "fail" {
		_, _ = os.Stdout.WriteString(`{"error":{"summary":"boom","details":"it broke"}}`)
		return 4
	}
	out, err := json.Marshal(map[string]any{
		"args":    os.Args[1:],
		"context": os.Getenv("GCX_CONTEXT"),
		"config":  os.Getenv("GCX_CONFIG"),
		"agent":   os.Getenv("GCX_AGENT_MODE"),
	})
	if err != nil {
		return 1
	}
	_, _ = os.Stdout.Write(append(out, '\n'))
	return 0
}

const bigOutput = 70 << 20

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func writeManifest(t *testing.T, dir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, extensions.ManifestFile), []byte(body), 0o600))
}

func newStore(t *testing.T) *extensions.Store {
	t.Helper()
	return &extensions.Store{Dir: filepath.Join(t.TempDir(), "extensions"), GCXVersion: "1.0.0", HTTPClient: http.DefaultClient}
}

// localExtension creates a source directory whose manifest points at the
// built test extension.
func localExtension(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	data, err := os.ReadFile(testExt)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, exe("testext")), data, 0o755)) //nolint:gosec // test executable
	writeManifest(t, dir, fmt.Sprintf(`name: %s
version: 0.1.0
description: test extension
platforms:
  - os: "*"
    arch: "*"
    path: %s
`, name, exe("testext")))
	return dir
}

func TestLocalInstallListUninstall(t *testing.T) {
	store := newStore(t)
	src := localExtension(t, "hello")

	ext, err := store.Install(t.Context(), src)
	require.NoError(t, err)
	assert.Equal(t, "hello", ext.Name)
	assert.True(t, ext.Local)
	assert.Equal(t, filepath.Join(src, exe("testext")), ext.Executable)

	_, err = store.Install(t.Context(), src)
	require.ErrorContains(t, err, "already installed")

	list, err := store.List()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, *ext, list[0])

	results, err := store.Update(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, []extensions.UpdateResult{{Name: "hello", From: "0.1.0", To: "0.1.0", Status: extensions.StatusSkipped}}, results)

	require.NoError(t, os.Mkdir(filepath.Join(store.Dir, "not-an-extension"), 0o755))
	list, err = store.List()
	require.NoError(t, err)
	assert.Len(t, list, 1, "directories without a record are skipped")

	_, err = store.Uninstall("hello")
	require.NoError(t, err)
	assert.FileExists(t, ext.Executable, "a local install's source is left untouched")
	_, err = store.Get("hello")
	require.ErrorIs(t, err, extensions.ErrNotInstalled)
}

func TestInstallRejectsInvalidManifests(t *testing.T) {
	sum := strings.Repeat("a", 64)
	tests := []struct {
		name     string
		manifest string
		err      string
	}{
		{"unknown field", "name: x\nversion: 1.0.0\nplatform: []\n", "unknown field"},
		{"bad name", "name: Hello\nversion: 1.0.0\nplatforms: [{os: '*', arch: '*', path: x}]\n", "must start with a lowercase letter"},
		{"reserved name", "name: list\nversion: 1.0.0\nplatforms: [{os: '*', arch: '*', path: x}]\n", "reserved"},
		{"bad version", "name: x\nversion: latest\nplatforms: [{os: '*', arch: '*', path: x}]\n", "not a semantic version"},
		{"no platforms", "name: x\nversion: 1.0.0\n", "at least one platform"},
		{"url and path", "name: x\nversion: 1.0.0\nplatforms: [{os: linux, arch: amd64, path: x, url: 'https://e/x', sha256: " + sum + "}]\n", "exactly one of url and path"},
		{"http url", "name: x\nversion: 1.0.0\nplatforms: [{os: linux, arch: amd64, url: 'http://e/x', sha256: " + sum + "}]\n", "https"},
		{"missing checksum", "name: x\nversion: 1.0.0\nplatforms: [{os: linux, arch: amd64, url: 'https://e/x'}]\n", "sha256"},
		{"archive without bin", "name: x\nversion: 1.0.0\nplatforms: [{os: linux, arch: amd64, url: 'https://e/x.tar.gz', sha256: " + sum + "}]\n", "bin is required"},
		{"bin named like the record", "name: x\nversion: 1.0.0\nplatforms: [{os: linux, arch: amd64, url: 'https://e/x.zip', sha256: " + sum + ", bin: dir/extension.json}]\n", "bin cannot be named"},
		{"bin named like the record in another case", "name: x\nversion: 1.0.0\nplatforms: [{os: linux, arch: amd64, url: 'https://e/x.zip', sha256: " + sum + ", bin: Extension.JSON}]\n", "bin cannot be named"},
		{"bin with a backslash", "name: x\nversion: 1.0.0\nplatforms: [{os: windows, arch: amd64, url: 'https://e/x', sha256: " + sum + ", bin: '..\\..\\x.exe'}]\n", "bin must use /"},
		{"wildcard release", "name: x\nversion: 1.0.0\nplatforms: [{os: '*', arch: '*', url: 'https://e/x', sha256: " + sum + "}]\n", "must name an os and arch"},
		{"newer gcx", "name: x\nversion: 1.0.0\nminGCXVersion: 2.0.0\nplatforms: [{os: '*', arch: '*', path: x}]\n", "requires gcx 2.0.0"},
		{"no local path", "name: x\nversion: 1.0.0\nplatforms: [{os: plan9, arch: amd64, path: x}]\n", "no local path"},
		{"missing executable", "name: x\nversion: 1.0.0\nplatforms: [{os: '*', arch: '*', path: x}]\n", "build it first"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifest(t, dir, tt.manifest)
			_, err := newStore(t).Install(t.Context(), dir)
			require.ErrorContains(t, err, tt.err)
		})
	}
}

func TestInstallRejectsUnencryptedSources(t *testing.T) {
	for _, source := range []string{"http://example.com/ext.git", "git://example.com/ext.git", "./does-not-exist"} {
		_, err := newStore(t).Install(t.Context(), source)
		require.ErrorContains(t, err, "neither a local directory nor a git URL", source)
	}
}

// releaseRepo is a git repository whose tags publish the test extension
// through a TLS server.
type releaseRepo struct {
	t      *testing.T
	dir    string
	server *httptest.Server
	files  map[string][]byte
}

func newReleaseRepo(t *testing.T) *releaseRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	// Keep the developer's git config (signing, hooks) out of the test.
	empty := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r := &releaseRepo{t: t, dir: t.TempDir(), files: map[string][]byte{}}
	r.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, ok := r.files[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(r.server.Close)
	r.git("init", "--quiet")
	return r
}

func (r *releaseRepo) url() string {
	path := filepath.ToSlash(r.dir)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path // file:///C:/...
	}
	return "file://" + path
}

func (r *releaseRepo) git(args ...string) {
	r.t.Helper()
	cmd := exec.CommandContext(r.t.Context(), "git", append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)...) //nolint:gosec // test-controlled arguments
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	require.NoError(r.t, err, string(out))
}

// release tags version with a manifest whose artifact is a tar.gz holding
// the test extension. sum overrides the artifact's checksum when set.
func (r *releaseRepo) release(version, sum string) {
	r.t.Helper()
	bin, err := os.ReadFile(testExt)
	require.NoError(r.t, err)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	name := "bin/" + exe("hello")
	require.NoError(r.t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(bin)), Typeflag: tar.TypeReg}))
	_, err = tw.Write(bin)
	require.NoError(r.t, err)
	require.NoError(r.t, tw.Close())
	require.NoError(r.t, gz.Close())

	path := "/" + version + "/hello.tar.gz"
	r.files[path] = buf.Bytes()
	if sum == "" {
		digest := sha256.Sum256(buf.Bytes())
		sum = hex.EncodeToString(digest[:])
	}
	writeManifest(r.t, r.dir, fmt.Sprintf(`name: hello
version: %s
platforms:
  - os: %s
    arch: %s
    url: %s%s
    sha256: "%s"
    bin: %s
`, strings.TrimPrefix(version, "v"), runtime.GOOS, runtime.GOARCH, r.server.URL, path, sum, name))
	r.git("add", ".")
	r.git("commit", "--quiet", "-m", version)
	r.git("tag", version)
}

func TestGitInstallAndUpdate(t *testing.T) {
	repo := newReleaseRepo(t)
	repo.release("v0.1.0", "")
	repo.release("v0.2.0-rc.1", "")
	store := newStore(t)
	store.HTTPClient = repo.server.Client()

	ext, err := store.Install(t.Context(), repo.url())
	require.NoError(t, err)
	assert.Equal(t, "0.1.0", ext.Version, "pre-releases are not installed")
	assert.Equal(t, "v0.1.0", ext.Tag)
	assert.Len(t, ext.Commit, 40)
	assert.Equal(t, filepath.Join(store.Dir, "hello", exe("hello")), ext.Executable)
	assertRuns(t, ext)

	results, err := store.Update(t.Context(), []string{"hello"})
	require.NoError(t, err)
	assert.Equal(t, extensions.StatusUpToDate, results[0].Status)

	repo.release("v0.2.0", "")
	results, err = store.Update(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, []extensions.UpdateResult{{Name: "hello", From: "0.1.0", To: "0.2.0", Status: extensions.StatusUpdated}}, results)
	ext, err = store.Get("hello")
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", ext.Tag)
	assertRuns(t, ext)

	repo.git("tag", "-d", "v0.2.0")
	results, err = store.Update(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, extensions.StatusUpToDate, results[0].Status, "a deleted tag does not downgrade")
}

func TestUpdateContinuesAfterFailure(t *testing.T) {
	store := newStore(t)
	repo := newReleaseRepo(t)
	repo.release("v0.1.0", "")
	store.HTTPClient = repo.server.Client()
	_, err := store.Install(t.Context(), repo.url())
	require.NoError(t, err)
	_, err = store.Install(t.Context(), localExtension(t, "local"))
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(repo.dir))

	results, err := store.Update(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, extensions.StatusFailed, results[0].Status)
	assert.NotEmpty(t, results[0].Error)
	assert.Equal(t, extensions.StatusSkipped, results[1].Status)
	ext, err := store.Get("hello")
	require.NoError(t, err)
	assertRuns(t, ext)
}

func TestGitInstallVerifiesChecksum(t *testing.T) {
	repo := newReleaseRepo(t)
	repo.release("v0.1.0", strings.Repeat("0", 64))
	store := newStore(t)
	store.HTTPClient = repo.server.Client()

	_, err := store.Install(t.Context(), repo.url())
	require.ErrorContains(t, err, "checksum mismatch")
	list, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, list, "a failed install leaves nothing behind")
}

func TestGitInstallRequiresReleaseTag(t *testing.T) {
	repo := newReleaseRepo(t)
	writeManifest(t, repo.dir, "name: hello\n")
	repo.git("add", ".")
	repo.git("commit", "--quiet", "-m", "init")

	_, err := newStore(t).Install(t.Context(), repo.url())
	require.ErrorContains(t, err, "no release tags")
}

func assertRuns(t *testing.T, ext *extensions.Extension) {
	t.Helper()
	var stdout bytes.Buffer
	code, err := ext.Run(t.Context(), extensions.RunOptions{Args: []string{"echo", "hi"}, Stdout: &stdout, Stderr: &bytes.Buffer{}})
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "[hi]\n", stdout.String())
}

func TestRun(t *testing.T) {
	t.Setenv("FAKE_GCX", "1")
	defer agent.SetFlag(agent.IsAgentMode())
	agent.SetFlag(true) // as with --agent, which children cannot see
	gcx, err := os.Executable()
	require.NoError(t, err)
	store := newStore(t)
	ext, err := store.Install(t.Context(), localExtension(t, "hello"))
	require.NoError(t, err)

	tests := []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{name: "output", args: []string{"echo", "a", "--b"}, stdout: "[a --b]\n", stderr: "to stderr\n"},
		{name: "exit code", args: []string{"exit", "3"}, code: 3},
		{name: "exit code out of range", args: []string{"exit", "300"}, code: 1},
		{
			name:   "gcx command",
			args:   []string{"gcx", "resources", "get", "dashboards"},
			stdout: `{"agent":"true","args":["resources","get","dashboards","--output","json"],"config":"/tmp/gcx.yaml","context":"prod"}` + "\n",
		},
		{name: "gcx command fails", args: []string{"gcx", "fail"}, code: 4, stderr: "Error: boom: it broke\n"},
		{name: "large gcx output", args: []string{"size", "big"}, stdout: fmt.Sprintf("%d 3\n", bigOutput)},
		{name: "output before returning", args: []string{"flood"}, code: 1, stdout: strings.Repeat("x", 2<<20) + "END\n", stderr: "Error: after output\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, err := ext.Run(t.Context(), extensions.RunOptions{
				Args: tt.args, GCX: gcx, Context: "prod", ConfigFile: "/tmp/gcx.yaml",
				Stdout: &stdout, Stderr: &stderr,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.code, code)
			assert.Equal(t, tt.stdout, stdout.String())
			assert.Equal(t, tt.stderr, stderr.String())
		})
	}
}

func TestRunCancellation(t *testing.T) {
	ext, err := newStore(t).Install(t.Context(), localExtension(t, "hello"))
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = ext.Run(ctx, extensions.RunOptions{Args: []string{"wait"}, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
}
