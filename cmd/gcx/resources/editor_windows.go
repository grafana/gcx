package resources

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/grafana/grafana-app-sdk/logging"
)

func (e editor) openEditor(ctx context.Context, file string) error {
	logger := logging.FromContext(ctx).With(slog.String("component", "editor"))

	args := make([]string, len(e.shellArgs)+1)
	copy(args, e.shellArgs)

	// Use double-quotation around whole command line string
	// See https://stackoverflow.com/a/6378038
	args[len(args)-1] = fmt.Sprintf(`"%s %s"`, e.editorName, cmdQuoteArg(file))

	logger.Debug("Starting editor", slog.String("command", strings.Join(args, " ")))

	// Pass all arguments to cmd.exe as one string
	// See https://pkg.go.dev/os/exec#Command
	//nolint:gosec
	cmd := exec.Command(args[0])
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.CmdLine = strings.Join(args, " ")

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

// Enclose argument in double-quotes. Double each double-quote character as
// an escape sequence.
func cmdQuoteArg(arg string) string {
	var result strings.Builder
	result.WriteString(`"`)
	result.WriteString(strings.ReplaceAll(arg, `"`, `""`))
	result.WriteString(`"`)
	return result.String()
}
