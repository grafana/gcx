//go:build !windows

package resources

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/grafana/grafana-app-sdk/logging"
)

func (e editor) openEditor(ctx context.Context, file string) error {
	logger := logging.FromContext(ctx).With(slog.String("component", "editor"))

	args := make([]string, len(e.shellArgs)+1)
	copy(args, e.shellArgs)

	args[len(e.shellArgs)] = fmt.Sprintf("%s %q", e.editorName, file)

	logger.Debug("Starting editor", slog.String("command", strings.Join(args, " ")))

	//nolint:gosec
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}
