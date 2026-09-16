package skills_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/grafana/gcx/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestLoadCatalog(t *testing.T) {
	t.Parallel()
	source := fstest.MapFS{"alpha/SKILL.md": {Data: []byte("alpha")}}
	for _, tc := range []struct {
		name string
		data string
		err  string
	}{
		{name: "active", data: "skills: {alpha: {status: active}}"},
		{name: "deprecated with replacement", data: "skills: {alpha: {status: deprecated, replacement: old}, old: {status: retired}}"},
		{name: "retired with replacement", data: "skills: {alpha: {status: active}, old: {status: retired, replacement: alpha}}"},
		{name: "missing catalog entry", data: "skills: {}", err: "missing from the catalog"},
		{name: "missing content", data: "skills: {alpha: {status: active}, beta: {status: active}}", err: "requires a bundled SKILL.md"},
		{name: "retired content", data: "skills: {alpha: {status: retired}}", err: "must not have bundled content"},
		{name: "bad status", data: "skills: {alpha: {status: deleted}}", err: "invalid status"},
		{name: "missing status", data: "skills: {alpha: {}}", err: "invalid status"},
		{name: "unknown replacement", data: "skills: {alpha: {status: deprecated, replacement: unknown}}", err: "unknown replacement"},
		{name: "self replacement", data: "skills: {alpha: {status: deprecated, replacement: alpha}}", err: "replacement cycle"},
		{name: "replacement cycle", data: "skills: {alpha: {status: deprecated, replacement: old}, old: {status: retired, replacement: alpha}}", err: "replacement cycle"},
		{name: "invalid name", data: "skills: {alpha: {status: active}, '../old': {status: retired}}", err: "invalid skill name"},
		{name: "unknown field", data: "skills: {alpha: {status: active, typo: true}}", err: "field typo"},
		{name: "duplicate key", data: "skills: {alpha: {status: active}, alpha: {status: active}}", err: "already defined"},
		{name: "missing skills", data: "{}", err: "skills map"},
		{name: "multiple documents", data: "skills: {}\n---\nskills: {}", err: "exactly one"},
		{name: "invalid yaml", data: "[", err: "decode skills catalog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := skills.LoadCatalog(source, []byte(tc.data))
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBundledCatalog(t *testing.T) {
	t.Parallel()
	catalog, err := skills.LoadCatalog(claudeplugin.SkillsFS(), claudeplugin.SkillsCatalog())
	require.NoError(t, err)
	require.NotEmpty(t, catalog.Skills)
	// Each newly shipped directory needs metadata; retirement keeps metadata but
	// removes content. This validates the actual embedded release, not a fixture.
}

func TestReconcile(t *testing.T) {
	t.Parallel()
	source := fstest.MapFS{
		"alpha/SKILL.md": {Data: []byte("alpha description")},
		"beta/SKILL.md":  {Data: []byte("beta description")},
	}
	catalog := []byte(`skills:
  alpha: {status: active}
  beta: {status: deprecated, replacement: alpha}
  old: {status: retired, replacement: alpha}
`)
	for _, tc := range []struct {
		name       string
		local      []string
		incomplete bool
	}{
		{name: "missing root"},
		{name: "installed", local: []string{"alpha", "beta", "old", "external"}},
		{name: "partial installation", local: []string{"old"}, incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), ".agents")
			for _, name := range tc.local {
				dir := filepath.Join(root, "skills", name)
				require.NoError(t, os.MkdirAll(dir, 0o755))
				if !tc.incomplete {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("local"), 0o600))
				}
			}
			states, err := skills.Reconcile(source, catalog, root)
			require.NoError(t, err)
			byName := make(map[string]skills.SkillState)
			for _, state := range states {
				byName[state.Name] = state
			}
			require.Equal(t, skills.Active, byName["alpha"].Status)
			require.Equal(t, "alpha description", byName["alpha"].ShortDescription)
			require.Equal(t, skills.Deprecated, byName["beta"].Status)
			require.Equal(t, skills.Retired, byName["old"].Status)
			require.Equal(t, "alpha", byName["old"].Replacement)
			for _, name := range tc.local {
				require.True(t, byName[name].Present)
				require.Equal(t, !tc.incomplete, byName[name].Installed)
			}
			switch tc.name {
			case "installed":
				require.Equal(t, skills.Unmanaged, byName["external"].Status)
			case "missing root":
				require.Len(t, states, 3)
				require.False(t, byName["alpha"].Present)
				require.False(t, byName["old"].Installed)
				require.NoDirExists(t, root)
			}
		})
	}
}

func TestValidateSkillName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", " ", ".", "..", "../alpha", "a/b", `a\b`, " alpha", "alpha ", "a\x00b"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, skills.ValidateSkillName(name), "invalid skill name")
		})
	}
}
