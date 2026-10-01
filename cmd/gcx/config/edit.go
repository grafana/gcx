package config

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	internalConfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/host"
	"github.com/spf13/cobra"
)

func editCmd(configOpts *Options) *cobra.Command {
	var create bool

	cmd := &cobra.Command{
		Use:   "edit [type]",
		Short: "Open a config file in $EDITOR",
		Long: `Open a config file in your editor. If multiple config files are loaded,
specify which one to edit: system, user, or local.

If only one config file exists, it is opened directly.`,
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"system", "user", "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			// The editor protocol is unconditionally interactive. Agent mode
			// is an intentional non-interactive contract: never launch an
			// editor (it would hang or fail against a pipe); hand back a
			// structured error naming the scripted alternatives instead.
			if agent.IsAgentMode() {
				return gcxerrors.DetailedError{
					Summary: "interactive editor disabled in agent mode",
					Suggestions: []string{
						"Use 'gcx config set PROPERTY_NAME PROPERTY_VALUE' to change a value",
						"Use 'gcx config unset PROPERTY_NAME' to remove a value",
						"Use 'gcx config view' to inspect the merged configuration",
					},
				}
			}

			target, err := resolveRawEditTarget(cmd.Context(), configOpts.ConfigFile, args, create)
			if err != nil {
				return err
			}
			return openInEditor(cmd.Context(), target)
		},
	}

	cmd.Flags().BoolVar(&create, "create", false, "Create the config file if it doesn't exist")

	return cmd
}

// resolveRawEditTarget selects the config document to edit without decoding it.
// Editing is the recovery path for malformed YAML, unsupported future versions,
// and semantic errors that prevent the ordinary loader from returning a Config.
// It must therefore depend only on explicit selection and filesystem discovery.
func resolveRawEditTarget(ctx context.Context, explicitFile string, args []string, create bool) (string, error) {
	if explicitFile != "" {
		if len(args) != 0 {
			return "", errors.New("cannot combine --config with a config layer; remove the layer argument to edit the explicit file")
		}
		if err := ensureEditableConfigExists(ctx, explicitFile); err != nil {
			return "", err
		}
		return explicitFile, nil
	}

	if len(args) == 1 {
		typ := args[0]
		if create {
			return createConfigForType(ctx, typ)
		}
		// A named layer is an explicit repair choice and therefore wins over
		// GCX_CONFIG. This lets users repair a discovered document even while
		// their shell normally selects a separate explicit config.
		sources, err := internalConfig.DiscoverSources(ctx)
		if err != nil {
			return "", err
		}
		for _, source := range sources {
			if source.Type == typ {
				return source.Path, nil
			}
		}
		return "", fmt.Errorf("no %s config file found (use --create to create one)", typ)
	}

	// With no named layer, GCX_CONFIG is the same explicit-file bypass used by
	// the ordinary loader. Do not fall through to discovery and accidentally
	// open a different document.
	if envFile := host.Getenv(ctx, internalConfig.ConfigFileEnvVar); envFile != "" {
		if err := ensureEditableConfigExists(ctx, envFile); err != nil {
			return "", err
		}
		return envFile, nil
	}

	sources, err := internalConfig.DiscoverSources(ctx)
	if err != nil {
		return "", err
	}
	switch len(sources) {
	case 0:
		return "", errors.New("no config files found; use 'gcx config edit user --create' to create one")
	case 1:
		return sources[0].Path, nil
	default:
		var b strings.Builder
		b.WriteString("multiple config files loaded; specify which to edit:\n")
		for _, source := range sources {
			fmt.Fprintf(&b, "  gcx config edit %s\n", source.Type)
		}
		return "", errors.New(b.String())
	}
}

func ensureEditableConfigExists(ctx context.Context, path string) error {
	info, err := host.Stat(ctx, path)
	if err != nil {
		return fmt.Errorf("cannot edit config %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot edit config %s: not a regular file", path)
	}
	return nil
}

func createConfigForType(ctx context.Context, typ string) (string, error) {
	switch typ {
	case "local":
		localPath, err := host.Abs(ctx, internalConfig.LocalConfigFileName)
		if err != nil {
			return "", err
		}
		if err := internalConfig.CreateDefaultConfigFile(ctx, localPath); err != nil {
			return "", fmt.Errorf("failed to create %s: %w", localPath, err)
		}
		return localPath, nil
	case "user":
		// Use XDG to find the user config path.
		source := internalConfig.StandardLocation()
		path, err := source(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to create user config: %w", err)
		}
		return path, nil
	default:
		return "", fmt.Errorf("cannot create %s config file; only 'local' and 'user' are supported with --create", typ)
	}
}

func openInEditor(ctx context.Context, path string) error {
	editor := host.Getenv(ctx, "EDITOR")
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "vi"
		}
	}

	abs, err := host.Abs(ctx, path)
	if err != nil {
		return err
	}

	editorCmd, err := host.Command(ctx, editor, abs)
	if err != nil {
		return err
	}
	editorCmd.Stdin = host.Stdin(ctx)
	editorCmd.Stdout = host.Stdout(ctx)
	editorCmd.Stderr = host.Stderr(ctx)
	return editorCmd.Run()
}
