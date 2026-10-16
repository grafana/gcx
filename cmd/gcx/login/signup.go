package login

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/login"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SignupCommand returns the `signup` Cobra command. It creates a Grafana Cloud
// account in the browser and saves a connection to the account's first stack.
// It runs the `gcx login` pipeline, so every login safety gate applies, with
// the target fixed to a new Grafana Cloud stack.
func SignupCommand() *cobra.Command {
	opts := &loginOpts{signup: true}

	cmd := &cobra.Command{
		Use:   "signup [CONTEXT_NAME]",
		Args:  cobra.MaximumNArgs(1),
		Short: "Create a Grafana Cloud account and connect gcx to it",
		Long: `Create a free Grafana Cloud account in the browser and connect gcx to the
account's first stack.

gcx opens the Grafana Cloud sign-up page and waits. In the browser, create the
account, verify your email (the emailed link may open a new tab), create your
first stack, and approve "Connect gcx". The browser then returns to gcx, which
saves the connection. No stack URL or token is needed. If the browser asks you
to choose a stack instead of creating an account, it is already signed in to
Grafana Cloud: choose one and approve "Connect gcx". If the browser loses the
page, press Enter in a local terminal to open it again, or open the printed URL.

The connection is saved to CONTEXT_NAME. Without it, gcx uses the current
context, or a context named "default" when none is set. signup only ever saves
a new connection: it refuses, before the browser opens, a context that already
has a stack or Grafana Cloud entry, or a name that an existing stack entry
uses. Pass an unused CONTEXT_NAME instead. For an account you already have,
run gcx login.

A person completes the browser steps. In agent mode gcx prints the URL instead
of opening the browser, and asks no questions. signup does not save Grafana
Cloud management credentials. To manage Grafana Cloud products (SLOs,
Synthetic Monitoring, k6 and more), save a Cloud Access Policy token afterwards
with gcx cloud login --cloud-token <token>. If signup fails once the browser
step has started, the error shows the gcx login command that finishes the
connection; do not run signup again, which could start a second account.`,
		Example: `  gcx signup
  gcx signup my-stack
  gcx signup --oauth-manual`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The whole route is a browser login to a new Grafana Cloud stack.
			// Preset both before runLogin, so its preflight gates (repository
			// config, layered config, credential storage) refuse before the
			// browser opens instead of after the account exists.
			opts.Cloud = true
			opts.OAuth = true
			if err := opts.Validate(args); err != nil {
				return err
			}
			return runLogin(cmd, opts, args)
		},
	}

	opts.setupSignup(cmd.Flags())

	return cmd
}

func (opts *loginOpts) setupSignup(flags *pflag.FlagSet) {
	opts.bindConfigAndOutputFlags(flags, &signupTextCodec{})

	flags.IntVar(&opts.OAuthCallbackPort, "oauth-callback-port", 0, oauthCallbackPortUsage)
	flags.BoolVar(&opts.OAuthManual, "oauth-manual", false, oauthManualUsage)
}

// refuseSignupDestinationEnvironment stops a signup when the environment
// describes an existing Grafana destination. Signup learns its server from the
// browser and connects a stack that did not exist a minute ago, so
// GRAFANA_SERVER would send login to that server's consent page instead of
// account creation, and GRAFANA_PROXY_ENDPOINT or GRAFANA_TLS_* would only be
// rejected after the browser step, once the account exists.
func refuseSignupDestinationEnvironment() error {
	if server := strings.TrimSpace(os.Getenv("GRAFANA_SERVER")); server != "" {
		return signupEnvironmentError(fmt.Sprintf(
			"GRAFANA_SERVER is set to %s. gcx signup creates a new Grafana Cloud stack and learns its URL from the browser, so it cannot target an existing server.",
			server,
		), "GRAFANA_SERVER", "To connect the server it names, run gcx login instead")
	}
	var set []string
	for _, key := range []string{"GRAFANA_PROXY_ENDPOINT", "GRAFANA_TLS_CERT_FILE", "GRAFANA_TLS_KEY_FILE", "GRAFANA_TLS_CA_FILE"} {
		if _, ok := os.LookupEnv(key); ok {
			set = append(set, key)
		}
	}
	if len(set) > 0 {
		variables := strings.Join(set, ", ")
		return signupEnvironmentError(
			variables+" set how gcx reaches an existing Grafana server. gcx signup connects a new Grafana Cloud stack, which needs neither.",
			variables, "To connect a server that needs them, run gcx login instead")
	}
	return nil
}

func signupEnvironmentError(details, variables, alternative string) error {
	return gcxerrors.DetailedError{
		Summary:     "Invalid command usage",
		Details:     details,
		Suggestions: []string{"Unset " + variables + ", then run gcx signup again", alternative},
	}
}

// signupTargetConflict refuses a signup whose save could touch an existing
// connection, before the browser opens. Signup writes a new connection only:
// the target context must be new or empty, and the stack entry named after it
// must not exist yet. That is where login saves the connection of a context
// without a stack (mergeGrafanaAuthIntoStack in internal/login), reusing the
// entry and everything in it (server, credentials, TLS, providers) when one
// exists. Contexts bound to that entry, including from another layer, would
// silently move to the new stack as well. Run it against the effective config
// and against the file the save writes.
func signupTargetConflict(cfg config.Config, contextName string) error {
	if existing := cfg.Contexts[contextName]; existing != nil {
		switch {
		case existing.Stack != "":
			return signupTargetTakenError(fmt.Sprintf("Context %q already exists and uses the stack entry %q.", contextName, existing.Stack))
		case existing.Cloud != "":
			return signupTargetTakenError(fmt.Sprintf("Context %q already exists and uses the Grafana Cloud entry %q, which belongs to another account.", contextName, existing.Cloud))
		}
	}
	if _, ok := cfg.Stacks[contextName]; ok {
		return signupTargetTakenError(fmt.Sprintf("A stack entry named %q already exists, and a new context %q would reuse it.", contextName, contextName))
	}

	var sharing []string
	for name, other := range cfg.Contexts {
		if name != contextName && other != nil && other.Stack == contextName {
			sharing = append(sharing, name)
		}
	}
	if len(sharing) > 0 {
		sort.Strings(sharing)
		return signupTargetTakenError(fmt.Sprintf(
			"%s already point at a stack entry named %q, so saving the new stack there would move them too.",
			quotedList(sharing), contextName,
		))
	}
	return nil
}

func signupTargetTakenError(details string) error {
	return gcxerrors.DetailedError{
		Summary: "Invalid command usage",
		Details: details + " gcx signup saves a new connection and never changes an existing one.",
		Suggestions: []string{
			"Pass a context name that is not in use: gcx signup <new-name>",
			"To sign in to an account you already have, run gcx login",
		},
	}
}

func quotedList(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = fmt.Sprintf("context %q", name)
	}
	return strings.Join(quoted, ", ")
}

// printSignupHeader does not promise a new account: the sign-up page sends a
// browser that is already signed in to Grafana Cloud, with a confirmed email,
// on to its stacks.
func printSignupHeader(cmd *cobra.Command, contextName string) {
	fmt.Fprintf(cmd.ErrOrStderr(), "Connecting gcx to Grafana Cloud. The connection will be saved as context %q.\n\n", contextName)
}

// signupLoginCommand is the gcx login command that finishes a signup whose
// browser step already ran: it keeps the signup's context and config file, and
// runs the browser login with oauthArgs. With server it connects that stack;
// without, it signs in through the stack launcher, which cannot create a
// second account.
func signupLoginCommand(flags *loginOpts, contextName, server string, oauthArgs ...string) string {
	parts := []string{"gcx login", shellArg(contextName)}
	if server != "" {
		parts = append(parts, "--server", shellArg(server))
	} else {
		parts = append(parts, "--cloud")
	}
	parts = append(parts, oauthArgs...)
	return signupFollowUpCommand(flags, strings.Join(parts, " "))
}

// signupFollowUpCommand appends the signup's --config to command, so a
// command that signup prints acts on the file the connection is saved to.
func signupFollowUpCommand(flags *loginOpts, command string) string {
	if flags.Config.ConfigFile == "" {
		return command
	}
	return command + " --config " + shellArg(flags.Config.ConfigFile)
}

// signupOAuthArgs are the gcx login flags that reach the browser the way the
// signup did. A signup run with --oauth-manual has no callback the browser can
// reach, and one run with --oauth-callback-port may rely on a port forward for
// that port, so the recovery must keep either choice.
func signupOAuthArgs(flags *loginOpts) []string {
	switch {
	case flags.OAuthManual:
		// --oauth-manual implies --oauth.
		return []string{"--oauth-manual"}
	case flags.OAuthCallbackPort != 0:
		return []string{"--oauth", "--oauth-callback-port", strconv.Itoa(flags.OAuthCallbackPort)}
	default:
		return []string{"--oauth"}
	}
}

// shellArg quotes value for a printed command only when a shell would
// otherwise split or expand it, so the common case stays readable.
func shellArg(value string) string {
	safe := func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._/:@=+-,%_", r)
	}
	if value != "" && strings.IndexFunc(value, func(r rune) bool { return !safe(r) }) == -1 {
		return value
	}
	return shellQuote(value)
}

// signupManualRetryCommand is what the remote session hint offers when the
// browser cannot reach the callback. The hint shows before the browser step,
// but the person usually finds out at its end, after creating the account, so
// the rerun signs in instead of signing up again. The launcher's sign in page
// still offers account creation to someone who has not started.
func signupManualRetryCommand(flags *loginOpts, contextName string) string {
	return signupLoginCommand(flags, contextName, "", "--oauth-manual")
}

// signupIncompleteError wraps a failure that came after signup's browser step
// started. server is set once the browser step finished.
func signupIncompleteError(err error, flags *loginOpts, contextName, server string) error {
	recovery := signupLoginCommand(flags, contextName, server, signupOAuthArgs(flags)...)
	return &login.SignupIncompleteError{Err: err, Server: server, Recovery: recovery}
}
