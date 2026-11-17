//go:build wasip1

package tea

// No terminal on wasip1: input is read as a plain stream.
func (p *Program) initInput() error { return nil }

const suspendSupported = false

func suspendProcess() {}

func (p *Program) listenForResize(done chan struct{}) { close(done) }
