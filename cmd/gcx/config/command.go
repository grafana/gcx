package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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

	mutationResolved bool
	mutationTarget   config.ConfigSource
	mutationErr      error
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
	var overrides []config.Override

	// Select the target context before applying context-scoped environment
	// variables. Otherwise --context switches only after env values have already
	// been written into the context named by current-context.
	if opts.Context != "" {
		overrides = append(overrides, func(cfg *config.Config) error {
			if !cfg.HasContext(opts.Context) {
				return config.ContextNotFound(opts.Context)
			}

			cfg.CurrentContext = opts.Context
			return nil
		})
	}

	overrides = append(overrides,
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
				if providers.IsBlankProviderCredentialEnvironmentOverride(key, val) {
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
	)
	overrides = append(overrides, extraOverrides...)

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
	restCfg.WireTokenPersistence(ctx, opts.ConfigSource(), cfg.CurrentContext, current.Stack, cfg.Sources)

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

// MutationConfigSource resolves the single raw config document a mutating
// command may safely rewrite. Layered reads can combine several documents, but
// writing that merged view to an arbitrary user file can be shadowed by a local
// layer or flatten unrelated layers. Explicit --config and GCX_CONFIG remain
// authoritative; otherwise a sole discovered source is used and ambiguity is
// rejected with guidance to choose one.
func (opts *Options) MutationConfigSource() config.Source {
	return func() (string, error) {
		target, err := opts.resolveMutationConfigTarget()
		return target.Path, err
	}
}

// MutationConfigContext carries the sole discovered source's trust provenance
// into raw login/provider mutations. Explicit --config and GCX_CONFIG paths
// remain explicit user intent (including symlink support).
func (opts *Options) MutationConfigContext(ctx context.Context) context.Context {
	target, err := opts.resolveMutationConfigTarget()
	if err != nil || target.Type == "explicit" || target.Type == "" {
		return ctx
	}
	return config.ContextWithConfigSource(ctx, target)
}

// MutationConfigTarget returns the exact raw config document selected for a
// mutation together with its discovery provenance. Credential-accepting
// commands use the provenance to require an explicit --config/GCX_CONFIG trust
// decision before handing a fresh secret to an auto-discovered repository
// config.
func (opts *Options) MutationConfigTarget() (config.ConfigSource, error) {
	return opts.resolveMutationConfigTarget()
}

func (opts *Options) resolveMutationConfigTarget() (config.ConfigSource, error) {
	if !opts.mutationResolved {
		opts.mutationResolved = true
		switch {
		case opts.ConfigFile != "":
			opts.mutationTarget = config.ConfigSource{Path: opts.ConfigFile, Type: "explicit"}
			return opts.mutationTarget, nil
		case os.Getenv(config.ConfigFileEnvVar) != "":
			opts.mutationTarget = config.ConfigSource{Path: os.Getenv(config.ConfigFileEnvVar), Type: "explicit"}
			return opts.mutationTarget, nil
		}

		sources, err := config.DiscoverSources()
		if err != nil {
			opts.mutationErr = fmt.Errorf("discover config write target: %w", err)
			return opts.mutationTarget, opts.mutationErr
		}
		switch len(sources) {
		case 0:
			path, err := config.StandardLocation()()
			if err != nil {
				opts.mutationErr = err
				return opts.mutationTarget, opts.mutationErr
			}
			opts.mutationTarget = config.ConfigSource{Path: path, Type: "user"}
		case 1:
			opts.mutationTarget = sources[0]
		default:
			paths := make([]string, 0, len(sources))
			for _, source := range sources {
				paths = append(paths, source.Path)
			}
			opts.mutationErr = gcxerrors.DetailedError{
				Summary: "Configuration write target is ambiguous",
				Details: "This command reads a layered configuration from multiple files and cannot safely choose one to update: " +
					strings.Join(paths, ", "),
				Suggestions: []string{
					"Re-run with --config <path> to choose the file that should own the updated credentials",
				},
			}
		}
	}
	return opts.mutationTarget, opts.mutationErr
}

func Command() *cobra.Command {
	configOpts := &Options{}

	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or manipulate configuration settings",
		Long: fmt.Sprintf(`View or manipulate configuration settings.

--config or $%[3]s selects one explicit file and bypasses layering.
Otherwise gcx merges every existing source from lowest to highest priority:

1. System configuration: $XDG_CONFIG_DIRS/%[1]s/%[2]s (for example, /etc/xdg/%[1]s/%[2]s).
2. User configuration: $HOME/.config/%[1]s/%[2]s, then the platform $XDG_CONFIG_HOME fallback.
3. Repository configuration: .gcx.yaml in the current directory.

Credential-bearing stack and Cloud entries are atomic across layers; contexts
merge only their references and datasource defaults.
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
				for _, stack := range cfg.Stacks {
					if stack != nil && stack.Providers != nil {
						providers.RedactSecrets(stack.Providers, registered)
					}
				}
			}

			return opts.IO.Encode(cmd.OutOrStdout(), cfg)
		},
	}

	opts.BindFlags(cmd.Flags())

	return cmd
}

func currentContextCmd(configOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "current-context",
		Args:    cobra.NoArgs,
		Short:   "Display the current context name",
		Long:    "Display the current context name.",
		Example: "\n\tgcx config current-context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configOpts.LoadConfigTolerant(cmd.Context())
			if err != nil {
				return err
			}

			cmd.Println(cfg.CurrentContext)

			return nil
		},
	}

	return cmd
}

func listContextsCmd(configOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list-contexts",
		Args:    cobra.NoArgs,
		Short:   "List the contexts defined in the configuration",
		Long:    "List the contexts defined in the configuration.",
		Example: "\n\tgcx config list-contexts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configOpts.LoadConfigTolerant(cmd.Context())
			if err != nil {
				return err
			}

			t := style.NewTable("CURRENT", "NAME", "GRAFANA SERVER")
			for _, context := range cfg.Contexts {
				server := " "
				if context.Grafana != nil {
					server = context.Grafana.Server
				}

				current := " "
				if cfg.CurrentContext == context.Name {
					current = "*"
				}

				t.Row(current, context.Name, server)
			}

			return t.Render(cmd.OutOrStdout())
		},
	}

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
			reportWriter := stdout
			var agentReport bytes.Buffer
			if agent.IsAgentMode() {
				reportWriter = &agentReport
			}

			cmdio.Success(reportWriter, "Configuration file: %s", cmdio.Green(cfg.Source))

			failedChecks := 0
			failureExitCodes := make(map[int]struct{})
			recordFailure := func(err error) {
				failedChecks++
				exitCode := gcxerrors.ExitGeneralError
				if err != nil {
					detailedErr := fail.ErrorToDetailedError(err)
					if detailedErr != nil && detailedErr.ExitCode != nil {
						exitCode = *detailedErr.ExitCode
					}
				}
				failureExitCodes[exitCode] = struct{}{}
			}
			switch {
			case cfg.CurrentContext == "":
				cmdio.Error(reportWriter, "Current context: %s", cmdio.Red("<undefined>"))
				recordFailure(nil)
			case !cfg.HasContext(cfg.CurrentContext):
				cmdio.Error(reportWriter, "Current context: %s", cmdio.Red(config.ContextNotFound(cfg.CurrentContext).Error()))
				recordFailure(nil)
			default:
				cmdio.Success(reportWriter, "Current context: %s", cmdio.Green(cfg.CurrentContext))
			}

			fmt.Fprintln(reportWriter)

			for _, gCtx := range cfg.Contexts {
				if err := cmd.Context().Err(); err != nil {
					return err
				}
				if err := checkContext(cmd, &cfg, gCtx, configOpts.ConfigSource(), reportWriter); err != nil {
					if ctxErr := cmd.Context().Err(); ctxErr != nil {
						return ctxErr
					}
					if errors.Is(err, context.Canceled) {
						return context.Canceled
					}
					recordFailure(err)
				}
			}

			if failedChecks != 0 {
				exitCode := gcxerrors.ExitGeneralError
				if len(failureExitCodes) == 1 {
					for code := range failureExitCodes {
						exitCode = code
					}
				}
				if agent.IsAgentMode() {
					if _, err := cmd.ErrOrStderr().Write(agentReport.Bytes()); err != nil {
						return fmt.Errorf("write configuration check report: %w", err)
					}
					return gcxerrors.DetailedError{
						Summary:  "Configuration check failed",
						Details:  strings.TrimSpace(agentReport.String()),
						ExitCode: &exitCode,
						Suggestions: []string{
							"Review the failed contexts in the diagnostic report",
							"Resolve the reported issues, then rerun: gcx config check",
						},
					}
				}
				return fmt.Errorf("%d configuration check(s) failed: %w", failedChecks, gcxerrors.NewAlreadyReportedError(exitCode))
			}

			if agent.IsAgentMode() {
				if _, err := stdout.Write(agentReport.Bytes()); err != nil {
					return fmt.Errorf("write configuration check report: %w", err)
				}
			}
			return nil
		},
	}

	return cmd
}

func checkContext(cmd *cobra.Command, cfg *config.Config, gCtx *config.Context, source config.Source, stdout io.Writer) error {
	title := "Context: "
	titleLen := len(title) + len(gCtx.Name)
	title += cmdio.Bold(gCtx.Name)

	summarizeError := func(err error) string {
		detailedErr := fail.ErrorToDetailedError(err)

		return fmt.Sprintf("%s: %s", detailedErr.Summary, err.Error())
	}

	printSuggestions := func(err error) {
		detailedErr := fail.ErrorToDetailedError(err)
		if len(detailedErr.Suggestions) != 0 {
			cmdio.Info(stdout, "Suggestions:\n")
			for _, suggestion := range detailedErr.Suggestions {
				fmt.Fprintf(stdout, "  • %s\n", suggestion)
			}
			fmt.Fprintln(stdout)
		}
	}

	fmt.Fprintln(stdout, cmdio.Yellow(title))
	fmt.Fprintln(stdout, cmdio.Yellow(strings.Repeat("=", titleLen)))

	// Load resolves only the current context eagerly. config check validates
	// every context, so resolve this context's deferred keychain references
	// before validation can construct a namespace-discovery transport. Missing
	// or foreign references become typed rejection evidence and are therefore
	// reported with connectivity skipped instead of being sent upstream.
	cfg.ResolveContext(gCtx.Name)

	if err := gCtx.Validate(cmd.Context()); err != nil {
		cmdio.Error(stdout, "Configuration: %s", cmdio.Red(summarizeError(err)))
		cmdio.Warning(stdout, "Connectivity: %s", cmdio.Yellow("skipped"))
		cmdio.Warning(stdout, "Grafana version: %s", cmdio.Yellow("skipped")+"\n")

		printSuggestions(err)
		return err
	}

	cmdio.Success(stdout, "Configuration: %s", cmdio.Green("valid"))

	authMethod, err := gCtx.EffectiveGrafanaAuthMethod()
	if err != nil {
		// Validate above uses the same selector, so this is defensive against a
		// future validation/transport drift rather than an expected branch.
		cmdio.Error(stdout, "Configuration: %s", cmdio.Red(err.Error()))
		cmdio.Warning(stdout, "Connectivity: %s", cmdio.Yellow("skipped"))
		cmdio.Warning(stdout, "Grafana version: %s", cmdio.Yellow("skipped")+"\n")
		return err
	}
	switch {
	case gCtx.Grafana.AuthMethod == "":
		authMethod += " (inferred)"
	case !strings.EqualFold(authMethod, gCtx.Grafana.AuthMethod):
		authMethod += " (environment override)"
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
		cmdio.Warning(stdout, "Connectivity: %s", cmdio.Yellow("skipped"))
		cmdio.Warning(stdout, "Grafana version: %s", cmdio.Yellow("skipped")+"\n")
		return err
	}
	restCfg.WireTokenPersistence(cmd.Context(), source, gCtx.Name, gCtx.Stack, cfg.Sources)

	if _, err := discovery.NewDefaultRegistry(cmd.Context(), restCfg); err != nil {
		cmdio.Error(stdout, "Connectivity: %s", cmdio.Red(summarizeError(err)))
		cmdio.Warning(stdout, "Grafana version: %s", cmdio.Yellow("skipped")+"\n")
		printSuggestions(err)
		return err
	}

	cmdio.Success(stdout, "Connectivity: %s", cmdio.Green("online"))

	version, raw, err := grafana.GetVersion(cmd.Context(), gCtx)
	if err != nil {
		cmdio.Error(stdout, "Grafana version: %s", cmdio.Red(summarizeError(err))+"\n")
		printSuggestions(err)
		return err
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
		err := &grafana.VersionIncompatibleError{Version: version}
		cmdio.Error(stdout, "Grafana version: %s", cmdio.Red(err.Error())+"\n")
		printSuggestions(err)
		return err
	default:
		cmdio.Success(stdout, "Grafana version: %s", cmdio.Green(version.String())+"\n")
	}

	return nil
}

func useContextCmd(configOpts *Options) *cobra.Command {
	var fileType string

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

			prev := cfg.CurrentContext
			if prev == target {
				cmdio.Success(cmd.OutOrStdout(), "Context already set to \"%s\"", target)
				return nil
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

			cmdio.Success(cmd.OutOrStdout(), "Context set to \"%s\"", cfg.CurrentContext)
			return nil
		},
	}

	cmd.Flags().StringVar(&fileType, "file", "", "Config layer to write to (system, user, local)")

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

func setCmd(configOpts *Options) *cobra.Command {
	var fileType string

	cmd := &cobra.Command{
		Use:   "set PROPERTY_NAME PROPERTY_VALUE",
		Args:  cobra.ExactArgs(2),
		Short: "Set a single value in a configuration file",
		Long: `Set a single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to set. It can either represent a field or a map entry.

Paths are literal: they name the exact location in the configuration file, starting from a top-level section ("stacks.<name>.", "cloud.<entry>.", "contexts.<name>.", "resources.", "current-context"). Nothing is resolved against the current context - the path you type is the path you see in "gcx config view".

PROPERTY_VALUE is the new value to set.`,
		Example: `
	# Set the "server" field on the "dev-instance" stack
	gcx config set stacks.dev-instance.grafana.server https://grafana-dev.example

	# Disable the validation of the server's SSL certificate on a stack
	gcx config set stacks.dev-instance.grafana.tls.insecure-skip-verify true

	# Set the default prometheus datasource for a context
	gcx config set contexts.dev.datasources.prometheus my-prom-uid

	# Set a cloud entry's token in the local config layer
	gcx config set --file local cloud.grafana-com.token my-token`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, target, err := config.LoadForWrite(cmd.Context(), configOpts.ConfigFile, fileType)
			if err != nil && (configOpts.ConfigFile == "" || !config.CanInitializeMissingSource(cfg, err)) {
				return err
			}
			// A missing explicit --config path is a deliberate new write target.
			// LoadForWrite's returned Config retains its absent-source revision,
			// so Write atomically installs the first document without replacing a
			// file created concurrently. Other mutators keep treating ENOENT as an
			// error; only `config set` has constructive intent.

			path, err := config.ValidateConfigPath(cfg, args[0])
			if err != nil {
				return err
			}

			if err := setConfigValue(&cfg, path, args[1]); err != nil {
				return err
			}

			return config.Write(cmd.Context(), target, cfg)
		},
	}

	cmd.Flags().StringVar(&fileType, "file", "", "Config layer to write to (system, user, local)")

	return cmd
}

func setConfigValue(cfg *config.Config, path, value string) error {
	mutationPaths := []string{path}
	clearPaths := []string{}
	parts := strings.Split(path, ".")
	if value != "" && len(parts) == 3 && parts[0] == "cloud" {
		prefix := strings.Join(parts[:2], ".") + "."
		switch parts[2] {
		case "token":
			mutationPaths = append(mutationPaths, prefix+"oauth-token")
			clearPaths = append(clearPaths,
				prefix+"oauth-token",
				prefix+"oauth-token-expires-at",
				prefix+"oauth-scopes",
			)
		case "oauth-token":
			mutationPaths = append(mutationPaths, prefix+"token")
			clearPaths = append(clearPaths,
				prefix+"token",
				prefix+"oauth-token-expires-at",
				prefix+"oauth-scopes",
			)
		}
	}

	completeMutations := make([]func() error, 0, len(mutationPaths))
	for _, mutationPath := range mutationPaths {
		completeMutations = append(completeMutations, cfg.PrepareSecretPathMutation(mutationPath))
	}
	if err := config.SetValue(cfg, path, value); err != nil {
		return err
	}
	for _, clearPath := range clearPaths {
		if err := config.UnsetValue(cfg, clearPath); err != nil {
			return err
		}
	}
	for _, completeMutation := range completeMutations {
		if err := completeMutation(); err != nil {
			return err
		}
	}
	return nil
}

func unsetCmd(configOpts *Options) *cobra.Command {
	var fileType string

	cmd := &cobra.Command{
		Use:   "unset PROPERTY_NAME",
		Args:  cobra.ExactArgs(1),
		Short: "Unset a single value in a configuration file",
		Long: `Unset a single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to unset. It can either represent a field or a map entry.

Paths are literal: they name the exact location in the configuration file, starting from a top-level section ("stacks.<name>.", "cloud.<entry>.", "contexts.<name>.", "resources.", "current-context"). Nothing is resolved against the current context - the path you type is the path you see in "gcx config view".`,
		Example: `
	# Unset the "foo" context
	gcx config unset contexts.foo

	# Unset the "insecure-skip-verify" TLS setting on the "dev-instance" stack
	gcx config unset stacks.dev-instance.grafana.tls.insecure-skip-verify

	# Unset a cloud entry's token in the local config layer
	gcx config unset --file local cloud.grafana-com.token`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, target, err := config.LoadForWrite(cmd.Context(), configOpts.ConfigFile, fileType)
			if err != nil {
				return err
			}

			path, err := config.ValidateConfigPath(cfg, args[0])
			if err != nil {
				return err
			}

			completeSecretMutation := cfg.PrepareSecretPathMutation(path)
			if err := config.UnsetValue(&cfg, path); err != nil {
				return err
			}
			if err := completeSecretMutation(); err != nil {
				return err
			}

			return config.Write(cmd.Context(), target, cfg)
		},
	}

	cmd.Flags().StringVar(&fileType, "file", "", "Config layer to write to (system, user, local)")

	return cmd
}
