package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gcxClient invokes the gcx binary that dispatched this extension. Extensions
// never receive a Grafana credential: every Grafana call goes back through gcx,
// which owns auth, token refresh, and context resolution.
type gcxClient struct {
	bin     string
	context string
	stderr  *os.File
}

func newGCXClient() (*gcxClient, error) {
	bin := os.Getenv("GCX_EXT_GCX_BIN")
	if bin == "" {
		return nil, fmt.Errorf("GCX_EXT_GCX_BIN is not set: run this through `gcx ext %s`", extensionName())
	}
	return &gcxClient{bin: bin, context: os.Getenv("GCX_EXT_CONTEXT"), stderr: os.Stderr}, nil
}

func extensionName() string {
	if n := os.Getenv("GCX_EXT_NAME"); n != "" {
		return n
	}
	return "azure-datasources"
}

// args appends the context the parent gcx resolved, so a --context on the outer
// invocation is honoured rather than silently falling back to current-context.
func (g *gcxClient) args(args []string) []string {
	if g.context == "" {
		return args
	}
	return append(args, "--context", g.context)
}

func (g *gcxClient) run(ctx context.Context, env []string, args ...string) ([]byte, error) {
	full := g.args(args)
	cmd := exec.CommandContext(ctx, g.bin, full...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("gcx %s: %w: %s", strings.Join(full, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// json runs a gcx command and decodes its stdout. It decodes even when gcx
// exits non-zero: a partial failure (exit 4) still writes a complete result
// document on stdout, and that document carries the per-item reason. The
// command error is still returned so the caller knows the run did not fully
// succeed.
func (g *gcxClient) json(ctx context.Context, out any, args ...string) error {
	data, runErr := g.run(ctx, nil, append(args, "--output", "json")...)
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil && runErr == nil {
			return fmt.Errorf("parsing output of `gcx %s`: %w", strings.Join(args, " "), err)
		}
	}
	return runErr
}

// stdinJSON pipes a manifest into a gcx command that reads from stdin.
func (g *gcxClient) stdinJSON(ctx context.Context, in []byte, env []string, out any, args ...string) error {
	full := g.args(append(args, "--output", "json"))
	cmd := exec.CommandContext(ctx, g.bin, full...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = bytes.NewReader(in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gcx %s: %w: %s", strings.Join(full, " "), err, strings.TrimSpace(stderr.String()))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(stdout.Bytes(), out)
}

// datasource is the subset of `gcx datasources list --output json` this
// extension reads.
type datasource struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (g *gcxClient) listDatasources(ctx context.Context) ([]datasource, error) {
	var payload struct {
		Datasources []datasource `json:"datasources"`
		Items       []datasource `json:"items"`
	}
	if err := g.json(ctx, &payload, "datasources", "list"); err != nil {
		return nil, err
	}
	if len(payload.Datasources) > 0 {
		return payload.Datasources, nil
	}
	return payload.Items, nil
}

// currentContext reports the config context gcx will use, for naming and for
// the confirmation prompt.
func (g *gcxClient) currentContext(ctx context.Context) string {
	if g.context != "" {
		return g.context
	}
	var payload struct {
		CurrentContext string `json:"current-context"`
	}
	if err := g.json(ctx, &payload, "config", "current-context"); err != nil {
		return ""
	}
	return payload.CurrentContext
}
