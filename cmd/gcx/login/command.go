package login

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	internalauth "github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/login"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

type loginOpts struct {
	Config      configcmd.Options
	Server      string
	Token       string
	CloudToken  string
	CloudAPIURL string
	Cloud       bool
	Yes         bool
}

func (opts *loginOpts) setup(flags *pflag.FlagSet) {
	opts.Config.BindFlags(flags)
	flags.StringVar(&opts.Server, "server", "", "Grafana server URL (e.g. https://my-stack.grafana.net)")
	flags.StringVar(&opts.Token, "token", "", "Grafana service account token")
	flags.StringVar(&opts.CloudToken, "cloud-token", "", "Grafana Cloud API token (enables Cloud management features)")
	flags.StringVar(&opts.CloudAPIURL, "cloud-api-url", "", "Override Grafana Cloud API URL")
	flags.BoolVar(&opts.Cloud, "cloud", false, "Force Grafana Cloud target (skip auto-detection)")
	flags.BoolVar(&opts.Yes, "yes", false, "Non-interactive: skip optional prompts and use defaults")
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
			return runLogin(cmd, opts, args)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func runLogin(cmd *cobra.Command, flags *loginOpts, args []string) error {
	ctx := cmd.Context()

	// Positional arg takes precedence; --context flag is compat.
	// Error if both are given to prevent silent confusion.
	var contextName string
	switch {
	case len(args) == 1 && flags.Config.Context != "":
		return fail.DetailedError{
			Summary: "conflicting context specification",
			Details: fmt.Sprintf(
				"Positional argument %q and --context=%q both specified. Use one.",
				args[0], flags.Config.Context,
			),
			Suggestions: []string{
				"Drop --context and use the positional form: gcx login " + args[0],
			},
		}
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

	opts := login.Options{
		Server:        flags.Server,
		ContextName:   contextName,
		GrafanaToken:  flags.Token,
		CloudToken:    flags.CloudToken,
		CloudAPIURL:   flags.CloudAPIURL,
		Yes:           flags.Yes,
		Cloud:         flags.Cloud,
		ConfigSource:  flags.Config.ConfigSource(),
		StagedContext: &config.Context{}, // enables Run() to cache across sentinel retries
		NewAuthFlow: func(server string, ao internalauth.Options) login.AuthFlow {
			return internalauth.NewFlow(server, ao)
		},
		Writer: cmd.ErrOrStderr(),
	}

	if flags.Cloud {
		opts.Target = login.TargetCloud
	}

	for {
		result, err := login.Run(ctx, opts)
		if err == nil {
			printResult(cmd, flags.Server, result)
			return nil
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
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
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
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
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
			form := huh.NewForm(huh.NewGroup(
				huh.NewInput().
					Title("Grafana server URL").
					Description("e.g. https://my-stack.grafana.net").
					Validate(func(s string) error {
						if s == "" {
							return errors.New("server URL is required")
						}
						return nil
					}).
					Value(&opts.Server),
			))
			if err := form.Run(); err != nil {
				return err
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
func askGrafanaAuth(opts *login.Options, existingToken string) error {
	authMethod := "token"
	methodForm := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Authentication method").
			Options(
				huh.NewOption("Service account token (requires permissions for managing service accounts)", "token"),
				huh.NewOption("OAuth (browser) — experimental, some functionality may not work; fall back to a service account token if you hit auth issues", "oauth"),
			).
			Value(&authMethod),
	))
	if err := methodForm.Run(); err != nil {
		return err
	}
	if authMethod == "oauth" {
		opts.UseOAuth = true
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
			opts.Cloud = true
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
			suggestions = append(suggestions, "Pass --token <token> for a service account token")
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
				"Pass --yes to confirm the override non-interactively",
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
func printModeHeader(cmd *cobra.Command, cfg config.Config, contextName string, sourceCtx *config.Context) {
	w := cmd.OutOrStdout()
	switch {
	case sourceCtx != nil && sourceCtx.Grafana != nil:
		// Re-auth path.
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

// printResult writes the post-login summary to stdout.
func printResult(cmd *cobra.Command, server string, result login.Result) {
	w := cmd.OutOrStdout()
	if server == "" {
		server = result.ContextName
	}
	fmt.Fprintf(w, "Logged in to %s\n", server)
	fmt.Fprintf(w, "  Context:     %s\n", result.ContextName)
	fmt.Fprintf(w, "  Auth method: %s\n", result.AuthMethod)
	if result.GrafanaVersion != "" {
		fmt.Fprintf(w, "  Version:     %s\n", result.GrafanaVersion)
	}
	if result.IsCloud {
		fmt.Fprintln(w, "  Grafana Cloud: yes")
		if result.StackSlug != "" {
			fmt.Fprintf(w, "  Stack:       %s\n", result.StackSlug)
		}
		if !result.HasCloudToken {
			fmt.Fprintf(w, "\nNote: Cloud API commands require a CAP token.\n")
			fmt.Fprintf(w, "Run 'gcx login --context %s' to add one.\n", result.ContextName)
		}
	} else {
		fmt.Fprintln(w, "  Grafana Cloud: no")
	}
}
