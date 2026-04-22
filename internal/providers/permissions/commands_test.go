package permissions_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/permissions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestReadItemsFromFile_BareArray(t *testing.T) {
	content := `[
		{"role": "Viewer", "permission": 1},
		{"userLogin": "admin", "permission": 4}
	]`

	path := writeTempFile(t, "perms.json", content)

	items, err := permissions.ReadItemsFromFileForTest(path, nil)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "Viewer", items[0].Role)
	assert.Equal(t, 1, items[0].Permission)
	assert.Equal(t, "admin", items[1].UserLogin)
	assert.Equal(t, 4, items[1].Permission)
}

func TestReadItemsFromFile_ItemsEnvelope(t *testing.T) {
	content := `{
		"items": [
			{"role": "Editor", "permission": 2},
			{"teamId": 7, "permission": 2}
		]
	}`

	path := writeTempFile(t, "perms.json", content)

	items, err := permissions.ReadItemsFromFileForTest(path, nil)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "Editor", items[0].Role)
	assert.Equal(t, 7, items[1].TeamID)
	assert.Equal(t, 2, items[1].Permission)
}

func TestReadItemsFromFile_Stdin(t *testing.T) {
	content := `[{"role":"Viewer","permission":1}]`
	items, err := permissions.ReadItemsFromFileForTest("-", strings.NewReader(content))
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "Viewer", items[0].Role)
}

func TestReadItemsFromFile_MissingPath(t *testing.T) {
	_, err := permissions.ReadItemsFromFileForTest("", nil)
	require.Error(t, err)
}

func TestReadItemsFromFile_InvalidJSON(t *testing.T) {
	path := writeTempFile(t, "bad.json", `not json`)
	_, err := permissions.ReadItemsFromFileForTest(path, nil)
	require.Error(t, err)
}

func TestPermissionName(t *testing.T) {
	assert.Equal(t, "View", permissions.PermissionNameForTest(1))
	assert.Equal(t, "Edit", permissions.PermissionNameForTest(2))
	assert.Equal(t, "Admin", permissions.PermissionNameForTest(4))
	assert.Equal(t, "7", permissions.PermissionNameForTest(7))
}
