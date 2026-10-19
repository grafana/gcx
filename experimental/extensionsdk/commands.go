// Package extensionsdk provides generic subprocess access to gcx for extensions.
package extensionsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CommandRequest struct {
	Args  []string
	Stdin []byte
}

type Result struct {
	Stdout, Stderr []byte
	ExitCode       int
}

type Executor interface {
	Execute(ctx context.Context, request CommandRequest) (Result, error)
}

type Options struct {
	Binary     string
	Prefix     []string
	Context    string
	ConfigFile string
}

type Client struct{ options Options }

func FromEnvironment() (*Client, error) {
	return New(Options{Binary: os.Getenv("GCX_EXT_GCX_BIN"), Context: os.Getenv("GCX_CONTEXT"), ConfigFile: os.Getenv("GCX_CONFIG")})
}

func New(options Options) (*Client, error) {
	if !filepath.IsAbs(options.Binary) {
		return nil, errors.New("GCX_EXT_GCX_BIN must be an absolute executable path: launch through gcx ext")
	}
	if options.ConfigFile != "" {
		path, err := filepath.Abs(options.ConfigFile)
		if err != nil {
			return nil, err
		}
		options.ConfigFile = path
	}
	options.Prefix = append([]string(nil), options.Prefix...)
	return &Client{options: options}, nil
}

func (c *Client) Execute(ctx context.Context, request CommandRequest) (Result, error) {
	if len(request.Args) == 0 {
		return Result{}, errors.New("gcx command arguments are required")
	}
	args := append(append([]string(nil), c.options.Prefix...), request.Args...)
	cmd := exec.CommandContext(ctx, c.options.Binary, args...) //nolint:gosec // The dispatcher selects the executable; arguments never pass through a shell.
	cmd.Env = append(os.Environ(), "GCX_CONTEXT="+c.options.Context)
	if c.options.ConfigFile != "" {
		cmd.Env = append(cmd.Env, "GCX_CONFIG="+c.options.ConfigFile)
	}
	cmd.Stdin = bytes.NewReader(request.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.ExitCode = exit.ExitCode()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if exit != nil {
		return result, nil //nolint:nilerr // Nonzero exits are results so callers can retain partial stdout.
	}
	return result, err
}

type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

func (r Result) Err() error {
	if r.ExitCode == 0 {
		return nil
	}
	message := strings.TrimSpace(string(r.Stderr))
	var envelope struct {
		Error struct {
			Summary string `json:"summary"`
			Details string `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(r.Stdout, &envelope) == nil && envelope.Error.Summary != "" {
		message = envelope.Error.Summary
		if envelope.Error.Details != "" {
			message += ": " + envelope.Error.Details
		}
	}
	if message == "" {
		message = fmt.Sprintf("gcx exited with status %d", r.ExitCode)
	}
	return &ExitError{Code: r.ExitCode, Message: message}
}

func DecodeJSON(ctx context.Context, executor Executor, out any, args ...string) error {
	result, err := executor.Execute(ctx, CommandRequest{Args: append(args, "--output", "json")})
	if err != nil {
		return err
	}
	if err := result.Err(); err != nil {
		return err
	}
	if err := json.Unmarshal(result.Stdout, out); err != nil {
		return fmt.Errorf("decode gcx JSON: %w", err)
	}
	return nil
}
