package native_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/native"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const yamlManifest = `apiVersion: notifications.alerting.grafana.app/v1beta1
kind: RoutingTree
metadata:
  name: team-a
spec:
  defaults:
    receiver: grafana-default-email
`

const jsonManifest = `{"apiVersion":"notifications.alerting.grafana.app/v1beta1","kind":"RoutingTree","metadata":{"name":"team-a"}}`

func TestReadManifest(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		return p
	}

	tests := []struct {
		name     string
		filename string
		stdin    string
		wantName string
		wantErr  string
	}{
		{name: "yaml file", filename: write("a.yaml", yamlManifest), wantName: "team-a"},
		{name: "json file", filename: write("a.json", jsonManifest), wantName: "team-a"},
		{name: "yaml stdin", filename: "-", stdin: yamlManifest, wantName: "team-a"},
		{name: "json stdin", filename: "-", stdin: jsonManifest, wantName: "team-a"},
		{name: "empty filename", filename: "", wantErr: "--filename / -f is required"},
		{name: "missing file", filename: filepath.Join(dir, "nope.yaml"), wantErr: "failed to open"},
		{name: "empty input", filename: "-", stdin: "  \n", wantErr: "manifest is empty"},
		{name: "invalid json", filename: "-", stdin: `{"kind":`, wantErr: "failed to parse JSON manifest"},
		{name: "invalid yaml", filename: "-", stdin: "just a string", wantErr: "neither valid JSON nor YAML"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			obj, err := native.ReadManifest(tc.filename, strings.NewReader(tc.stdin))
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantName, obj.GetName())
			assert.Equal(t, "RoutingTree", obj.GetKind())
		})
	}
}

func TestReadManifestYAMLNumbers(t *testing.T) {
	tests := []struct{ name, number string }{{"integer", "8"}, {"large exact integer", "9007199254740993"}, {"negative", "-8"}, {"fraction", "0.25"}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := "apiVersion: notifications.alerting.grafana.app/v1beta1\nkind: TimeInterval\nmetadata:\n  generation: 8\nspec:\n  unknown: " + tc.number + "\n"
			obj, err := native.ReadManifest("-", strings.NewReader(input))
			require.NoError(t, err)
			var copyObj any
			require.NotPanics(t, func() { copyObj = obj.DeepCopy() })
			require.Equal(t, obj, copyObj)
			require.Equal(t, int64(8), obj.GetGeneration())
			if tc.name == "large exact integer" {
				unknown, found, err := unstructured.NestedInt64(obj.Object, "spec", "unknown")
				require.NoError(t, err)
				require.True(t, found)
				require.Equal(t, int64(9007199254740993), unknown)
			}
		})
	}
}
