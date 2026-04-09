package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	internalConfig "github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
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
			cfg, err := configOpts.loadConfigTolerantLayered(cmd.Context())
			if err != nil {
				// If config is unparseable (invalid YAML / unknown fields), fall back
				// to opening the broken file directly so the user can fix it.
				var unmarshalErr internalConfig.UnmarshalError
				if !errors.As(err, &unmarshalErr) {
					return err
				}

				fallbackPath, fbErr := editFallbackPath(unmarshalErr, args)
				if fbErr != nil {
					return fbErr
				}

				cmdio.Warning(cmd.ErrOrStderr(),
					"Configuration in %s could not be parsed; opening in editor so you can fix it.", fallbackPath)
				return openInEditor(cmd.Context(), fallbackPath)
			}

			sources := cfg.Sources
			var target internalConfig.ConfigSource

			switch {
			case len(args) == 1:
				typ := args[0]
				if create {
					// Create config file if missing.
					path, createErr := createConfigForType(typ)
					if createErr != nil {
						return createErr
					}
					target = internalConfig.ConfigSource{Path: path, Type: typ}
				} else {
					found := false
					for _, s := range sources {
						if s.Type == typ {
							target = s
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("no %s config file found (use --create to create one)", typ)
					}
				}
			case len(sources) == 1:
				target = sources[0]
			case len(sources) == 0:
				return errors.New("no config files found; use 'gcx config edit user --create' to create one")
			default:
				var b strings.Builder
				b.WriteString("multiple config files loaded; specify which to edit:\n")
				for _, s := range sources {
					fmt.Fprintf(&b, "  gcx config edit %s\n", s.Type)
				}
				return errors.New(b.String())
			}

			return openInEditor(cmd.Context(), target.Path)
		},
	}

	cmd.Flags().BoolVar(&create, "create", false, "Create the config file if it doesn't exist")

	return cmd
}

func createConfigForType(typ string) (string, error) {
	switch typ {
	case "local":
		localPath, err := filepath.Abs(internalConfig.LocalConfigFileName)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			if err := os.WriteFile(localPath, []byte(internalConfig.DefaultEmptyConfigFile), 0o600); err != nil {
				return "", fmt.Errorf("failed to create %s: %w", localPath, err)
			}
		}
		return localPath, nil
	case "user":
		// Use XDG to find the user config path.
		source := internalConfig.StandardLocation()
		path, err := source()
		if err != nil {
			return "", fmt.Errorf("failed to create user config: %w", err)
		}
		return path, nil
	default:
		return "", fmt.Errorf("cannot create %s config file; only 'local' and 'user' are supported with --create", typ)
	}
}

// editFallbackPath determines which file to open when the config cannot be parsed.
// If the user asked for a specific type, DiscoverSources finds it without parsing.
// Otherwise the file reported in the UnmarshalError is used.
func editFallbackPath(unmarshalErr internalConfig.UnmarshalError, args []string) (string, error) {
	if len(args) == 1 {
		sources, err := internalConfig.DiscoverSources()
		if err != nil {
			return "", err
		}
		for _, s := range sources {
			if s.Type == args[0] {
				return s.Path, nil
			}
		}
		return "", fmt.Errorf("no %s config file found", args[0])
	}

	if unmarshalErr.File != "" {
		return unmarshalErr.File, nil
	}

	return "", fmt.Errorf("could not determine which config file to edit: %w", unmarshalErr)
}

func openInEditor(ctx context.Context, path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "vi"
		}
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	editorCmd := exec.CommandContext(ctx, editor, abs)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	return editorCmd.Run()
}
