package style

import "io"

// RenderStyled exposes the styled renderer to external tests, which cannot
// reach it through Render because tests run without a TTY.
func (tb *TableBuilder) RenderStyled(w io.Writer) error { return tb.renderStyled(w) }
