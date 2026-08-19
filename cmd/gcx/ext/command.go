// Package ext implements `gcx ext`: installing, listing, and running
// third-party gcx extensions (ADR-023).
package ext

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/extensions"
	"github.com/grafana/gcx/internal/gcxerrors"
	appversion "github.com/grafana/gcx/internal/version"
	"github.com/spf13/cobra"
)

// CommandName is the area name extensions live under.
const CommandName = "ext"

// Verbs are the subcommand names `ext` owns. Anything else in the first
// argument position is treated as an installed extension's name — the same
// "known verbs win, anything else is a name" rule git uses.
func Verbs() []string {
	return []string{"install", "list", "uninstall", "update"}
}

// Command returns the `ext` command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   CommandName + " [name] [args...]",
		Short: "Install and run third-party extensions",
		Long: "Install, manage, and run third-party gcx extensions.\n\n" +
			"Extensions are not audited by Grafana. Anything you install runs with " +
			"your full user permissions on this machine: review an extension's source " +
			"and its publisher before installing it.\n\n" +
			"Arguments after an extension's name are passed to it verbatim, so gcx's " +
			"own global flags must come before 'ext' (gcx --context prod ext my-ext --flag).",
		Args: cobra.ArbitraryArgs,
		RunE: run,
		Example: "  # Install from a local checkout, a manifest URL, or any git remote\n" +
			"  gcx ext install ./my-extension\n" +
			"  gcx ext install https://example.com/my-extension/gcx-extension.yaml\n" +
			"  gcx ext install https://github.com/acme/gcx-ext-thing.git\n\n" +
			"  # Run it\n" +
			"  gcx ext my-extension --help",
	}

	cmd.AddCommand(newInstallCommand(), newListCommand(), newUninstallCommand(), newUpdateCommand())
	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	name := args[0]
	store, err := extensions.DefaultStore()
	if err != nil {
		return err
	}

	installed, err := store.Lookup(name)
	if err != nil {
		return unknownExtensionError(store, name)
	}

	gcxBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving the path to gcx: %w", err)
	}

	runErr := installed.Run(cmd.Context(), extensions.RunOptions{
		Args:       args[1:],
		Stdin:      cmd.InOrStdin(),
		Stdout:     cmd.OutOrStdout(),
		Stderr:     cmd.ErrOrStderr(),
		GCXBin:     gcxBin,
		Context:    resolveContextName(cmd),
		AgentMode:  agent.IsAgentMode(),
		GCXVersion: appversion.Get(),
	})

	// An extension owns its own output and exit-code contract. Propagate its
	// code without wrapping it in a gcx-formatted error, which would print a
	// second, misleading error after the extension already reported one.
	var exitErr *extensions.ExitError
	if errors.As(runErr, &exitErr) {
		return gcxerrors.NewAlreadyReportedError(exitErr.Code)
	}
	return runErr
}

// resolveContextName returns the config context this invocation targets, so the
// extension can pass it back on every `gcx` call it makes.
func resolveContextName(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("context"); f != nil && f.Value.String() != "" {
		return f.Value.String()
	}
	if name := internalconfig.ContextNameFromCtx(cmd.Context()); name != "" {
		return name
	}
	cfg, err := internalconfig.Load(cmd.Context(), internalconfig.StandardLocation())
	if err != nil {
		return ""
	}
	return cfg.CurrentContext
}

func unknownExtensionError(store *extensions.Store, name string) error {
	installed, err := store.List()
	if err != nil {
		return err
	}
	suggestions := []string{"Install it with 'gcx ext install <source>'"}
	if len(installed) > 0 {
		names := make([]string, 0, len(installed))
		for _, e := range installed {
			names = append(names, e.Name)
		}
		suggestions = append(suggestions, "Installed extensions: "+strings.Join(names, ", "))
	} else {
		suggestions = append(suggestions, "No extensions are installed yet")
	}
	return &fail.UsageError{
		Message:     fmt.Sprintf("unknown command or extension %q", name),
		Suggestions: suggestions,
	}
}
