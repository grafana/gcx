package permissions

import "io"

// ReadItemsFromFileForTest exposes readItemsFromFile for external tests.
func ReadItemsFromFileForTest(path string, stdin io.Reader) ([]Item, error) {
	return readItemsFromFile(path, stdin)
}

// PermissionNameForTest exposes permissionName for external tests.
func PermissionNameForTest(p int) string {
	return permissionName(p)
}
