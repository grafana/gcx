package azure

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/grafana/gcx/internal/agent"
	azonboard "github.com/grafana/gcx/internal/onboard/azure"
	"github.com/grafana/gcx/internal/terminal"
	"golang.org/x/term"
)

// isInteractive reports whether to render the TUI pickers. Both --yes and
// --force imply "accept all suggestions", so either one (like a non-TTY or
// agent mode) turns the picker off. This lets agent/CI callers provision with a
// single --force flag instead of pairing it with --yes.
func isInteractive(opts *azureOpts) bool {
	return terminal.StdoutIsTerminal() &&
		term.IsTerminal(int(os.Stdin.Fd())) &&
		!opts.Yes &&
		!opts.Force &&
		!agent.IsAgentMode()
}

// pickSuggestions renders the interactive datasource multiselect. Disabled
// suggestions (e.g. stopped ADX clusters) are shown unchecked and labelled
// "(not selectable)"; a Validate guard refuses to submit if one is checked,
// since huh has no native per-option disabling.
func pickSuggestions(suggestions []azonboard.Suggestion) ([]azonboard.Suggestion, error) {
	options := make([]huh.Option[int], 0, len(suggestions))
	var picked []int
	for i, s := range suggestions {
		label := s.Label
		if s.Disabled {
			label += " (not selectable — " + s.DisabledReason + ")"
		}
		options = append(options, huh.NewOption(label, i).Selected(!s.Disabled))
		if !s.Disabled {
			picked = append(picked, i)
		}
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[int]().
			Title("Datasources to create").
			Options(options...).
			Validate(func(sel []int) error {
				for _, i := range sel {
					if suggestions[i].Disabled {
						return fmt.Errorf("%s is not selectable: %s", suggestions[i].Label, suggestions[i].DisabledReason)
					}
				}
				return nil
			}).
			Value(&picked),
	))
	if err := form.Run(); err != nil {
		return nil, err
	}
	chosen := make([]azonboard.Suggestion, 0, len(picked))
	for _, i := range picked {
		chosen = append(chosen, suggestions[i])
	}
	return chosen, nil
}

// confirmRollback lists the changes that can be reverted and asks the user
// whether to undo them. Defaults to reverting.
func confirmRollback(errOut io.Writer, steps []string) (bool, error) {
	fmt.Fprintln(errOut, "The following changes were made before the error and can be reverted:")
	for _, s := range steps {
		fmt.Fprintf(errOut, "  - %s\n", s)
	}
	revert := true
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Revert these changes?").
			Affirmative("Revert").
			Negative("Keep them").
			Value(&revert),
	))
	if err := form.Run(); err != nil {
		return false, err
	}
	return revert, nil
}

// confirmInstallPlugin asks whether to install a missing required plugin.
// Defaults to installing.
func confirmInstallPlugin(pluginID string) (bool, error) {
	install := true
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("The %q plugin is required but not installed. Install it now?", pluginID)).
			Affirmative("Install").
			Negative("Skip").
			Value(&install),
	))
	if err := form.Run(); err != nil {
		return false, err
	}
	return install, nil
}
