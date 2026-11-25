package checks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateNameInFile(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "rewrites metadata.name",
			in:   "apiVersion: v1\nmetadata:\n  name: old\n  namespace: default\nspec:\n  job: j\n",
			want: "apiVersion: v1\nmetadata:\n  name: \"new-1\"\n  namespace: default\nspec:\n  job: j\n",
		},
		{
			name: "leaves a name label that sorts before metadata.name alone",
			in:   "metadata:\n  labels:\n    name: keep\n  name: old\nspec:\n  job: j\n",
			want: "metadata:\n  labels:\n    name: keep\n  name: \"new-1\"\nspec:\n  job: j\n",
		},
		{
			name: "leaves a name annotation alone and skips a nested name when metadata.name is absent",
			in:   "metadata:\n  annotations:\n    name: keep\nspec:\n  job: j\n",
			want: "metadata:\n  annotations:\n    name: keep\nspec:\n  job: j\n",
		},
		{
			name: "keeps CRLF line endings",
			in:   "metadata:\r\n  name: old\r\nspec:\r\n  job: j\r\n",
			want: "metadata:\r\n  name: \"new-1\"\r\nspec:\r\n  job: j\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "check.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.in), 0o600))
			require.NoError(t, updateNameInFile(path, "new-1"))
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}
