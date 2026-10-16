package agento11y_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/agento11y"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgento11yProvider_Interface(t *testing.T) {
	p := &agento11y.Agento11yProvider{}

	assert.Equal(t, "agento11y", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	assert.NoError(t, p.Validate(nil))
	assert.NoError(t, p.Validate(map[string]string{}))
	assert.Nil(t, p.ConfigKeys())
}

func TestAgento11yProvider_Commands(t *testing.T) {
	p := &agento11y.Agento11yProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	agento11yCmd := cmds[0]
	assert.Equal(t, "agento11y", agento11yCmd.Use)
	assert.Empty(t, agento11yCmd.Aliases)

	subNames := commandNames(agento11yCmd)
	for _, exp := range []string{"conversations", "agents", "evaluators", "rules", "guards"} {
		assert.Contains(t, subNames, exp)
	}

	convsCmd := findSubcommand(agento11yCmd, "conversations")
	require.NotNil(t, convsCmd)

	convSubNames := commandNames(convsCmd)
	for _, exp := range []string{"list", "get", "search"} {
		assert.Contains(t, convSubNames, exp)
	}
}

func TestAgento11yProvider_GroupCompatibility(t *testing.T) {
	regs := (&agento11y.Agento11yProvider{}).TypedRegistrations()
	require.Len(t, regs, 4)
	index := discovery.NewRegistryIndex()
	for _, reg := range regs {
		index.RegisterStatic(reg.Descriptor, reg.Aliases)
	}
	require.Len(t, index.GetDescriptors(), 4)
	require.Len(t, index.GetPreferredVersions(), 4)
	assert.ElementsMatch(t, []string{"evaluators", "evalrules", "hookrules", "collections"}, []string{
		regs[0].Descriptor.Plural, regs[1].Descriptor.Plural, regs[2].Descriptor.Plural, regs[3].Descriptor.Plural,
	})
	for _, reg := range regs {
		t.Run(reg.Descriptor.Plural, func(t *testing.T) {
			desc := reg.Descriptor
			require.Equal(t, "agento11y.ext.grafana.app", desc.GroupVersion.Group)
			legacy := desc.GroupVersionKind()
			legacy.Group = "sigil.ext.grafana.app"
			assert.Equal(t, desc.GroupVersionKind(), resources.NormalizeGVK(legacy))
			for _, tt := range []struct {
				name, group, version, kind string
			}{
				{"unknown version", legacy.Group, "v99", legacy.Kind},
				{"unknown kind", legacy.Group, legacy.Version, "Unrelated"},
				{"unknown group", "unrelated.ext.grafana.app", legacy.Version, legacy.Kind},
			} {
				t.Run(tt.name, func(t *testing.T) {
					bad := legacy
					bad.Group, bad.Version, bad.Kind = tt.group, tt.version, tt.kind
					assert.Equal(t, bad, resources.NormalizeGVK(bad))
					assert.False(t, desc.Matches(bad))
				})
			}
			for _, tt := range []struct {
				name, suffix string
				want         bool
			}{
				{"unqualified", "", true},
				{"new short group", ".agento11y", true},
				{"legacy short group", ".sigil", true},
				{"new full group", ".v1alpha1.agento11y.ext.grafana.app", true},
				{"legacy full group", ".v1alpha1.sigil.ext.grafana.app", true},
				{"legacy unsupported version", ".v99.sigil.ext.grafana.app", false},
				{"new unsupported version", ".v99.agento11y.ext.grafana.app", false},
				{"unrelated selector group", ".unrelated", false},
			} {
				t.Run(tt.name, func(t *testing.T) {
					var sel resources.Selector
					require.NoError(t, sel.ParseString(desc.Plural+tt.suffix+"/example"))
					for _, lookup := range []func(resources.PartialGVK) (resources.Descriptors, bool){index.LookupPreferredPerGroup, index.LookupAllVersionsForPartialGVK} {
						descs, ok := lookup(sel.GroupVersionKind)
						require.Equal(t, tt.want, ok)
						if tt.want {
							require.Equal(t, resources.Descriptors{desc}, descs)
						}
					}
					resolved, ok := index.LookupPartialGVK(sel.GroupVersionKind)
					require.Equal(t, tt.want, ok)
					if !tt.want {
						return
					}
					filter := resources.Filter{Type: sel.Type, Descriptor: resolved, ResourceUIDs: sel.ResourceUIDs}
					for _, group := range []string{legacy.Group, desc.GroupVersion.Group} {
						obj := map[string]any{"apiVersion": group + "/v1alpha1", "kind": desc.Kind, "metadata": map[string]any{"name": "example"}, "spec": map[string]any{}}
						data, err := json.Marshal(obj)
						require.NoError(t, err)
						path := filepath.Join(t.TempDir(), "manifest.json")
						require.NoError(t, os.WriteFile(path, data, 0600))
						reader := local.FSReader{Decoders: format.Codecs(), StopOnError: true}
						items := resources.NewResources()
						require.NoError(t, reader.Read(t.Context(), items, resources.Filters{filter}, []string{path}))
						require.Equal(t, 1, items.Len())
						filter.ResourceUIDs = []string{"other"}
						assert.False(t, filter.Matches(*resources.MustFromObject(obj, resources.SourceInfo{})))
						filter.ResourceUIDs = sel.ResourceUIDs
					}
				})
			}
		})
	}
}

func commandNames(cmd *cobra.Command) []string {
	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	return names
}

func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}
