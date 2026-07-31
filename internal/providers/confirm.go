package providers

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
)

// ErrAgentModeRequiresForce is returned when agent mode is active but --force
// was not passed. Exported so callers with custom prompts can check for it.
var ErrAgentModeRequiresForce = errors.New("destructive operation in agent mode: use --force to proceed")

// CheckDestructiveBypass runs the bypass chain for destructive operations.
// Returns (true, nil) if the operation should proceed without prompting
// (--force or GCX_AUTO_APPROVE), (false, nil) if the caller should prompt
// interactively, or (false, error) if the operation must be rejected
// (agent mode without --force).
//
// This is the single source of truth for bypass logic. Both
// [ConfirmDestructive] and callers with custom prompts (e.g. slug-typing
// confirmation for stack deletion) should use this.
func CheckDestructiveBypass(force bool) (bool, error) {
	if force {
		return true, nil
	}

	cliOpts, err := config.LoadCLIOptions()
	if err != nil {
		return false, err
	}

	if cliOpts.AutoApprove {
		return true, nil
	}

	if agent.IsAgentMode() {
		return false, ErrAgentModeRequiresForce
	}

	return false, nil
}

// ConfirmDestructive prompts the user to confirm a destructive operation.
//
// Bypass chain (via [CheckDestructiveBypass]):
//  1. --force flag → proceed immediately
//  2. GCX_AUTO_APPROVE env var → proceed (CI/CD pipelines)
//  3. Agent mode detected without --force → fail with actionable error
//  4. Otherwise → interactive [y/N] prompt (returns false on EOF or "no")
//
// Agent mode requires explicit --force so that agents must deliberately
// acknowledge destructive operations rather than silently proceeding.
func ConfirmDestructive(in io.Reader, out io.Writer, force bool, prompt string) (bool, error) {
	bypass, err := CheckDestructiveBypass(force)
	if bypass || err != nil {
		return bypass, err
	}

	fmt.Fprintf(out, "%s [y/N] ", prompt)

	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		// Typically EOF on a closed/non-interactive stdin (scripts, CI).
		return false, fmt.Errorf("confirmation unavailable (no interactive input): use --force to proceed: %w", err)
	}

	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		cmdio.Info(out, "Aborted.")
		return false, nil
	}

	return true, nil
}
