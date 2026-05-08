package providers

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
)

// ConfirmDestructive prompts the user to confirm a destructive operation.
// It auto-approves when force is true, agent mode is active, stdin is not a
// terminal, or the GCX_AUTO_APPROVE env var is truthy. Returns true if the
// caller should proceed.
func ConfirmDestructive(in io.Reader, out io.Writer, force bool, prompt string) (bool, error) {
	if force {
		return true, nil
	}

	if agent.IsAgentMode() {
		return true, nil
	}

	cliOpts, err := config.LoadCLIOptions()
	if err != nil {
		return false, err
	}

	if cliOpts.AutoApprove {
		return true, nil
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
