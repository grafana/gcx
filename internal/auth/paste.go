package auth

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/terminal"
)

// pasteWatcher reads redirect URLs that the user types while a callback server
// also listens. It exists for the remote case: gcx over SSH, browser on the
// user's own computer. The user can add a port forward and let the callback
// arrive, or paste the redirect URL. gcx accepts whichever completes first, so
// neither choice needs a restart.
//
// The watcher reads a separately opened /dev/tty, never os.Stdin. Go keeps the
// standard streams out of its poller, so a blocking read on os.Stdin cannot be
// cancelled; a stale reader would then steal keystrokes from the prompts that
// run after login. A file opened with os.OpenFile is pollable, so Close unblocks
// the pending read and the goroutine ends before the caller returns.
type pasteWatcher struct {
	tty    *os.File
	writer io.Writer
	values chan pastedInput
	// stop closes first in Close. The reader goroutine selects on it while it
	// delivers, so a caller that has stopped reading cannot wedge the send.
	stop chan struct{}
	// done closes when the reader goroutine has ended. Close waits on it, so
	// the terminal has exactly one reader again by the time Close returns.
	done chan struct{}
}

// startPasteWatcher prints the remote-session instructions and starts reading
// redirect URLs from the terminal. It returns nil when the paste path does not
// apply: a local session, agent mode, or no usable terminal. A nil watcher is
// safe to use — every method handles it.
func startPasteWatcher(w io.Writer, port int) *pasteWatcher {
	if !terminal.IsRemoteSession() || agent.IsAgentMode() {
		return nil
	}

	tty, ok := openPasteTerminal()
	if !ok {
		return nil
	}

	watcher := &pasteWatcher{
		tty:    tty,
		writer: w,
		values: make(chan pastedInput, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	watcher.printInstructions(port)

	go watcher.run()

	return watcher
}

// openPasteTerminal opens the controlling terminal for the paste watcher. It
// is a variable so tests can substitute a pipe.
//
// It deliberately opens /dev/tty instead of using os.Stdin. Go keeps the
// standard streams out of its poller, so Close cannot unblock a read on
// os.Stdin, and a stale reader would then compete with the prompts that run
// after login. os.OpenFile puts this file in non-blocking mode and registers it
// with the poller, so Close does unblock the read.
//
// Never call File.Fd on this file. Fd puts the descriptor back into blocking
// mode, the read then blocks inside the syscall instead of the poller, and
// Close can no longer stop it. That is also why there is no term.IsTerminal
// check here: /dev/tty is the controlling terminal by definition, and the open
// fails when the process has none.
var openPasteTerminal = func() (*os.File, bool) { //nolint:gochecknoglobals // test seam
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, false
	}
	// A read deadline is only supported on a file the poller accepted, so this
	// reports pollability. A non-zero deadline is required: it is cleared
	// immediately afterwards.
	if err := tty.SetReadDeadline(time.Now().Add(time.Hour)); err != nil {
		_ = tty.Close()
		return nil, false
	}
	if err := tty.SetReadDeadline(time.Time{}); err != nil {
		_ = tty.Close()
		return nil, false
	}
	return tty, true
}

// pastedInput is one line that the user pasted: the parsed query parameters, or
// the error that says why the line was not usable.
//
// The watcher delivers the parse error instead of printing it, because the
// caller writes to the same terminal. One goroutine therefore owns every
// message, and the two cannot interleave.
type pastedInput struct {
	Values url.Values
	Err    error
	// Closed reports that the reader ended, because the user pressed Ctrl-D or
	// the terminal reported an error. No further line arrives. The caller says
	// so and keeps waiting for the callback server.
	Closed bool
}

// Input reports each pasted line. A nil watcher returns a nil channel, which
// blocks forever in a select — exactly the "no paste path" behaviour.
func (p *pasteWatcher) Input() <-chan pastedInput {
	if p == nil {
		return nil
	}
	return p.values
}

// Reject reports why a pasted URL did not work and asks for another one. The
// callback server is still listening, so the flow stays alive. err is never the
// pasted string: no message may echo the authorization code.
func (p *pasteWatcher) Reject(err error) {
	if p == nil {
		return
	}
	fmt.Fprintf(p.writer, "\nThat URL did not work: %v\n", err)
	fmt.Fprint(p.writer, pastePrompt)
}

// closeGrace bounds the wait for the reader goroutine. Closing a pollable file
// unblocks its read at once, so the wait is normally instant. The bound is a
// backstop: a future regression that makes the file blocking must degrade to a
// short delay, never to a login that hangs forever.
const closeGrace = 2 * time.Second

// Close stops the watcher and releases the terminal. It ends the pending read,
// waits for the reader goroutine, and discards what the terminal still holds.
// The terminal therefore has exactly one reader again before login continues to
// the prompts that follow.
func (p *pasteWatcher) Close() {
	if p == nil {
		return
	}
	close(p.stop)

	// A deadline in the past ends the pending read at once, because the file is
	// registered with the poller. Close the file only after the flush below: a
	// closed descriptor accepts no ioctl.
	_ = p.tty.SetReadDeadline(time.Now())
	select {
	case <-p.done:
	case <-time.After(closeGrace):
	}

	// Discard what the user typed without a newline. A terminal in canonical
	// mode holds that text in its input queue, and the shell reads it once gcx
	// exits. A partly typed redirect URL would then enter the shell history,
	// which is the leak that the whole paste route exists to avoid.
	_ = flushTerminalInput(p.tty)
	_ = p.tty.Close()
}

func (p *pasteWatcher) run() {
	defer close(p.done)

	for {
		line, err := readLine(p.tty)
		select {
		case <-p.stop:
			// Close shut the terminal, which caused this read error. The caller
			// already has a result or an error of its own, so stop quietly.
			return
		default:
		}
		if err != nil {
			// The user pressed Ctrl-D, or the terminal reported an error. Say
			// so and stop. A silent stop left the prompt on screen with no
			// reader behind it, and a retry would spin on a terminal that
			// reports the same error forever.
			p.deliver(pastedInput{Closed: true})
			return
		}

		// Step 1 of the instructions asks the user to press Enter before the
		// SSH escape ~C. Drop that empty line: a rejection on the route that the
		// instructions recommend first is wrong, and the ssh escape prompt owns
		// the terminal at that moment.
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Keep reading after a delivery. The caller runs the semantic checks
		// (state, code, token exchange) and calls Reject when they fail, which
		// re-prompts. That prompt needs a reader. Without one the next pasted
		// line stays in the terminal buffer, and the shell reads it after gcx
		// exits, which writes the authorization code into the shell history.
		values, err := ParseCallbackInput(line)
		if !p.deliver(pastedInput{Values: values, Err: err}) {
			return
		}
	}
}

// deliver sends one result to the caller. It reports false when Close ran
// first, so a caller that stopped reading cannot wedge the send.
func (p *pasteWatcher) deliver(in pastedInput) bool {
	select {
	case p.values <- in:
		return true
	case <-p.stop:
		return false
	}
}

func (p *pasteWatcher) printInstructions(port int) {
	printRemoteSessionPreamble(p.writer)
	fmt.Fprintln(p.writer, "Do one of these two steps. gcx accepts the one that completes first.")
	fmt.Fprintln(p.writer)
	fmt.Fprintln(p.writer, "  1. Add a port forward to this SSH session. Press Enter, then press ~C,")
	fmt.Fprintln(p.writer, "     then type this line:")
	fmt.Fprintf(p.writer, "       -L %d:127.0.0.1:%d\n", port, port)
	fmt.Fprintln(p.writer)
	fmt.Fprintln(p.writer, "  2. Approve the login in the browser on your computer. The browser then")
	fmt.Fprintln(p.writer, "     goes to an address that does not load. Copy that address and paste it")
	fmt.Fprintln(p.writer, "     here.")
	fmt.Fprintln(p.writer)
	fmt.Fprint(p.writer, pastePrompt)
}
