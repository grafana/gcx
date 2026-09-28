package login

import (
	"errors"
	"fmt"
	"io"
	"net/url"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/login"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
)

// signupSuccessHeading is the line signup's text output opens with. It does
// not say the account was created: the sign-up page also lets someone sign in
// to an account they already have.
const signupSuccessHeading = "Connected to your Grafana Cloud stack"

// signupNextStep is one entry of the list signup prints after the saved
// connection: a command to run, a page to open or read, or both.
type signupNextStep struct {
	Summary string
	Command string
	Link    string
}

// printSignupResult is printResult for gcx signup. Stdout carries the same
// LoginResult as gcx login, so structured output is unchanged; the human text
// codec renders it as the success summary. The next steps are advice and go
// to stderr: a plain list in text mode, hints otherwise.
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

	steps := signupNextSteps(flags, lr)
	if !text {
		for _, step := range steps {
			summary := step.Summary
			if step.Link != "" {
				summary += ": " + step.Link
			}
			cmdio.EmitHint(ew, summary, step.Command)
		}
		return nil
	}

	fmt.Fprintln(ew)
	fmt.Fprintln(ew, "Next steps")
	for _, step := range steps {
		fmt.Fprintf(ew, "  %s\n", step.Summary)
		if step.Command != "" {
			fmt.Fprintf(ew, "    %s\n", step.Command)
		}
		if step.Link != "" {
			fmt.Fprintf(ew, "    %s\n", step.Link)
		}
	}
	return nil
}

// signupNextSteps lists what to do once the new stack is connected. Commands
// keep the signup's context and config file, like the recovery commands.
func signupNextSteps(flags *loginOpts, lr LoginResult) []signupNextStep {
	var steps []signupNextStep

	if link := addConnectionURL(lr.Server); link != "" {
		steps = append(steps, signupNextStep{
			Summary: "Connect your first app or service",
			Link:    link,
		})
	}

	steps = append(steps, signupNextStep{
		Summary: "Check the connection anytime",
		Command: signupFollowUpCommand(flags, "gcx config check --context "+shellArg(lr.ContextName)),
	})

	if lr.Cloud && !lr.HasCloudToken {
		// Agents get the Markdown rendering of the docs page, people the HTML one.
		link := docs.AccessPolicies
		if !agent.IsAgentMode() {
			link = docs.HumanURL(link)
		}
		steps = append(steps, signupNextStep{
			Summary: "Manage SLOs, Synthetic Monitoring, k6 and more with a Cloud Access Policy token",
			Command: signupFollowUpCommand(flags, "gcx cloud login --context "+shellArg(lr.ContextName)+" --cloud-token <token>"),
			Link:    link,
		})
	}

	return steps
}

// signupFollowUpCommand appends the signup's --config to command, so a
// command that signup prints acts on the file the connection is saved to.
func signupFollowUpCommand(flags *loginOpts, command string) string {
	if flags.Config.ConfigFile == "" {
		return command
	}
	return command + " --config " + shellArg(flags.Config.ConfigFile)
}

// addConnectionURL returns the stack's Add new connection page, Grafana's
// starting point for sending data from an app or service, or "" when server
// is not an https URL.
func addConnectionURL(server string) string {
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return u.JoinPath("connections", "add-new-connection").String()
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
	if agent.IsAgentMode() {
		// Agent mode keeps every format plain ASCII, so no check mark.
		fmt.Fprintln(w, signupSuccessHeading)
	} else {
		cmdio.Success(w, "%s", cmdio.Bold(signupSuccessHeading))
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Stack:    %s\n", lr.Server)
	fmt.Fprintf(w, "  Context:  %s\n", lr.ContextName)
	if lr.GrafanaVersion != "" {
		fmt.Fprintf(w, "  Version:  %s\n", lr.GrafanaVersion)
	}
	return nil
}

func (c *signupTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("signup text codec does not support decoding")
}
