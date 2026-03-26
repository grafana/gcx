package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/caarlos0/env/v11"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/grafana"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/secrets"
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

			if curCtx.Grafana == nil {
				curCtx.Grafana = &config.GrafanaConfig{}
			}

			if err := env.Parse(curCtx); err != nil {
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

		return cfg.GetCurrentContext().Validate()
	}

	return opts.LoadConfigTolerant(ctx, validator)
}

// LoadGrafanaConfig loads the configuration file and constructs a REST config from it.
func (opts *Options) LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error) {
	cfg, err := opts.LoadConfig(ctx)
	if err != nil {
		return config.NamespacedRESTConfig{}, err
	}

	return cfg.GetCurrentContext().ToRESTConfig(ctx), nil
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

			if opts.IO.JSONDiscovery {
				cfgMap, err := structToMap(cfg)
				if err != nil {
					return err
				}
				for _, field := range cmdio.DiscoverFields(cfgMap) {
					fmt.Fprintln(cmd.OutOrStdout(), field)
				}
				return nil
			}

			if len(opts.IO.JSONFields) > 0 {
				codec := cmdio.NewFieldSelectCodec(opts.IO.JSONFields)
				return codec.Encode(cmd.OutOrStdout(), cfg)
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

			tab := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', tabwriter.TabIndent|tabwriter.DiscardEmptyColumns)

			fmt.Fprintf(tab, "CURRENT\tNAME\tGRAFANA SERVER\n")
			for _, context := range cfg.Contexts {
				server := " "
				if context.Grafana != nil {
					server = context.Grafana.Server
				}

				current := " "
				if cfg.CurrentContext == context.Name {
					current = "*"
				}

				fmt.Fprintf(tab, "%s\t%s\t%s\n", current, context.Name, server)
			}

			return tab.Flush()
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

			cmdio.Success(stdout, "Configuration file: %s", cmdio.Green(cfg.Source))

			switch {
			case cfg.CurrentContext == "":
				cmdio.Error(stdout, "Current context: %s", cmdio.Red("<undefined>"))
			case !cfg.HasContext(cfg.CurrentContext):
				cmdio.Error(stdout, "Current context: %s", cmdio.Red(config.ContextNotFound(cfg.CurrentContext).Error()))
			default:
				cmdio.Success(stdout, "Current context: %s", cmdio.Green(cfg.CurrentContext))
			}

			cmd.Println()

			var checkErr error
			for _, gCtx := range cfg.Contexts {
				if err := checkContext(cmd, gCtx); err != nil {
					checkErr = err
				}
			}

			return checkErr
		},
	}

	return cmd
}

func checkContext(cmd *cobra.Command, gCtx *config.Context) error {
	stdout := cmd.OutOrStdout()
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
			stdout.Write([]byte("\n"))
		}
	}

	cmd.Println(cmdio.Yellow(title))
	cmd.Println(cmdio.Yellow(strings.Repeat("=", titleLen)))

	if err := gCtx.Validate(); err != nil {
		cmdio.Error(stdout, "Configuration: %s", cmdio.Red(summarizeError(err)))
		cmdio.Warning(stdout, "Connectivity: %s", cmdio.Yellow("skipped"))
		cmdio.Warning(stdout, "Grafana version: %s", cmdio.Yellow("skipped")+"\n")

		printSuggestions(err)
		return nil
	}

	cmdio.Success(stdout, "Configuration: %s", cmdio.Green("valid"))

	if _, err := discovery.NewDefaultRegistry(cmd.Context(), config.NewNamespacedRESTConfig(cmd.Context(), *gCtx)); err != nil {
		cmdio.Error(stdout, "Connectivity: %s", cmdio.Red(summarizeError(err)))
		cmdio.Warning(stdout, "Grafana version: %s", cmdio.Yellow("skipped")+"\n")
		printSuggestions(err)
		return nil
	}

	cmdio.Success(stdout, "Connectivity: %s", cmdio.Green("online"))

	version, err := grafana.GetVersion(gCtx)
	if err != nil {
		cmdio.Error(stdout, "Grafana version: %s", cmdio.Red(summarizeError(err))+"\n")
		return nil
	}

	if version.Major() < 12 {
		return &grafana.VersionIncompatibleError{Version: version}
	}

	cmdio.Success(stdout, "Grafana version: %s", cmdio.Green(version.String())+"\n")

	return nil
}

func useContextCmd(configOpts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "use-context CONTEXT_NAME",
		Args:    cobra.ExactArgs(1),
		Aliases: []string{"use"},
		Short:   "Set the current context",
		Long:    "Set the current context and updates the configuration file.",
		Example: "\n\tgcx config use-context dev-instance",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configOpts.LoadConfigTolerant(cmd.Context())
			if err != nil {
				return err
			}

			if !cfg.HasContext(args[0]) {
				return config.ContextNotFound(args[0])
			}

			cfg.CurrentContext = args[0]

			if err := config.Write(cmd.Context(), configOpts.ConfigSource(), cfg); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Context set to \"%s\"", cfg.CurrentContext)
			return nil
		},
	}

	return cmd
}

// resolveWriteTarget determines which config file to write to based on --config flag,
// --file flag, and the number of discovered sources.
func resolveWriteTarget(configOpts *Options, fileType string, ctx context.Context) (config.Source, error) {
	// --config flag always wins.
	if configOpts.ConfigFile != "" {
		return config.ExplicitConfigFile(configOpts.ConfigFile), nil
	}

	// --file flag targets a specific layer.
	if fileType != "" {
		layered, err := config.LoadLayered(ctx, "")
		if err != nil {
			return nil, err
		}
		for _, s := range layered.Sources {
			if s.Type == fileType {
				return config.ExplicitConfigFile(s.Path), nil
			}
		}
		return nil, fmt.Errorf("no %s config file found", fileType)
	}

	// No flags — check if ambiguous.
	layered, err := config.LoadLayered(ctx, "")
	if err != nil {
		return nil, err
	}
	if len(layered.Sources) > 1 {
		return nil, errors.New("multiple config files loaded; specify which to edit with --file (system, user, local)")
	}

	// Single source or no sources — use existing behavior.
	return configOpts.ConfigSource(), nil
}

func setCmd(configOpts *Options) *cobra.Command {
	var fileType string

	cmd := &cobra.Command{
		Use:   "set PROPERTY_NAME PROPERTY_VALUE",
		Args:  cobra.ExactArgs(2),
		Short: "Set a single value in a configuration file",
		Long: `Set a single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to set. It can either represent a field or a map entry.

PROPERTY_VALUE is the new value to set.`,
		Example: `
	# Set the "server" field on the "dev-instance" context to "https://grafana-dev.example"
	gcx config set contexts.dev-instance.grafana.server https://grafana-dev.example

	# Disable the validation of the server's SSL certificate in the "dev-instance" context
	gcx config set contexts.dev-instance.grafana.insecure-skip-tls-verify true

	# Set a value in the local config layer
	gcx config set --file local contexts.prod.cloud.token my-token`,
		RunE: func(cmd *cobra.Command, args []string) error {
			targetSource, err := resolveWriteTarget(configOpts, fileType, cmd.Context())
			if err != nil {
				return err
			}

			cfg, err := config.Load(cmd.Context(), targetSource)
			if err != nil {
				return err
			}

			if err := config.SetValue(&cfg, args[0], args[1]); err != nil {
				return err
			}

			return config.Write(cmd.Context(), targetSource, cfg)
		},
	}

	cmd.Flags().StringVar(&fileType, "file", "", "Config layer to write to (system, user, local)")

	return cmd
}

func unsetCmd(configOpts *Options) *cobra.Command {
	var fileType string

	cmd := &cobra.Command{
		Use:   "unset PROPERTY_NAME",
		Args:  cobra.ExactArgs(1),
		Short: "Unset a single value in a configuration file",
		Long: `Unset a single value in a configuration file.

PROPERTY_NAME is a dot-delimited reference to the value to unset. It can either represent a field or a map entry.`,
		Example: `
	# Unset the "foo" context
	gcx config unset contexts.foo

	# Unset the "insecure-skip-tls-verify" flag in the "dev-instance" context
	gcx config unset contexts.dev-instance.grafana.insecure-skip-tls-verify

	# Unset a value in the local config layer
	gcx config unset --file local contexts.prod.cloud.token`,
		RunE: func(cmd *cobra.Command, args []string) error {
			targetSource, err := resolveWriteTarget(configOpts, fileType, cmd.Context())
			if err != nil {
				return err
			}

			cfg, err := config.Load(cmd.Context(), targetSource)
			if err != nil {
				return err
			}

			if err := config.UnsetValue(&cfg, args[0]); err != nil {
				return err
			}

			return config.Write(cmd.Context(), targetSource, cfg)
		},
	}

	cmd.Flags().StringVar(&fileType, "file", "", "Config layer to write to (system, user, local)")

	return cmd
}

// structToMap marshals an arbitrary struct to map[string]any via JSON round-trip.
// Used by viewCmd for --json ? field discovery.
func structToMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
