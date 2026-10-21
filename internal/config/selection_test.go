package config_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const selectionConfig = `
stacks:
  s:
    grafana:
      server: http://localhost:3000
contexts:
  a:
    stack: s
  b:
    stack: s
current-context: a
`

const selectionConfigNoCurrent = `
stacks:
  s:
    grafana:
      server: http://localhost:3000
contexts:
  a:
    stack: s
`

// selectionTarget describes where the config under test lives: an explicit
// file (the --config path) or the layered user config discovered from XDG.
type selectionTarget struct {
	name string
	// setup writes content and returns the explicitFile argument for LoadLayered.
	setup func(t *testing.T, content string) string
}

func selectionTargets() []selectionTarget {
	return []selectionTarget{
		{name: "explicit file", setup: func(t *testing.T, content string) string {
			t.Helper()
			isolatedLoaderEnv(t)
			path := filepath.Join(t.TempDir(), "config.yaml")
			writeLoaderConfig(t, path, content)
			return path
		}},
		{name: "layered", setup: func(t *testing.T, content string) string {
			t.Helper()
			userDir, _ := isolatedLoaderEnv(t)
			writeLoaderConfig(t, filepath.Join(userDir, "gcx", "config.yaml"), content)
			return ""
		}},
	}
}

func scrubStrictEnv(t *testing.T) {
	t.Helper()
	scrubTargetKindEnv(t)
	t.Setenv("GCX_CONTEXT", "")
}

func TestLoadLayered_StrictRefusesInheritedContext(t *testing.T) {
	for _, target := range selectionTargets() {
		t.Run(target.name, func(t *testing.T) {
			scrubStrictEnv(t)
			t.Setenv("GCX_REQUIRE_CONTEXT", "true")
			file := target.setup(t, selectionConfig)

			_, err := config.LoadLayered(t.Context(), file, config.SelectContext(""))
			require.ErrorIs(t, err, config.ErrContextRequired)

			var reqErr *config.ContextRequiredError
			require.ErrorAs(t, err, &reqErr)
			assert.Equal(t, "a", reqErr.Inherited)
			assert.Equal(t, []string{"a", "b"}, reqErr.Available)
			assert.Contains(t, err.Error(), `current-context is "a"`)
		})
	}
}

func TestLoadLayered_StrictAcceptsExplicitContext(t *testing.T) {
	for _, target := range selectionTargets() {
		t.Run(target.name, func(t *testing.T) {
			scrubStrictEnv(t)
			t.Setenv("GCX_REQUIRE_CONTEXT", "true")
			file := target.setup(t, selectionConfig)

			cfg, err := config.LoadLayered(t.Context(), file, config.SelectContext("b"))
			require.NoError(t, err)
			assert.Equal(t, "b", cfg.CurrentContext)
		})
	}
}

func TestLoadLayered_StrictExplicitContextNotFound(t *testing.T) {
	scrubStrictEnv(t)
	t.Setenv("GCX_REQUIRE_CONTEXT", "true")
	file := selectionTargets()[0].setup(t, selectionConfig)

	_, err := config.LoadLayered(t.Context(), file, config.SelectContext("nope"))
	require.ErrorIs(t, err, config.ErrContextNotFound)
	var nf *config.ContextNotFoundError
	require.ErrorAs(t, err, &nf)
	assert.Equal(t, []string{"a", "b"}, nf.Available)
}

func TestLoadLayered_StrictAcceptsFileScope(t *testing.T) {
	for _, target := range selectionTargets() {
		t.Run(target.name, func(t *testing.T) {
			scrubStrictEnv(t)
			t.Setenv("GCX_REQUIRE_CONTEXT", "true")
			file := target.setup(t, selectionConfig)

			cfg, err := config.LoadLayered(t.Context(), file, config.FileScope)
			require.NoError(t, err)
			assert.Equal(t, "a", cfg.CurrentContext)
		})
	}
}

func TestLoadLayered_StrictRefusesUndeclaredLoad(t *testing.T) {
	for _, target := range selectionTargets() {
		t.Run(target.name, func(t *testing.T) {
			scrubStrictEnv(t)
			t.Setenv("GCX_REQUIRE_CONTEXT", "true")
			file := target.setup(t, selectionConfig)

			_, err := config.LoadLayered(t.Context(), file)
			require.ErrorIs(t, err, config.ErrContextSelectionUndeclared)
			assert.NotErrorIs(t, err, config.ErrContextRequired)
		})
	}
}

func TestLoadLayered_StrictAllowsEmptyCurrentContext(t *testing.T) {
	for _, target := range selectionTargets() {
		t.Run(target.name, func(t *testing.T) {
			scrubStrictEnv(t)
			t.Setenv("GCX_REQUIRE_CONTEXT", "true")
			file := target.setup(t, selectionConfigNoCurrent)

			_, err := config.LoadLayered(t.Context(), file, config.SelectContext(""))
			require.NoError(t, err)
		})
	}
}

func TestLoadLayered_NonStrictUnchanged(t *testing.T) {
	for _, target := range selectionTargets() {
		t.Run(target.name, func(t *testing.T) {
			scrubStrictEnv(t)
			t.Setenv("GCX_REQUIRE_CONTEXT", "")
			file := target.setup(t, selectionConfig)

			cfg, err := config.LoadLayered(t.Context(), file, config.SelectContext(""))
			require.NoError(t, err)
			assert.Equal(t, "a", cfg.CurrentContext)

			cfg, err = config.LoadLayered(t.Context(), file, config.SelectContext("b"))
			require.NoError(t, err)
			assert.Equal(t, "b", cfg.CurrentContext)

			_, err = config.LoadLayered(t.Context(), file, config.FileScope)
			require.NoError(t, err)

			_, err = config.LoadLayered(t.Context(), file)
			require.NoError(t, err)
		})
	}
}

func TestLoadLayered_StrictRefusalCapturesNoTargetKind(t *testing.T) {
	scrubStrictEnv(t)
	t.Setenv("GCX_REQUIRE_CONTEXT", "true")
	file := selectionTargets()[0].setup(t, selectionConfig)

	config.ClearCapturedTargetKind()
	_, err := config.LoadLayered(t.Context(), file, config.SelectContext(""))
	require.ErrorIs(t, err, config.ErrContextRequired)
	assert.Empty(t, config.CapturedTargetKind())

	// Control: an accepted load against the same file does classify the target.
	_, err = config.LoadLayered(t.Context(), file, config.SelectContext("a"))
	require.NoError(t, err)
	assert.Equal(t, "self-hosted", config.CapturedTargetKind())
}

func TestLoadForWrite_StrictNotEnforced(t *testing.T) {
	scrubStrictEnv(t)
	t.Setenv("GCX_REQUIRE_CONTEXT", "true")
	userDir, _ := isolatedLoaderEnv(t)
	path := filepath.Join(userDir, "gcx", "config.yaml")
	writeLoaderConfig(t, path, selectionConfig)

	cfg, _, err := config.LoadForWrite(t.Context(), path, "")
	require.NoError(t, err)
	assert.Equal(t, "a", cfg.CurrentContext)
}

func TestResolveContextName(t *testing.T) {
	withCurrent := config.Config{CurrentContext: "a"}

	tests := []struct {
		name     string
		strict   string
		override string
		cfg      config.Config
		want     string
		wantErr  bool
	}{
		{name: "override wins", override: "x", cfg: withCurrent, want: "x"},
		{name: "override wins in strict", strict: "true", override: "x", cfg: withCurrent, want: "x"},
		{name: "current context, non-strict", cfg: withCurrent, want: "a"},
		{name: "inherited current context refused in strict", strict: "true", cfg: withCurrent, wantErr: true},
		{name: "default without current context, non-strict", cfg: config.Config{}, want: config.DefaultContextName},
		{name: "default without current context, strict", strict: "true", cfg: config.Config{}, want: config.DefaultContextName},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GCX_REQUIRE_CONTEXT", tc.strict)

			got, err := config.ResolveContextName(tc.override, tc.cfg)
			if tc.wantErr {
				require.ErrorIs(t, err, config.ErrContextRequired)
				var reqErr *config.ContextRequiredError
				require.ErrorAs(t, err, &reqErr)
				assert.Equal(t, "a", reqErr.Inherited)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLoadLayered_StrictRefusalWinsOverLaterOverrideError(t *testing.T) {
	boom := errors.New("later override failed")
	failing := func(*config.Config) error { return boom }

	tests := []struct {
		name      string
		strict    string
		overrides []config.Override
		wantErr   error
		wantKind  bool
	}{
		{"strict inherited then failure", "true", []config.Override{config.SelectContext(""), failing}, config.ErrContextRequired, false},
		{"strict explicit then failure", "true", []config.Override{config.SelectContext("b"), failing}, boom, true},
		{"non-strict inherited then failure", "", []config.Override{config.SelectContext(""), failing}, boom, true},
		{"strict failure before selection", "true", []config.Override{failing, config.SelectContext("")}, boom, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scrubStrictEnv(t)
			t.Setenv("GCX_REQUIRE_CONTEXT", tc.strict)
			file := selectionTargets()[0].setup(t, selectionConfig)

			config.ClearCapturedTargetKind()
			_, err := config.LoadLayered(t.Context(), file, tc.overrides...)
			require.ErrorIs(t, err, tc.wantErr)
			require.NotErrorIs(t, err, config.ErrContextSelectionUndeclared)
			if tc.wantKind {
				assert.NotEmpty(t, config.CapturedTargetKind())
			} else {
				assert.Empty(t, config.CapturedTargetKind())
			}
		})
	}
}
