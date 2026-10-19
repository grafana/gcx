package extensionsdk_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	sdk "github.com/grafana/gcx/experimental/extensionsdk"
)

func TestChild(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	args := os.Args[separator+1:]
	if args[0] == "wait" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	err = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": args, "stdin": string(stdin), "context": os.Getenv("GCX_CONTEXT"), "config": os.Getenv("GCX_CONFIG")})
	if err != nil {
		t.Fatal(err)
	}
	os.Exit(4)
}

func TestExecute(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCX_CONTEXT", "wrong")
	client, err := sdk.New(sdk.Options{Binary: binary, Prefix: []string{"-test.run=TestChild", "--"}, Context: "selected", ConfigFile: "config.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"literal", "$(exit 1)", "a b", "--context", "child"}
	result, err := client.Execute(t.Context(), sdk.CommandRequest{Args: args, Stdin: []byte("input")})
	if err != nil || result.ExitCode != 4 {
		t.Fatalf("result=%v err=%v", result, err)
	}
	var output struct {
		Args    []string `json:"args"`
		Stdin   string   `json:"stdin"`
		Context string   `json:"context"`
		Config  string   `json:"config"`
	}
	if err := json.Unmarshal(result.Stdout, &output); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(output.Args, args) || output.Stdin != "input" || output.Context != "selected" || !filepath.IsAbs(output.Config) {
		t.Fatalf("output=%+v", output)
	}
	var exit *sdk.ExitError
	if !errors.As(result.Err(), &exit) || exit.Code != 4 {
		t.Fatal(result.Err())
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := client.Execute(ctx, sdk.CommandRequest{Args: []string{"wait"}}); done <- err }()
	other, err := client.Execute(t.Context(), sdk.CommandRequest{Args: []string{"other"}})
	if err != nil || other.ExitCode != 4 {
		t.Fatalf("unrelated request: %v %v", other, err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled operation did not stop")
	}
}

type executorFunc func(context.Context, sdk.CommandRequest) (sdk.Result, error)

func (f executorFunc) Execute(ctx context.Context, r sdk.CommandRequest) (sdk.Result, error) {
	return f(ctx, r)
}

func TestDecodeJSON(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
		wantErr    bool
	}{{"success", `{"value":42}`, 0, false}, {"malformed", "not JSON", 0, true}, {"partial", `{"value":42}`, 4, true}} {
		t.Run(tc.name, func(t *testing.T) {
			var out struct{ Value int }
			err := sdk.DecodeJSON(t.Context(), executorFunc(func(ctx context.Context, r sdk.CommandRequest) (sdk.Result, error) {
				if ctx != t.Context() || !slices.Equal(r.Args, []string{"version", "--output", "json"}) {
					t.Fatalf("request=%v", r)
				}
				return sdk.Result{Stdout: []byte(tc.body), ExitCode: tc.code}, nil
			}), &out, "version")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
			if !tc.wantErr && out.Value != 42 {
				t.Fatal(out)
			}
		})
	}
}
