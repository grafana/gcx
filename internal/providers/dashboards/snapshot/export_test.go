package snapshot

// Exported aliases for unexported types, available to external test packages only.

// SnapshotOptsForTest wraps snapshotOpts for use in _test packages.
type SnapshotOptsForTest = snapshotOpts

// RenderSnapshotTableForTest exposes the unexported renderSnapshotTable
// function so contract tests can assert the command's default human stdout is
// byte-identical to the direct table rendering.
//
//nolint:gochecknoglobals // test-only export.
var RenderSnapshotTableForTest = renderSnapshotTable
