package login

import (
	"errors"
	"fmt"
	"io"
	"net/url"

	"charm.land/lipgloss/v2"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/login"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
)

// signupSuccessHeading is the line signup's text output opens with. It does
// not say the account was created: the sign-up page also lets someone sign in
// to an account they already have.
const signupSuccessHeading = "You're connected to Grafana Cloud"

// printSignupResult is printResult for gcx signup. Stdout carries the same
// LoginResult as gcx login, so structured output is unchanged; the human text
// codec renders it as the success summary. The next step is advice and goes to
// stderr: a plain list in text mode, a hint otherwise. The consent page moved
// its browser tab away from the stack, so that step is the way back to it.
func printSignupResult(cmd *cobra.Command, flags *loginOpts, server string, result login.Result) error {
	lr := newLoginResult(server, result)
	ew := cmd.ErrOrStderr()
	text := flags.IO.OutputFormat == "text"

	if text {
		fmt.Fprintln(ew)
	}
	if err := flags.IO.Encode(cmd.OutOrStdout(), lr); err != nil {
		return err
	}

	link := stackBrowserURL(lr.Server)
	switch {
	case link == "":
		return nil
	case !text:
		cmdio.EmitHint(ew, "Open Grafana: "+link, "")
		return nil
	}
	fmt.Fprintln(ew)
	fmt.Fprintln(ew, "Next steps")
	fmt.Fprintln(ew, "  Open Grafana")
	fmt.Fprintf(ew, "    %s\n", link)
	return nil
}

// stackBrowserURL returns server, the saved stack URL, for a browser to open,
// or "" when it is not an https URL.
func stackBrowserURL(server string) string {
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return server
}

// signupTextCodec renders LoginResult as signup's success summary. It is
// signup's "text" codec, the default for interactive terminals; gcx login
// keeps loginTextCodec.
type signupTextCodec struct{}

func (c *signupTextCodec) Format() format.Format { return "text" }

func (c *signupTextCodec) Encode(w io.Writer, value any) error {
	lr, ok := value.(LoginResult)
	if !ok {
		return fmt.Errorf("signup text codec: unsupported type %T", value)
	}
	// The logo goes only to a terminal, and RenderLogo is empty unless styling
	// is on, which agent mode, a pipe, NO_COLOR and --no-color all turn off.
	if style.IsTerminalWriter(w) {
		if logo := style.RenderLogo(); logo != "" {
			if _, err := lipgloss.Fprintln(w, logo); err != nil {
				return err
			}
		}
	}
	if agent.IsAgentMode() {
		// Agent mode keeps every format plain ASCII, so no check mark.
		fmt.Fprintln(w, signupSuccessHeading)
	} else {
		cmdio.Success(w, "%s", cmdio.Bold(signupSuccessHeading))
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Stack     %s\n", lr.Server)
	fmt.Fprintf(w, "  Context   %s\n", lr.ContextName)
	return nil
}

func (c *signupTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("signup text codec does not support decoding")
}
