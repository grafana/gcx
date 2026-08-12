package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/terminal"
)

// manualCallbackPort is the port gcx puts in the callback URL when it runs
// without a listener. The browser cannot connect to that port, and that
// failure is the point: the full redirect URL stays in the address bar for the
// user to copy. 54321 is the first port of the normal auto-pick range, so both
// the Grafana plugin (which requires 1024-65535) and grafana.com already
// accept it.
const manualCallbackPort = 54321

// maxPastedURLBytes bounds one pasted line.
const maxPastedURLBytes = 8192

// ParseCallbackInput extracts the query parameters from a line that the user
// copied out of the browser address bar.
//
// The check is deliberately syntactic only. Every semantic check (state, code,
// endpoint) stays in handleCallbackParams, so the paste path and the HTTP
// callback path cannot diverge.
//
// An error never quotes the input, because the input holds a single-use
// authorization code.
func ParseCallbackInput(line string) (url.Values, error) {
	raw := strings.TrimSpace(line)
	raw = trimMatchingQuotes(raw)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("no URL supplied")
	}

	parsed, err := url.Parse(raw)
	// Some browsers hide the scheme. A pasted "127.0.0.1:54321/callback?..."
	// parses as scheme "127.0.0.1", so try again with an explicit scheme.
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		if retry, retryErr := url.Parse("http://" + raw); retryErr == nil && retry.Host != "" {
			parsed, err = retry, nil
		}
	}
	if err != nil {
		return nil, errors.New("the input is not a URL")
	}
	if parsed.Host == "" {
		return nil, errors.New("the input is not a full URL: copy the whole address")
	}
	if parsed.RawQuery == "" {
		return nil, errors.New("the URL has no query parameters: copy the address after the browser was redirected")
	}

	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return nil, errors.New("the URL query is malformed")
	}
	return values, nil
}

// trimMatchingQuotes removes one layer of matching single or double quotes.
func trimMatchingQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// readLine reads bytes until the first newline. It reads one byte at a time on
// purpose: a buffered reader would consume data after the newline, and the
// Cloud follow-up prompt reads the same stream directly after this call.
func readLine(r io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return b.String(), nil
			}
			if b.Len() >= maxPastedURLBytes {
				return "", errors.New("the pasted URL is too long")
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if b.Len() == 0 {
					return "", errors.New("no input received")
				}
				return b.String(), nil
			}
			return "", fmt.Errorf("failed to read the redirect URL: %w", err)
		}
	}
}

// readLineContext runs readLine in a goroutine so a cancelled context ends the
// wait. A terminal read is not interruptible in Go, so the read goroutine
// stays blocked until the process exits. The channel is buffered, so that
// goroutine can never block on the send.
func readLineContext(ctx context.Context, r io.Reader) (string, error) {
	type lineResult struct {
		line string
		err  error
	}
	ch := make(chan lineResult, 1)

	go func() {
		line, err := readLine(r)
		ch <- lineResult{line: line, err: err}
	}()

	select {
	case res := <-ch:
		return res.line, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// printRemoteSessionPreamble states why the browser cannot reach the callback
// address. Every remote-session message opens with it. It prints nothing, and
// reports false, for a local session.
func printRemoteSessionPreamble(w io.Writer) bool {
	if !terminal.IsRemoteSession() {
		return false
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Note: gcx runs in an SSH session.")
	fmt.Fprintln(w, "The browser on your computer cannot open the callback address on this host.")
	return true
}

// printRemoteSessionHint explains how to finish the flow when gcx runs on a
// remote host and has no terminal to read a pasted URL from. It prints nothing
// for a local session. command is the exact invocation to repeat, for example
// "gcx login --oauth-manual".
func printRemoteSessionHint(w io.Writer, port int, command string) {
	if !printRemoteSessionPreamble(w) {
		return
	}

	fmt.Fprintln(w, "Do one of these two steps:")
	fmt.Fprintln(w, "  1. Forward the port. On your computer, run:")
	fmt.Fprintf(w, "       ssh -L %d:127.0.0.1:%d REMOTE_HOST\n", port, port)
	fmt.Fprintf(w, "  2. Stop this command. Run it again with %s.\n", command)
	fmt.Fprintln(w, "     gcx then prints the login URL. You copy the redirect URL from the")
	fmt.Fprintln(w, "     browser and paste it in the terminal.")
	fmt.Fprintln(w)
}

// printManualInstructions prints the numbered steps of the manual paste flow.
// verification is the code that the consent page shows. Pass an empty string
// for a flow that does not show one.
func printManualInstructions(w io.Writer, authURL, verification string) {
	fmt.Fprintln(w, "Manual OAuth mode. gcx does not start a callback server.")
	fmt.Fprintln(w)

	step := 1
	fmt.Fprintf(w, "%d. Open this URL in a browser on your computer:\n", step)
	fmt.Fprintf(w, "     %s\n\n", authURL)

	if verification != "" {
		step++
		fmt.Fprintf(w, "%d. Verification code: %s\n", step, verification)
		fmt.Fprintln(w, "   Make sure that the browser shows the same code. Then approve.")
		fmt.Fprintln(w)
	}

	step++
	fmt.Fprintf(w, "%d. The browser goes to an address that does not load. This is correct.\n\n", step)

	step++
	fmt.Fprintf(w, "%d. Copy the full address from the browser address bar.\n", step)
	fmt.Fprintln(w, "   Do these steps quickly. The code expires.")
	fmt.Fprintln(w)
	fmt.Fprint(w, manualRedirectPrompt)
}

// manualRedirectPrompt asks for the pasted URL when gcx runs no callback
// server. pastePrompt is the variant for the race against a live callback.
const manualRedirectPrompt = "Redirect URL: "

// pastePrompt asks for the pasted URL while a callback server still listens, so
// it names the second route too.
const pastePrompt = "Redirect URL (or wait for the browser): "

// manualCallbackHygieneNotice tells the user to clear the terminal. The pasted
// URL holds a single-use code.
const manualCallbackHygieneNotice = "The URL that you pasted holds a single-use code. Clear the terminal if other people can read it."

// pasteRejection turns a callback error into the message shown before gcx asks
// for another redirect URL. A state mismatch on a paste nearly always means the
// URL came from a different login attempt, so say that instead of naming CSRF.
func pasteRejection(err error) error {
	if errors.Is(err, errStateMismatch) {
		return errManualForeignState
	}
	return err
}

var errManualForeignState = errors.New(
	"the pasted URL belongs to a different login attempt: run the command again and paste the URL from this attempt")
