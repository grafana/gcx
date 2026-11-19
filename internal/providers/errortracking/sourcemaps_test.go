package errortracking_test

import (
	"archive/tar"
	"bytes"
	"cmp"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const okMap = `{"version":3,"sources":["main.js"],"mappings":"AAAA"}`

const (
	stackRoute  = "grafana GET /api/instances/test"
	readProject = "grafana GET " + etPrefix + "projects/314"
	listProject = "grafana GET " + etPrefix + "projects"
	uploadRoute = "upload POST /u/314/sourcemaps/v1 a/b"
)

func sent() []string { return []string{stackRoute, readProject, uploadRoute} }

func cloudEnv(t *testing.T, gcom string) {
	t.Helper()
	t.Setenv("GRAFANA_CLOUD_TOKEN", "tok")
	t.Setenv("GRAFANA_CLOUD_API_URL", gcom)
	t.Setenv("GRAFANA_CLOUD_STACK", "test")
}

func serveStack(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/instances/test" {
		return false
	}
	_, _ = io.WriteString(w, `{"id":123,"slug":"test"}`)
	return true
}

func agentMode(t *testing.T) {
	t.Helper()
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })
}

func runUpsert(t *testing.T, stderr io.Writer, args ...string) (string, error) {
	t.Helper()
	cmd := root.Command("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&out)
	cmd.SetErr(stderr)
	cmd.SetArgs(append([]string{"error-tracking"}, args...))
	err := cmd.ExecuteContext(t.Context())
	return out.String(), err
}

func configFor(t *testing.T, dir, server string) string {
	t.Helper()
	return write(t, dir, "config.yaml", `version: 1
current-context: test
contexts:
  test:
    stack: test
stacks:
  test:
    grafana:
      server: `+server+`
`)
}

type tarEntry struct{ Name, Body string }

func untgz(t *testing.T, body []byte) []tarEntry {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(body))
	require.NoError(t, err)
	tr := tar.NewReader(zr)
	var out []tarEntry
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		b, err := io.ReadAll(tr)
		require.NoError(t, err)
		out = append(out, tarEntry{h.Name, string(b)})
	}
}

func sparse(t *testing.T, name string, size int64) {
	t.Helper()
	require.NoError(t, os.WriteFile(name, []byte("{"), 0o600))
	require.NoError(t, os.Truncate(name, size))
}

func symlink(t *testing.T, target, name string) {
	t.Helper()
	require.NoError(t, os.Symlink(target, name))
}

type upsertEnv struct {
	got           *http.Request
	body          []byte
	seen          []string
	stderr        bytes.Buffer
	cfg, noServer string
	host          string
}

type upsertCase struct {
	name, project, release, format, cfg string
	files                               []string
	write                               map[string]string
	setup                               func(t *testing.T)
	exit                                int
	err                                 string
	calls                               []string
	check                               func(t *testing.T, out string, err error)
}

func newUpsertEnv(t *testing.T) *upsertEnv {
	t.Helper()
	e := &upsertEnv{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.seen = append(e.seen, "upload "+r.Method+" "+r.URL.Path)
		e.got = r
		e.body, _ = io.ReadAll(r.Body)
		switch r.URL.Query().Get("file") {
		case "denied.js.map":
			http.Error(w, "The token does not cover stack 123.", http.StatusForbidden)
			return
		case "gone.js.map":
			http.NotFound(w, r)
			return
		case "expired.js.map":
			http.Error(w, "token expired", http.StatusUnauthorized)
			return
		case "busy.js.map":
			http.Error(w, "auth service did not answer; try again", http.StatusServiceUnavailable)
			return
		case "ok200.js.map":
			w.WriteHeader(http.StatusOK)
			return
		case "ok204.js.map":
			w.WriteHeader(http.StatusNoContent)
			return
		case "moved.js.map":
			w.Header().Set("Location", "/else\u202ewhere")
			w.WriteHeader(http.StatusFound)
			return
		case "json.js.map":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"message":"bad\u001b]0;x\u0007 \u202emap\u2028end","traceID":"t\u001b[2J1"}`)
			return
		case "text.js.map":
			http.Error(w, "bad\x1b[31m \u2029map\u2066", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(up.Close)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.seen = append(e.seen, "grafana "+r.Method+" "+r.URL.Path)
		if serveStack(w, r) {
			return
		}
		switch r.URL.Path {
		case "/bootdata":
			_, _ = io.WriteString(w, `{"settings":{"namespace":"stacks-123"}}`)
		case etPrefix + "projects":
			_, _ = io.WriteString(w, `{"projects":[{"id":314,"name":"Checkout API"}]}`)
		case etPrefix + "projects/314":
			_, _ = io.WriteString(w, `{"id":314,"name":"Checkout API","sourceMapUploadUrl":"`+up.URL+`/u/314/sourcemaps/"}`)
		case etPrefix + "projects/315":
			_, _ = io.WriteString(w, `{"id":315,"name":"web","sourceMapUploadUrl":"`+closed.URL+`/u/315/sourcemaps"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"project 99 not found"}}`)
		}
	}))
	t.Cleanup(srv.Close)

	dir := scratchEnv(t)
	e.cfg = configFor(t, dir, srv.URL)
	e.noServer = write(t, dir, "no-server.yaml", `version: 1
current-context: test
contexts:
  test:
    stack: test
stacks:
  test:
    slug: test
`)
	cloudEnv(t, srv.URL)
	e.host = strings.TrimPrefix(up.URL, "http://")
	return e
}

func (e *upsertEnv) calls() []string {
	var out []string
	for _, s := range e.seen {
		if s != "grafana GET /bootdata" {
			out = append(out, s)
		}
	}
	return out
}

func (e *upsertEnv) runCases(t *testing.T, cases []upsertCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(false)
			t.Chdir(t.TempDir())
			write(t, ".", "main.js.map", okMap)
			for name, content := range tc.write {
				require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o700))
				write(t, ".", name, content)
			}
			if tc.setup != nil {
				tc.setup(t)
			}
			project, release := cmp.Or(tc.project, "314"), cmp.Or(tc.release, " v1 a/b ")
			files := tc.files
			if files == nil {
				files = []string{"main.js.map"}
			}
			args := []string{"--config", cmp.Or(tc.cfg, e.cfg), "sourcemaps", "upsert", "--project", project, "--release", release}
			for _, f := range files {
				args = append(args, "-f", f)
			}
			if tc.format != "none" {
				args = append(args, "-o", cmp.Or(tc.format, "json"))
			}
			e.got, e.body, e.seen = nil, nil, nil
			e.stderr.Reset()
			out, err := runUpsert(t, &e.stderr, args...)
			if tc.exit == 0 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, tc.exit, exitCode(err), "%v", err)
			}
			if tc.err != "" {
				assert.Contains(t, rendered(t, err), tc.err)
			}
			assert.Equal(t, tc.calls, e.calls(), "what reaches Grafana besides /bootdata, and the upload host")
			if tc.exit == gcxerrors.ExitUsageError {
				assert.Empty(t, e.seen, "a usage error sends nothing at all")
			}
			if tc.check != nil {
				tc.check(t, out, err)
			}
		})
	}
}

func usageCase(name string, files []string, write map[string]string, want string) upsertCase {
	return upsertCase{name: name, files: files, write: write, exit: gcxerrors.ExitUsageError, err: want}
}

func TestUpsertInput(t *testing.T) {
	e := newUpsertEnv(t)
	e.runCases(t, []upsertCase{
		{name: "an empty release fails before any request", release: " ", exit: gcxerrors.ExitUsageError, err: "--release must not be empty"},
		{name: "a project ID of 0", project: "0", exit: gcxerrors.ExitUsageError, err: "--project must be a positive project ID"},
		usageCase("an empty -f", []string{" "}, nil, "-f must not be empty"),
		usageCase("a file of another type", []string{"main.js"}, map[string]string{"main.js": "x"},
			"-f main.js: the file must end in .map or be a directory"),
		usageCase("an empty file", []string{"empty.js.map"}, map[string]string{"empty.js.map": ""}, "-f empty.js.map: not a file, or empty"),
		usageCase("a directory without maps", []string{"dir.js.map"}, map[string]string{"dir.js.map/notes.txt": "hi"},
			"-f dir.js.map: no .map files in the directory"),
		{name: "a file over 256 MiB", files: []string{"big.js.map"}, setup: func(t *testing.T) {
			t.Helper()
			sparse(t, "big.js.map", 256<<20+1)
		}, exit: gcxerrors.ExitUsageError, err: "-f big.js.map: 268435457 bytes is over the 256 MiB limit"},
		{name: "maps that add up to over 256 MiB", files: []string{"main.js.map", "big.js.map"}, setup: func(t *testing.T) {
			t.Helper()
			sparse(t, "big.js.map", 256<<20)
		}, exit: gcxerrors.ExitUsageError, err: "-f: the maps add up to over the 256 MiB limit at big.js.map"},
		usageCase("a map that is not JSON", []string{"bad.js.map"}, map[string]string{"bad.js.map": "//# not json"},
			"-f bad.js.map: not a source map (not JSON)"),
		usageCase("a map without mappings", []string{"nomap.js.map"}, map[string]string{"nomap.js.map": `{"version":3}`},
			`-f nomap.js.map: not a source map (no "mappings")`),
		usageCase("a map whose mappings are null", []string{"nullmap.js.map"}, map[string]string{"nullmap.js.map": `{"version":3,"mappings":null}`},
			`-f nullmap.js.map: not a source map (no "mappings")`),
		usageCase("a map whose version is null", []string{"nullver.js.map"}, map[string]string{"nullver.js.map": `{"version":null,"mappings":"AAAA"}`},
			`-f nullver.js.map: not a source map (no "version")`),
		usageCase("a map without a version", []string{"nover.js.map"}, map[string]string{"nover.js.map": `{"mappings":"AAAA"}`},
			`-f nover.js.map: not a source map (no "version")`),
		usageCase("a map that is a JSON array", []string{"list.js.map"}, map[string]string{"list.js.map": `[1]`},
			"-f list.js.map: not a source map (not a JSON object)"),
		usageCase("one bad map in a directory stops the whole upload", []string{"mixed"},
			map[string]string{"mixed/a.js.map": okMap, "mixed/b.js.map": "nope"},
			"-f "+filepath.FromSlash("mixed/b.js.map")+": not a source map (not JSON)"),
		usageCase("two maps with the same name", []string{"one", "two"},
			map[string]string{"one/app.js.map": okMap, "two/app.js.map": okMap},
			"-f: "+filepath.FromSlash("one/app.js.map")+" and "+filepath.FromSlash("two/app.js.map")+" are both named app.js.map"),
		{name: "a directory whose only map is a broken link", files: []string{"dist"}, write: map[string]string{"dist/notes.txt": "hi"}, setup: func(t *testing.T) {
			t.Helper()
			symlink(t, "nowhere.js.map", "dist/gone.js.map")
		}, exit: gcxerrors.ExitUsageError, err: "-f dist: no .map files in the directory"},
		{name: "a dangling link named with -f", files: []string{"gone.js.map"}, setup: func(t *testing.T) {
			t.Helper()
			symlink(t, "nowhere.js.map", "gone.js.map")
		}, exit: gcxerrors.ExitUsageError, err: "gone.js.map"},
		usageCase("only AppleDouble files", []string{"._main.js.map"}, map[string]string{"._main.js.map": "x"},
			"no source maps to upload: every -f input was skipped"),
		{name: "a missing file is a usage error before any request", files: []string{"missing.js.map"}, exit: gcxerrors.ExitUsageError,
			check: func(t *testing.T, _ string, err error) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "-f missing.js.map: the file does not exist")
				assert.Empty(t, e.seen)
			}},
	})
}

const (
	appMap    = `{"version":3,"sources":["app.js"],"mappings":";;AAAA"}`
	vendorMap = `{"version":3,"sources":["vendor.js"],"mappings":"A"}`
)

func nativeList(key string, paths ...string) string {
	native := make([]string, len(paths))
	for i, p := range paths {
		native[i] = filepath.FromSlash(p)
	}
	b, err := json.Marshal(native)
	if err != nil {
		panic(err)
	}
	return `,"` + key + `":` + string(b)
}

func (e *upsertEnv) mutation(files, extra string) string {
	return `{"type":"gcx.mutation","schema_version":"1","action":"uploaded","changed":true,` +
		`"target":{"kind":"SourceMap","namespace":"stacks-123"},"project":{"id":314,"name":"Checkout API"},"release":"v1 a/b",` +
		`"host":"` + e.host + `","files":` + files + extra + `}`
}

func entry(file, body string) string {
	return `{"file":"` + file + `","bundle":"` + strings.TrimSuffix(file, ".map") + `","size":` + strconv.Itoa(len(body)) + `}`
}

func (e *upsertEnv) asJSON(t *testing.T, name string) {
	t.Helper()
	assert.Equal(t, "/u/314/sourcemaps/v1%20a%2Fb", e.got.URL.EscapedPath())
	assert.Equal(t, name, e.got.URL.Query().Get("file"))
	assert.Equal(t, "application/json", e.got.Header.Get("Content-Type"))
}

func (e *upsertEnv) asArchive(t *testing.T) {
	t.Helper()
	assert.Equal(t, "/u/314/sourcemaps/v1%20a%2Fb", e.got.URL.EscapedPath())
	assert.Empty(t, e.got.URL.RawQuery)
	assert.Equal(t, "application/gzip", e.got.Header.Get("Content-Type"))
}

func TestUpsert(t *testing.T) {
	e := newUpsertEnv(t)
	one := `[` + entry("main.js.map", okMap) + `]`
	e.runCases(t, []upsertCase{
		{name: "one map", calls: sent(), check: func(t *testing.T, out string, _ error) {
			t.Helper()
			e.asJSON(t, "main.js.map")
			assert.Equal(t, "Bearer 123:tok", e.got.Header.Get("Authorization"))
			assert.JSONEq(t, okMap, string(e.body))
			assert.Equal(t, int64(len(okMap)), e.got.ContentLength)
			assert.Equal(t, "grafana GET /bootdata", e.seen[0])
			assert.JSONEq(t, e.mutation(one, ""), out)
			assert.Empty(t, e.stderr.String())
		}},
		{name: "text says what went where", format: "text", calls: sent(), check: func(t *testing.T, out string, _ error) {
			t.Helper()
			e.asJSON(t, "main.js.map")
			assert.Contains(t, out, "✔ Uploaded 1 source map for release v1 a/b to project Checkout API (ID 314)\n")
			for _, s := range []string{"FILE", "BUNDLE", "SIZE", "main.js.map", "main.js", strconv.Itoa(len(okMap)) + " B"} {
				assert.Contains(t, out, s)
			}
			assert.NotContains(t, out, "skipped")
			assert.Empty(t, e.stderr.String())
		}},
		{name: "several files go up as one archive", files: []string{"a/app.js.map", "vendor.js.map"}, calls: sent(),
			write: map[string]string{"a/app.js.map": appMap, "vendor.js.map": vendorMap},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asArchive(t)
				assert.Equal(t, []tarEntry{{"app.js.map", appMap}, {"vendor.js.map", vendorMap}}, untgz(t, e.body))
				assert.JSONEq(t, e.mutation(`[`+entry("app.js.map", appMap)+`,`+entry("vendor.js.map", vendorMap)+`]`, ""), out)
			}},
		{name: "the same file twice is sent once", files: []string{"main.js.map", "./main.js.map"}, calls: sent(),
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asJSON(t, "main.js.map")
				assert.JSONEq(t, okMap, string(e.body))
				assert.JSONEq(t, e.mutation(one, ""), out)
			}},
		{name: "an AppleDouble file named explicitly is skipped", files: []string{"main.js.map", "._main.js.map"}, calls: sent(),
			write: map[string]string{"._main.js.map": "x"},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asJSON(t, "main.js.map")
				assert.JSONEq(t, e.mutation(one, `,"skipped":["._main.js.map"]`), out)
			}},
		{name: "a project by name", project: "Checkout API", calls: []string{stackRoute, listProject, readProject, uploadRoute},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asJSON(t, "main.js.map")
				assert.JSONEq(t, e.mutation(one, ""), out)
			}},
		{name: "yaml", format: "yaml", calls: sent(), check: func(t *testing.T, out string, _ error) {
			t.Helper()
			j, yerr := yaml.YAMLToJSON([]byte(out))
			require.NoError(t, yerr)
			assert.JSONEq(t, e.mutation(one, ""), string(j))
		}},
		{name: "agents", format: "none", setup: agentMode, calls: sent(), check: func(t *testing.T, out string, _ error) {
			t.Helper()
			assert.JSONEq(t, e.mutation(one, ""), out)
			assert.Empty(t, e.stderr.String())
		}},
	})
}

func TestUpsertDirectories(t *testing.T) {
	e := newUpsertEnv(t)
	e.runCases(t, []upsertCase{
		{name: "a directory sends its maps as one archive and skips the rest", files: []string{"dist"}, calls: sent(),
			write: map[string]string{"dist/app.js.map": appMap, "dist/._app.js.map": "x", "dist/notes.txt": "hi", "dist/vendor/vendor.js.map": vendorMap},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asArchive(t)
				assert.Equal(t, []tarEntry{{"app.js.map", appMap}, {"vendor.js.map", vendorMap}}, untgz(t, e.body))
				assert.JSONEq(t, e.mutation(`[`+entry("app.js.map", appMap)+`,`+entry("vendor.js.map", vendorMap)+`]`,
					nativeList("skipped", "dist/._app.js.map", "dist/notes.txt")), out)
			}},
		{name: "a directory in text warns about what was skipped", files: []string{"web"}, format: "text", calls: sent(),
			write: map[string]string{"web/app.js.map": appMap, "web/._app.js.map": "x", "web/notes.txt": "hi", "web/vendor.js.map": vendorMap},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asArchive(t)
				assert.Contains(t, out, "✔ Uploaded 2 source maps for release v1 a/b to project Checkout API (ID 314)\n")
				assert.Contains(t, out, "vendor.js.map")
				assert.NotContains(t, out, "skipped")
				assert.Contains(t, e.stderr.String(), "⚠ skipped 2 files that are not source maps: "+filepath.FromSlash("web/._app.js.map")+", "+filepath.FromSlash("web/notes.txt")+"\n")
			}},
		{name: "links in a directory are skipped, not followed", files: []string{"dist"}, calls: sent(),
			write: map[string]string{"dist/app.js.map": appMap, "linked.js.map": vendorMap, "vendor/vendor.js.map": vendorMap},
			setup: func(t *testing.T) {
				t.Helper()
				symlink(t, "../linked.js.map", "dist/linked.js.map")
				symlink(t, "../vendor", "dist/vendor")
			},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asJSON(t, "app.js.map")
				assert.JSONEq(t, appMap, string(e.body))
				assert.JSONEq(t, e.mutation(`[`+entry("app.js.map", appMap)+`]`, nativeList("skippedLinks", "dist/linked.js.map", "dist/vendor")), out)
			}},
		{name: "links are reported as links in text, not as files that are not maps", files: []string{"dist"}, format: "text", calls: sent(),
			write: map[string]string{"dist/app.js.map": appMap, "dist/notes.txt": "hi", "linked.js.map": vendorMap},
			setup: func(t *testing.T) {
				t.Helper()
				symlink(t, "../linked.js.map", "dist/linked.js.map")
			},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				assert.NotContains(t, out, "skipped")
				assert.Contains(t, e.stderr.String(), "⚠ skipped 1 file that is not a source map: "+filepath.FromSlash("dist/notes.txt")+"\n")
				assert.Contains(t, e.stderr.String(), "⚠ skipped 1 link, since links are not followed: "+filepath.FromSlash("dist/linked.js.map")+"\n")
			}},
		{name: "a broken link in a directory is skipped", files: []string{"dist"}, calls: sent(),
			write: map[string]string{"dist/app.js.map": appMap},
			setup: func(t *testing.T) {
				t.Helper()
				symlink(t, "nowhere.js.map", "dist/gone.js.map")
			},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asJSON(t, "app.js.map")
				assert.JSONEq(t, appMap, string(e.body))
				assert.JSONEq(t, e.mutation(`[`+entry("app.js.map", appMap)+`]`, nativeList("skippedLinks", "dist/gone.js.map")), out)
			}},
		{name: "a link to a directory named with -f walks that directory", files: []string{"link"}, calls: sent(),
			write: map[string]string{"dist/app.js.map": appMap, "dist/vendor/vendor.js.map": vendorMap},
			setup: func(t *testing.T) {
				t.Helper()
				symlink(t, "dist", "link")
			},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asArchive(t)
				assert.Equal(t, []tarEntry{{"app.js.map", appMap}, {"vendor.js.map", vendorMap}}, untgz(t, e.body))
				assert.JSONEq(t, e.mutation(`[`+entry("app.js.map", appMap)+`,`+entry("vendor.js.map", vendorMap)+`]`, ""), out)
			}},
		{name: "a directory and a directory inside it send each map once", files: []string{"dist", "dist/assets"}, calls: sent(),
			write: map[string]string{"dist/app.js.map": appMap, "dist/assets/vendor.js.map": vendorMap},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asArchive(t)
				assert.Equal(t, []tarEntry{{"app.js.map", appMap}, {"vendor.js.map", vendorMap}}, untgz(t, e.body))
				assert.JSONEq(t, e.mutation(`[`+entry("app.js.map", appMap)+`,`+entry("vendor.js.map", vendorMap)+`]`, ""), out)
			}},
		{name: "a directory after one inside it sends each map once", files: []string{"dist/assets", "dist"}, calls: sent(),
			write: map[string]string{"dist/app.js.map": appMap, "dist/assets/vendor.js.map": vendorMap},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asArchive(t)
				assert.ElementsMatch(t, []tarEntry{{"app.js.map", appMap}, {"vendor.js.map", vendorMap}}, untgz(t, e.body))
				assert.JSONEq(t, `["app.js.map","vendor.js.map"]`, fileNames(t, out))
			}},
		{name: "a link named with -f is followed", files: []string{"link.js.map"}, calls: sent(),
			write: map[string]string{"dist/app.js.map": appMap},
			setup: func(t *testing.T) {
				t.Helper()
				symlink(t, "dist/app.js.map", "link.js.map")
			},
			check: func(t *testing.T, out string, _ error) {
				t.Helper()
				e.asJSON(t, "link.js.map")
				assert.JSONEq(t, appMap, string(e.body))
				assert.JSONEq(t, e.mutation(`[`+entry("link.js.map", appMap)+`]`, ""), out)
			}},
	})
}

func TestUpsertRefused(t *testing.T) {
	e := newUpsertEnv(t)
	upload := func(file string) map[string]string { return map[string]string{file: okMap} }
	e.runCases(t, []upsertCase{
		{name: "without a Cloud token nothing is sent", setup: func(t *testing.T) {
			t.Helper()
			t.Setenv("GRAFANA_CLOUD_TOKEN", "")
			os.Unsetenv("GRAFANA_CLOUD_TOKEN")
		}, exit: gcxerrors.ExitGeneralError, err: "context has no cloud auth: run `gcx cloud login`, or set GRAFANA_CLOUD_TOKEN"},
		{name: "a plain-http Cloud API URL is named, not taken for a missing token", setup: func(t *testing.T) {
			t.Helper()
			t.Setenv("GRAFANA_CLOUD_API_URL", "http://gcom.example.com")
		}, exit: gcxerrors.ExitGeneralError, err: "base URL must use HTTPS",
			check: func(t *testing.T, _ string, err error) {
				t.Helper()
				assert.NotContains(t, rendered(t, err), "no cloud auth")
			}},
		{name: "a refused upload", files: []string{"denied.js.map"}, write: upload("denied.js.map"), calls: sent(),
			exit: gcxerrors.ExitAuthFailure, err: "The upload host refused the source maps for stack 123 (HTTP 403): The token does not cover stack 123.",
			check: func(t *testing.T, _ string, err error) {
				t.Helper()
				out := rendered(t, err)
				assert.Contains(t, out, gcxerrors.SummaryAuthorizationFailed)
				assert.Contains(t, out, "Use a Cloud token with the sourcemaps:write scope for stack 123")
			}},
		{name: "an expired token names the stack and scope", files: []string{"expired.js.map"}, write: upload("expired.js.map"), calls: sent(),
			exit: gcxerrors.ExitAuthFailure, err: "The upload host refused the source maps for stack 123 (HTTP 401): token expired",
			check: func(t *testing.T, _ string, err error) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "Use a Cloud token with the sourcemaps:write scope for stack 123")
			}},
		{name: "an upload 404", files: []string{"gone.js.map"}, write: upload("gone.js.map"), calls: sent(),
			exit: gcxerrors.ExitGeneralError, err: gcxerrors.SummaryResourceNotFound},
		{name: "an upload 503 is not an unavailable endpoint", files: []string{"busy.js.map"}, write: upload("busy.js.map"), calls: sent(),
			exit: gcxerrors.ExitGeneralError, err: "auth service did not answer; try again",
			check: func(t *testing.T, _ string, err error) {
				t.Helper()
				assert.NotContains(t, rendered(t, err), gcxerrors.SummaryEndpointNotAvailable)
			}},
		{name: "a redirect is not followed", files: []string{"moved.js.map"}, write: upload("moved.js.map"), calls: sent(),
			exit: gcxerrors.ExitGeneralError, err: `the upload address redirects to "/else\u202ewhere" (HTTP 302)`},
		{name: "a missing project uploads nothing", project: "99", calls: []string{stackRoute, "grafana GET " + etPrefix + "projects/99"},
			exit: gcxerrors.ExitGeneralError, err: gcxerrors.SummaryResourceNotFound},
		{name: "a project name that matches nothing", project: "nope", calls: []string{stackRoute, listProject},
			exit: gcxerrors.ExitGeneralError, err: `no Error Tracking project has the ID or name "nope"`},
		{name: "an unreachable upload host", project: "315", calls: []string{stackRoute, "grafana GET " + etPrefix + "projects/315"},
			exit: gcxerrors.ExitGeneralError, err: "error tracking: upload source maps:"},
		{name: "no Grafana server to read the project from", cfg: e.noServer,
			exit: gcxerrors.ExitGeneralError, err: "uploading source maps needs the context's Grafana server to read the project",
			check: func(t *testing.T, _ string, err error) {
				t.Helper()
				assert.Contains(t, rendered(t, err), "gcx config set stacks.<name>.grafana.server <url>")
				assert.Empty(t, e.seen)
			}},
		{name: "a 200 from the upload host is a success", files: []string{"ok200.js.map"}, write: upload("ok200.js.map"), calls: sent()},
		{name: "and so is a 204", files: []string{"ok204.js.map"}, write: upload("ok204.js.map"), calls: sent()},
		{name: "a JSON error drops control characters", files: []string{"json.js.map"}, write: upload("json.js.map"), calls: sent(),
			exit: gcxerrors.ExitGeneralError, check: func(t *testing.T, _ string, err error) {
				t.Helper()
				assertPrintable(t, err, "bad]0;x mapend")
			}},
		{name: "a text error drops control characters", files: []string{"text.js.map"}, write: upload("text.js.map"), calls: sent(),
			exit: gcxerrors.ExitGeneralError, check: func(t *testing.T, _ string, err error) {
				t.Helper()
				assertPrintable(t, err, "bad[31m map")
			}},
	})
}

func fileNames(t *testing.T, out string) string {
	t.Helper()
	var m struct {
		Files []struct {
			File string `json:"file"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &m))
	names := make([]string, len(m.Files))
	for i, f := range m.Files {
		names[i] = f.File
	}
	slices.Sort(names)
	b, err := json.Marshal(names)
	require.NoError(t, err)
	return string(b)
}

func assertPrintable(t *testing.T, err error, want string) {
	t.Helper()
	printed := rendered(t, err)
	assert.NotContains(t, printed, "\x1b")
	assert.False(t, strings.ContainsAny(printed, "\a\u202e\u2028\u2029\u2066"), "%q", printed)
	assert.Contains(t, printed, want)
}

func TestUpsertUploadURL(t *testing.T) {
	agent.SetFlag(false)
	var uploads, grafana []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploads = append(uploads, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusCreated)
	}))
	defer up.Close()
	field := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			_, _ = io.WriteString(w, `{"settings":{"namespace":"stacks-123"}}`)
			return
		}
		grafana = append(grafana, r.Method+" "+r.URL.Path)
		_, _ = io.WriteString(w, `{"id":314,"name":"web"`+field+`}`)
	}))
	defer srv.Close()

	dir := scratchEnv(t)
	file := write(t, dir, "main.js.map", okMap)
	cfg := configFor(t, dir, srv.URL)
	cloudEnv(t, srv.URL)

	const notEnabled = gcxerrors.SummaryEndpointNotAvailable + "\nError Tracking isn't enabled for this stack, so source maps cannot be uploaded.\nAsk your Grafana Labs account team to enable it."
	const refused = "the project's upload address is not an https URL without a query, fragment or user info (http only for localhost)"
	for _, tc := range []struct {
		field, want string
		exit        int
		uploads     []string
	}{
		{field: `,"sourceMapUploadUrl":"` + up.URL + `"`, uploads: []string{"POST /v1"}},
		{field: `,"sourceMapUploadUrl":"` + up.URL + `/"`, uploads: []string{"POST /v1"}},
		{field: `,"sourceMapUploadUrl":"` + strings.Replace(up.URL, "127.0.0.1", "localhost", 1) + `"`, uploads: []string{"POST /v1"}},
		{field: "", exit: gcxerrors.ExitGeneralError, want: notEnabled},
		{field: `,"sourceMapUploadUrl":""`, exit: gcxerrors.ExitGeneralError, want: notEnabled},
		{field: `,"sourceMapUploadUrl":" "`, exit: gcxerrors.ExitGeneralError, want: notEnabled},
		{field: `,"sourceMapUploadUrl":"` + strings.Replace(up.URL, "127.0.0.1", "upload.localhost", 1) + `"`, exit: gcxerrors.ExitGeneralError, want: refused},
		{field: `,"sourceMapUploadUrl":"http://upload.example.com"`, exit: gcxerrors.ExitGeneralError, want: refused},
		{field: `,"sourceMapUploadUrl":"ftp://upload.example.com"`, exit: gcxerrors.ExitGeneralError, want: refused},
		{field: `,"sourceMapUploadUrl":"https://someone@upload.example.com"`, exit: gcxerrors.ExitGeneralError, want: refused},
		{field: `,"sourceMapUploadUrl":"https://upload.example.com?x=1"`, exit: gcxerrors.ExitGeneralError, want: refused},
		{field: `,"sourceMapUploadUrl":"https://upload.example.com?"`, exit: gcxerrors.ExitGeneralError, want: refused},
		{field: `,"sourceMapUploadUrl":"https://upload.example.com#frag"`, exit: gcxerrors.ExitGeneralError, want: refused},
		{field: `,"sourceMapUploadUrl":"https://"`, exit: gcxerrors.ExitGeneralError, want: refused},
		{field: `,"sourceMapUploadUrl":"https://files.other.test/secret"`, exit: gcxerrors.ExitGeneralError,
			want: "the project's upload address is on files.other.test, outside the domain of the Grafana server 127.0.0.1, so the Cloud token is not sent"},
		{field: `,"sourceMapUploadUrl":"https://127.0.0.1.other.test"`, exit: gcxerrors.ExitGeneralError, want: "is on 127.0.0.1.other.test, outside the domain of the Grafana server 127.0.0.1"},
		{field: `,"sourceMapUploadUrl":"https://ev\u202eil.test"`, exit: gcxerrors.ExitGeneralError, want: "is on evil.test, outside the domain of the Grafana server 127.0.0.1"},
	} {
		t.Run(cmp.Or(tc.field, "no field"), func(t *testing.T) {
			field = tc.field
			uploads, grafana = nil, nil
			_, err := runUpsert(t, io.Discard, "--config", cfg, "sourcemaps", "upsert", "--project", "314", "--release", "v1", "-f", file)
			assert.Equal(t, []string{"GET /api/instances/test", "GET " + etPrefix + "projects/314"}, grafana, "the stack is looked up and the project is read")
			assert.Equal(t, tc.uploads, uploads, "what reaches the upload host")
			if tc.exit == 0 {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tc.exit, exitCode(err), "%v", err)
			out := rendered(t, err)
			for line := range strings.SplitSeq(tc.want, "\n") {
				assert.Contains(t, out, line)
			}
			assert.NotContains(t, out, "upload.example.com")
			assert.NotContains(t, out, "secret")
			assertNoEscapes(t, "the error", out)
		})
	}
}

func TestUpsertCloudAuth(t *testing.T) {
	const known = `{"settings":{"namespace":"stacks-123"}}`
	const project = "GET /apis/error-tracking.ext.grafana.app/v0alpha1/namespaces/stacks-0/projects/314"
	for _, tc := range []struct {
		name, bootdata, token, stack, want, stderr, summary string
		noSlug, agentMode                                   bool
		exit, stackStatus                                   int
		grafana, cloud, upload                              []string
	}{
		{name: "a known stack ID is still looked up for the token", bootdata: known, token: "tok", cloud: []string{"GET /api/instances/test"},
			grafana: []string{"GET " + etPrefix + "projects/314"}, upload: []string{"POST /v1 Bearer 123:tok"}},
		{name: "an unknown stack ID is looked up", token: "tok", cloud: []string{"GET /api/instances/test"},
			grafana: []string{project}, upload: []string{"POST /v1 Bearer 123:tok"}},
		{name: "a looked-up stack at the Grafana server's host is used", token: "tok", stack: `{"id":456,"slug":"test","url":"{grafana}"}`,
			cloud: []string{"GET /api/instances/test"}, grafana: []string{project}, upload: []string{"POST /v1 Bearer 456:tok"}},
		{name: "a looked-up stack at another host, as with a custom domain, is named on stderr and used", token: "tok",
			stack: `{"id":456,"slug":"test","url":"https://other.example.com"}`,
			cloud: []string{"GET /api/instances/test"}, grafana: []string{project}, upload: []string{"POST /v1 Bearer 456:tok"},
			stderr: "uploading for the Cloud stack test (ID 456) at other.example.com, which differs from the Grafana server 127.0.0.1"},
		{name: "a stack URL with control characters is not compared, and the warning is clean", token: "tok",
			stack: `{"id":456,"slug":"te\u001b[2Jst\u202e","url":"https://other\u001b]0;x\u0007.example.com"}`,
			cloud: []string{"GET /api/instances/test"}, grafana: []string{project}, upload: []string{"POST /v1 Bearer 456:tok"},
			stderr: "uploading for the Cloud stack te[2Jst (ID 456), whose URL could not be read to compare with the Grafana server 127.0.0.1"},
		{name: "a stack URL that cannot be parsed is named and the upload goes ahead", token: "tok",
			stack: `{"id":456,"slug":"test","url":"https://[bad"}`,
			cloud: []string{"GET /api/instances/test"}, grafana: []string{project}, upload: []string{"POST /v1 Bearer 456:tok"},
			stderr: "uploading for the Cloud stack test (ID 456), whose URL could not be read to compare with the Grafana server 127.0.0.1"},
		{name: "a hostile stack slug on another host is cleaned in the warning", token: "tok",
			stack: `{"id":456,"slug":"te\u001b[2Jst\u009b","url":"https://other.example.com"}`,
			cloud: []string{"GET /api/instances/test"}, grafana: []string{project}, upload: []string{"POST /v1 Bearer 456:tok"},
			stderr: "uploading for the Cloud stack te[2Jst (ID 456) at other.example.com"},
		{name: "in agent mode the host warning is a JSON line", token: "tok", agentMode: true,
			stack: `{"id":456,"slug":"test","url":"https://other.example.com"}`,
			cloud: []string{"GET /api/instances/test"}, grafana: []string{project}, upload: []string{"POST /v1 Bearer 456:tok"},
			stderr: `{"class":"warning","summary":"uploading for the Cloud stack test (ID 456) at other.example.com, which differs from the Grafana server 127.0.0.1"}`},
		{name: "a known stack ID without a token sends nothing", bootdata: known, exit: gcxerrors.ExitGeneralError,
			want: "context has no cloud auth: run `gcx cloud login`, or set GRAFANA_CLOUD_TOKEN"},
		{name: "an unknown stack ID without a token sends nothing", exit: gcxerrors.ExitGeneralError,
			want: "context has no cloud auth: run `gcx cloud login`, or set GRAFANA_CLOUD_TOKEN"},
		{name: "a stack the Cloud API has no ID for is refused before the upload", token: "tok", stack: `{"id":0,"slug":"test"}`,
			cloud: []string{"GET /api/instances/test"}, exit: gcxerrors.ExitGeneralError,
			want: "the stack ID is unknown, so the upload cannot be authorized", summary: gcxerrors.SummaryInvalidConfiguration},
		{name: "a stack lookup refused with control characters in its body is printed clean", token: "tok", stackStatus: http.StatusBadRequest,
			stack: "bad \u001b]0;owned\u0007\u001b[2Jstack\u202e\u009b", cloud: []string{"GET /api/instances/test"}, exit: gcxerrors.ExitGeneralError,
			want: `failed to get stack info for "test": gcom client: unexpected status 400 Bad Request: bad ]0;owned[2Jstack`},
		{name: "a stack lookup refused with a JSON body is printed without live control characters", token: "tok", stackStatus: http.StatusBadRequest,
			stack: `{"message":"bad \u001b]0;owned\u0007\u001b[2J\u0000\u009b\u202estack"}`, cloud: []string{"GET /api/instances/test"}, exit: gcxerrors.ExitGeneralError,
			want: `failed to get stack info for "test": gcom client: unexpected status 400 Bad Request: {"message":"bad \u001b]0;owned`},
		{name: "a stack lookup refused with 401 and a hostile message is printed clean", token: "tok", stackStatus: http.StatusUnauthorized,
			stack: `{"message":"bad \u001b]0;owned\u0007\u001b[2J\u0000\u009b\u202etoken"}`, cloud: []string{"GET /api/instances/test"}, exit: gcxerrors.ExitAuthFailure, summary: gcxerrors.SummaryAuthenticationFailed,
			want: `failed to get stack info for "test": gcom client: unexpected status 401 Unauthorized: {"message":"bad \u001b]0;owned`},
		{name: "a stack lookup refused with 403 exits as an authentication failure", token: "tok", stackStatus: http.StatusForbidden,
			stack: `{"message":"forbidden"}`, cloud: []string{"GET /api/instances/test"}, exit: gcxerrors.ExitAuthFailure, summary: gcxerrors.SummaryAuthenticationFailed,
			want: `failed to get stack info for "test": gcom client: unexpected status 403 Forbidden`},
		{name: "an unknown stack ID without a stack slug is refused", token: "tok", noSlug: true, exit: gcxerrors.ExitGeneralError,
			want: "gcx config set stacks.<name>.slug"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(false)
			var uploads, cloud, grafana []string
			var grafanaURL string
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				uploads = append(uploads, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusCreated)
			}))
			defer up.Close()
			gcom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cloud = append(cloud, r.Method+" "+r.URL.Path)
				if tc.stackStatus != 0 {
					w.WriteHeader(tc.stackStatus)
				}
				if tc.stack != "" {
					_, _ = io.WriteString(w, strings.ReplaceAll(tc.stack, "{grafana}", grafanaURL))
					return
				}
				if !serveStack(w, r) {
					http.NotFound(w, r)
				}
			}))
			defer gcom.Close()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path != "/bootdata":
					grafana = append(grafana, r.Method+" "+r.URL.Path)
					_, _ = io.WriteString(w, `{"id":314,"name":"web","sourceMapUploadUrl":"`+up.URL+`"}`)
				case tc.bootdata == "":
					http.NotFound(w, r)
				default:
					_, _ = io.WriteString(w, tc.bootdata)
				}
			}))
			defer srv.Close()
			grafanaURL = srv.URL

			dir := scratchEnv(t)
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })
			file := write(t, dir, "main.js.map", okMap)
			cfg := configFor(t, dir, srv.URL)
			t.Setenv("GRAFANA_CLOUD_API_URL", gcom.URL)
			if !tc.noSlug {
				t.Setenv("GRAFANA_CLOUD_STACK", "test")
			}
			if tc.token != "" {
				t.Setenv("GRAFANA_CLOUD_TOKEN", tc.token)
			}

			var stderr bytes.Buffer
			_, err := runUpsert(t, &stderr, "--config", cfg, "sourcemaps", "upsert", "--project", "314", "--release", "v1", "-f", file)

			assertNoEscapes(t, "stderr", stderr.String())
			if tc.stderr != "" {
				assert.Equal(t, 1, strings.Count(stderr.String(), tc.stderr), stderr.String())
			} else {
				assert.NotContains(t, stderr.String(), "differs from the Grafana server")
			}
			assert.Equal(t, tc.grafana, grafana, "what reaches Grafana besides /bootdata")
			assert.Equal(t, tc.cloud, cloud, "what reaches the Cloud API")
			assert.Equal(t, tc.upload, uploads, "what reaches the upload host")
			if tc.exit == 0 {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Equal(t, tc.exit, exitCode(err), "%v", err)
			out := rendered(t, err)
			assertNoEscapes(t, "the error", out)
			assert.Contains(t, out, tc.summary)
			assert.Contains(t, out, tc.want)
		})
	}
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestSourcemapsList(t *testing.T) {
	agent.SetFlag(false)
	f := newFakeET(t)
	const route = "GET projects/314/sourcemaps"
	const two = `{"sourcemaps":[{"release":"v1.2.3","file":"main.js.map","uploadedAt":"2026-11-17T10:00:00Z"},{"release":"v1.2.2","file":"main.js.map","uploadedAt":"2026-11-16T10:00:00Z"}]}`
	f.reply(route, 200, two)
	none := func(f *fakeET) { f.reply(route, 200, `{"sourcemaps":[]}`) }
	noStderr := func(t *testing.T, _ string, _ error, _ []etCall) {
		t.Helper()
		assert.Empty(t, f.stderr.String())
	}
	f.runCases(t, []cliCase{
		{name: "every map", args: []string{"sourcemaps", "list", "--project", "314", "-o", "json"}, calls: []string{route}, query: map[string][]string{},
			json:  `{"items":[{"release":"v1.2.3","file":"main.js.map","uploadedAt":"2026-11-17T10:00:00Z"},{"release":"v1.2.2","file":"main.js.map","uploadedAt":"2026-11-16T10:00:00Z"}]}`,
			check: noStderr},
		{name: "a table", args: []string{"sourcemaps", "list", "--project", "314", "-o", "table"}, calls: []string{route},
			has: []string{"RELEASE", "FILE", "v1.2.3", "v1.2.2", "main.js.map"}},
		{name: "one release", args: []string{"sourcemaps", "list", "--project", "314", "--release", "v1.2.2", "-o", "json"}, calls: []string{route}, query: map[string][]string{},
			json: `{"items":[{"release":"v1.2.2","file":"main.js.map","uploadedAt":"2026-11-16T10:00:00Z"}]}`},
		{name: "--release filters before --limit", args: []string{"sourcemaps", "list", "--project", "314", "--release", "v1.2.2", "--limit", "1", "-o", "json"},
			calls: []string{route}, json: `{"items":[{"release":"v1.2.2","file":"main.js.map","uploadedAt":"2026-11-16T10:00:00Z"}]}`},
		{name: "a release without maps says how to upload them", args: []string{"sourcemaps", "list", "--project", "314", "--release", "v9", "-o", "json"},
			calls: []string{route}, json: `{"items":[]}`,
			stderr: "hint: No source maps for release v9 in project 314; upload them: gcx error-tracking sourcemaps upsert --project 314 --release v9 -f <dir>"},
		{name: "a release with a space is quoted in the hint", args: []string{"sourcemaps", "list", "--project", "314", "--release", "v9 beta", "-o", "json"},
			calls: []string{route}, json: `{"items":[]}`,
			stderr: "upload them: " + shellWithDir("gcx", "error-tracking", "sourcemaps", "upsert", "--project", "314", "--release", "v9 beta", "-f") + "\n",
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.Contains(t, f.stderr.String(), "'v9 beta'")
				assert.NotContains(t, f.stderr.String(), "& 'v9 beta'")
				assert.NotContains(t, f.stderr.String(), "'<dir>'", "a placeholder stays bare")
			}},
		{name: "an empty release", args: []string{"sourcemaps", "list", "--project", "314", "--release", " "}, exit: gcxerrors.ExitUsageError},
		{name: "a list cut off at --limit says so", args: []string{"sourcemaps", "list", "--project", "314", "--limit", "1", "--jq", ".list_meta | [.truncated, .returned, .total]", "-o", "json"},
			calls: []string{route}, json: `[true,1,2]`},
		{name: "a list at the API's 1,000 cap says so", setup: func(f *fakeET) {
			f.reply(route, 200, `{"sourcemaps":[`+strings.Repeat(`{"release":"v1","file":"a.js.map"},`, 999)+`{"release":"v1","file":"a.js.map"}]}`)
		}, args: []string{"sourcemaps", "list", "--project", "314", "--limit", "0", "--jq", ".list_meta", "-o", "json"},
			calls: []string{route}, json: `{"truncated":true,"returned":1000,"cap":1000}`},
		{name: "a release with no map among the API's 1,000 names the cap, not a --limit cut", setup: func(f *fakeET) {
			f.reply(route, 200, `{"sourcemaps":[`+strings.Repeat(`{"release":"v1","file":"a.js.map"},`, 999)+`{"release":"v1","file":"a.js.map"}]}`)
		}, args: []string{"sourcemaps", "list", "--project", "314", "--release", "v9"}, calls: []string{route},
			stderr: "hint: No source maps for release v9 among the newest 1,000 the API returns in project 314; upload them: gcx error-tracking sourcemaps upsert --project 314 --release v9 -f <dir>",
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "showing first")
				assert.Equal(t, 1, strings.Count(f.stderr.String(), "hint:"), f.stderr.String())
			}},
		{name: "a release found among the API's 1,000 says older maps are not listed", setup: func(f *fakeET) {
			f.reply(route, 200, `{"sourcemaps":[`+strings.Repeat(`{"release":"v1","file":"a.js.map"},`, 999)+`{"release":"v2","file":"b.js.map"}]}`)
		}, args: []string{"sourcemaps", "list", "--project", "314", "--release", "v2", "-o", "json"}, calls: []string{route},
			json:   `{"items":[{"release":"v2","file":"b.js.map","uploadedAt":""}],"list_meta":{"truncated":true,"returned":1,"cap":1000}}`,
			stderr: "hint: The API returns only the newest 1,000 source maps of project 314; older ones are not listed",
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				assert.NotContains(t, f.stderr.String(), "showing first")
			}},
		{name: "a negative limit", args: []string{"sourcemaps", "list", "--project", "314", "--limit", "-1"}, exit: gcxerrors.ExitUsageError,
			err: "invalid --limit -1: must be >= 0"},
		{name: "none says how to upload some", setup: none,
			args: []string{"sourcemaps", "list", "--project", "314", "-o", "json"}, calls: []string{route}, json: `{"items":[]}`,
			stderr: "hint: No source maps in project 314; upload some: gcx error-tracking sourcemaps upsert --project 314 --release <release> -f <dir>"},
		{name: "none in JSON discovery prints no hint", setup: none,
			args: []string{"sourcemaps", "list", "--project", "314", "--json", "?"}, calls: []string{route}, check: noStderr},
		{name: "none in agent mode is a JSON hint", setup: func(f *fakeET) {
			none(f)
			agent.SetFlag(true)
		}, args: []string{"sourcemaps", "list", "--project", "314", "--release", "v9"}, calls: []string{route},
			json: `{"items":[]}`,
			check: func(t *testing.T, _ string, _ error, _ []etCall) {
				t.Helper()
				agent.SetFlag(false)
				assert.JSONEq(t, `{"class":"hint","summary":"No source maps for release v9 in project 314; upload them","command":"gcx error-tracking sourcemaps upsert --project 314 --release v9 -f <dir>"}`, f.stderr.String())
			}},
		{name: "a bad project", args: []string{"sourcemaps", "list", "--project", "0"}, exit: gcxerrors.ExitUsageError},
	})
}
