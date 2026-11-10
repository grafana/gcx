package local_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/stretchr/testify/require"
)

func titledResource(name string, title any) *resources.Resource {
	spec := map[string]any{}
	if title != nil {
		spec["title"] = title
	}
	return resources.MustFromObject(map[string]any{
		"apiVersion": "dashboard.grafana.app/v1",
		"kind":       "Dashboard",
		"metadata":   map[string]any{"name": name},
		"spec":       spec,
	}, resources.SourceInfo{})
}

func TestGroupResourcesByKindTitle_Sanitization(t *testing.T) {
	for _, tt := range []struct {
		name  string
		title any
		want  string
	}{
		{name: "title", title: "CPU Overview", want: "cpu-overview"},
		{name: "unsafe characters", title: "  A/B\\C:D*E?F\"G<H>I|J\x00\n ", want: "a-b-c-d-e-f-g-h-i-j"},
		{name: "hyphens", title: "--CPU--- Overview--", want: "cpu-overview"},
		{name: "path traversal sanitized", title: "../../etc", want: "etc"},
		{name: "missing title", want: "uid"},
		{name: "empty title", title: "", want: "uid"},
		{name: "whitespace title", title: " \t\n", want: "uid"},
		{name: "punctuation title", title: "...?!", want: "uid"},
		{name: "non-string title", title: 42, want: "uid"},
		{name: "CJK title", title: "\u58f2\u4e0a", want: "uid"},
		{name: "Spanish title", title: "Página Ventas", want: "p-gina-ventas"},
		{name: "literal resource title", title: "Resource!", want: "resource"},
		{name: "long title", title: strings.Repeat("A", 150), want: strings.Repeat("a", 100)},
		{name: "truncated hyphen", title: strings.Repeat("a", 99) + " b", want: strings.Repeat("a", 99)},
		{name: "reserved CON", title: "CON", want: "con-uid"},
		{name: "reserved PRN", title: "PRN", want: "prn-uid"},
		{name: "reserved AUX", title: "AUX", want: "aux-uid"},
		{name: "reserved NUL", title: "NUL", want: "nul-uid"},
		{name: "reserved COM1", title: "COM1", want: "com1-uid"},
		{name: "reserved COM9", title: "COM9", want: "com9-uid"},
		{name: "reserved LPT1", title: "LPT1", want: "lpt1-uid"},
		{name: "reserved LPT9", title: "LPT9", want: "lpt9-uid"},
		{name: "COM10 allowed", title: "COM10", want: "com10"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resource := titledResource("uid", tt.title)
			for _, ext := range []string{"json", "yaml"} {
				namer := titleNamer(t, ext, nil, []*resources.Resource{resource})
				path, err := namer(resource)
				require.NoError(t, err)
				require.Equal(t, filepath.Join("dashboards.v1.dashboard.grafana.app", tt.want+"."+ext), path)
				defaultPath, err := local.GroupResourcesByKind(ext, nil)(resource)
				require.NoError(t, err)
				require.Equal(t, filepath.Join("dashboards.v1.dashboard.grafana.app", "uid."+ext), defaultPath)
			}
		})
	}
}

func TestGroupResourcesByKindTitle_CollisionsRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input []*resources.Resource
		want  []string
	}{
		{name: "same title", input: []*resources.Resource{titledResource("a", "CPU"), titledResource("b", "CPU")}, want: []string{"cpu-a", "cpu-b"}},
		{name: "case collision", input: []*resources.Resource{titledResource("a", "CPU"), titledResource("b", "cpu")}, want: []string{"cpu-a", "cpu-b"}},
		{name: "sanitization collision", input: []*resources.Resource{titledResource("a", "A/B"), titledResource("b", "A:B")}, want: []string{"a-b-a", "a-b-b"}},
		{name: "fallback collision", input: []*resources.Resource{titledResource("a", "CPU"), titledResource("cpu", nil)}, want: []string{"cpu-a", "cpu-cpu"}},
		{name: "secondary collision", input: []*resources.Resource{titledResource("a", "CPU"), titledResource("b", "CPU"), titledResource("c", "CPU-a"), titledResource("d", "CPU-a-1")}, want: []string{"cpu-a-2", "cpu-b", "cpu-a-3", "cpu-a-1"}},
		{name: "reserved collision", input: []*resources.Resource{titledResource("a", "CON"), titledResource("b", "CON-a")}, want: []string{"con-a-1", "con-a-2"}},
		{name: "reserved fallback", input: []*resources.Resource{titledResource("CON", nil)}, want: []string{"CON"}},
		{name: "reserved dotted fallback", input: []*resources.Resource{titledResource("CON.backup", nil)}, want: []string{"CON.backup"}},
		{name: "truncation collision", input: []*resources.Resource{titledResource("a", strings.Repeat("a", 101)), titledResource("b", strings.Repeat("a", 102))}, want: []string{strings.Repeat("a", 100) + "-a", strings.Repeat("a", 100) + "-b"}},
		{name: "name suffix truncation", input: []*resources.Resource{titledResource(strings.Repeat("a", 150)+"b", "CPU"), titledResource(strings.Repeat("a", 150)+"c", "CPU")}, want: []string{"cpu-" + strings.Repeat("a", 100) + "-1", "cpu-" + strings.Repeat("a", 100) + "-2"}},
		{name: "case-only names", input: []*resources.Resource{titledResource("A", "CPU"), titledResource("a", "CPU")}, want: []string{"cpu-a-1", "cpu-a-2"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, ext := range []string{"json", "yaml"} {
				for _, reverse := range []bool{false, true} {
					input := slices.Clone(tt.input)
					if reverse {
						slices.Reverse(input)
					}
					plurals := map[string]string{input[0].GroupVersionKind().String(): "discovered-dashboards"}
					namer := titleNamer(t, ext, plurals, input)
					codecs := format.Codecs()
					writer := local.FSWriter{Path: t.TempDir(), Encoder: codecs[format.Format(ext)], Namer: namer, StopOnError: true}
					require.NoError(t, writer.Write(t.Context(), resources.NewResources(input...)))
					reader := local.FSReader{Decoders: codecs, StopOnError: true}
					read := resources.NewResources()
					require.NoError(t, reader.Read(t.Context(), read, nil, []string{writer.Path}))
					require.Equal(t, len(input), read.Len(), "no resource may be overwritten")
					for i, resource := range tt.input {
						path, err := namer(resource)
						require.NoError(t, err)
						require.Equal(t, filepath.Join("discovered-dashboards.v1.dashboard.grafana.app", tt.want[i]+"."+ext), path)
						found, ok := read.Find(resource.Kind(), resource.Name())
						require.True(t, ok)
						require.Equal(t, resource.Object.Object, found.Object.Object)
						require.Equal(t, filepath.Join(writer.Path, path), found.SourcePath())
					}
				}
			}
		})
	}
}

func TestGroupResourcesByKindTitle_SeparateDirectories(t *testing.T) {
	a, b := titledResource("a", "CPU"), titledResource("b", "CPU")
	b.Object.SetAPIVersion("dashboard.grafana.app/v2beta1")
	namer := titleNamer(t, "json", nil, []*resources.Resource{a, b})
	for _, resource := range []*resources.Resource{a, b} {
		path, err := namer(resource)
		require.NoError(t, err)
		require.Equal(t, "cpu.json", filepath.Base(path))
	}
	unknown := titledResource("unknown", "Ignored")
	path, err := namer(unknown)
	require.NoError(t, err)
	require.Equal(t, "unknown.json", filepath.Base(path))
}

func TestGroupResourcesByKindTitle_MissingName(t *testing.T) {
	resource := titledResource("", "Title")
	namer := titleNamer(t, "json", nil, []*resources.Resource{resource})
	_, err := namer(resource)
	require.EqualError(t, err, "resource has no name")
}

func TestFSWriter_TitleNamesPreserveWriteErrors(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(map[bool]string{false: "continue", true: "abort"}[abort], func(t *testing.T) {
			input := resources.NewResources(titledResource("", "Missing name"), titledResource("valid", "Sales"))
			failures := 0
			writer := local.FSWriter{
				Path: t.TempDir(), Encoder: format.NewJSONCodec(), StopOnError: abort,
				Namer: titleNamer(t, "json", nil, input.AsList()),
				OnWriteError: func(_ *resources.Resource, _ error) {
					failures++
				},
			}
			err := writer.Write(t.Context(), input)
			path := filepath.Join(writer.Path, "dashboards.v1.dashboard.grafana.app", "sales.json")
			if abort {
				require.ErrorContains(t, err, "resource has no name")
				require.NoFileExists(t, path)
				require.Zero(t, failures)
			} else {
				require.NoError(t, err)
				require.FileExists(t, path)
				require.Equal(t, 1, failures)
			}
		})
	}
}

func titleNamer(t *testing.T, extension string, plurals map[string]string, all []*resources.Resource) local.FileNamer {
	t.Helper()
	namer, err := local.GroupResourcesByKindTitle(t.TempDir(), extension, plurals, all, nil)
	require.NoError(t, err)
	return namer
}

func writeManifest(t *testing.T, root, path string, resource *resources.Resource) []byte {
	t.Helper()
	var data bytes.Buffer
	codec := format.Codecs()[format.Format(strings.TrimPrefix(filepath.Ext(path), "."))]
	require.NoError(t, codec.Encode(&data, &resource.Object))
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, path), data.Bytes(), 0o600))
	return data.Bytes()
}

func TestTitleNames_SuccessivePulls(t *testing.T) {
	for _, ext := range []string{"json", "yaml"} {
		t.Run(ext, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "backup")
			pull := func(input ...*resources.Resource) {
				t.Helper()
				namer, err := local.GroupResourcesByKindTitle(root, ext, nil, input, nil)
				require.NoError(t, err)
				writer := local.FSWriter{Path: root, Namer: namer, Encoder: format.Codecs()[format.Format(ext)], StopOnError: true}
				require.NoError(t, writer.Write(t.Context(), resources.NewResources(input...)))
			}
			pull(titledResource("aaa", "CPU"))
			originalPath := filepath.Join(root, "dashboards.v1.dashboard.grafana.app", "cpu."+ext)
			original, err := os.ReadFile(originalPath)
			require.NoError(t, err)
			pull(titledResource("bbb", "CPU"))
			pull(titledResource("bbb", "CPU")) // Repeat the narrower pull without growing the backup.
			unchanged, err := os.ReadFile(originalPath)
			require.NoError(t, err)
			require.Equal(t, original, unchanged)
			reader := local.FSReader{Decoders: format.Codecs(), StopOnError: true}
			read := resources.NewResources()
			require.NoError(t, reader.Read(t.Context(), read, nil, []string{root}))
			require.Equal(t, 2, read.Len())
			entries, err := os.ReadDir(filepath.Dir(originalPath))
			require.NoError(t, err)
			require.Len(t, entries, 2)
			require.FileExists(t, filepath.Join(filepath.Dir(originalPath), "cpu-bbb."+ext))
		})
	}
}

func TestTitleNames_ExistingIdentity(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*resources.Resource)
		want   string
	}{
		{name: "same identity", change: func(r *resources.Resource) { r.Object.Object["spec"] = map[string]any{"title": "old content"} }, want: "cpu"},
		{name: "different name", change: func(r *resources.Resource) { r.Object.SetName("other") }, want: "cpu-uid"},
		{name: "different namespace", change: func(r *resources.Resource) { r.Object.SetNamespace("other") }, want: "cpu-uid"},
		{name: "different kind", change: func(r *resources.Resource) { r.Object.SetKind("Folder") }, want: "cpu-uid"},
		{name: "different apiVersion", change: func(r *resources.Resource) { r.Object.SetAPIVersion("dashboard.grafana.app/v2beta1") }, want: "cpu-uid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dir := "dashboards.v1.dashboard.grafana.app"
			old := titledResource("uid", "CPU")
			tt.change(old)
			original := writeManifest(t, root, filepath.Join(dir, "cpu.json"), old)
			incoming := titledResource("uid", "CPU")
			namer, err := local.GroupResourcesByKindTitle(root, "json", nil, []*resources.Resource{incoming}, nil)
			require.NoError(t, err)
			path, err := namer(incoming)
			require.NoError(t, err)
			require.Equal(t, filepath.Join(dir, tt.want+".json"), path)
			writer := local.FSWriter{Path: root, Namer: namer, Encoder: format.NewJSONCodec(), StopOnError: true}
			require.NoError(t, writer.Write(t.Context(), resources.NewResources(incoming)))
			current, err := os.ReadFile(filepath.Join(root, dir, "cpu.json"))
			require.NoError(t, err)
			if tt.want == "cpu" {
				require.NotEqual(t, original, current, "same identity must refresh its content")
			} else {
				require.Equal(t, original, current, "different identity must keep its content")
			}
		})
	}
}

func TestTitleNames_ExistingSecondaryCollisions(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "forward", true: "reversed"}[reverse], func(t *testing.T) {
			root := t.TempDir()
			dir := "dashboards.v1.dashboard.grafana.app"
			for _, stem := range []string{"CPU", "cpu-b", "cpu-b-1"} {
				writeManifest(t, root, filepath.Join(dir, stem+".json"), titledResource("existing-"+stem, "CPU"))
			}
			// Reserve undecodable files too, so they cannot be silently truncated.
			require.NoError(t, os.WriteFile(filepath.Join(root, dir, "cpu-b-2.json"), []byte("invalid json"), 0o600))
			input := []*resources.Resource{titledResource("b", "CPU"), titledResource("c", "CPU-b-3")}
			if reverse {
				slices.Reverse(input)
			}
			namer, err := local.GroupResourcesByKindTitle(root, "json", nil, input, nil)
			require.NoError(t, err)
			path, err := namer(titledResource("b", "CPU"))
			require.NoError(t, err)
			require.Equal(t, filepath.Join(dir, "cpu-b-4.json"), path)
		})
	}
}

func TestTitleNames_StaleWarnings(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
		warn bool
	}{
		{name: "rename", path: "old-title.json", warn: true},
		{name: "mode switch", path: "uid.json", warn: true},
		{name: "format switch", path: "cpu.yaml", warn: true},
		{name: "same path", path: "cpu.json"},
		{name: "same path different case", path: "CPU.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dir := "dashboards.v1.dashboard.grafana.app"
			resource := titledResource("uid", "CPU")
			writeManifest(t, root, filepath.Join(dir, tt.path), resource)
			var warnings bytes.Buffer
			namer, err := local.GroupResourcesByKindTitle(root, "json", nil, []*resources.Resource{resource}, &warnings)
			require.NoError(t, err)
			path, err := namer(resource)
			require.NoError(t, err)
			if tt.warn {
				require.Contains(t, warnings.String(), "stale file")
				require.Contains(t, warnings.String(), tt.path)
				require.Contains(t, warnings.String(), "cpu.json")
				require.Contains(t, warnings.String(), "remove stale files before pushing")
			} else {
				require.Empty(t, warnings.String())
				require.Equal(t, filepath.Join(dir, tt.path), path)
			}
		})
	}
}

func TestTitleNames_SymlinkedDirectories(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "root", true: "nested"}[nested], func(t *testing.T) {
			root, target := t.TempDir(), t.TempDir()
			dir := "dashboards.v1.dashboard.grafana.app"
			original := writeManifest(t, target, filepath.Join(dir, "cpu.json"), titledResource("aaa", "CPU"))
			if nested {
				require.NoError(t, os.Symlink(filepath.Join(target, dir), filepath.Join(root, dir)))
			} else {
				root = filepath.Join(root, "link")
				require.NoError(t, os.Symlink(target, root))
			}
			incoming := titledResource("bbb", "CPU")
			namer, err := local.GroupResourcesByKindTitle(root, "json", nil, []*resources.Resource{incoming}, nil)
			if nested {
				require.ErrorContains(t, err, "cannot inventory symlinked destination directory")
			} else {
				require.NoError(t, err)
				path, err := namer(incoming)
				require.NoError(t, err)
				require.Equal(t, filepath.Join(dir, "cpu-bbb.json"), path)
			}
			current, err := os.ReadFile(filepath.Join(target, dir, "cpu.json"))
			require.NoError(t, err)
			require.Equal(t, original, current)
		})
	}
}
