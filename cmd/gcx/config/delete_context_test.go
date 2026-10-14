package config_test

import (
	"os"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const deleteContextFixture = `version: 1
stacks:
  a:
    grafana:
      server: https://a.example.invalid
  shared:
    grafana:
      server: https://shared.example.invalid
  d:
    grafana:
      server: https://d.example.invalid
cloud:
  grafana-com:
    api-url: https://grafana.com
  own-cloud:
    api-url: https://own.example.invalid
contexts:
  a:
    stack: a
  b:
    stack: shared
    cloud: grafana-com
  c:
    stack: shared
    cloud: grafana-com
  d:
    stack: d
    cloud: own-cloud
current-context: a
`

type deletedConfig struct {
	Stacks         map[string]any `yaml:"stacks"`
	Cloud          map[string]any `yaml:"cloud"`
	Contexts       map[string]any `yaml:"contexts"`
	CurrentContext string         `yaml:"current-context"`
}

func readDeletedConfig(t *testing.T, path string) deletedConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var cfg deletedConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	return cfg
}

func TestDeleteContext(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantErr         string
		wantContexts    []string
		wantStacks      []string
		wantClouds      []string
		wantCurrent     string
		wantOutContains string
	}{
		{
			name:            "removes unreferenced stack and clears current-context",
			args:            []string{"delete-context", "a"},
			wantContexts:    []string{"b", "c", "d"},
			wantStacks:      []string{"shared", "d"},
			wantClouds:      []string{"grafana-com", "own-cloud"},
			wantCurrent:     "",
			wantOutContains: "current-context cleared",
		},
		{
			name:            "keeps stack and cloud entries still referenced",
			args:            []string{"delete-context", "b"},
			wantContexts:    []string{"a", "c", "d"},
			wantStacks:      []string{"a", "shared", "d"},
			wantClouds:      []string{"grafana-com", "own-cloud"},
			wantCurrent:     "a",
			wantOutContains: `Deleted context "b"`,
		},
		{
			name:            "removes unshared cloud entry",
			args:            []string{"delete-context", "d"},
			wantContexts:    []string{"a", "b", "c"},
			wantStacks:      []string{"a", "shared"},
			wantClouds:      []string{"grafana-com"},
			wantCurrent:     "a",
			wantOutContains: `Deleted context "d", stack "d", cloud "own-cloud"`,
		},
		{
			name:    "unknown context",
			args:    []string{"delete-context", "missing"},
			wantErr: `invalid context "missing"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userDir, _ := isolatedConfigEnv(t)
			path := writeUserConfig(t, userDir, deleteContextFixture)

			out, err := runConfigCmd(t, tc.args...)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, out, tc.wantOutContains)

			got := readDeletedConfig(t, path)
			assert.ElementsMatch(t, tc.wantContexts, keysOf(got.Contexts))
			assert.ElementsMatch(t, tc.wantStacks, keysOf(got.Stacks))
			assert.ElementsMatch(t, tc.wantClouds, keysOf(got.Cloud))
			assert.Equal(t, tc.wantCurrent, got.CurrentContext)
		})
	}
}

func TestDeleteContextKeepsEntriesReferencedByOtherLayer(t *testing.T) {
	userDir, workDir := isolatedConfigEnv(t)
	path := writeUserConfig(t, userDir, `version: 1
stacks:
  shared:
    grafana:
      server: https://shared.example.invalid
cloud:
  grafana-com:
    api-url: https://grafana.com
contexts:
  user-ctx:
    stack: shared
    cloud: grafana-com
`)
	writeLocalConfig(t, workDir, `version: 1
contexts:
  local-ctx:
    stack: shared
    cloud: grafana-com
`)

	out, err := runConfigCmd(t, "delete-context", "--file", "user", "user-ctx")
	require.NoError(t, err)
	assert.NotContains(t, out, "stack")

	got := readDeletedConfig(t, path)
	assert.Empty(t, got.Contexts)
	assert.Contains(t, got.Stacks, "shared")
	assert.Contains(t, got.Cloud, "grafana-com")
}

func TestDeleteContextReportsNameStillDefinedInOtherLayer(t *testing.T) {
	userDir, workDir := isolatedConfigEnv(t)
	path := writeUserConfig(t, userDir, `version: 1
stacks:
  shared:
    grafana:
      server: https://shared.example.invalid
contexts:
  dup:
    stack: shared
`)
	writeLocalConfig(t, workDir, `version: 1
contexts:
  dup:
    stack: shared
`)

	out, err := runConfigCmd(t, "delete-context", "--file", "user", "dup")
	require.NoError(t, err)
	assert.Contains(t, out, `Context "dup" is still defined in another config layer`)

	got := readDeletedConfig(t, path)
	assert.Contains(t, got.Stacks, "shared")
}

func TestDeleteContextClearsPreviousContext(t *testing.T) {
	userDir, _ := isolatedConfigEnv(t)
	writeUserConfig(t, userDir, deleteContextFixture)
	require.NoError(t, config.WritePreviousContext(t.Context(), "b"))

	_, err := runConfigCmd(t, "delete-context", "b")
	require.NoError(t, err)

	prev, err := config.ReadPreviousContext()
	require.NoError(t, err)
	assert.Empty(t, prev)
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
