package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/grafana"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/secrets"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type Options struct {
	ConfigFile string
	Context    string
}

func (opts *Options) BindFlags(flags *pflag.FlagSet) {
	flags.StringVar(&opts.ConfigFile, "config", "", "Path to the configuration file to use")
	flags.StringVar(&opts.Context, "context", "", "Name of the context to use")

	_ = cobra.MarkFlagFilename(flags, "config", "yaml", "yml")
}

// LoadConfigTolerant loads the configuration file (default, or explicitly set via flags)
// and returns it without validation.
// This function should only be used by config-related commands, to allow the
// user to iterate on the configuration until it becomes valid.
func (opts *Options) LoadConfigTolerant(ctx context.Context, extraOverrides ...config.Override) (config.Config, error) {
	overrides := append([]config.Override{
		// If Grafana-related env variables are set, use them to configure the
		// current context and Grafana config.
		func(cfg *config.Config) error {
			if cfg.CurrentContext == "" {
				cfg.CurrentContext = config.DefaultContextName
			}

			if !cfg.HasContext(cfg.CurrentContext) {
				cfg.SetContext(cfg.CurrentContext, true, config.Context{})
			}

			curCtx := cfg.Contexts[cfg.CurrentContext]

			if err := config.ParseEnvIntoContext(curCtx); err != nil {
				return err
			}

			// Resolve GRAFANA_PROVIDER_{NAME}_{KEY} environment variables
			// into the current context's Providers map.
			const providerEnvPrefix = "GRAFANA_PROVIDER_"
			for _, envVar := range os.Environ() {
				parts := strings.SplitN(envVar, "=", 2)
				if len(parts) != 2 {
					continue
				}

				key, val := parts[0], parts[1]
				if !strings.HasPrefix(key, providerEnvPrefix) {
					continue
				}

				suffix := key[len(providerEnvPrefix):]
				nameParts := strings.SplitN(suffix, "_", 2)
				if len(nameParts) != 2 || nameParts[0] == "" || nameParts[1] == "" {
					continue
				}

				providerName := strings.ToLower(nameParts[0])
				// Normalize underscores to dashes to match kebab-case YAML keys
				// (e.g. GRAFANA_PROVIDER_SLO_ORG_ID → provider=slo, key=org-id)
				configKey := strings.ReplaceAll(strings.ToLower(nameParts[1]), "_", "-")

				if curCtx.Providers == nil {
					curCtx.Providers = make(map[string]map[string]string)
				}
				if curCtx.Providers[providerName] == nil {
					curCtx.Providers[providerName] = make(map[string]string)
				}
				curCtx.Providers[providerName][configKey] = val
			}

			return nil
		},
	}, extraOverrides...)

	// The current context is being overridden by a flag
	if opts.Context != "" {
		overrides = append(overrides, func(cfg *config.Config) error {
			if !cfg.HasContext(opts.Context) {
				return config.ContextNotFound(opts.Context)
			}

			cfg.CurrentContext = opts.Context
			return nil
		})
	}

	return config.LoadLayered(ctx, opts.ConfigFile, overrides...)
}

// LoadConfig loads the configuration file (default, or explicitly set via flags) and validates it.
func (opts *Options) LoadConfig(ctx context.Context) (config.Config, error) {
	validator := func(cfg *config.Config) error {
		// Ensure that the current context actually exists.
		if !cfg.HasContext(cfg.CurrentContext) {
			return config.ContextNotFound(cfg.CurrentContext)
		}

		return cfg.GetCurrentContext().Validate(ctx)
	}

	return opts.LoadConfigTolerant(ctx, validator)
}

// LoadGrafanaConfig loads the configuration file and constructs a REST config from it.
// When OAuth proxy mode is active, it wires the OnRefresh callback to persist
// refreshed tokens back to the config file.
func (opts *Options) LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error) {
	restCfg, _, err := opts.LoadGrafanaConfigWithContext(ctx)
	return restCfg, err
}

// LoadGrafanaConfigWithContext is like LoadGrafanaConfig but also returns the current
// Context, so callers can read its per-context settings.
func (opts *Options) LoadGrafanaConfigWithContext(ctx context.Context) (config.NamespacedRESTConfig, *config.Context, error) {
	cfg, err := opts.LoadConfig(ctx)
	if err != nil {
		return config.NamespacedRESTConfig{}, nil, err
	}

	current := cfg.GetCurrentContext()
	restCfg, err := current.ToRESTConfig(ctx)
	if err != nil {
		return config.NamespacedRESTConfig{}, nil, err
	}
	restCfg.WireTokenPersistence(ctx, opts.ConfigSource(), cfg.CurrentContext, cfg.Sources)

	return restCfg, current, nil
}

// loadConfigTolerantLayered loads the configuration using the layered discovery
// mechanism (system → user → local), without validation.
// This function should only be used by config-related commands, to allow the
// user to iterate on the configuration until it becomes valid.
func (opts *Options) loadConfigTolerantLayered(ctx context.Context) (config.Config, error) {
	return config.LoadLayered(ctx, opts.ConfigFile)
}

func (opts *Options) ConfigSource() config.Source {
	if opts.ConfigFile != "" {
		return config.ExplicitConfigFile(opts.ConfigFile)
	}

	return config.StandardLocation()
}

func Command() *cobra.Command {
	configOpts := &Options{}

	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or manipulate configuration settings",
		Long: fmt.Sprintf(`View or manipulate configuration settings.

The configuration file to load is chosen as follows:

1. If the --config flag is set, then that file will be loaded. No other location will be considered.
2. If the $%[3]s environment variable is set, then that file will be loaded. No other location will be considered.
3. If the $XDG_CONFIG_HOME environment variable is set, then it will be used: $XDG_CONFIG_HOME/%[1]s/%[2]s
   Example: /home/user/.config/%[1]s/%[2]s
4. If the $HOME environment variable is set, then it will be used: $HOME/.config/%[1]s/%[2]s
   Example: /home/user/.config/%[1]s/%[2]s
5. If the $XDG_CONFIG_DIRS environment variable is set, then it will be used: $XDG_CONFIG_DIRS/%[1]s/%[2]s
   Example: /etc/xdg/%[1]s/%[2]s
`, config.StandardConfigFolder, config.StandardConfigFileName, config.ConfigFileEnvVar),
	}

	configOpts.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(checkCmd(configOpts))
	cmd.AddCommand(currentContextCmd(configOpts))
	cmd.AddCommand(editCmd(configOpts))
	cmd.AddCommand(pathCmd(configOpts))
	cmd.AddCommand(setCmd(configOpts))
	cmd.AddCommand(unsetCmd(configOpts))
	cmd.AddCommand(useContextCmd(configOpts))
	cmd.AddCommand(viewCmd(configOpts))
	cmd.AddCommand(listContextsCmd(configOpts))

	return cmd
}

type viewOpts struct {
	IO cmdio.Options

	Minify bool
	Raw    bool
}

func (opts *viewOpts) BindFlags(flags *pflag.FlagSet) {
	opts.IO.DefaultFormat("yaml")
	opts.IO.BindFlags(flags)

	// Override the default yaml codec to enable bytes ↔ base64 conversion
	opts.IO.RegisterCustomCodec("yaml", &format.YAMLCodec{
		BytesAsBase64: true,
	})

	flags.BoolVar(&opts.Minify, "minify", opts.Minify, "Remove all information not used by current-context from the output")
	flags.BoolVar(&opts.Raw, "raw", opts.Raw, "Display sensitive information")
}

func (opts *viewOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}

	return nil
}

func viewCmd(configOpts *Options) *cobra.Command {
	opts := &viewOpts{}

	cmd := &cobra.Command{
		Use:     "view",
		Args:    cobra.NoArgs,
		Short:   "Display the current configuration",
		Example: "\n\tgcx config view",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			cfg, err := configOpts.LoadConfigTolerant(cmd.Context())
			if err != nil {
				return err
			}

			if opts.Minify {
				cfg, err = config.Minify(cfg)
				if err != nil {
					return err
				}
			}

			if !opts.Raw {
				if err := secrets.Redact(&cfg); err != nil {
					return fmt.Errorf("could not redact secrets from configuration: %w", err)
				}

				registered := providers.All()
				for _, ctx := range cfg.Contexts {
					if ctx.Providers != nil {
						providers.RedactSecrets(ctx.Providers, registered)
					}
				}
			}

			return opts.IO.Encode(cmd.OutOrStdout(), cfg)
		},
	}

	opts.BindFlags(cmd.Flags())

	return cmd
}

// ioOpts is the shared options struct for config subcommands whose only
// output concern is the shared codec flags (-o/--json/--jq). The default
// format is a custom codec reproducing the command's historical human
// rendering byte-for-byte; agent mode and explicit -o json/yaml get the
// structured value through the same Encode call.
type ioOpts struct {
	IO cmdio.Options
}

func (opts *ioOpts) setup(flags *pflag.FlagSet, defaultFormat string, codec format.Codec) {
	opts.IO.RegisterCustomCodec(defaultFormat, codec)
	opts.IO.DefaultFormat(defaultFormat)
	opts.IO.BindFlags(flags)
}

func (opts *ioOpts) Validate() error {
	return opts.IO.Validate()
}

// currentContextResult is the structured result of `config current-context`.
// The key matches the config-file field so agents see the same name in
// `config view` and here.
type currentContextResult struct {
	CurrentContext string `json:"current-context" yaml:"current-context"`
}

// currentContextTextCodec renders the historical kubectl-style bare context
// name, byte-identical to the pre-codec output.
type currentContextTextCodec struct{}

func (c *currentContextTextCodec) Format() format.Format { return "text" }

func (c *currentContextTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *currentContextTextCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(currentContextResult)
	if !ok {
		return errors.New("invalid data type for current-context text codec: expected currentContextResult")
	}
	_, err := fmt.Fprintln(w, result.CurrentContext)
	return err
}

func currentContextCmd(configOpts *Options) *cobra.Command {
	opts := &ioOpts{}

	cmd := &cobra.Command{
		Use:     "current-context",
		Args:    cobra.NoArgs,
		Short:   "Display the current context name",
		Long:    "Display the current context name.",
		Example: "\n\tgcx config current-context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			cfg, err := configOpts.LoadConfigTolerant(cmd.Context())
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), currentContextResult{CurrentContext: cfg.CurrentContext})
		},
	}

	opts.setup(cmd.Flags(), "text", &currentContextTextCodec{})

	return cmd
}

// contextListEntry is one context row in the `config list-contexts` result.
type contextListEntry struct {
	Current bool   `json:"current" yaml:"current"`
	Name    string `json:"name" yaml:"name"`
	Server  string `json:"server,omitempty" yaml:"server,omitempty"`
}

// contextListResult is the single shape passed to every codec for
// `config list-contexts` (Pattern 13: format-agnostic data).
type contextListResult struct {
	Contexts []contextListEntry `json:"contexts" yaml:"contexts"`
}

// contextsTableCodec renders the historical CURRENT/NAME/GRAFANA SERVER
// table, byte-identical to the pre-codec output.
type contextsTableCodec struct{}

func (c *contextsTableCodec) Format() format.Format { return "table" }

func (c *contextsTableCodec) Decode(io.Reader, any) error {
	return errors.New("table codec does not support decoding")
}

func (c *contextsTableCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(contextListResult)
	if !ok {
		return errors.New("invalid data type for contexts table codec: expected contextListResult")
	}

	t := style.NewTable("CURRENT", "NAME", "GRAFANA SERVER")
	for _, entry := range result.Contexts {
		server := entry.Server
		if server == "" {
			server = " "
		}

		current := " "
		if entry.Current {
			current = "*"
		}

		t.Row(current, entry.Name, server)
	}

	return t.Render(w)
}

func listContextsCmd(configOpts *Options) *cobra.Command {
	opts := &ioOpts{}

	cmd := &cobra.Command{
		Use:     "list-contexts",
		Args:    cobra.NoArgs,
		Short:   "List the contexts defined in the configuration",
		Long:    "List the contexts defined in the configuration.",
		Example: "\n\tgcx config list-contexts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			cfg, err := configOpts.LoadConfigTolerant(cmd.Context())
			if err != nil {
				return err
			}

			// Sorted by name for determinism — Contexts is a map, and the
			// historical row order was whatever map iteration produced.
			names := slices.Sorted(maps.Keys(cfg.Contexts))
			entries := make([]contextListEntry, 0, len(names))
			for _, name := range names {
				server := ""
				if gCtx := cfg.Contexts[name]; gCtx.Grafana != nil {
					server = gCtx.Grafana.Server
				}

				entries = append(entries, contextListEntry{
					Current: cfg.CurrentContext == name,
					Name:    name,
					Server:  server,
				})
			}

			return opts.IO.Encode(cmd.OutOrStdout(), contextListResult{Contexts: entries})
		},
	}

	opts.setup(cmd.Flags(), "table", &contextsTableCodec{})

	return cmd
}

func checkCmd(configOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "check",
		Args:    cobra.NoArgs,
		Short:   "Check the current configuration for issues",
		Long:    "Check the current configuration for issues.",
		Example: "\n\tgcx config check",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configOpts.LoadConfigTolerant(cmd.Context())
			if err != nil {
				return err
			}

			stdout := cmd.OutOrStdout()

			cmdio.Success(stdout, "Configuration file: %s", cmdio.Green(cfg.Source))

			hasIssues := false
			switch {
			case cfg.CurrentContext == "":
				cmdio.Error(stdout, "Current context: %s", cmdio.Red("<undefined>"))
				hasIssues = true
			case !cfg.HasContext(cfg.CurrentContext):
				cmdio.Error(stdout, "Current context: %s", cmdio.Red(config.ContextNotFound(cfg.CurrentContext).Error()))
				hasIssues = true
			default:
				cmdio.Success(stdout, "Current context: %s", cmdio.Green(cfg.CurrentContext))
			}

			cmd.Println()

			var checkErr error
			for _, gCtx := range cfg.Contexts {
				healthy, err := checkContext(cmd, cfg, gCtx, configOpts.ConfigSource())
				if err != nil {
					checkErr = err
				}
				if !healthy {
					hasIssues = true
				}
			}

			if checkErr != nil {
				return reportCheckError(cmd.ErrOrStderr(), checkErr)
			}

			if hasIssues {
				// The prose report above — failures, suggestions and all — IS
				// this command's complete output. EmittedError carries the
				// non-zero exit without rendering a second (error) document,
				// so the exit code finally agrees with the findings.
				return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError,
					errors.New("configuration check found issues"))
			}

			return nil
		},
	}

	return cmd
}

// reportCheckError surfaces a context-check failure the prose report could
// not carry itself (Grafana < 12). In agent mode the prose report is already
// on stdout and reportError would append a gcx.error document after it on
// the same stream — the exact mixed-output shape the hasIssues branch
// avoids — so the failure becomes a typed stderr diagnostic + EmittedError.
// Human mode keeps the reporter's rendering.
func reportCheckError(stderr io.Writer, checkErr error) error {
	if !agent.IsAgentMode() {
		return checkErr
	}
	summary := checkErr.Error()
	code := gcxerrors.ExitGeneralError
	if detailed := fail.ErrorToDetailedError(checkErr); detailed != nil {
		summary = fmt.Sprintf("%s: %s", detailed.Summary, checkErr.Error())
		if detailed.ExitCode != nil {
			code = *detailed.ExitCode
		}
	}
	cmdio.EmitWarn(stderr, summary)
	return gcxerrors.NewEmittedError(code, checkErr)
}

// checkContext prints the diagnostic report for one context. It returns
// healthy=false when any error-level finding was reported (the prose already
// carries the details and suggestions); the error return is reserved for
// failures that the caller must surface itself (Grafana < 12).
func checkContext(cmd *cobra.Command, cfg config.Config, gCtx *config.Context, source config.Source) (bool, error) {
	stdout := cmd.OutOrStdout()
	title := "Context: "
	titleLen := len(title) + len(gCtx.Name)
	title += cmdio.Bold(gCtx.Name)

	summarizeError := func(err error) string {
		// ErrorToDetailedError returns nil for chains carrying an
		// EmittedError (result already on stdout — nothing to render).
		detailedErr := fail.ErrorToDetailedError(err)
		if detailedErr == nil {
			return err.Error()
		}

		return fmt.Sprintf("%s: %s", detailedErr.Summary, err.Error())
	}

	printSuggestions := func(err error) {
		detailedErr := fail.ErrorToDetailedError(err)
		if detailedErr != nil && len(detailedErr.Suggestions) != 0 {
			cmdio.Info(stdout, "Suggestions:\n")
			for _, suggestion := range detailedErr.Suggestions {
				fmt.Fprintf(stdout, "  • %s\n", suggestion)
			}
			stdout.Write([]byte("\n"))
		}
	}

	cmd.Println(cmdio.Yellow(title))
	cmd.Println(cmdio.Yellow(strings.Repeat("=", titleLen)))

	if err := gCtx.Validate(cmd.Context()); err != nil {
		cmdio.Error(stdout, "Configuration: %s", cmdio.Red(summarizeError(err)))
		cmdio.Warning(stdout, "Connectivity: %s", cmdio.Yellow("skipped"))
		cmdio.Warning(stdout, "Grafana version: %s", cmdio.Yellow("skipped")+"\n")

		printSuggestions(err)
		return false, nil
	}

	cmdio.Success(stdout, "Configuration: %s", cmdio.Green("valid"))

	authMethod := gCtx.Grafana.AuthMethod
	if authMethod == "" {
		authMethod = gCtx.Grafana.InferredAuthMethod() + " (inferred)"
	}
	cmdio.Info(stdout, "Auth method: %s", authMethod)

	isCloud := gCtx.IsCloud()
	contextType := "On-prem"
	if isCloud {
		contextType = "Grafana Cloud"
	}
	cmdio.Info(stdout, "Context type: %s", contextType)

	restCfg, err := gCtx.ToRESTConfig(cmd.Context())
	if err != nil {
		cmdio.Error(stdout, "Configuration: %s", cmdio.Red(err.Error()))
		return false, nil
	}
	restCfg.WireTokenPersistence(cmd.Context(), source, gCtx.Name, cfg.Sources)

	if _, err := discovery.NewDefaultRegistry(cmd.Context(), restCfg); err != nil {
		cmdio.Error(stdout, "Connectivity: %s", cmdio.Red(summarizeError(err)))
		cmdio.Warning(stdout, "Grafana version: %s", cmdio.Yellow("skipped")+"\n")
		printSuggestions(err)
		return false, nil
	}

	cmdio.Success(stdout, "Connectivity: %s", cmdio.Green("online"))

	version, raw, err := grafana.GetVersion(cmd.Context(), gCtx)
	if err != nil {
		cmdio.Error(stdout, "Grafana version: %s", cmdio.Red(summarizeError(err))+"\n")
		return false, nil
	}

	switch {
	case version == nil && raw == "" && isCloud:
		// Grafana Cloud (dev/ops) environments don't expose the version
		// field via /api/health. Report the platform instead of a cryptic
		// "hidden by server" line.
		cmdio.Success(stdout, "Grafana version: %s", cmdio.Green("Grafana Cloud")+"\n")
	case version == nil && raw == "":
		cmdio.Warning(stdout, "Grafana version: %s\n", cmdio.Yellow("hidden by server (anonymous /api/health)"))
	case version == nil:
		cmdio.Warning(stdout, "Grafana version: %s\n", cmdio.Yellow("unparseable: "+raw))
	case version.Major() < 12:
		return false, &grafana.VersionIncompatibleError{Version: version}
	default:
		cmdio.Success(stdout, "Grafana version: %s", cmdio.Green(version.String())+"\n")
	}

	return true, nil
}

// useContextTextCodec renders the historical confirmation line for the
// use-context SingleMutation result, byte-identical to the pre-codec output.
type useContextTextCodec struct{}

func (c *useContextTextCodec) Format() format.Format { return "text" }

func (c *useContextTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *useContextTextCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(cmdio.SingleMutation)
	if !ok {
		return errors.New("invalid data type for use-context text codec: expected SingleMutation")
	}
	if result.Changed != nil && !*result.Changed {
		cmdio.Success(w, "Context already set to \"%s\"", result.Target.Name)
		return nil
	}
	cmdio.Success(w, "Context set to \"%s\"", result.Target.Name)
	return nil
}

func useContextCmd(configOpts *Options) *cobra.Command {
	var fileType string
	opts := &ioOpts{}

	cmd := &cobra.Command{
		Use:     "use-context [CONTEXT_NAME]",
		Args:    cobra.MaximumNArgs(1),
		Aliases: []string{"use"},
		Short:   "Set the current context",
		Long: `Set the current context and update the configuration file.

Run without arguments to pick a context interactively, or pass "-" to switch
back to the previously active context.

In agent mode or when stdout is not a TTY, a context name is required.

When multiple config files are loaded (e.g. a local .gcx.yaml alongside the
user config), use --file to choose which layer to update.`,
		Example: `
	gcx config use-context dev-instance
	gcx config use                 # interactive picker
	gcx config use -               # previous context

	# Update the local .gcx.yaml when both user and local configs exist
	gcx config use-context --file local dev-instance`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			// Cross-layer load: a context defined only in the user layer is
			// still a valid target when --file local is specified, and the
			// interactive picker needs the merged view.
			layered, err := config.LoadLayered(cmd.Context(), configOpts.ConfigFile)
			if err != nil {
				return err
			}

			target, err := resolveUseContextTarget(layered, args)
			if err != nil {
				if errors.Is(err, huh.ErrUserAborted) {
					// Interactive-only path (the picker requires a TTY and is
					// disabled in agent mode), so the note stays on stdout.
					cmdio.Info(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
				return err
			}

			if !layered.HasContext(target) {
				return config.ContextNotFound(target)
			}

			// Load only the target layer so we don't write cross-layer entries.
			cfg, src, err := config.LoadForWrite(cmd.Context(), configOpts.ConfigFile, fileType)
			if err != nil {
				return err
			}

			result := cmdio.NewSingleMutation("use-context", cmdio.MutationTarget{Kind: "context", Name: target})

			prev := cfg.CurrentContext
			if prev == target {
				changed := false
				result.Changed = &changed
				return opts.IO.Encode(cmd.OutOrStdout(), result)
			}

			cfg.CurrentContext = target
			if err := config.Write(cmd.Context(), src, cfg); err != nil {
				return err
			}

			if prev != "" {
				if err := config.WritePreviousContext(cmd.Context(), prev); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not record previous context at %s: %v\n", config.PreviousContextPath(), err)
				}
			}

			changed := true
			result.Changed = &changed
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	cmd.Flags().StringVar(&fileType, "file", "", "Config layer to write to (system, user, local)")
	opts.setup(cmd.Flags(), "text", &useContextTextCodec{})

	return cmd
}

func resolveUseContextTarget(cfg config.Config, args []string) (string, error) {
	if len(args) == 0 {
		return pickContextInteractively(cfg)
	}
	name := args[0]
	if name == "-" {
		prev, err := config.ReadPreviousContext()
		if err != nil {
			return "", err
		}
		if prev == "" {
			return "", errors.New("no previous context recorded — switch contexts at least once with 'gcx config use-context <name>' to enable '-'")
		}
		return prev, nil
	}
	return name, nil
}

func pickContextInteractively(cfg config.Config) (string, error) {
	// Agent mode is an intentional non-interactive contract: never prompt, and
	// hand back a structured error the agent can act on.
	if agent.IsAgentMode() {
		return "", gcxerrors.DetailedError{
			Summary:     "interactive picker disabled in agent mode",
			Suggestions: []string{"Pass a context name, e.g. gcx config use-context dev-instance"},
		}
	}
	// The picker reads from and writes to the controlling terminal directly —
	// huh does not honor cobra's writers — so the TTY check must consult the
	// process stdout via terminal.StdoutIsTerminal(), not cmd.OutOrStdout(). A
	// piped invocation (e.g. `gcx config use | cat`) has no terminal to drive.
	if !terminal.StdoutIsTerminal() {
		return "", gcxerrors.DetailedError{
			Summary:     "interactive picker requires a TTY",
			Suggestions: []string{"Pass a context name, e.g. gcx config use-context dev-instance"},
		}
	}
	if len(cfg.Contexts) == 0 {
		return "", gcxerrors.DetailedError{
			Summary:     "no contexts defined",
			Suggestions: []string{"Create one with 'gcx login' or 'gcx config set'"},
		}
	}

	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)

	// Surface the current context first (kubectx-style) and pre-select it: the
	// common case is toggling away from and back to it, so it should be the
	// default highlight rather than buried in alphabetical order.
	if cur := cfg.CurrentContext; cur != "" {
		if _, ok := cfg.Contexts[cur]; ok {
			reordered := make([]string, 0, len(names))
			reordered = append(reordered, cur)
			for _, name := range names {
				if name != cur {
					reordered = append(reordered, name)
				}
			}
			names = reordered
		}
	}

	options := make([]huh.Option[string], 0, len(names))
	for _, name := range names {
		label := name
		if cfg.CurrentContext == name {
			label = name + " (current)"
		}
		options = append(options, huh.NewOption(label, name))
	}

	selected := cfg.CurrentContext // pre-select the current context, if any
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Select a context").
			Options(options...).
			Value(&selected),
	))
	if err := form.Run(); err != nil {
		return "", err
	}
	return selected, nil
}

// configMutation is the structured result of `config set` / `config unset`.
// The shared cmdio mutation family targets kind/name/uid objects; a config
// mutation acts on a dot-path property within a layered config file, so this
// bespoke shape carries the discriminators (type, schema_version) required of
// result-family documents plus the fields the domain actually has.
type configMutation struct {
	Type          string `json:"type" yaml:"type"`
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Action        string `json:"action" yaml:"action"`
	// Property is the fully resolved dot-path (bare paths are resolved
	// against the current context before the write).
	Property string `json:"property" yaml:"property"`
	// File is the config file the mutation was written to.
	File string `json:"file,omitempty" yaml:"file,omitempty"`
}

// newConfigMutation builds the result document for a completed config write.
// The file path is resolved best-effort from the write target: the write
// already succeeded, so a resolution error only omits the field.
func newConfigMutation(action, property string, target config.Source) configMutation {
	result := configMutation{
		Type:          "gcx.config.mutation",
		SchemaVersion: "1",
		Action:        action,
		Property:      property,
	}
	if target != nil {
		if path, err := target(); err == nil {
			result.File = path
		}
	}
	return result
}

// silentTextCodec reproduces the historical human output of `config set` and
// `config unset`: nothing on success. The structured result exists for agent
// mode and explicit -o json/yaml only.
type silentTextCodec struct{}

func (c *silentTextCodec) Format() format.Format { return "text" }

func (c *silentTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *silentTextCodec) Encode(io.Writer, any) error { return nil }

func setCmd(configOpts *Options) *cobra.Command {
	var fileType string
	opts := &ioOpts{}

	cmd := &cobra.Command{
		Use:   "set PROPERTY_NAME PROPERTY_VALUE",
		Args:  cobra.ExactArgs(2),
		Short: "Set a single value in a configuration file",
		Long: `Set a single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to set. It can either represent a field or a map entry.

A bare path (e.g. "cloud.token") is resolved against the current context and is equivalent to "contexts.<current-context>.<path>". Use a fully qualified path (starting with "contexts.<name>.") to target a specific context.

PROPERTY_VALUE is the new value to set.`,
		Example: `
	# Set the "server" field on the current context to "https://grafana-dev.example"
	gcx config set grafana.server https://grafana-dev.example

	# Set the "server" field on the "dev-instance" context to "https://grafana-dev.example"
	gcx config set contexts.dev-instance.grafana.server https://grafana-dev.example

	# Disable the validation of the server's SSL certificate in the current context
	gcx config set grafana.insecure-skip-tls-verify true

	# Set a value in the local config layer
	gcx config set --file local contexts.prod.cloud.token my-token`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			cfg, target, err := config.LoadForWrite(cmd.Context(), configOpts.ConfigFile, fileType)
			if err != nil {
				return err
			}

			path, err := config.ResolveContextPath(cfg, args[0])
			if err != nil {
				return err
			}

			if err := config.SetValue(&cfg, path, args[1]); err != nil {
				return err
			}

			if err := config.Write(cmd.Context(), target, cfg); err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), newConfigMutation("set", path, target))
		},
	}

	cmd.Flags().StringVar(&fileType, "file", "", "Config layer to write to (system, user, local)")
	opts.setup(cmd.Flags(), "text", &silentTextCodec{})

	return cmd
}

func unsetCmd(configOpts *Options) *cobra.Command {
	var fileType string
	opts := &ioOpts{}

	cmd := &cobra.Command{
		Use:   "unset PROPERTY_NAME",
		Args:  cobra.ExactArgs(1),
		Short: "Unset a single value in a configuration file",
		Long: `Unset a single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to unset. It can either represent a field or a map entry.

A bare path (e.g. "cloud.token") is resolved against the current context and is equivalent to "contexts.<current-context>.<path>". Use a fully qualified path (starting with "contexts.<name>.") to target a specific context.`,
		Example: `
	# Unset the "foo" context
	gcx config unset contexts.foo

	# Unset the "insecure-skip-tls-verify" flag in the current context
	gcx config unset grafana.insecure-skip-tls-verify

	# Unset the "insecure-skip-tls-verify" flag in the "dev-instance" context
	gcx config unset contexts.dev-instance.grafana.insecure-skip-tls-verify

	# Unset a value in the local config layer
	gcx config unset --file local contexts.prod.cloud.token`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			cfg, target, err := config.LoadForWrite(cmd.Context(), configOpts.ConfigFile, fileType)
			if err != nil {
				return err
			}

			path, err := config.ResolveContextPath(cfg, args[0])
			if err != nil {
				return err
			}

			if err := config.UnsetValue(&cfg, path); err != nil {
				return err
			}

			if err := config.Write(cmd.Context(), target, cfg); err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), newConfigMutation("unset", path, target))
		},
	}

	cmd.Flags().StringVar(&fileType, "file", "", "Config layer to write to (system, user, local)")
	opts.setup(cmd.Flags(), "text", &silentTextCodec{})

	return cmd
}
