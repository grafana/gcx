package resources

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"

	"github.com/grafana/gcx/internal/host"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

type editor struct {
	shellArgs  []string
	editorName string

	stdin          io.Reader
	stdout, stderr io.Writer
}

const (
	defaultShell  = "/bin/bash"
	defaultEditor = "vi"
	windowsShell  = "cmd"
	windowsEditor = "notepad"
)

// editorFromEnv configures the editor from the command's environment; the
// editor process is attached to the command's stdio.
func editorFromEnv(cmd *cobra.Command) editor {
	ctx := cmd.Context()
	shell := host.Getenv(ctx, "SHELL")
	if shell == "" {
		shell = platformize(defaultShell, windowsShell)
	}

	flag := "-c"
	if shell == windowsShell {
		flag = "/C"
	}

	// VISUAL outranks EDITOR per Unix convention — and the agent-mode
	// interactive guard in edit.go treats either as "an editor is
	// configured", so the launcher must honor both or the guard's premise
	// is false (a VISUAL-only environment would fall back to interactive vi
	// against piped stdio in agent mode).
	editorName := host.Getenv(ctx, "VISUAL")
	if editorName == "" {
		editorName = host.Getenv(ctx, "EDITOR")
	}
	if editorName == "" {
		editorName = platformize(defaultEditor, windowsEditor)
	}

	return editor{
		shellArgs:  []string{shell, flag},
		editorName: editorName,
		stdin:      cmd.InOrStdin(),
		stdout:     cmd.OutOrStdout(),
		stderr:     cmd.ErrOrStderr(),
	}
}

func (e editor) Open(ctx context.Context, file string) error {
	logger := logging.FromContext(ctx).With(slog.String("component", "editor"))
	logger.Debug("Opening file", slog.String("path", file))

	absPath, err := filepath.Abs(file)
	if err != nil {
		return err
	}

	return e.openEditor(ctx, absPath)
}

func (e editor) OpenInTempFile(ctx context.Context, buffer io.Reader, format string) (func(), []byte, error) {
	logger := logging.FromContext(ctx).With(slog.String("component", "editor"))
	logger.Debug("Opening buffer")

	cleanup := func() {}

	tmpFilePattern := "gcx-*-edit"
	if format != "" {
		tmpFilePattern += "." + format
	}

	f, err := host.CreateTemp(ctx, "", tmpFilePattern)
	if err != nil {
		return cleanup, nil, err
	}
	defer f.Close()

	cleanup = func() {
		_ = host.Remove(ctx, f.Name())
	}

	logger.Debug("Temporary file created", slog.String("path", f.Name()))
	tmpFilePath := f.Name()

	if _, err := io.Copy(f, buffer); err != nil {
		_ = host.Remove(ctx, tmpFilePath)
		return cleanup, nil, err
	}
	// Release the file descriptor to make sure the editor can use it.
	f.Close()

	if err := e.Open(ctx, tmpFilePath); err != nil {
		return cleanup, nil, err
	}

	contents, err := host.ReadFile(ctx, tmpFilePath)
	if err != nil {
		return cleanup, nil, err
	}

	return cleanup, contents, err
}

func platformize(linux string, windows string) string {
	if runtime.GOOS == "windows" {
		return windows
	}
	return linux
}
