package cloudcli_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/cloudcli"
)

type fakeRunner struct {
	lookErr error
	stdout  []byte
	stderr  []byte
	runErr  error
	gotArgs []string
}

func (f *fakeRunner) LookPath(string) error { return f.lookErr }

func (f *fakeRunner) Run(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
	f.gotArgs = args
	return f.stdout, f.stderr, f.runErr
}

func TestEnsure_MissingBinaryReturnsActionableError(t *testing.T) {
	tool := cloudcli.New("az", "Azure CLI", "https://example.com/install").
		WithRunner(&fakeRunner{lookErr: errors.New("not found")})

	err := tool.Ensure()
	if err == nil {
		t.Fatal("expected error when binary is missing")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestEnsure_PresentBinaryReturnsNil(t *testing.T) {
	tool := cloudcli.New("az", "Azure CLI", "").WithRunner(&fakeRunner{})
	if err := tool.Ensure(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunJSON_DecodesStdout(t *testing.T) {
	tool := cloudcli.New("az", "Azure CLI", "").
		WithRunner(&fakeRunner{stdout: []byte(`{"id":"abc"}`)})

	var out struct {
		ID string `json:"id"`
	}
	if err := tool.RunJSON(context.Background(), &out, "account", "show"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID != "abc" {
		t.Fatalf("got %q, want %q", out.ID, "abc")
	}
}

func TestRunJSON_WrapsRunErrorWithStderr(t *testing.T) {
	tool := cloudcli.New("az", "Azure CLI", "").
		WithRunner(&fakeRunner{stderr: []byte("AuthorizationFailed"), runErr: errors.New("exit 1")})

	err := tool.RunJSON(context.Background(), nil, "ad", "app", "create")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunJSON_ErrorRedactsSecretArgs(t *testing.T) {
	tool := cloudcli.New("az", "Azure CLI", "").
		WithRunner(&fakeRunner{stderr: []byte("boom"), runErr: errors.New("exit 1")})

	err := tool.RunJSON(context.Background(), nil,
		"ad", "app", "credential", "reset", "--password", "hunter2", "--display-name=svc")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error must not leak secret value: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "***") {
		t.Fatalf("expected redacted arg in error, got %q", err.Error())
	}
}
