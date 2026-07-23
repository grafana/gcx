package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/require"
)

func TestLoginMutationGuardRejectsSelectedSourceChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("version: 1\ncontexts:\n  prod: {}\ncurrent-context: prod\n")
	require.NoError(t, os.WriteFile(path, original, 0o600))

	before, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)
	guard := before.NewLoginMutationGuard("prod", config.LoginMutationUnified)
	require.NoError(t, guard.Verify(&before))

	changed := []byte("version: 1\ncontexts:\n  attacker: {}\ncurrent-context: attacker\n")
	require.NoError(t, os.WriteFile(path, changed, 0o600))
	after, err := config.Load(t.Context(), config.ExplicitConfigFile(path))
	require.NoError(t, err)
	err = guard.Verify(&after)
	require.ErrorContains(t, err, "Configuration changed during authentication")
	require.ErrorContains(t, err, "freshly authenticated credential was not written")
}

func TestLoginMutationGuardRejectsNewlyDiscoveredLayer(t *testing.T) {
	home := t.TempDir()
	userDir := filepath.Join(home, ".config")
	workDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", userDir)
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv("GCX_CONFIG", "")
	t.Chdir(workDir)

	userPath := filepath.Join(userDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userPath), 0o755))
	require.NoError(t, os.WriteFile(userPath, []byte("version: 1\ncontexts:\n  prod: {}\ncurrent-context: prod\n"), 0o600))

	effective, err := config.LoadLayered(t.Context(), "")
	require.NoError(t, err)
	require.Len(t, effective.Sources, 1)
	persisted, err := config.Load(
		config.ContextWithConfigSource(t.Context(), effective.Sources[0]),
		config.ExplicitConfigFile(userPath),
	)
	require.NoError(t, err)
	guard, err := persisted.NewLoginMutationGuard("prod", config.LoginMutationUnified).WithDiscoverySnapshot(&effective)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(
		filepath.Join(workDir, config.LocalConfigFileName),
		[]byte("version: 1\ncontexts:\n  prod:\n    cloud: attacker\n"),
		0o600,
	))
	reloaded, err := config.Load(
		config.ContextWithConfigSource(t.Context(), effective.Sources[0]),
		config.ExplicitConfigFile(userPath),
	)
	require.NoError(t, err)
	err = guard.Verify(&reloaded)
	require.ErrorContains(t, err, "Configuration changed during authentication")
	require.ErrorContains(t, err, "source set changed")
}

func TestVerifyLoginMutationBindings(t *testing.T) {
	target := config.ConfigSource{Path: "/tmp/config.yaml", Type: "user"}
	effective := &config.Context{Stack: "prod", Cloud: "grafana-com"}

	require.NoError(t, config.VerifyLoginMutationBindings(
		target,
		"prod",
		effective,
		&config.Context{Stack: "prod", Cloud: "grafana-com"},
		config.LoginMutationUnified,
	))
	require.NoError(t, config.VerifyLoginMutationBindings(
		target,
		"prod",
		effective,
		&config.Context{Stack: "inherited", Cloud: "grafana-com"},
		config.LoginMutationCloud,
	))

	for name, persisted := range map[string]*config.Context{
		"missing context": nil,
		"stack changed":   {Stack: "staging", Cloud: "grafana-com"},
		"cloud changed":   {Stack: "prod", Cloud: "grafana-ops"},
	} {
		t.Run(name, func(t *testing.T) {
			err := config.VerifyLoginMutationBindings(target, "prod", effective, persisted, config.LoginMutationUnified)
			require.ErrorContains(t, err, "Configuration changed during login planning")
			require.ErrorContains(t, err, target.Path)
			require.ErrorContains(t, err, "No authentication request was sent")
		})
	}
}

func TestVerifyLoginMutationBindingsQuotesExactConfigPath(t *testing.T) {
	target := config.ConfigSource{Path: "/tmp/config owner/with spaces.yaml", Type: "user"}
	err := config.VerifyLoginMutationBindings(
		target,
		"prod",
		&config.Context{Cloud: "grafana-com"},
		&config.Context{Cloud: "grafana-ops"},
		config.LoginMutationCloud,
	)
	require.ErrorContains(t, err, `--config "/tmp/config owner/with spaces.yaml"`)
}
