package fleet

// Test-only exports (file suffix `_test.go` restricts visibility to the test
// binary). Lets external tests in package `fleet_test` exercise unexported
// helpers without widening the public API.

// FilterCollectorsByCluster exposes filterCollectorsByCluster to tests.
//
//nolint:gochecknoglobals // Test-only export for black-box test package.
var FilterCollectorsByCluster = filterCollectorsByCluster
