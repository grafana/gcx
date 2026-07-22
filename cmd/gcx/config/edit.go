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
			target, err := resolveRawEditTarget(configOpts.ConfigFile, args, create)
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
func resolveRawEditTarget(explicitFile string, args []string, create bool) (string, error) {
	if explicitFile != "" {
		if len(args) != 0 {
			return "", errors.New("cannot combine --config with a config layer; remove the layer argument to edit the explicit file")
		}
		if err := ensureEditableConfigExists(explicitFile); err != nil {
			return "", err
		}
		return explicitFile, nil
	}

	if len(args) == 1 {
		typ := args[0]
		if create {
			return createConfigForType(typ)
		}
		// A named layer is an explicit repair choice and therefore wins over
		// GCX_CONFIG. This lets users repair a discovered document even while
		// their shell normally selects a separate explicit config.
		sources, err := internalConfig.DiscoverSources()
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
	if envFile := os.Getenv(internalConfig.ConfigFileEnvVar); envFile != "" {
		if err := ensureEditableConfigExists(envFile); err != nil {
			return "", err
		}
		return envFile, nil
	}

	sources, err := internalConfig.DiscoverSources()
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

func ensureEditableConfigExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot edit config %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot edit config %s: not a regular file", path)
	}
	return nil
}

func createConfigForType(typ string) (string, error) {
	switch typ {
	case "local":
		localPath, err := filepath.Abs(internalConfig.LocalConfigFileName)
		if err != nil {
			return "", err
		}
		if err := internalConfig.CreateDefaultConfigFile(localPath); err != nil {
			return "", fmt.Errorf("failed to create %s: %w", localPath, err)
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
