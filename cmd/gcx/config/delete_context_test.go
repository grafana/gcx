package config_test

import (
	"os"
	"testing"

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
cloud:
  grafana-com:
    api-url: https://grafana.com
contexts:
  a:
    stack: a
  b:
    stack: shared
    cloud: grafana-com
  c:
    stack: shared
    cloud: grafana-com
current-context: a
`

func TestDeleteContext(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantErr      string
		wantAbsent   []string
		wantPresent  []string
		wantCurrentA bool
	}{
		{
			name:        "removes unreferenced stack and clears current-context",
			args:        []string{"delete-context", "a"},
			wantAbsent:  []string{"  a:\n", "current-context: a"},
			wantPresent: []string{"  b:\n    stack: shared", "shared:"},
		},
		{
			name:         "keeps stack and cloud entries still referenced",
			args:         []string{"delete-context", "b"},
			wantAbsent:   []string{"  b:\n    stack"},
			wantPresent:  []string{"shared:", "grafana-com:", "current-context: a"},
			wantCurrentA: true,
		},
		{
			name:    "unknown context",
			args:    []string{"delete-context", "missing"},
			wantErr: "missing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userDir, _ := isolatedConfigEnv(t)
			path := writeUserConfig(t, userDir, deleteContextFixture)

			_, err := runConfigCmd(t, tc.args...)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)

			data, err := os.ReadFile(path)
			require.NoError(t, err)
			for _, s := range tc.wantAbsent {
				assert.NotContains(t, string(data), s)
			}
			for _, s := range tc.wantPresent {
				assert.Contains(t, string(data), s)
			}
		})
	}
}

func TestDeleteContextTextOutput(t *testing.T) {
	userDir, _ := isolatedConfigEnv(t)
	writeUserConfig(t, userDir, deleteContextFixture)

	out, err := runConfigCmd(t, "delete-context", "a")
	require.NoError(t, err)
	assert.Contains(t, out, `Deleted context "a", stack "a"`)
	assert.NotContains(t, out, "cloud")
}
