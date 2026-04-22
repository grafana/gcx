package permissions_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/permissions"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
)

// Compile-time assertions that both domain types satisfy ResourceIdentity.
var (
	_ adapter.ResourceIdentity = &permissions.FolderPermissions{}
	_ adapter.ResourceIdentity = &permissions.DashboardPermissions{}
)

func TestFolderPermissions_ResourceIdentity(t *testing.T) {
	fp := permissions.FolderPermissions{UID: "folder-abc"}
	assert.Equal(t, "folder-abc", fp.GetResourceName())

	var mut permissions.FolderPermissions
	mut.SetResourceName("folder-xyz")
	assert.Equal(t, "folder-xyz", mut.UID)
	assert.Equal(t, "folder-xyz", mut.GetResourceName())
}

func TestDashboardPermissions_ResourceIdentity(t *testing.T) {
	dp := permissions.DashboardPermissions{UID: "dash-abc"}
	assert.Equal(t, "dash-abc", dp.GetResourceName())

	var mut permissions.DashboardPermissions
	mut.SetResourceName("dash-xyz")
	assert.Equal(t, "dash-xyz", mut.UID)
	assert.Equal(t, "dash-xyz", mut.GetResourceName())
}

func TestFolderPermissions_RoundTripIdentity(t *testing.T) {
	original := permissions.FolderPermissions{UID: "folder-abc", Items: []permissions.Item{{Role: "Viewer", Permission: permissions.PermissionView}}}
	name := original.GetResourceName()

	var restored permissions.FolderPermissions
	restored.SetResourceName(name)
	assert.Equal(t, original.UID, restored.UID)
}

func TestDashboardPermissions_RoundTripIdentity(t *testing.T) {
	original := permissions.DashboardPermissions{UID: "dash-abc", Items: []permissions.Item{{TeamID: 7, Permission: permissions.PermissionEdit}}}
	name := original.GetResourceName()

	var restored permissions.DashboardPermissions
	restored.SetResourceName(name)
	assert.Equal(t, original.UID, restored.UID)
}
