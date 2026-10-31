package pluginsdk

import (
	"context"
	"encoding/json"
	"fmt"
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

type commandClient struct{ host CommandHostClient }

func (c commandClient) Execute(ctx context.Context, request CommandRequest) (Result, error) {
	response, err := c.host.Execute(ctx, &ExecuteRequest{Args: request.Args, Stdin: request.Stdin})
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if err != nil {
		return Result{}, err
	}
	return Result{Stdout: response.GetStdout(), Stderr: response.GetStderr(), ExitCode: int(response.GetExitCode())}, nil
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
