package logs

// Test helpers — expose internal rendering for the external test package.

// RenderLinesForTest exposes renderLines for testing.
func (m Model) RenderLinesForTest(width int, wrap bool) string {
	return m.renderLines(width, wrap)
}

// SoftWrapForTest exposes the viewport's current SoftWrap setting for testing.
func (m Model) SoftWrapForTest() bool {
	return m.vp.SoftWrap
}

// PrefixWidthForTest exposes prefixWidth for testing.
func PrefixWidthForTest() int {
	return prefixWidth
}
