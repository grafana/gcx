//go:build !wasip1

package pluginhost_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	sdk "github.com/grafana/gcx/experimental/pluginsdk"
	"github.com/grafana/gcx/internal/pluginhost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMain(m *testing.M) {
	if os.Getenv("GCX_COMMAND_HELPER") != "1" {
		os.Exit(m.Run())
	}
	if os.Getenv("GCX_COMMAND_WAIT") == "1" {
		if err := os.WriteFile(os.Getenv("GCX_COMMAND_READY"), []byte("ready"), 0o600); err != nil {
			os.Exit(1)
		}
		time.Sleep(time.Minute)
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(1)
	}
	err = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args[1:], "stdin": string(input), "context": os.Getenv("GCX_CONTEXT"), "config": os.Getenv("GCX_CONFIG"), "pid": os.Getpid()})
	if err != nil {
		os.Exit(1)
	}
	_, _ = os.Stderr.WriteString("command diagnostic")
	os.Exit(4)
}

func TestExecute(t *testing.T) {
	t.Setenv("GCX_COMMAND_HELPER", "1")
	t.Setenv("GCX_CONTEXT", "inherited")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	host := &pluginhost.Commands{Binary: binary, Context: "selected", ConfigFile: "fixture.yaml"}
	args := []string{"resources", "get", "literal;$(nothing)", "--output", "json"}
	pids := map[int]bool{}
	for range 2 {
		result, err := host.Execute(t.Context(), &sdk.ExecuteRequest{Args: args, Stdin: []byte("input data")})
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Args    []string `json:"args"`
			Stdin   string   `json:"stdin"`
			Context string   `json:"context"`
			Config  string   `json:"config"`
			PID     int      `json:"pid"`
		}
		if err := json.Unmarshal(result.GetStdout(), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Args, args) || got.Stdin != "input data" || got.Context != "selected" || got.Config != "fixture.yaml" || result.GetExitCode() != 4 || string(result.GetStderr()) != "command diagnostic" {
			t.Fatalf("result=%v decoded=%+v", result, got)
		}
		pids[got.PID] = true
	}
	if len(pids) != 2 {
		t.Fatal("calls did not start separate processes")
	}
}

func TestExecutionFailures(t *testing.T) {
	host := &pluginhost.Commands{Binary: filepath.Join(t.TempDir(), "missing")}
	if _, err := host.Execute(t.Context(), &sdk.ExecuteRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty args: %v", err)
	}
	if _, err := host.Execute(t.Context(), &sdk.ExecuteRequest{Args: []string{"version"}}); status.Code(err) != codes.Internal {
		t.Fatalf("start failure: %v", err)
	}
}

func TestCancellationStopsCommand(t *testing.T) {
	t.Setenv("GCX_COMMAND_HELPER", "1")
	t.Setenv("GCX_COMMAND_WAIT", "1")
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("GCX_COMMAND_READY", ready)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	host := &pluginhost.Commands{Binary: binary}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := host.Execute(ctx, &sdk.ExecuteRequest{Args: []string{"wait"}}); done <- err }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if status.Code(err) != codes.Canceled {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled child did not exit")
	}
}
