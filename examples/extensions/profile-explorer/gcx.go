package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var errNoProfileData = errors.New("no profile data in this time range")

// gcxClient invokes the gcx binary that dispatched this extension. Extensions
// never receive a Grafana credential: every Grafana call goes back through gcx,
// which owns auth, token refresh, and context resolution.
type gcxClient struct {
	bin     string
	context string
}

func newGCXClient() (*gcxClient, error) {
	bin := os.Getenv("GCX_EXT_GCX_BIN")
	if bin == "" {
		return nil, fmt.Errorf("GCX_EXT_GCX_BIN is not set: run this through `gcx ext %s`", extensionName())
	}
	return &gcxClient{bin: bin, context: os.Getenv("GCX_EXT_CONTEXT")}, nil
}

func extensionName() string {
	if n := os.Getenv("GCX_EXT_NAME"); n != "" {
		return n
	}
	return "profile-explorer"
}

// args appends the context the parent gcx resolved, so a --context on the outer
// invocation is honoured rather than silently falling back to current-context.
func (g *gcxClient) args(args []string) []string {
	full := append([]string{}, args...)
	full = append(full, "--output", "json")
	if g.context != "" {
		full = append(full, "--context", g.context)
	}
	return full
}

// run executes a gcx command and returns its stdout. A failed run still returns
// stdout: a partial failure writes a complete result document there, and gcx's
// own error envelope carries a better message than the exit code does.
func (g *gcxClient) run(ctx context.Context, args ...string) ([]byte, error) {
	full := g.args(args)
	cmd := exec.CommandContext(ctx, g.bin, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("gcx %s: %s", strings.Join(args, " "), reason(stdout.Bytes(), stderr.String(), err))
	}
	return stdout.Bytes(), nil
}

func (g *gcxClient) decode(ctx context.Context, out any, args ...string) error {
	data, err := g.run(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parsing output of `gcx %s`: %w", strings.Join(args, " "), err)
	}
	return nil
}

// reason prefers gcx's structured error over the exit status, so a permission
// or scope problem reads as itself rather than as "exit status 4".
func reason(stdout []byte, stderr string, runErr error) string {
	var envelope struct {
		Error struct {
			Summary string `json:"summary"`
			Details string `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(stdout, &envelope) == nil && envelope.Error.Summary != "" {
		if envelope.Error.Details != "" {
			return envelope.Error.Summary + ": " + envelope.Error.Details
		}
		return envelope.Error.Summary
	}
	if s := strings.TrimSpace(stderr); s != "" {
		return s
	}
	return runErr.Error()
}

type datasource struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// isProfiling reports whether a datasource serves profiles. Pyroscope's plugin
// id has changed over time, so match on the family rather than one literal.
func (d datasource) isProfiling() bool {
	t := strings.ToLower(d.Type)
	return strings.Contains(t, "pyroscope") || strings.Contains(t, "phlare")
}

func (g *gcxClient) profilingDatasources(ctx context.Context) ([]datasource, error) {
	var payload struct {
		Datasources []datasource `json:"datasources"`
	}
	if err := g.decode(ctx, &payload, "datasources", "list"); err != nil {
		return nil, err
	}
	out := make([]datasource, 0, len(payload.Datasources))
	for _, d := range payload.Datasources {
		if d.isProfiling() {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("this context has no Pyroscope datasource")
	}
	return out, nil
}

type profileType struct {
	ID         string `json:"ID"`
	Name       string `json:"name"`
	SampleType string `json:"sampleType"`
	SampleUnit string `json:"sampleUnit"`
}

func (p profileType) label() string {
	if p.Name == "" {
		return p.ID
	}
	return p.Name + ":" + p.SampleType
}

func (g *gcxClient) profileTypes(ctx context.Context, dsUID string) ([]profileType, error) {
	var payload struct {
		ProfileTypes []profileType `json:"profileTypes"`
	}
	if err := g.decode(ctx, &payload, "datasources", "pyroscope", "list-profile-types", "-d", dsUID); err != nil {
		return nil, err
	}
	if len(payload.ProfileTypes) == 0 {
		return nil, errors.New("this datasource reports no profile types")
	}
	return payload.ProfileTypes, nil
}

func (g *gcxClient) services(ctx context.Context, dsUID, since string) ([]string, error) {
	var payload struct {
		Names []string `json:"names"`
	}
	err := g.decode(ctx, &payload, "datasources", "pyroscope", "labels",
		"-d", dsUID, "--label", "service_name", "--since", since)
	if err != nil {
		return nil, err
	}
	return payload.Names, nil
}

// query is one flamegraph request.
type query struct {
	datasource  string
	expr        string
	profileType profileType
	since       string
	maxNodes    int
}

func (g *gcxClient) flamegraph(ctx context.Context, q query) (*flame, error) {
	args := []string{
		"datasources", "pyroscope", "query", q.expr,
		"-d", q.datasource,
		"--profile-type", q.profileType.ID,
		"--since", q.since,
	}
	if q.maxNodes > 0 {
		args = append(args, "--max-nodes", fmt.Sprint(q.maxNodes))
	}
	data, err := g.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseFlamegraph(data, q.profileType.SampleUnit)
}
