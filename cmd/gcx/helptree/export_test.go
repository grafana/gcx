package helptree

// Exported aliases for unexported functions, usable from external test packages.
//
//nolint:gochecknoglobals // Test export pattern.
var (
	DetectEnum  = detectEnum
	FormatFlag  = formatFlag
	FindSubtree = findSubtree
)
