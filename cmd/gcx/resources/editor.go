package resources

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/grafana/grafana-app-sdk/logging"
)

type editor struct {
	shellArgs  []string
	editorName string
}

const (
	defaultShell  = "/bin/bash"
	defaultEditor = "vi"
	windowsShell  = "cmd"
	windowsEditor = "notepad"
)

func editorFromEnv() editor {
	shell := os.Getenv("SHELL")
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
	editorName := os.Getenv("VISUAL")
	if editorName == "" {
		editorName = os.Getenv("EDITOR")
	}
	if editorName == "" {
		editorName = platformize(defaultEditor, windowsEditor)
	}

	return editor{
		shellArgs:  []string{shell, flag},
		editorName: editorName,
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

	f, err := os.CreateTemp("", tmpFilePattern)
	if err != nil {
		return cleanup, nil, err
	}
	defer f.Close()

	cleanup = func() {
		os.Remove(f.Name())
	}

	logger.Debug("Temporary file created", slog.String("path", f.Name()))
	tmpFilePath := f.Name()

	if _, err := io.Copy(f, buffer); err != nil {
		os.Remove(tmpFilePath)
		return cleanup, nil, err
	}
	// Release the file descriptor to make sure the editor can use it.
	f.Close()

	if err := e.Open(ctx, tmpFilePath); err != nil {
		return cleanup, nil, err
	}

	contents, err := os.ReadFile(tmpFilePath)
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
