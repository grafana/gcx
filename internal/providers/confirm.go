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

// ConfirmDestructive prompts the user to confirm a destructive operation.
//
// Bypass chain:
//  1. --force flag → proceed immediately
//  2. GCX_AUTO_APPROVE env var → proceed (CI/CD pipelines)
//  3. Agent mode detected without --force → fail with actionable error
//  4. Otherwise → interactive prompt (returns false on EOF or "no")
//
// Agent mode requires explicit --force so that agents must deliberately
// acknowledge destructive operations rather than silently proceeding.
func ConfirmDestructive(in io.Reader, out io.Writer, force bool, prompt string) (bool, error) {
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
		return false, errors.New("destructive operation requires --force in agent mode")
	}

	fmt.Fprintf(out, "%s [y/N] ", prompt)

	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read confirmation: %w", err)
	}

	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		cmdio.Info(out, "Aborted.")
		return false, nil
	}

	return true, nil
}
