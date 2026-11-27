package sandbox_test

import (
	"bytes"
	"context"
	_ "embed"
	"testing"

	"github.com/grafana/gcx/experimental/sandbox"
)

//go:embed testdata/random.wasm
var randomGuest []byte

// Every instance must draw its own random bytes. wazero's default random
// source is deterministic, so without the sandbox's own source every run
// would see the same random_get bytes, and with them the same crypto/rand
// output and Go runtime seeds.
func TestRunRandomSourceDiffersPerInstance(t *testing.T) {
	rt, err := sandbox.New(t.Context(), randomGuest, sandbox.Config{MemoryLimitBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	draw := func() []byte {
		var out bytes.Buffer
		res, err := rt.Run(t.Context(), sandbox.Invocation{Stdout: &out})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("Run() = %+v, %v", res, err)
		}
		if out.Len() != 16 {
			t.Fatalf("guest wrote %d bytes, want 16", out.Len())
		}
		return out.Bytes()
	}
	first, second := draw(), draw()
	if bytes.Equal(first, second) {
		t.Fatalf("two instances drew the same random bytes %x", first)
	}
}
