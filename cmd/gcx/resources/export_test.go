package resources

// Exported aliases for unexported types, available to external test packages only.

// TableCodecForTest wraps tableCodec for use in _test packages.
type TableCodecForTest = tableCodec

// NewTableCodecForTest creates a tableCodec with the given wide setting.
func NewTableCodecForTest(wide bool) *tableCodec {
	return &tableCodec{wide: wide}
}

// TabCodecForTest wraps tabCodec for use in _test packages.
type TabCodecForTest = tabCodec
