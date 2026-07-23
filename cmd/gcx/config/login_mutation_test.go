package config_test

import (
	"os"
	"path/filepath"
	"testing"

	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanLoginMutationUsesEntryOwnerAcrossDatasourceOnlyOverlay(t *testing.T) {
	userDir, workDir := isolatedConfigEnv(t)
	userPath := writeUserConfig(t, userDir, `version: 1
stacks:
  prod:
    grafana:
      server: https://prod.example.invalid
cloud:
  grafana-com:
    api-url: https://grafana.com
    oauth-url: https://grafana.com
contexts:
  prod:
    stack: prod
    cloud: grafana-com
current-context: prod
`)
	writeLocalConfig(t, workDir, `version: 1
contexts:
  prod:
    datasources:
      prometheus: local-prom
`)

	opts := &configcmd.Options{}
	cfg, err := opts.LoadConfigTolerant(t.Context())
	require.NoError(t, err)

	target, err := opts.PlanLoginMutation(cfg, "prod", config.LoginMutationUnified)
	require.NoError(t, err)
	assert.Equal(t, userPath, target.Path)
	assert.Equal(t, "user", target.Type)
}

func TestPlanCloudLoginWithoutEntryUsesDeterministicStackOwner(t *testing.T) {
	userDir, workDir := isolatedConfigEnv(t)
	userPath := writeUserConfig(t, userDir, `version: 1
stacks:
  prod:
    grafana:
      server: https://prod.example.invalid
contexts:
  prod:
    stack: prod
current-context: prod
`)
	writeLocalConfig(t, workDir, `version: 1
contexts:
  prod:
    datasources:
      prometheus: local-prom
`)

	opts := &configcmd.Options{}
	cfg, err := opts.LoadConfigTolerant(t.Context())
	require.NoError(t, err)
	target, err := opts.PlanLoginMutation(cfg, "prod", config.LoginMutationCloud)
	require.NoError(t, err)
	assert.Equal(t, userPath, target.Path)
	assert.Equal(t, "user", target.Type)
}

func TestPlanLoginMutationCloudUsesCloudOwnerButUnifiedRejectsSplitOwners(t *testing.T) {
	userDir, workDir := isolatedConfigEnv(t)
	systemPath := filepath.Join(os.Getenv("XDG_CONFIG_DIRS"), "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(systemPath), 0o755))
	require.NoError(t, os.WriteFile(systemPath, []byte(`version: 1
stacks:
  prod:
    grafana:
      server: https://prod.example.invalid
contexts:
  prod:
    stack: prod
    cloud: grafana-com
current-context: prod
`), 0o600))
	userPath := writeUserConfig(t, userDir, `version: 1
cloud:
  grafana-com:
    api-url: https://grafana.com
    oauth-url: https://grafana.com
contexts:
  prod:
    cloud: grafana-com
`)
	writeLocalConfig(t, workDir, `version: 1
contexts:
  prod:
    datasources:
      prometheus: local-prom
`)

	cloudOpts := &configcmd.Options{}
	cfg, err := cloudOpts.LoadConfigTolerant(t.Context())
	require.NoError(t, err)
	target, err := cloudOpts.PlanLoginMutation(cfg, "prod", config.LoginMutationCloud)
	require.NoError(t, err)
	assert.Equal(t, userPath, target.Path)
	assert.Equal(t, "user", target.Type)

	unifiedOpts := &configcmd.Options{}
	_, err = unifiedOpts.PlanLoginMutation(cfg, "prod", config.LoginMutationUnified)
	require.Error(t, err)
	require.ErrorContains(t, err, "different files")
	require.ErrorContains(t, err, systemPath)
	require.ErrorContains(t, err, userPath)
}

func TestPlanLoginMutationRejectsHigherLayerShadowOfCloudBinding(t *testing.T) {
	userDir, workDir := isolatedConfigEnv(t)
	userPath := writeUserConfig(t, userDir, `version: 1
stacks:
  prod:
    grafana:
      server: https://prod.example.invalid
cloud:
  grafana-com:
    api-url: https://grafana.com
    oauth-url: https://grafana.com
contexts:
  prod:
    stack: prod
    cloud: grafana-com
current-context: prod
`)
	localPath := writeLocalConfig(t, workDir, `version: 1
contexts:
  prod:
    cloud: grafana-com
    datasources:
      prometheus: local-prom
`)

	for _, intent := range []config.LoginMutationIntent{config.LoginMutationUnified, config.LoginMutationCloud} {
		t.Run(string(intent), func(t *testing.T) {
			opts := &configcmd.Options{}
			cfg, err := opts.LoadConfigTolerant(t.Context())
			require.NoError(t, err)
			_, err = opts.PlanLoginMutation(cfg, "prod", intent)
			require.ErrorContains(t, err, "different files")
			require.ErrorContains(t, err, userPath)
			require.ErrorContains(t, err, localPath)
		})
	}
}

func TestPlanLoginMutationRejectsLocalSystemSplitAndListsEveryCandidate(t *testing.T) {
	_, workDir := isolatedConfigEnv(t)
	systemPath := filepath.Join(os.Getenv("XDG_CONFIG_DIRS"), "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(systemPath), 0o755))
	require.NoError(t, os.WriteFile(systemPath, []byte(`version: 1
stacks:
  prod:
    grafana:
      server: https://prod.example.invalid
contexts:
  prod:
    stack: prod
    cloud: grafana-com
current-context: prod
`), 0o600))
	localPath := writeLocalConfig(t, workDir, `version: 1
cloud:
  grafana-com:
    api-url: https://grafana.com
    oauth-url: https://grafana.com
contexts:
  prod:
    stack: prod
    cloud: grafana-com
`)

	opts := &configcmd.Options{}
	cfg, err := opts.LoadConfigTolerant(t.Context())
	require.NoError(t, err)
	_, err = opts.PlanLoginMutation(cfg, "prod", config.LoginMutationUnified)
	require.Error(t, err)
	require.ErrorContains(t, err, "different files")
	require.ErrorContains(t, err, systemPath)
	require.ErrorContains(t, err, localPath)
}

func TestPlanLoginMutationPreservesLayeredLocalOwnerProvenance(t *testing.T) {
	userDir, workDir := isolatedConfigEnv(t)
	writeUserConfig(t, userDir, `version: 1
diagnostics:
  telemetry: disabled
contexts:
  unrelated: {}
`)
	localPath := writeLocalConfig(t, workDir, `version: 1
stacks:
  prod:
    grafana:
      server: https://prod.example.invalid
contexts:
  prod:
    stack: prod
current-context: prod
`)

	opts := &configcmd.Options{}
	cfg, err := opts.LoadConfigTolerant(t.Context())
	require.NoError(t, err)
	target, err := opts.PlanLoginMutation(cfg, "prod", config.LoginMutationUnified)
	require.NoError(t, err)
	assert.Equal(t, localPath, target.Path)
	assert.Equal(t, "local", target.Type, "the caller must retain auto-local trust provenance")
}

func TestPlanLoginMutationRejectsNewContextAcrossLayers(t *testing.T) {
	userDir, workDir := isolatedConfigEnv(t)
	userPath := writeUserConfig(t, userDir, `version: 1
contexts:
  existing: {}
current-context: existing
`)
	localPath := writeLocalConfig(t, workDir, `version: 1
contexts:
  existing:
    datasources:
      prometheus: local-prom
`)

	opts := &configcmd.Options{}
	cfg, err := opts.LoadConfigTolerant(t.Context())
	require.NoError(t, err)
	_, err = opts.PlanLoginMutation(cfg, "new-context", config.LoginMutationUnified)
	require.Error(t, err)
	require.ErrorContains(t, err, "does not exist")
	require.ErrorContains(t, err, userPath)
	require.ErrorContains(t, err, localPath)
}

func TestPlanLoginMutationRejectsSameNamedStackOutsideContextOwner(t *testing.T) {
	userDir, workDir := isolatedConfigEnv(t)
	userPath := writeUserConfig(t, userDir, `version: 1
contexts:
  prod: {}
current-context: prod
`)
	localPath := writeLocalConfig(t, workDir, `version: 1
stacks:
  prod:
    slug: unrelated
    providers:
      synth:
        sm-url: https://attacker.invalid
contexts:
  unrelated: {}
`)

	opts := &configcmd.Options{}
	cfg, err := opts.LoadConfigTolerant(t.Context())
	require.NoError(t, err)
	_, err = opts.PlanLoginMutation(cfg, "prod", config.LoginMutationUnified)
	require.ErrorContains(t, err, "Stack entry \"prod\" already exists outside the selected context owner")
	require.ErrorContains(t, err, userPath)
	require.ErrorContains(t, err, localPath)
}

func TestPlanLoginMutationRejectsShadowedSameNamedStackInContextOwner(t *testing.T) {
	userDir, workDir := isolatedConfigEnv(t)
	userPath := writeUserConfig(t, userDir, `version: 1
stacks:
  prod:
    slug: user-unreferenced
    providers:
      synth:
        sm-url: https://user-sm.invalid
contexts:
  prod: {}
current-context: prod
`)
	localPath := writeLocalConfig(t, workDir, `version: 1
stacks:
  prod:
    slug: local-shadow
    providers:
      synth:
        sm-url: https://local-sm.invalid
contexts:
  unrelated: {}
`)

	opts := &configcmd.Options{}
	cfg, err := opts.LoadConfigTolerant(t.Context())
	require.NoError(t, err)
	_, err = opts.PlanLoginMutation(cfg, "prod", config.LoginMutationUnified)
	require.ErrorContains(t, err, "Stack entry \"prod\" already exists outside the selected context owner")
	require.ErrorContains(t, err, userPath)
	require.ErrorContains(t, err, localPath)
}

func TestPlanLoginMutationKeepsZeroOneAndExplicitSelectionBehavior(t *testing.T) {
	t.Run("sole auto local preserves provenance", func(t *testing.T) {
		_, workDir := isolatedConfigEnv(t)
		localPath := writeLocalConfig(t, workDir, "version: 1\ncontexts:\n  default: {}\ncurrent-context: default\n")

		opts := &configcmd.Options{}
		cfg, err := opts.LoadConfigTolerant(t.Context())
		require.NoError(t, err)
		target, err := opts.PlanLoginMutation(cfg, "default", config.LoginMutationUnified)
		require.NoError(t, err)
		assert.Equal(t, localPath, target.Path)
		assert.Equal(t, "local", target.Type)
	})

	t.Run("explicit selection is authoritative", func(t *testing.T) {
		userDir, workDir := isolatedConfigEnv(t)
		writeUserConfig(t, userDir, "version: 1\ncontexts:\n  default: {}\ncurrent-context: default\n")
		writeLocalConfig(t, workDir, "version: 1\ncontexts:\n  default: {}\ncurrent-context: default\n")
		explicit := filepath.Join(t.TempDir(), "selected.yaml")
		opts := &configcmd.Options{ConfigFile: explicit}

		target, err := opts.PlanLoginMutation(config.Config{}, "new-context", config.LoginMutationUnified)
		require.NoError(t, err)
		assert.Equal(t, explicit, target.Path)
		assert.Equal(t, "explicit", target.Type)
	})

	t.Run("GCX_CONFIG selection is authoritative", func(t *testing.T) {
		_, _ = isolatedConfigEnv(t)
		explicit := filepath.Join(t.TempDir(), "from-env.yaml")
		t.Setenv(config.ConfigFileEnvVar, explicit)
		opts := &configcmd.Options{}

		target, err := opts.PlanLoginMutation(config.Config{}, "new-context", config.LoginMutationUnified)
		require.NoError(t, err)
		assert.Equal(t, explicit, target.Path)
		assert.Equal(t, "explicit", target.Type)
	})
}
