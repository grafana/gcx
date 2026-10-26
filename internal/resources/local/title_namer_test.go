package local_test

import (
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
				namer := local.GroupResourcesByKindTitle(ext, nil, []*resources.Resource{resource})
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
		{name: "reserved fallback", input: []*resources.Resource{titledResource("CON", nil)}, want: []string{"con-con"}},
		{name: "reserved dotted fallback", input: []*resources.Resource{titledResource("CON.backup", nil)}, want: []string{"con-backup-con-backup"}},
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
					namer := local.GroupResourcesByKindTitle(ext, plurals, input)
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
	namer := local.GroupResourcesByKindTitle("json", nil, []*resources.Resource{a, b})
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
	namer := local.GroupResourcesByKindTitle("json", nil, []*resources.Resource{resource})
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
				Namer: local.GroupResourcesByKindTitle("json", nil, input.AsList()),
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
