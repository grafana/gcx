//go:build !windows

package resources

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/grafana/gcx/internal/host"
	"github.com/grafana/grafana-app-sdk/logging"
)

func (e editor) openEditor(ctx context.Context, file string) error {
	logger := logging.FromContext(ctx).With(slog.String("component", "editor"))

	args := make([]string, len(e.shellArgs)+1)
	copy(args, e.shellArgs)

	args[len(e.shellArgs)] = fmt.Sprintf("%s %q", e.editorName, file)

	logger.Debug("Starting editor", slog.String("command", strings.Join(args, " ")))

	cmd, err := host.Command(ctx, args[0], args[1:]...)
	if err != nil {
		return err
	}

	cmd.Stdout = e.stdout
	cmd.Stderr = e.stderr
	cmd.Stdin = e.stdin

	return cmd.Run()
}
