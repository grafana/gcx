package local_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestFSReader_Read(t *testing.T) {
	testutils.SetAgentMode(t, false)
	desc := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "current.reader-test.app", Version: "v1"},
		Kind:         "Widget", Plural: "widgets", Singular: "widget",
	}
	resources.RegisterGroupAliases(desc.GroupVersionKind(), []string{"legacy.reader-test.app"})
	for _, tt := range []struct {
		name, group, version, kind string
		filtered, silent           bool
		wantWarning                bool
	}{
		{name: "current", group: desc.GroupVersion.Group, version: "v1", kind: "Widget"},
		{name: "legacy", group: "legacy.reader-test.app", version: "v1", kind: "Widget", wantWarning: true},
		{name: "legacy filtered out", group: "legacy.reader-test.app", version: "v1", kind: "Widget", filtered: true},
		{name: "legacy nil writer", group: "legacy.reader-test.app", version: "v1", kind: "Widget", silent: true},
		{name: "unknown version", group: "legacy.reader-test.app", version: "v99", kind: "Widget"},
		{name: "unknown kind", group: "legacy.reader-test.app", version: "v1", kind: "Other"},
		{name: "unknown group", group: "unknown.reader-test.app", version: "v1", kind: "Widget"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"one", "two"} {
				data := fmt.Sprintf(`{"apiVersion":%q,"kind":%q,"metadata":{"name":%q},"spec":{}}`, tt.group+"/"+tt.version, tt.kind, name)
				require.NoError(t, os.WriteFile(filepath.Join(dir, name+".json"), []byte(data), 0600))
			}
			var warnings bytes.Buffer
			reader := local.FSReader{Decoders: format.Codecs(), StopOnError: true, MaxConcurrentReads: 2}
			if !tt.silent {
				reader.Warn = &warnings
			}
			var filters resources.Filters
			if tt.filtered {
				filters = resources.Filters{{Type: resources.FilterTypeSingle, Descriptor: desc, ResourceUIDs: []string{"other"}}}
			}
			items := resources.NewResources()
			require.NoError(t, reader.Read(t.Context(), items, filters, []string{dir}))
			if tt.filtered {
				assert.Zero(t, items.Len())
			} else {
				assert.Equal(t, 2, items.Len())
			}
			if tt.wantWarning {
				assert.Equal(t, "warn: apiVersion \"legacy.reader-test.app/v1\" is deprecated; use \"current.reader-test.app/v1\" instead\n", warnings.String())
			} else {
				assert.Empty(t, warnings.String())
			}
		})
	}
}

func TestFSReader_ReadBytes_YAMLExponentFloats(t *testing.T) {
	manifest := "apiVersion: test.reader-test.app/v1\nkind: Widget\nmetadata:\n  name: w\nspec:\n" +
		"  v: 1e-07\n  big: 1e400\n  tagged: !!str 1e5\n  sha: '4385e12'\n  text: |-\n    1e5\n"

	check := func(t *testing.T, spec map[string]any) {
		t.Helper()
		assert.InDelta(t, 1e-07, spec["v"], 0)
		assert.Equal(t, "1e400", spec["big"], "out-of-range values stay strings")
		assert.Equal(t, "1e5", spec["tagged"], "explicit !!str tag is respected")
		assert.Equal(t, "4385e12", spec["sha"])
		assert.Equal(t, "1e5", spec["text"])
	}
	reader := local.FSReader{Decoders: format.Codecs()}

	t.Run("ReadBytes", func(t *testing.T) {
		items := resources.NewResources()
		require.NoError(t, reader.ReadBytes(t.Context(), items, []byte(manifest), "yaml"))
		require.Equal(t, 1, items.Len())
		spec, err := items.AsList()[0].Spec()
		require.NoError(t, err)
		specMap, ok := spec.(map[string]any)
		require.True(t, ok)
		check(t, specMap)
	})

	t.Run("ReadFile", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "w.yaml")
		require.NoError(t, os.WriteFile(path, []byte(manifest), 0600))
		res := &resources.Resource{}
		require.NoError(t, reader.ReadFile(t.Context(), res, path))
		spec, err := res.Spec()
		require.NoError(t, err)
		specMap, ok := spec.(map[string]any)
		require.True(t, ok)
		check(t, specMap)
	})
}
