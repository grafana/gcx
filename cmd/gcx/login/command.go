package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	internalauth "github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
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
	OAuth               bool
	Cloud               bool
	Yes                 bool
	AllowServerOverride bool
	OAuthCallbackPort   int
	OrgID               int
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
	flags.BoolVar(&opts.OAuth, "oauth", false, "Authenticate via browser-based OAuth (recommended for Grafana Cloud). Works non-interactively and in agent mode: opens a browser for the user to approve.")
	flags.BoolVar(&opts.Cloud, "cloud", false, "Force Grafana Cloud target (skip auto-detection)")
	flags.BoolVar(&opts.Yes, "yes", false, "Non-interactive: skip optional prompts and use defaults")
	flags.BoolVar(&opts.AllowServerOverride, "allow-server-override", false, "Allow re-pointing an existing context at a different server URL")
	flags.IntVar(&opts.OAuthCallbackPort, "oauth-callback-port", 0, "Fixed local port for the OAuth callback server (default: auto-pick from 54321-54399). Useful when only specific ports are forwarded between a remote host and your browser")
	flags.IntVar(&opts.OrgID, "org-id", 0, "Grafana organization ID (defaults to 1 for on-prem)")
}

// Validate checks opts and args for internal consistency before runLogin executes.
// Returns an error if a positional CONTEXT_NAME argument is combined with the
// --context flag (they're mutually exclusive to prevent silent confusion).
// Also validates the output codec options (format name, --json flag shape).
func (opts *loginOpts) Validate(args []string) error {
	if len(args) == 1 && opts.Config.Context != "" {
		return gcxerrors.DetailedError{
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
	if opts.OAuth && opts.Token != "" {
		return gcxerrors.DetailedError{
			Summary: "conflicting authentication methods",
			Details: "--oauth and --token are mutually exclusive. OAuth authenticates via browser; --token uses a service account token.",
			Suggestions: []string{
				"Use --oauth for browser-based login, or --token <token> for a service account token",
			},
		}
	}
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.OAuthCallbackPort < 0 || opts.OAuthCallbackPort > 65535 {
		return gcxerrors.DetailedError{
			Summary: "invalid --oauth-callback-port",
			Details: fmt.Sprintf("Port must be between 1 and 65535 (or 0 to auto-pick); got %d.", opts.OAuthCallbackPort),
		}
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
first-time setup if no current context is configured.

Auth sources (for non-interactive use):
  --oauth        Browser-based OAuth (recommended for Grafana Cloud). Opens a browser for the user to approve; works in agent mode.
  --token        Grafana service-account token (created inside the Grafana instance).
                 See: ` + docs.ServiceAccounts + `
  --cloud-token  Grafana Cloud access-policy token (created at grafana.com).
                 See: ` + docs.AccessPolicies,
		Example: `  gcx login
  gcx login prod
  gcx login prod --server https://prod.grafana.net
  gcx login prod --server https://prod.grafana.net --oauth
  gcx login --yes prod --token glsa_xxx
  gcx login --yes --server https://localhost:3000 --token glsa_xxx`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Token = strings.TrimSpace(opts.Token)
			opts.CloudToken = strings.TrimSpace(opts.CloudToken)
			if err := opts.Validate(args); err != nil {
				return err
			}
			return runLogin(cmd, opts, args)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

//nolint:gocyclo,maintidx // Login deliberately keeps trust preflight, auth selection, and retry setup in one auditable flow.
func runLogin(cmd *cobra.Command, flags *loginOpts, args []string) error {
	ctx := cmd.Context()
	preflightTarget, targetIsDeterministic, err := flags.Config.PreflightLoginMutationTarget()
	if err != nil {
		return err
	}
	if targetIsDeterministic && preflightTarget.Type == "local" &&
		(flags.OAuth || credentialProvided(flags.Token, "GRAFANA_TOKEN") ||
			credentialProvided(flags.CloudToken, "GRAFANA_CLOUD_TOKEN")) {
		return autoLocalFreshCredentialError(preflightTarget)
	}

	// Positional arg takes precedence; --context flag is compat.
	// Mutual exclusion is enforced earlier in loginOpts.Validate.
	var contextName string
	switch {
	case len(args) == 1:
		contextName = args[0]
	default:
		contextName = flags.Config.Context
	}
	// Canonicalize an explicit --server before it participates in context-name
	// inference. A bare host means HTTPS, so it must select the same context as
	// the equivalent persisted https:// URL.
	flags.Server = login.NormalizeServerURL(flags.Server)

	cfg, sourceCtx, contextName, err := loadLoginSourceContext(ctx, flags, contextName)
	if err != nil {
		return err
	}
	mutationTarget, err := flags.Config.PlanLoginMutation(cfg, contextName, config.LoginMutationUnified)
	if err != nil {
		return err
	}
	var autoLocalTarget *config.ConfigSource
	if mutationTarget.Type == "local" {
		target := mutationTarget
		autoLocalTarget = &target
		if flags.OAuth || credentialProvided(flags.Token, "GRAFANA_TOKEN") ||
			credentialProvided(flags.CloudToken, "GRAFANA_CLOUD_TOKEN") {
			return autoLocalFreshCredentialError(target)
		}
	}
	flags.Server = requestedLoginServer(flags.Server, sourceCtx)
	mutationSource := config.ExplicitConfigFile(mutationTarget.Path)
	ctx = flags.Config.LoginMutationContext(ctx, mutationTarget)
	cmd.SetContext(ctx)
	persistedSourceConfig, persistedSourceCtx, err := loadPersistedLoginSource(ctx, mutationSource, contextName)
	if err != nil {
		return err
	}
	if len(cfg.Sources) > 1 {
		if err := config.VerifyLoginMutationBindings(
			mutationTarget,
			contextName,
			sourceCtx,
			persistedSourceCtx,
			config.LoginMutationUnified,
		); err != nil {
			return err
		}
	}
	credentialSourceCtx := persistedSourceCtx
	if credentialSourceCtx == nil {
		credentialSourceCtx = sourceCtx
	}
	cloudMutationSafety, err := cfg.LoginCloudMutationSafety(contextName, mutationTarget)
	if err != nil {
		return err
	}
	loginMutationGuard := persistedSourceConfig.NewLoginMutationGuard(contextName, config.LoginMutationUnified)
	if mutationTarget.Type != "explicit" {
		loginMutationGuard, err = loginMutationGuard.WithDiscoverySnapshot(&cfg)
		if err != nil {
			return err
		}
	}

	printModeHeader(cmd, cfg, contextName, sourceCtx)

	isInteractive := term.IsTerminal(int(os.Stdin.Fd())) &&
		!flags.Yes &&
		!agent.IsAgentMode()
	grafanaTokenExplicit := credentialProvided(flags.Token, "GRAFANA_TOKEN")
	cloudTokenExplicit := credentialProvided(flags.CloudToken, "GRAFANA_CLOUD_TOKEN")

	// Non-interactive callers (agent mode, --yes, piped stdin, CI) can't answer
	// the auth prompt, so fall back to credentials resolved from the selected
	// raw owner. resolveNonInteractiveTokens reads GRAFANA_TOKEN /
	// GRAFANA_CLOUD_TOKEN directly before considering that stored context, so a
	// headless login still consumes explicit runtime credentials without ever
	// copying a resolved credential out of the merged layered view. Interactive
	// callers keep the prompt flow — which offers "keep existing token" and
	// auth-method switching — so we leave their flags untouched.
	flags.Token, flags.CloudToken = resolveNonInteractiveTokens(
		flags.Token,
		flags.CloudToken,
		credentialSourceCtx,
		isInteractive,
		flags.OAuth,
	)
	storedGrafanaTokenBlocked := storedGrafanaTokenDestinationChanged(
		flags.Server,
		persistedSourceCtx,
		sourceCtx,
		isInteractive,
		grafanaTokenExplicit,
	)
	if storedGrafanaTokenBlocked {
		// Never present a credential loaded for one server to another server. A
		// later write-time binding check is too late: validation sends the token.
		flags.Token = ""
	}

	// Re-auth default: a non-interactive `gcx login <ctx>` on a context that
	// previously authenticated via OAuth defaults to OAuth instead of failing
	// for missing grafana-auth. Runs after token resolution so a stored token
	// still takes precedence.
	flags.OAuth = defaultOAuthFromContext(flags.OAuth, flags.Token, persistedSourceCtx, isInteractive)
	if autoLocalTarget != nil && flags.OAuth {
		return autoLocalFreshCredentialError(*autoLocalTarget)
	}
	if storedGrafanaTokenBlocked && !flags.OAuth {
		return grafanaDestinationChangeAuthError(persistedSourceCtx.Grafana.Server, flags.Server)
	}

	// Carry existing TLS settings into the login flow so that mTLS keeps
	// working on re-auth without requiring the user to re-specify certs.
	var storedTLS *config.TLS
	if persistedSourceCtx != nil && persistedSourceCtx.Grafana != nil && persistedSourceCtx.Grafana.TLS != nil &&
		!persistedSourceCtx.Grafana.TLS.IsEmpty() {
		storedTLS = persistedSourceCtx.Grafana.TLS
		// Advisory: when --allow-server-override re-points the context at a
		// different server, the existing TLS client cert will be presented to
		// the new server. This is gated by explicit user opt-in.
		if flags.AllowServerOverride && flags.Server != "" &&
			persistedSourceCtx.Grafana.Server != "" &&
			flags.Server != persistedSourceCtx.Grafana.Server {
			logging.FromContext(cmd.Context()).Warn("reusing existing TLS client certificate for a different server",
				"previous_server", persistedSourceCtx.Grafana.Server,
				"new_server", flags.Server,
			)
		}
	}
	envTLS := loginTLSFromEnvironment()
	runtimeTLS := storedTLS
	if sourceCtx != nil && sourceCtx.Grafana != nil {
		runtimeTLS = sourceCtx.Grafana.TLS
	} else if envTLS != nil {
		runtimeTLS = envTLS
	}
	runtimeTLSFromEnvironment := grafanaTLSEnvironmentOverridePresent() &&
		!config.GrafanaBearerCredentialDestinationMatches(
			&config.GrafanaConfig{TLS: storedTLS},
			&config.GrafanaConfig{TLS: runtimeTLS},
		)
	runtimeProxyEndpoint := runtimeGrafanaProxyEndpoint(sourceCtx)
	storedProxyEndpoint := storedGrafanaProxyEndpoint(persistedSourceCtx)
	runtimeProxyFromEnvironment := grafanaProxyEnvironmentOverridePresent() &&
		!config.GrafanaBearerCredentialDestinationMatches(
			&config.GrafanaConfig{ProxyEndpoint: storedProxyEndpoint},
			&config.GrafanaConfig{ProxyEndpoint: runtimeProxyEndpoint},
		)
	runtimeDestinationFromEnvironment := runtimeTLSFromEnvironment || runtimeProxyFromEnvironment

	cloudAPIURL, cloudOAuthURL, err := cloudLoginEndpoints(flags, persistedSourceCtx, cmd.Flags().Changed("cloud-api-url"))
	if err != nil {
		return err
	}
	var existingGrafanaAuthMethod string
	if credentialSourceCtx != nil && credentialSourceCtx.Grafana != nil {
		// This is only a pre-auth transport safety hint. Interactive login may
		// select a different final method later, but target detection must not
		// present a stale client certificate merely because the prompt has not
		// run yet. Persisted explicit mTLS remains the sole method that keeps the
		// existing client identity on that probe.
		existingGrafanaAuthMethod = credentialSourceCtx.Grafana.AuthMethod
	}

	opts := login.Options{
		Inputs: login.Inputs{
			Server:                      flags.Server,
			ContextName:                 contextName,
			GrafanaToken:                flags.Token,
			ExistingGrafanaAuthMethod:   existingGrafanaAuthMethod,
			CloudToken:                  flags.CloudToken,
			CloudAPIURL:                 cloudAPIURL,
			CloudOAuthURL:               cloudOAuthURL,
			UseOAuth:                    flags.OAuth,
			OAuthCallbackPort:           flags.OAuthCallbackPort,
			Yes:                         flags.Yes,
			OrgID:                       flags.OrgID,
			Writer:                      cmd.ErrOrStderr(),
			TLS:                         runtimeTLS,
			PreserveStoredTLS:           true,
			StoredTLS:                   storedTLS,
			PreserveStoredProxyEndpoint: grafanaProxyEnvironmentOverridePresent(),
			RuntimeProxyEndpoint:        runtimeProxyEndpoint,
			StoredProxyEndpoint:         storedProxyEndpoint,
		},
		Hooks: login.Hooks{
			ConfigSource:        mutationSource,
			CloudMutationSafety: cloudMutationSafety,
			LoginMutationGuard:  loginMutationGuard,
			NewAuthFlow: func(server string, ao internalauth.Options) login.AuthFlow {
				return internalauth.NewFlow(server, ao)
			},
		},
		RetryState: login.RetryState{
			StagedContext: &config.Context{}, // enables Run() to cache across sentinel retries
		},
	}
	if err := reuseNonInteractiveCloudCredential(&opts, cloudTokenExplicit, credentialSourceCtx, isInteractive); err != nil {
		return err
	}

	if flags.Cloud {
		opts.Target = login.TargetCloud
	}
	if flags.AllowServerOverride {
		opts.AllowOverride = true
	}
	if err := preflightServerOverride(&opts, persistedSourceCtx, isInteractive); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
			return nil
		}
		return err
	}

	err = runLoginLoop(
		cmd,
		flags,
		&opts,
		credentialSourceCtx,
		sourceCtx,
		isInteractive,
		autoLocalTarget,
		runtimeDestinationFromEnvironment,
	)
	var runtimeOnlyDestination *login.RuntimeOnlyBearerDestinationError
	if errors.As(err, &runtimeOnlyDestination) {
		return runtimeOnlyBearerDestinationError(mutationTarget, persistedSourceCtx, &opts, runtimeOnlyDestination)
	}
	return err
}

func autoLocalFreshCredentialError(target config.ConfigSource) error {
	return gcxerrors.DetailedError{
		Summary: "Refusing fresh credentials for an auto-discovered repository config",
		Details: fmt.Sprintf(
			"The repository config %s was discovered automatically. Its server, proxy, TLS, Cloud endpoints, and routing are repository-controlled, so gcx will only reuse credentials already bound to that unchanged config.",
			target.Path,
		),
		Suggestions: []string{
			fmt.Sprintf("Review the file, then rerun with --config %s to supply a fresh credential", target.Path),
			fmt.Sprintf("Or set %s=%s after reviewing the file", config.ConfigFileEnvVar, target.Path),
		},
	}
}

func enforceAutoLocalCredentialPolicy(opts *login.Options, sourceCtx *config.Context, target config.ConfigSource) error {
	if opts.UseOAuth || opts.UseCloudInstanceSelector {
		return autoLocalFreshCredentialError(target)
	}
	if opts.GrafanaToken != "" {
		if sourceCtx == nil || sourceCtx.Grafana == nil || sourceCtx.Grafana.APIToken == "" ||
			opts.GrafanaToken != sourceCtx.Grafana.APIToken ||
			login.NormalizeServerURL(opts.Server) != login.NormalizeServerURL(sourceCtx.Grafana.Server) {
			return autoLocalFreshCredentialError(target)
		}
	}
	if opts.CloudToken != "" {
		if sourceCtx == nil || sourceCtx.CloudEntry == nil {
			return autoLocalFreshCredentialError(target)
		}
		kind, ok := cloudCredentialKind(sourceCtx.CloudEntry)
		var token string
		if ok {
			token, _ = sourceCtx.CloudEntry.ResolveToken()
		}
		if !ok || token == "" || token != opts.CloudToken || kind != opts.CloudCredentialKind ||
			cloudEndpointRequestDiffers(opts, sourceCtx.CloudEntry, sourceContextServer(sourceCtx)) {
			return autoLocalFreshCredentialError(target)
		}
	}
	return nil
}

func runLoginLoop(
	cmd *cobra.Command,
	flags *loginOpts,
	opts *login.Options,
	credentialSourceCtx *config.Context,
	runtimeSourceCtx *config.Context,
	isInteractive bool,
	autoLocalTarget *config.ConfigSource,
	runtimeDestinationFromEnvironment bool,
) error {
	for {
		if autoLocalTarget != nil {
			if err := enforceAutoLocalCredentialPolicy(opts, credentialSourceCtx, *autoLocalTarget); err != nil {
				return err
			}
		}
		result, err := login.Run(cmd.Context(), opts)
		if err == nil {
			if shouldWarnRuntimeOnlyDestination(runtimeDestinationFromEnvironment, result) {
				warnRuntimeOnlyDestination(cmd.ErrOrStderr())
			}
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
			if formErr := askForInput(cmd.Context(), needInput, opts, credentialSourceCtx, runtimeSourceCtx, autoLocalTarget); formErr != nil {
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
			if formErr := askForClarification(needClarification, opts); formErr != nil {
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

func warnRuntimeOnlyDestination(w io.Writer) {
	fmt.Fprintln(w, "Warning: destination settings from GRAFANA_PROXY_ENDPOINT or GRAFANA_TLS_* were applied only to this login and were not written to config. Persist those settings before using this context without the environment overrides.")
}

func shouldWarnRuntimeOnlyDestination(changed bool, result login.Result) bool {
	return changed && result.AuthMethod == "mtls"
}

type destinationRecoveryCommand struct {
	ConfigFile string
	Path       string
	Value      string
}

func (command destinationRecoveryCommand) args() []string {
	return []string{"set", "--config", command.ConfigFile, command.Path, command.Value}
}

func (command destinationRecoveryCommand) String() string {
	return "gcx config set --config " + shellQuote(command.ConfigFile) + " " +
		shellQuote(command.Path) + " " + shellQuote(command.Value)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func runtimeOnlyBearerDestinationError(
	target config.ConfigSource,
	persisted *config.Context,
	opts *login.Options,
	cause *login.RuntimeOnlyBearerDestinationError,
) error {
	if cause.OAuthIssuerProxyMismatch {
		return gcxerrors.DetailedError{
			Summary: "GRAFANA_PROXY_ENDPOINT conflicts with the OAuth login destination",
			Details: fmt.Sprintf(
				"The OAuth issuer selected proxy endpoint %q, but this process forced %q through GRAFANA_PROXY_ENDPOINT. Persisting the environment value would not resolve that conflict, so gcx stopped before presenting or saving the OAuth credential.",
				cause.OAuthIssuerProxyEndpoint, cause.RuntimeProxyEndpoint,
			),
			Parent: cause,
			Suggestions: []string{
				"Unset the conflicting override: unset GRAFANA_PROXY_ENDPOINT",
				"Then rerun the same gcx login --oauth command",
			},
		}
	}
	commands, environmentKeys := runtimeOnlyDestinationRecoveryCommands(target.Path, persisted, opts)
	if runtimeOnlyDestinationRecoveryNeedsEditor(persisted, opts) {
		return runtimeOnlyDestinationEditorRecoveryError(target.Path, persisted, opts, environmentKeys, cause)
	}
	suggestions := make([]string, 0, len(commands)+2)
	for _, command := range commands {
		suggestions = append(suggestions, command.String())
	}
	suggestions = append(suggestions,
		"Then rerun the same gcx login command",
		"Or unset the listed GRAFANA_* overrides and retry if they were not intended for this context",
	)

	contextName := opts.ContextName
	if contextName == "" {
		contextName = config.ContextNameFromServerURL(opts.Server)
	}
	return gcxerrors.DetailedError{
		Summary: "Login destination settings must be persisted before saving this credential",
		Details: fmt.Sprintf(
			"Context %q uses runtime-only destination settings from %s. gcx stopped before presenting or saving the token because the next process would reject a credential bound to settings that are absent from config. Run these commands to persist the exact server, proxy/TLS settings, and context binding, then retry login.",
			contextName, strings.Join(environmentKeys, ", "),
		),
		Parent:      cause,
		Suggestions: suggestions,
	}
}

func runtimeOnlyDestinationRecoveryCommands(
	configFile string,
	persisted *config.Context,
	opts *login.Options,
) ([]destinationRecoveryCommand, []string) {
	contextName, stackName, needsContextBinding := runtimeOnlyDestinationRecoveryNames(persisted, opts)

	prefix := "stacks." + stackName + ".grafana."
	commands := []destinationRecoveryCommand{{
		ConfigFile: configFile,
		Path:       prefix + "server",
		Value:      opts.Server,
	}}
	environmentKeys := make([]string, 0, 4)
	if _, ok := os.LookupEnv("GRAFANA_PROXY_ENDPOINT"); ok {
		environmentKeys = append(environmentKeys, "GRAFANA_PROXY_ENDPOINT")
		commands = append(commands, destinationRecoveryCommand{
			ConfigFile: configFile,
			Path:       prefix + "proxy-endpoint",
			Value:      opts.RuntimeProxyEndpoint,
		})
	}

	tlsValues := map[string]string{}
	if opts.TLS != nil {
		tlsValues = map[string]string{
			"GRAFANA_TLS_CERT_FILE": opts.TLS.CertFile,
			"GRAFANA_TLS_KEY_FILE":  opts.TLS.KeyFile,
			"GRAFANA_TLS_CA_FILE":   opts.TLS.CAFile,
		}
	}
	for _, field := range []struct {
		EnvKey string
		Path   string
	}{
		{EnvKey: "GRAFANA_TLS_CERT_FILE", Path: "tls.cert-file"},
		{EnvKey: "GRAFANA_TLS_KEY_FILE", Path: "tls.key-file"},
		{EnvKey: "GRAFANA_TLS_CA_FILE", Path: "tls.ca-file"},
	} {
		if _, ok := os.LookupEnv(field.EnvKey); !ok {
			continue
		}
		environmentKeys = append(environmentKeys, field.EnvKey)
		commands = append(commands, destinationRecoveryCommand{
			ConfigFile: configFile,
			Path:       prefix + field.Path,
			Value:      tlsValues[field.EnvKey],
		})
	}
	if needsContextBinding {
		commands = append(commands, destinationRecoveryCommand{
			ConfigFile: configFile,
			Path:       "contexts." + contextName + ".stack",
			Value:      stackName,
		})
	}
	return commands, environmentKeys
}

func runtimeOnlyDestinationRecoveryNames(persisted *config.Context, opts *login.Options) (string, string, bool) {
	contextName := opts.ContextName
	if contextName == "" {
		contextName = config.ContextNameFromServerURL(opts.Server)
	}
	stackName := contextName
	if persisted != nil && persisted.Stack != "" {
		stackName = persisted.Stack
	}
	needsContextBinding := persisted == nil || persisted.Stack == ""
	return contextName, stackName, needsContextBinding
}

func runtimeOnlyDestinationRecoveryNeedsEditor(persisted *config.Context, opts *login.Options) bool {
	contextName, stackName, needsContextBinding := runtimeOnlyDestinationRecoveryNames(persisted, opts)
	return strings.Contains(stackName, ".") || (needsContextBinding && strings.Contains(contextName, "."))
}

func runtimeOnlyDestinationEditorRecoveryError(
	configFile string,
	persisted *config.Context,
	opts *login.Options,
	environmentKeys []string,
	cause *login.RuntimeOnlyBearerDestinationError,
) error {
	contextName, stackName, needsContextBinding := runtimeOnlyDestinationRecoveryNames(persisted, opts)
	fields := []string{fmt.Sprintf("stack key %q: grafana.server=%q", stackName, opts.Server)}
	if _, ok := os.LookupEnv("GRAFANA_PROXY_ENDPOINT"); ok {
		fields = append(fields, fmt.Sprintf("grafana.proxy-endpoint=%q", opts.RuntimeProxyEndpoint))
	}
	if opts.TLS != nil {
		for _, field := range []struct {
			EnvKey string
			Name   string
			Value  string
		}{
			{EnvKey: "GRAFANA_TLS_CERT_FILE", Name: "grafana.tls.cert-file", Value: opts.TLS.CertFile},
			{EnvKey: "GRAFANA_TLS_KEY_FILE", Name: "grafana.tls.key-file", Value: opts.TLS.KeyFile},
			{EnvKey: "GRAFANA_TLS_CA_FILE", Name: "grafana.tls.ca-file", Value: opts.TLS.CAFile},
		} {
			if _, ok := os.LookupEnv(field.EnvKey); ok {
				fields = append(fields, fmt.Sprintf("%s=%q", field.Name, field.Value))
			}
		}
	}
	if needsContextBinding {
		fields = append(fields, fmt.Sprintf("context key %q: stack=%q", contextName, stackName))
	}

	return gcxerrors.DetailedError{
		Summary: "Login destination settings require editor-based recovery",
		Details: fmt.Sprintf(
			"Context or stack names containing dots cannot be addressed by gcx config set's literal dot-path grammar. Runtime-only settings from %s were not persisted. In the selected config, persist these values: %s.",
			strings.Join(environmentKeys, ", "), strings.Join(fields, "; "),
		),
		Parent: cause,
		Suggestions: []string{
			"If the explicit config does not exist yet, initialize it: gcx config set --config " + shellQuote(configFile) + " version '1'",
			"Open the selected config: gcx config edit --config " + shellQuote(configFile),
			"Then rerun the same gcx login command",
			"Or unset the listed GRAFANA_* overrides and retry if they were not intended for this context",
		},
	}
}

func loadLoginSourceContext(ctx context.Context, flags *loginOpts, contextName string) (config.Config, *config.Context, string, error) {
	// Select from the persisted view before applying GRAFANA_* overlays. Applying
	// an unnamed GRAFANA_SERVER to the current context first can reject that
	// context's stored credential (or make it appear to be the mutation target)
	// before we have inferred the server-derived context the user actually chose.
	cfg, err := config.LoadLayered(ctx, flags.Config.ConfigFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return config.Config{}, nil, contextName, err
	}
	selectionServer := requestedLoginServer(flags.Server, nil)
	sourceCtx, resolvedName := resolveSourceContext(cfg, contextName, selectionServer)
	if sourceCtx == nil {
		return cfg, sourceCtx, resolvedName, nil
	}

	// Reload with the inferred context selected before environment overrides so
	// deferred keychain resolution and runtime credentials apply to the actual
	// target, not whichever context happened to be current in the file.
	selected := flags.Config
	selected.Context = resolvedName
	cfg, err = selected.LoadConfigTolerant(ctx)
	if err != nil {
		return config.Config{}, nil, resolvedName, err
	}
	return cfg, cfg.Contexts[resolvedName], resolvedName, nil
}

func loadPersistedLoginSource(ctx context.Context, source config.Source, contextName string) (config.Config, *config.Context, error) {
	cfg, err := config.Load(ctx, source)
	if errors.Is(err, os.ErrNotExist) {
		// A new explicit --config path is a valid login target; persistContext
		// creates it after authentication succeeds.
		return cfg, nil, nil // nil context intentionally means no persisted context yet.
	}
	if err != nil {
		return config.Config{}, nil, err
	}
	// LoadLayered resolves only its effective current context. Resolve the
	// selected target explicitly before reading credentials so a non-current
	// context never exposes a deferred keychain reference to the login flow.
	cfg.ResolveContext(contextName)
	return cfg, cfg.Contexts[contextName], nil
}

func loadPersistedLoginSourceContext(ctx context.Context, source config.Source, contextName string) (*config.Context, error) {
	_, persisted, err := loadPersistedLoginSource(ctx, source, contextName)
	return persisted, err
}

func credentialProvided(flagValue, envKey string) bool {
	if strings.TrimSpace(flagValue) != "" {
		return true
	}
	envValue, ok := os.LookupEnv(envKey)
	return ok && !config.IsBlankCredentialEnvironmentOverride(envKey, envValue)
}

func requestedLoginServer(flagServer string, sourceCtx *config.Context) string {
	if flagServer != "" {
		return login.NormalizeServerURL(flagServer)
	}
	if envServer := strings.TrimSpace(os.Getenv("GRAFANA_SERVER")); envServer != "" {
		return login.NormalizeServerURL(envServer)
	}
	if sourceCtx != nil && sourceCtx.Grafana != nil {
		return login.NormalizeServerURL(sourceCtx.Grafana.Server)
	}
	return ""
}

func loginTLSFromEnvironment() *config.TLS {
	tlsConfig := &config.TLS{
		CertFile: strings.TrimSpace(os.Getenv("GRAFANA_TLS_CERT_FILE")),
		KeyFile:  strings.TrimSpace(os.Getenv("GRAFANA_TLS_KEY_FILE")),
		CAFile:   strings.TrimSpace(os.Getenv("GRAFANA_TLS_CA_FILE")),
	}
	if tlsConfig.IsEmpty() {
		return nil
	}
	return tlsConfig
}

func grafanaTLSEnvironmentOverridePresent() bool {
	for _, key := range []string{"GRAFANA_TLS_CERT_FILE", "GRAFANA_TLS_KEY_FILE", "GRAFANA_TLS_CA_FILE"} {
		if _, ok := os.LookupEnv(key); ok {
			return true
		}
	}
	return false
}

func grafanaProxyEnvironmentOverridePresent() bool {
	_, ok := os.LookupEnv("GRAFANA_PROXY_ENDPOINT")
	return ok
}

func runtimeGrafanaProxyEndpoint(sourceCtx *config.Context) string {
	if sourceCtx != nil && sourceCtx.Grafana != nil {
		return sourceCtx.Grafana.ProxyEndpoint
	}
	return os.Getenv("GRAFANA_PROXY_ENDPOINT")
}

func storedGrafanaProxyEndpoint(sourceCtx *config.Context) string {
	if sourceCtx == nil || sourceCtx.Grafana == nil {
		return ""
	}
	return sourceCtx.Grafana.ProxyEndpoint
}

func preflightServerOverride(opts *login.Options, sourceCtx *config.Context, interactive bool) error {
	requestedServer := login.NormalizeServerURL(opts.Server)
	persistedServer := ""
	if sourceCtx != nil && sourceCtx.Grafana != nil {
		persistedServer = login.NormalizeServerURL(sourceCtx.Grafana.Server)
	}
	if opts.AllowOverride || sourceCtx == nil || sourceCtx.Grafana == nil ||
		requestedServer == "" || persistedServer == "" || requestedServer == persistedServer {
		return nil
	}
	need := &login.ErrNeedClarification{
		Field: "allow-override",
		Question: fmt.Sprintf(
			"Context %q already exists with server %s.\nOverride with %s?",
			opts.ContextName, sourceCtx.Grafana.Server, requestedServer,
		),
		Choices: []string{"yes", "no"},
	}
	if !interactive {
		return structuredClarificationError(need)
	}
	return askForClarification(need, opts)
}

func storedGrafanaTokenDestinationChanged(
	server string,
	stored, effective *config.Context,
	interactive, tokenExplicit bool,
) bool {
	return !interactive && !tokenExplicit && stored != nil && stored.Grafana != nil &&
		stored.Grafana.APIToken != "" &&
		!config.GrafanaTokenBindingMatches(stored, effective, server)
}

func grafanaDestinationChangeAuthError(previousServer, requestedServer string) error {
	return gcxerrors.DetailedError{
		Summary: "Stored Grafana token cannot be reused for a different destination",
		Details: fmt.Sprintf(
			"The selected context's token is bound to server %s and its persisted proxy/TLS identity, but this login targets server %s with a different destination binding. Validation would disclose the stored token outside its credential binding.",
			previousServer, requestedServer,
		),
		Suggestions: []string{
			"Supply a new credential explicitly with --token",
			"Or use --oauth to authenticate to the new server",
		},
	}
}

func cloudLoginEndpoints(flags *loginOpts, sourceCtx *config.Context, apiURLExplicit bool) (string, string, error) {
	if apiURLExplicit {
		// Unified login exposes one Cloud environment override. Use it for both
		// the OAuth origin and subsequent API calls so a token is never minted in
		// one environment and silently persisted against another.
		return coherentCloudLoginEndpoints(flags.Server, flags.CloudAPIURL, flags.CloudAPIURL)
	}
	envAPIURL := strings.TrimSpace(os.Getenv("GRAFANA_CLOUD_API_URL"))
	envOAuthURL := strings.TrimSpace(os.Getenv("GRAFANA_CLOUD_OAUTH_URL"))
	if envAPIURL != "" || envOAuthURL != "" {
		switch {
		case envAPIURL == "":
			envAPIURL = envOAuthURL
		case envOAuthURL == "":
			envOAuthURL = envAPIURL
		}
		return coherentCloudLoginEndpoints(flags.Server, envAPIURL, envOAuthURL)
	}
	if sourceCtx != nil && sourceCtx.CloudEntry != nil {
		// Endpoint settings are sticky across re-auth, matching gcx cloud login.
		return coherentCloudLoginEndpoints(flags.Server, sourceCtx.CloudEntry.APIUrl, sourceCtx.CloudEntry.OAuthUrl)
	}
	return coherentCloudLoginEndpoints(flags.Server, flags.CloudAPIURL, "")
}

func coherentCloudLoginEndpoints(server, apiURL, oauthURL string) (string, string, error) {
	resolvedOAuth, resolvedAPI := login.ResolveCloudEndpoints(login.Options{Inputs: login.Inputs{
		Server:        server,
		CloudAPIURL:   apiURL,
		CloudOAuthURL: oauthURL,
	}})
	return resolvedAPI, resolvedOAuth, nil
}

func reuseNonInteractiveCloudCredential(opts *login.Options, tokenExplicit bool, sourceCtx *config.Context, interactive bool) error {
	if interactive || tokenExplicit || sourceCtx == nil || sourceCtx.CloudEntry == nil {
		return nil
	}
	// resolveNonInteractiveTokens may have copied a stored CAP. Clear it before
	// evaluating the destination; a rejected reuse must leave no token for
	// validation to send.
	opts.CloudToken = ""
	sourceServer := sourceContextServer(sourceCtx)
	if cloudEndpointRequestDiffers(opts, sourceCtx.CloudEntry, sourceServer) {
		return cloudDestinationChangeAuthError(opts, sourceCtx.CloudEntry, sourceServer)
	}
	envToken, tokenFromEnv := os.LookupEnv("GRAFANA_CLOUD_TOKEN")
	tokenFromEnv = tokenFromEnv && !config.IsBlankCredentialEnvironmentOverride("GRAFANA_CLOUD_TOKEN", envToken)
	useExistingCloudEntry(opts, sourceCtx.CloudEntry, !tokenFromEnv, sourceServer)
	return nil
}

func cloudDestinationChangeAuthError(opts *login.Options, entry *config.CloudEntry, sourceServer string) error {
	requestedOAuth, requestedAPI := login.ResolveCloudEndpoints(*opts)
	existingOAuth, existingAPI := login.ResolveCloudEndpoints(login.Options{Inputs: login.Inputs{
		Server:        sourceServer,
		CloudOAuthURL: entry.OAuthUrl,
		CloudAPIURL:   entry.APIUrl,
	}})
	return gcxerrors.DetailedError{
		Summary: "Stored Grafana Cloud credential cannot be reused for different endpoints",
		Details: fmt.Sprintf(
			"The credential is bound to OAuth/API endpoints %s / %s, but this login requests %s / %s.",
			existingOAuth, existingAPI, requestedOAuth, requestedAPI,
		),
		Suggestions: []string{
			"Supply a new Cloud Access Policy credential explicitly with --cloud-token",
			"Or run interactively and choose OAuth for the Grafana Cloud step",
		},
	}
}

func sourceContextServer(sourceCtx *config.Context) string {
	if sourceCtx == nil || sourceCtx.Grafana == nil {
		return ""
	}
	return sourceCtx.Grafana.Server
}

// askForInput shows an interactive huh prompt for each field in ErrNeedInput.
// For "cloud-token" the optional Grafana Cloud login step is delegated to
// askCloudAuth (browser OAuth, pasted token, or skip). When sourceCtx carries
// an existing stored token (re-auth), the prompts offer a "keep existing token"
// affordance instead of skipping or erroring.
func askForInput(
	ctx context.Context,
	e *login.ErrNeedInput,
	opts *login.Options,
	sourceCtx *config.Context,
	runtimeSourceCtx *config.Context,
	autoLocalTarget *config.ConfigSource,
) error {
	existingGrafanaToken := existingGrafanaTokenForDestination(opts.Server, sourceCtx, runtimeSourceCtx)
	var existingCloudEntry *config.CloudEntry
	existingServer := sourceContextServer(sourceCtx)
	if sourceCtx != nil {
		if sourceCtx.CloudEntry != nil {
			existingCloudEntry = sourceCtx.CloudEntry
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
			if autoLocalTarget != nil {
				if existingGrafanaToken == "" {
					return autoLocalFreshCredentialError(*autoLocalTarget)
				}
				opts.GrafanaToken = existingGrafanaToken
				opts.UseOAuth = false
				continue
			}
			if err := askGrafanaAuth(opts, existingGrafanaToken); err != nil {
				return err
			}

		case "cloud-token":
			if autoLocalTarget != nil {
				if existingCloudEntry == nil || !useExistingCloudEntry(opts, existingCloudEntry, true, existingServer) {
					return autoLocalFreshCredentialError(*autoLocalTarget)
				}
				continue
			}
			if err := askCloudAuth(ctx, e, opts, existingCloudEntry, existingServer); err != nil {
				return err
			}
		}
	}
	return nil
}

func existingGrafanaTokenForDestination(server string, stored, effective *config.Context) string {
	if stored == nil || stored.Grafana == nil || stored.Grafana.APIToken == "" ||
		!config.GrafanaTokenBindingMatches(stored, effective, server) {
		return ""
	}
	return stored.Grafana.APIToken
}

// askCloudAuth handles the optional Grafana Cloud (grafana.com) login shown
// after stack auth completes. It offers browser OAuth — the same GCOM PKCE flow
// as `gcx cloud login` — alongside pasting a Cloud Access Policy token and
// skipping. On re-auth, keeping the existing token is offered first.
//
// The resolved credential is written to opts.CloudToken, consumed by the next
// Run() as the Cloud API token. Skipping sets opts.Yes so the next Run()
// bypasses this sentinel instead of re-prompting.
func askCloudAuth(ctx context.Context, e *login.ErrNeedInput, opts *login.Options, existingEntry *config.CloudEntry, existingServer string) error {
	const (
		choiceKeep  = "keep"
		choiceOAuth = "oauth"
		choiceToken = "token"
		choiceSkip  = "skip"
	)

	// "recommended" marks the default (first) option: keeping a still-valid
	// token on re-auth, or browser login when there's no token yet.
	var options []huh.Option[string]
	existingKind, hasUsableExisting := cloudCredentialKind(existingEntry)
	if hasUsableExisting && cloudEndpointRequestDiffers(opts, existingEntry, existingServer) {
		// Both OAuth and CAP bearer tokens are destination-bound. An endpoint
		// change requires a freshly supplied credential, never a keep choice.
		hasUsableExisting = false
	}
	if hasUsableExisting {
		label := "Keep the existing Cloud Access Policy token (recommended)"
		if existingKind == login.CloudCredentialOAuth {
			label = "Keep the existing Cloud OAuth token (recommended)"
		}
		options = append(options,
			huh.NewOption(label, choiceKeep),
			huh.NewOption("OAuth (browser)", choiceOAuth),
		)
	} else {
		options = append(options, huh.NewOption("OAuth (browser) (recommended)", choiceOAuth))
	}
	options = append(options,
		huh.NewOption("Paste a Cloud Access Policy token", choiceToken),
		huh.NewOption("Skip — Cloud management features will be unavailable", choiceSkip),
	)

	choice := options[0].Value
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Also log in to Grafana Cloud (grafana.com)?").
			Description("Enables managing Cloud resources like stacks and access policies.").
			Options(options...).
			Value(&choice),
	))
	if err := form.Run(); err != nil {
		return err
	}

	switch choice {
	case choiceKeep:
		if !useExistingCloudEntry(opts, existingEntry, true, existingServer) {
			return errors.New("existing Cloud credential cannot be reused; choose OAuth to re-authenticate")
		}
		return nil
	case choiceOAuth:
		return runCloudOAuth(ctx, opts)
	case choiceSkip:
		// Set Yes=true so the next Run() call bypasses this sentinel instead
		// of re-prompting.
		opts.Yes = true
		return nil
	}

	// choiceToken: prompt for a pasted Cloud Access Policy token.
	opts.CloudCredentialKind = login.CloudCredentialCAP
	opts.CloudTokenTrusted = false
	opts.CloudOAuthTokenExpiresAt = ""
	opts.CloudOAuthScopes = nil
	hint := e.Hint
	if hint == "" {
		hint = "Press Enter to skip (Cloud management features will be unavailable)"
	}
	tokenForm := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Grafana Cloud API token").
			Description(hint).
			EchoMode(huh.EchoModePassword).
			Value(&opts.CloudToken),
	))
	if err := tokenForm.Run(); err != nil {
		return err
	}
	if opts.CloudToken == "" {
		opts.Yes = true
	}
	return nil
}

// runCloudOAuth runs the GCOM OAuth2 PKCE browser flow (the same flow used by
// `gcx cloud login`) and stores the resulting access token in opts.CloudToken so
// the next Run() persists it as the Cloud API token. The OAuth origin and API
// destination are resolved as one environment choice: explicit/sticky values
// win, then dev/ops stack origins are derived, then production is the fallback.
func runCloudOAuth(ctx context.Context, opts *login.Options) error {
	oauthURL, apiURL := login.ResolveCloudEndpoints(*opts)
	flowOpts := internalauth.GCOMOptions{
		ClientID: internalauth.DefaultGCOMClientID,
		GCOMURL:  oauthURL,
		Scopes:   internalauth.DefaultGCOMScopes(),
		Writer:   opts.Writer,
	}
	var flow login.CloudAuthFlow = internalauth.NewGCOMFlow(flowOpts)
	if opts.NewCloudAuthFlow != nil {
		flow = opts.NewCloudAuthFlow(flowOpts)
	}
	result, err := flow.Run(ctx)
	if err != nil {
		return gcxerrors.DetailedError{
			Summary: "Grafana Cloud authentication failed",
			Parent:  err,
			Suggestions: []string{
				"Ensure you are logged in to Grafana Cloud in your browser",
				"Re-run and choose \"Skip\" to finish without Cloud management features",
				"Or add a token later with: gcx cloud login",
			},
		}
	}
	opts.CloudToken = result.AccessToken
	opts.CloudCredentialKind = login.CloudCredentialOAuth
	opts.CloudTokenTrusted = true
	opts.CloudOAuthTokenExpiresAt = result.ExpiresAt
	opts.CloudOAuthScopes = strings.Fields(result.Scope)
	opts.CloudOAuthURL = oauthURL
	opts.CloudAPIURL = apiURL
	return nil
}

// cloudCredentialKind reports the usable credential stored in entry. CAP wins
// when malformed legacy data contains both fields, matching ResolveToken.
func cloudCredentialKind(entry *config.CloudEntry) (login.CloudCredentialKind, bool) {
	if entry == nil {
		return "", false
	}
	if entry.Token != "" {
		return login.CloudCredentialCAP, true
	}
	if entry.OAuthToken == "" {
		return "", false
	}
	if _, err := entry.ResolveToken(); err != nil {
		return "", false
	}
	return login.CloudCredentialOAuth, true
}

// useExistingCloudEntry copies a usable entry into login.Options without
// changing its credential kind or losing OAuth metadata/endpoints.
func useExistingCloudEntry(opts *login.Options, entry *config.CloudEntry, trusted bool, sourceServer string) bool {
	kind, ok := cloudCredentialKind(entry)
	if !ok {
		return false
	}
	if cloudEndpointRequestDiffers(opts, entry, sourceServer) {
		return false
	}
	token, err := entry.ResolveToken()
	if err != nil || token == "" {
		return false
	}
	opts.CloudToken = token
	opts.CloudCredentialKind = kind
	opts.CloudTokenTrusted = trusted
	opts.CloudOAuthTokenExpiresAt = entry.OAuthTokenExpiresAt
	opts.CloudOAuthScopes = append([]string(nil), entry.OAuthScopes...)
	opts.CloudOAuthURL = entry.OAuthUrl
	opts.CloudAPIURL = entry.APIUrl
	return true
}

// cloudEndpointRequestDiffers reports whether opts carries an endpoint intent
// different from the entry's effective OAuth/API pair. Empty opts endpoints
// mean no override, so legacy entries without endpoint metadata remain usable.
func cloudEndpointRequestDiffers(opts *login.Options, entry *config.CloudEntry, sourceServer string) bool {
	if entry == nil {
		return false
	}
	if sourceServer == "" && opts.CloudOAuthURL == "" && opts.CloudAPIURL == "" {
		// No prior server or explicit endpoint intent is available to compare.
		return false
	}
	if sourceServer == "" {
		sourceServer = opts.Server
	}
	requestedOAuth, requestedAPI := login.ResolveCloudEndpoints(*opts)
	existingOAuth, existingAPI := login.ResolveCloudEndpoints(login.Options{Inputs: login.Inputs{
		Server:        sourceServer,
		CloudOAuthURL: entry.OAuthUrl,
		CloudAPIURL:   entry.APIUrl,
	}})
	return requestedOAuth != existingOAuth || requestedAPI != existingAPI
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

	// Default to the first option in the menu: OAuth for Cloud, mTLS when certs
	// are present (non-Cloud), token otherwise. Deriving from options[0] keeps
	// the highlighted default in sync with the per-target menu order above.
	authMethod := options[0].Value
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

// structuredMissingFieldsError converts ErrNeedInput to a gcxerrors.DetailedError for non-interactive callers.
func structuredMissingFieldsError(e *login.ErrNeedInput) error {
	suggestions := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		switch f {
		case "server":
			suggestions = append(suggestions, "Pass --server <url> or set GRAFANA_SERVER")
		case "grafana-auth":
			suggestions = append(suggestions,
				"Pass --oauth to authenticate via browser (recommended for Grafana Cloud; opens a browser for the user to approve, works in agent mode)",
				"Pass --token <token> (or set the GRAFANA_TOKEN env var) for a service account token, or configure TLS client certs for mTLS auth (GRAFANA_TLS_CERT_FILE / GRAFANA_TLS_KEY_FILE env vars, or gcx config set stacks.<name>.grafana.tls.cert-file ...)")
		case "cloud-token":
			suggestions = append(suggestions, "Pass --cloud-token <token> (or set the GRAFANA_CLOUD_TOKEN env var) to enable Cloud features, or --yes to skip")
		default:
			suggestions = append(suggestions, "Provide --"+strings.ReplaceAll(f, "_", "-"))
		}
	}

	details := "Missing required fields: " + strings.Join(e.Fields, ", ")
	if e.Hint != "" {
		details += "\n" + e.Hint
	}

	return gcxerrors.DetailedError{
		Summary:     "Login requires additional input",
		Details:     details,
		Suggestions: suggestions,
	}
}

// structuredClarificationError converts ErrNeedClarification to a gcxerrors.DetailedError.
func structuredClarificationError(e *login.ErrNeedClarification) error {
	switch e.Field {
	case "allow-override":
		return gcxerrors.DetailedError{
			Summary: "Login would overwrite an existing context",
			Details: e.Question,
			Suggestions: []string{
				"Pass --allow-server-override to confirm the server change non-interactively",
				"Pick a different positional context name to create a new one",
			},
		}
	case "save-unvalidated":
		return gcxerrors.DetailedError{
			Summary: "Connectivity validation failed",
			Details: e.Question,
			Suggestions: []string{
				"Re-run interactively to confirm saving without validation",
				"Check server URL, network, and credentials",
			},
		}
	default:
		return gcxerrors.DetailedError{
			Summary: "Login requires clarification",
			Details: fmt.Sprintf("%s\nChoices: %s", e.Question, strings.Join(e.Choices, ", ")),
			Suggestions: []string{
				"Pass --cloud to force Grafana Cloud target",
				"Pass --yes to default to on-premises",
			},
		}
	}
}

// resolveSourceContext picks which context this login targets, returning it
// alongside its (possibly-derived) name. A nil context signals new-context
// creation to downstream code.
//
// When no name is given, the name is derived from --server so that
// `gcx login --server <new>` doesn't clobber the unrelated current context.
// With neither name nor server, falls back to the current context.
func resolveSourceContext(cfg config.Config, contextName, server string) (*config.Context, string) {
	server = login.NormalizeServerURL(server)
	switch {
	case contextName != "":
		return cfg.Contexts[contextName], contextName
	case server != "":
		name := config.ContextNameFromServerURL(server)
		return cfg.Contexts[name], name
	default:
		return cfg.GetCurrentContext(), cfg.CurrentContext
	}
}

// resolveNonInteractiveTokens fills empty token flags from the (already
// env-overridden) source context when the login is non-interactive, so that
// `gcx login` honours GRAFANA_TOKEN / GRAFANA_CLOUD_TOKEN — and reuses a stored
// token on re-auth — without an interactive prompt. Interactive logins are
// returned unchanged: their prompt flow owns auth-method selection and already
// offers a "keep existing token" affordance, so pre-filling here would skip the
// menu. Explicitly-passed flags always win over the context value, and an
// explicit OAuth selection suppresses every Grafana token fallback while
// leaving Cloud-token resolution unchanged.
func resolveNonInteractiveTokens(
	grafanaToken, cloudToken string,
	sourceCtx *config.Context,
	interactive, explicitOAuth bool,
) (string, string) {
	grafanaToken = strings.TrimSpace(grafanaToken)
	cloudToken = strings.TrimSpace(cloudToken)
	if interactive {
		return grafanaToken, cloudToken
	}
	// An explicit --oauth selection is authoritative. In particular, do not
	// silently replace it with GRAFANA_TOKEN or a stored service-account token
	// merely because this invocation cannot prompt.
	if explicitOAuth {
		grafanaToken = ""
	} else if grafanaToken == "" {
		if envToken, ok := os.LookupEnv("GRAFANA_TOKEN"); ok && !config.IsBlankCredentialEnvironmentOverride("GRAFANA_TOKEN", envToken) {
			grafanaToken = strings.TrimSpace(envToken)
		} else if sourceCtx != nil && sourceCtx.Grafana != nil {
			grafanaToken = sourceCtx.Grafana.APIToken
		}
	}
	if cloudToken == "" {
		if envToken, ok := os.LookupEnv("GRAFANA_CLOUD_TOKEN"); ok && !config.IsBlankCredentialEnvironmentOverride("GRAFANA_CLOUD_TOKEN", envToken) {
			cloudToken = strings.TrimSpace(envToken)
		} else if sourceCtx != nil && sourceCtx.CloudEntry != nil {
			cloudToken = sourceCtx.CloudEntry.Token
		}
	}
	return grafanaToken, cloudToken
}

// defaultOAuthFromContext decides whether to default to OAuth when re-authing
// an existing context that previously used OAuth. Like resolveNonInteractiveTokens,
// it only applies to non-interactive logins: a bare `gcx login <oauth-ctx>` in
// agent mode / CI would otherwise fail with a "missing grafana-auth" error, since
// OAuth credentials aren't reusable as a token. Interactive logins keep their
// auth-method menu (where OAuth is already the default for Cloud), and an
// explicit --oauth/--token always wins.
func defaultOAuthFromContext(useOAuth bool, grafanaToken string, sourceCtx *config.Context, interactive bool) bool {
	if useOAuth || interactive || grafanaToken != "" ||
		sourceCtx == nil || sourceCtx.Grafana == nil {
		return useOAuth
	}
	return sourceCtx.Grafana.AuthMethod == "oauth"
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
// stdout using the configured output codec. Advisory prose (next-step and
// CAP-token guidance) is routed to stderr so that JSON/YAML consumers receive
// clean, parseable output on stdout.
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
	// json/yaml consumers while still surfacing guidance to humans
	// (terminals typically merge stderr into the visible stream).
	ew := cmd.ErrOrStderr()
	if ioOpts.OutputFormat == "text" {
		fmt.Fprintln(ew)
		fmt.Fprintln(ew, "Verify access anytime with: gcx config check")
	}
	if result.IsCloud && !result.HasCloudToken {
		fmt.Fprintln(ew)
		fmt.Fprintln(ew, "You're authenticated for the Grafana API (dashboards, datasources, queries, alerts, folders).")
		fmt.Fprintln(ew, "Grafana Cloud product management (SLOs, Synthetic Monitoring, Fleet, k6, IRM, Adaptive telemetry)")
		fmt.Fprintln(ew, "additionally requires a Cloud Access Policy (CAP) token.")
		fmt.Fprintln(ew, "See: https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/")
		fmt.Fprintf(ew, "Add one with: gcx login --context %s --cloud-token <token>\n", result.ContextName)
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
