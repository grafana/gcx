package login

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	internalauth "github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/login"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// LoginResult is the structured post-login summary that the output codecs
// render. It mirrors the human-facing information emitted by the legacy prose
// output, plus fields that structured consumers (agent mode, scripts) may want
// (StackSlug, HasCloudToken).
type LoginResult struct {
	ContextName    string `json:"contextName" yaml:"contextName"`
	Server         string `json:"server" yaml:"server"`
	AuthMethod     string `json:"authMethod" yaml:"authMethod"`
	Cloud          bool   `json:"cloud" yaml:"cloud"`
	GrafanaVersion string `json:"grafanaVersion,omitempty" yaml:"grafanaVersion,omitempty"`
	StackSlug      string `json:"stackSlug,omitempty" yaml:"stackSlug,omitempty"`
	HasCloudToken  bool   `json:"hasCloudToken" yaml:"hasCloudToken"`
}

type loginOpts struct {
	Config              configcmd.Options
	IO                  cmdio.Options
	Server              string
	Token               string
	CloudToken          string
	CloudAPIURL         string
	Cloud               bool
	Yes                 bool
	AllowServerOverride bool
}

func (opts *loginOpts) setup(flags *pflag.FlagSet) {
	opts.Config.BindFlags(flags)
	// Register a human-text codec and use it as the default for interactive
	// terminals. cmdio.BindFlags overrides the default with "json" when
	// agent.IsAgentMode() is true, so we don't branch on agent mode here.
	opts.IO.RegisterCustomCodec("text", &loginTextCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(flags)

	flags.StringVar(&opts.Server, "server", "", "Grafana server URL (e.g. https://my-stack.grafana.net)")
	flags.StringVar(&opts.Token, "token", "", "Grafana service account token")
	flags.StringVar(&opts.CloudToken, "cloud-token", "", "Grafana Cloud API token (enables Cloud management features)")
	flags.StringVar(&opts.CloudAPIURL, "cloud-api-url", "", "Override Grafana Cloud API URL")
	flags.BoolVar(&opts.Cloud, "cloud", false, "Force Grafana Cloud target (skip auto-detection)")
	flags.BoolVar(&opts.Yes, "yes", false, "Non-interactive: skip optional prompts and use defaults")
	flags.BoolVar(&opts.AllowServerOverride, "allow-server-override", false, "Allow re-pointing an existing context at a different server URL")
}

// Validate checks opts and args for internal consistency before runLogin executes.
// Returns an error if a positional CONTEXT_NAME argument is combined with the
// --context flag (they're mutually exclusive to prevent silent confusion).
// Also validates the output codec options (format name, --json flag shape).
func (opts *loginOpts) Validate(args []string) error {
	if len(args) == 1 && opts.Config.Context != "" {
		return fail.DetailedError{
			Summary: "conflicting context specification",
			Details: fmt.Sprintf(
				"Positional argument %q and --context=%q both specified. Use one.",
				args[0], opts.Config.Context,
			),
			Suggestions: []string{
				"Drop --context and use the positional form: gcx login " + args[0],
			},
		}
	}
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return nil
}

// Command returns the `login` Cobra command.
func Command() *cobra.Command {
	opts := &loginOpts{}

	cmd := &cobra.Command{
		Use:   "login [CONTEXT_NAME]",
		Args:  cobra.MaximumNArgs(1),
		Short: "Log in to a Grafana instance",
		Long: `Authenticate to a Grafana instance (Cloud or on-premises) and save the
credentials to the selected config context.

Pass CONTEXT_NAME to target a specific context:
  - If the context exists, re-authenticate it (server and other fields preserved).
  - If it does not exist, create a new context with that name.

Without CONTEXT_NAME, re-authenticates the current context, or starts a
first-time setup if no current context is configured.`,
		Example: `  gcx login
  gcx login prod
  gcx login prod --server https://prod.grafana.net
  gcx login --yes prod --token glsa_xxx
  gcx login --yes --server https://localhost:3000 --token glsa_xxx`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(args); err != nil {
				return err
			}
			return runLogin(cmd, opts, args)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func runLogin(cmd *cobra.Command, flags *loginOpts, args []string) error {
	ctx := cmd.Context()

	// Positional arg takes precedence; --context flag is compat.
	// Mutual exclusion is enforced earlier in loginOpts.Validate.
	var contextName string
	switch {
	case len(args) == 1:
		contextName = args[0]
	default:
		contextName = flags.Config.Context
	}

	// Pre-populate Server from the correct source context:
	//   contextName set + context exists   -> use named context (re-auth)
	//   contextName set + context absent   -> leave Server empty (new context)
	//   contextName empty + current exists -> use current context (re-auth current)
	//   contextName empty + no current     -> leave Server empty (first-time setup)
	cfg, _ := flags.Config.LoadConfigTolerant(ctx) // tolerate missing file
	var sourceCtx *config.Context
	if contextName != "" {
		if existing, ok := cfg.Contexts[contextName]; ok {
			sourceCtx = existing
		}
	} else {
		sourceCtx = cfg.GetCurrentContext()
	}
	if flags.Server == "" && sourceCtx != nil && sourceCtx.Grafana != nil {
		flags.Server = sourceCtx.Grafana.Server
	}

	// Print mode header so the user can confirm which context they're targeting.
	// sourceCtx != nil implies re-auth (existing context).
	// sourceCtx == nil with contextName set implies new-context creation.
	// sourceCtx == nil with contextName empty implies first-time setup.
	printModeHeader(cmd, cfg, contextName, sourceCtx)

	isInteractive := term.IsTerminal(int(os.Stdin.Fd())) &&
		!flags.Yes &&
		!agent.IsAgentMode()

	// Carry existing TLS settings into the login flow so that mTLS keeps
	// working on re-auth without requiring the user to re-specify certs.
	var existingTLS *config.TLS
	if sourceCtx != nil && sourceCtx.Grafana != nil && sourceCtx.Grafana.TLS != nil &&
		!sourceCtx.Grafana.TLS.IsEmpty() {
		existingTLS = sourceCtx.Grafana.TLS
		// Advisory: when --allow-server-override re-points the context at a
		// different server, the existing TLS client cert will be presented to
		// the new server. This is gated by explicit user opt-in.
		if flags.AllowServerOverride && flags.Server != "" &&
			sourceCtx.Grafana != nil && sourceCtx.Grafana.Server != "" &&
			flags.Server != sourceCtx.Grafana.Server {
			logging.FromContext(cmd.Context()).Warn("reusing existing TLS client certificate for a different server",
				"previous_server", sourceCtx.Grafana.Server,
				"new_server", flags.Server,
			)
		}
	}

	opts := login.Options{
		Inputs: login.Inputs{
			Server:       flags.Server,
			ContextName:  contextName,
			GrafanaToken: flags.Token,
			CloudToken:   flags.CloudToken,
			CloudAPIURL:  flags.CloudAPIURL,
			Yes:          flags.Yes,
			Writer:       cmd.ErrOrStderr(),
			TLS:          existingTLS,
		},
		Hooks: login.Hooks{
			ConfigSource: flags.Config.ConfigSource(),
			NewAuthFlow: func(server string, ao internalauth.Options) login.AuthFlow {
				return internalauth.NewFlow(server, ao)
			},
		},
		RetryState: login.RetryState{
			StagedContext: &config.Context{}, // enables Run() to cache across sentinel retries
		},
	}

	if flags.Cloud {
		opts.Target = login.TargetCloud
	}
	if flags.AllowServerOverride {
		opts.AllowOverride = true
	}

	for {
		result, err := login.Run(ctx, &opts)
		if err == nil {
			// Use opts.Server (the canonical runtime value mutated by
			// interactive prompts / retries) rather than flags.Server, which
			// can be empty on first-time setup when the user typed the URL
			// into the huh form.
			return printResult(cmd, &flags.IO, opts.Server, result)
		}

		var needInput *login.ErrNeedInput
		var needClarification *login.ErrNeedClarification

		switch {
		case errors.As(err, &needInput):
			if !isInteractive {
				return structuredMissingFieldsError(needInput)
			}
			if formErr := askForInput(needInput, &opts, sourceCtx); formErr != nil {
				if errors.Is(formErr, huh.ErrUserAborted) {
					// Route advisory to stderr so stdout remains parseable
					// for -o json / -o yaml consumers.
					fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
					return nil
				}
				return formErr
			}

		case errors.As(err, &needClarification):
			if !isInteractive {
				return structuredClarificationError(needClarification)
			}
			if formErr := askForClarification(needClarification, &opts); formErr != nil {
				if errors.Is(formErr, huh.ErrUserAborted) {
					// Route advisory to stderr so stdout remains parseable
					// for -o json / -o yaml consumers.
					fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
					return nil
				}
				return formErr
			}

		default:
			return err
		}
	}
}

// askForInput shows an interactive huh prompt for each field in ErrNeedInput.
// For "cloud-token" with Optional=true: empty user input sets opts.Yes=true so
// that the next Run() call skips this step instead of looping forever (AC-002).
//
// When sourceCtx carries an existing stored token (re-auth), the prompt offers
// "Press Enter to keep existing token" semantics — empty input reuses the
// stored value instead of skipping or erroring.
func askForInput(e *login.ErrNeedInput, opts *login.Options, sourceCtx *config.Context) error {
	existingGrafanaToken := ""
	existingCloudToken := ""
	if sourceCtx != nil {
		if sourceCtx.Grafana != nil {
			existingGrafanaToken = sourceCtx.Grafana.APIToken
		}
		if sourceCtx.Cloud != nil {
			existingCloudToken = sourceCtx.Cloud.Token
		}
	}

	for _, field := range e.Fields {
		switch field {
		case "server":
			description := "e.g. https://my-stack.grafana.net"
			if opts.GrafanaToken == "" {
				description += "\nLeave empty to select your Grafana Cloud instance interactively"
			}
			form := huh.NewForm(huh.NewGroup(
				huh.NewInput().
					Title("Grafana server URL").
					Description(description).
					Validate(func(s string) error {
						if opts.GrafanaToken != "" && s == "" {
							return errors.New("server URL is required")
						}
						return nil
					}).
					Value(&opts.Server),
			))
			if err := form.Run(); err != nil {
				return err
			}
			if opts.Server == "" {
				opts.UseCloudInstanceSelector = true
				return nil
			}

		case "grafana-auth":
			if err := askGrafanaAuth(opts, existingGrafanaToken); err != nil {
				return err
			}

		case "cloud-token":
			hint := e.Hint
			switch {
			case existingCloudToken != "":
				hint = "Press Enter to keep existing token"
			case hint == "":
				hint = "Press Enter to skip (Cloud management features will be unavailable)"
			}
			form := huh.NewForm(huh.NewGroup(
				huh.NewInput().
					Title("Grafana Cloud API token").
					Description(hint).
					EchoMode(huh.EchoModePassword).
					Value(&opts.CloudToken),
			))
			if err := form.Run(); err != nil {
				return err
			}
			switch {
			case opts.CloudToken == "" && existingCloudToken != "":
				// Re-auth: user kept the existing token.
				opts.CloudToken = existingCloudToken
			case opts.CloudToken == "":
				// New context or user chose to skip Cloud auth. Set Yes=true
				// so the next Run() call bypasses this sentinel instead of
				// re-prompting.
				opts.Yes = true
			}
		}
	}
	return nil
}

// askGrafanaAuth prompts for an authentication method and, when "token" is
// chosen, for the token itself. When existingToken is non-empty (re-auth),
// the token prompt allows empty input to reuse the stored token.
//
// The auth-method menu is tailored to the resolved target:
//   - On-prem: OAuth is not offered (the Grafana instance cannot issue the
//     tokens our OAuth flow relies on). The token prompt is shown directly.
//   - Cloud: OAuth is offered first as the recommended path, with token as
//     the fallback.
//   - Unknown (target still ambiguous): both options are offered, token
//     first to match the historical default.
func askGrafanaAuth(opts *login.Options, existingToken string) error {
	// When TLS client certs are configured, mTLS is a valid standalone auth
	// method (e.g. Teleport proxy). Offer it as the default choice.
	hasMTLS := opts.TLS != nil && !opts.TLS.IsEmpty() &&
		(len(opts.TLS.CertData) > 0 || opts.TLS.CertFile != "")
	if hasMTLS && opts.Yes {
		// Non-interactive with certs configured: default to mTLS.
		return nil // resolveGrafanaAuth will pick up the TLS case.
	}

	tokenOption := huh.NewOption("Service account token (requires permissions for managing service accounts)", "token")
	oauthOption := huh.NewOption("OAuth (browser) — recommended for cloud stacks; experimental on some configurations, fall back to a service account token if you hit issues", "oauth")
	mtlsOption := huh.NewOption("Client certificate (mTLS) — authenticate via TLS client cert (e.g. Teleport)", "mtls")

	var options []huh.Option[string]
	switch opts.Target {
	case login.TargetOnPrem:
		if hasMTLS {
			options = []huh.Option[string]{mtlsOption, tokenOption}
		} else {
			options = []huh.Option[string]{tokenOption}
		}
	case login.TargetCloud:
		options = []huh.Option[string]{oauthOption, tokenOption}
	default: // TargetUnknown
		if hasMTLS {
			options = []huh.Option[string]{mtlsOption, tokenOption, oauthOption}
		} else {
			options = []huh.Option[string]{tokenOption, oauthOption}
		}
	}

	// Default to the first option in the menu. For Cloud targets, mTLS is not
	// offered so we must not default to it even when TLS certs are present.
	authMethod := "token"
	if hasMTLS && opts.Target != login.TargetCloud {
		authMethod = "mtls"
	}
	// Single option: skip the menu and fall through directly.
	if len(options) > 1 {
		methodForm := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Authentication method").
				Options(options...).
				Value(&authMethod),
		))
		if err := methodForm.Run(); err != nil {
			return err
		}
	}
	if authMethod == "oauth" {
		opts.UseOAuth = true
		return nil
	}
	if authMethod == "mtls" {
		// mTLS needs no additional input — the certs are already in opts.TLS.
		return nil
	}

	description := "Grafana service account token"
	validate := func(s string) error {
		if s == "" {
			return errors.New("token is required")
		}
		return nil
	}
	if existingToken != "" {
		description = "Press Enter to keep existing token"
		validate = func(string) error { return nil }
	}
	tokenForm := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Service account token").
			Description(description).
			EchoMode(huh.EchoModePassword).
			Validate(validate).
			Value(&opts.GrafanaToken),
	))
	if err := tokenForm.Run(); err != nil {
		return err
	}
	if opts.GrafanaToken == "" && existingToken != "" {
		opts.GrafanaToken = existingToken
	}
	return nil
}

// askForClarification shows a huh select for ErrNeedClarification (e.g. cloud vs on-prem).
func askForClarification(e *login.ErrNeedClarification, opts *login.Options) error {
	// Unvalidated-save confirmation: yes/no dialog; sets ForceSave so the
	// next Run() invocation skips validation and persists anyway. This is
	// an interactive-only debug escape hatch.
	if e.Field == "save-unvalidated" {
		confirmed := false
		form := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title("Save context despite validation failure?").
				Description(e.Question).
				Affirmative("Yes, save anyway").
				Negative("Cancel").
				Value(&confirmed),
		))
		if err := form.Run(); err != nil {
			return err
		}
		if !confirmed {
			return huh.ErrUserAborted
		}
		opts.ForceSave = true
		return nil
	}

	// Server-override confirmation: yes/no dialog; sets AllowOverride
	// for the next Run() invocation.
	if e.Field == "allow-override" {
		confirmed := false
		form := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title("Override existing context?").
				Description(e.Question).
				Affirmative("Yes, override").
				Negative("Cancel").
				Value(&confirmed),
		))
		if err := form.Run(); err != nil {
			return err
		}
		if !confirmed {
			// User chose Cancel; propagate a "user aborted" sentinel so the
			// caller returns cleanly.
			return huh.ErrUserAborted
		}
		opts.AllowOverride = true
		return nil
	}

	var choice string

	options := make([]huh.Option[string], len(e.Choices))
	for i, c := range e.Choices {
		options[i] = huh.NewOption(c, c)
	}

	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(e.Question).
			Options(options...).
			Value(&choice),
	))
	if err := form.Run(); err != nil {
		return err
	}

	if e.Field == "target" {
		switch choice {
		case "cloud":
			opts.Target = login.TargetCloud
		default:
			opts.Target = login.TargetOnPrem
		}
	}

	return nil
}

// structuredMissingFieldsError converts ErrNeedInput to a fail.DetailedError for non-interactive callers.
func structuredMissingFieldsError(e *login.ErrNeedInput) error {
	suggestions := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		switch f {
		case "server":
			suggestions = append(suggestions, "Pass --server <url> or set GRAFANA_SERVER")
		case "grafana-auth":
			suggestions = append(suggestions, "Pass --token <token> for a service account token, or configure TLS client certs for mTLS auth (GRAFANA_TLS_CERT_FILE / GRAFANA_TLS_KEY_FILE env vars, or gcx config set contexts.<ctx>.grafana.tls.cert-file ...)")
		case "cloud-token":
			suggestions = append(suggestions, "Pass --cloud-token <token> to enable Cloud features, or --yes to skip")
		default:
			suggestions = append(suggestions, "Provide --"+strings.ReplaceAll(f, "_", "-"))
		}
	}

	details := "Missing required fields: " + strings.Join(e.Fields, ", ")
	if e.Hint != "" {
		details += "\n" + e.Hint
	}

	return fail.DetailedError{
		Summary:     "Login requires additional input",
		Details:     details,
		Suggestions: suggestions,
	}
}

// structuredClarificationError converts ErrNeedClarification to a fail.DetailedError.
func structuredClarificationError(e *login.ErrNeedClarification) error {
	switch e.Field {
	case "allow-override":
		return fail.DetailedError{
			Summary: "Login would overwrite an existing context",
			Details: e.Question,
			Suggestions: []string{
				"Pass --allow-server-override to confirm the server change non-interactively",
				"Pick a different positional context name to create a new one",
			},
		}
	case "save-unvalidated":
		return fail.DetailedError{
			Summary: "Connectivity validation failed",
			Details: e.Question,
			Suggestions: []string{
				"Re-run interactively to confirm saving without validation",
				"Check server URL, network, and credentials",
			},
		}
	default:
		return fail.DetailedError{
			Summary: "Login requires clarification",
			Details: fmt.Sprintf("%s\nChoices: %s", e.Question, strings.Join(e.Choices, ", ")),
			Suggestions: []string{
				"Pass --cloud to force Grafana Cloud target",
				"Pass --yes to default to on-premises",
			},
		}
	}
}

// printModeHeader writes a one- or two-line status banner so the user
// can see what the upcoming login will do before any prompts appear.
// It routes to stderr so that `-o json`/`-o yaml` leave stdout clean for
// downstream parsing; terminal users still see the banner alongside normal
// output because stderr is typically merged into the visible stream.
func printModeHeader(cmd *cobra.Command, cfg config.Config, contextName string, sourceCtx *config.Context) {
	w := cmd.ErrOrStderr()
	switch {
	case sourceCtx != nil && sourceCtx.Grafana != nil && sourceCtx.Grafana.Server != "":
		// Re-auth path. Guard on non-empty Server so the synthetic default
		// context injected by LoadConfigTolerant (empty Server) doesn't print
		// a misleading "Refreshing context \"default\" (server: )" banner on
		// first-time setup — that case falls through to the new-context arm.
		name := contextName
		if name == "" {
			name = cfg.CurrentContext
		}
		fmt.Fprintf(w, "Refreshing context %q (server: %s)\n\n",
			name, sourceCtx.Grafana.Server)
	case contextName != "":
		// Creating a new named context.
		fmt.Fprintf(w, "Creating new context %q\n", contextName)
		if names := existingContextNames(cfg); len(names) > 0 {
			fmt.Fprintf(w, "Existing contexts: %s\n", strings.Join(names, ", "))
		}
		fmt.Fprintln(w)
	default:
		// First-time setup: no name yet, no current context.
		fmt.Fprintln(w, "First-time setup: no existing context configured.")
		fmt.Fprintln(w)
	}
}

// existingContextNames returns a sorted list of context names in the config.
func existingContextNames(cfg config.Config) []string {
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// printResult converts the login.Result into a LoginResult and writes it to
// stdout using the configured output codec. Advisory prose (the CAP token
// note) is routed to stderr so that JSON/YAML consumers receive clean,
// parseable output on stdout.
func printResult(cmd *cobra.Command, ioOpts *cmdio.Options, server string, result login.Result) error {
	if server == "" {
		server = result.ContextName
	}
	lr := LoginResult{
		ContextName:    result.ContextName,
		Server:         server,
		AuthMethod:     result.AuthMethod,
		Cloud:          result.IsCloud,
		GrafanaVersion: result.GrafanaVersion,
		StackSlug:      result.StackSlug,
		HasCloudToken:  result.HasCloudToken,
	}
	if err := ioOpts.Encode(cmd.OutOrStdout(), lr); err != nil {
		return err
	}
	// Route advisory prose to stderr. This keeps stdout parseable for
	// json/yaml consumers while still surfacing the CAP-token guidance
	// to humans (terminals typically merge stderr into the visible stream).
	if result.IsCloud && !result.HasCloudToken {
		ew := cmd.ErrOrStderr()
		fmt.Fprintln(ew)
		fmt.Fprintln(ew, "Note: Cloud API commands require a Cloud Access Policy (CAP) token.")
		fmt.Fprintln(ew, "See: https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/")
		fmt.Fprintf(ew, "Run 'gcx login --context %s --cloud-token <token>' to add one.\n", result.ContextName)
	}
	return nil
}

// loginTextCodec renders LoginResult as the human-friendly multi-line summary
// that was previously printed inline. It's registered as the "text" codec and
// is the default for interactive terminals.
type loginTextCodec struct{}

func (c *loginTextCodec) Format() format.Format { return "text" }

func (c *loginTextCodec) Encode(w io.Writer, value any) error {
	lr, ok := value.(LoginResult)
	if !ok {
		return fmt.Errorf("login text codec: unsupported type %T", value)
	}
	fmt.Fprintf(w, "Logged in to %s\n", lr.Server)
	fmt.Fprintf(w, "  Context:     %s\n", lr.ContextName)
	fmt.Fprintf(w, "  Auth method: %s\n", lr.AuthMethod)
	if lr.GrafanaVersion != "" {
		fmt.Fprintf(w, "  Version:     %s\n", lr.GrafanaVersion)
	}
	if lr.Cloud {
		fmt.Fprintln(w, "  Grafana Cloud: yes")
		if lr.StackSlug != "" {
			fmt.Fprintf(w, "  Stack:       %s\n", lr.StackSlug)
		}
	} else {
		fmt.Fprintln(w, "  Grafana Cloud: no")
	}
	return nil
}

func (c *loginTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("login text codec does not support decoding")
}
