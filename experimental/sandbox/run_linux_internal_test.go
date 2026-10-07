//go:build linux

package sandbox

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/experimental"
)

//go:embed testdata/stdin.wasm
var stdinGuest []byte

// countingRuntime returns a Runtime for the stdin guest that records every
// linear memory its runs allocate.
func countingRuntime(t *testing.T) (*Runtime, *memories) {
	t.Helper()
	r, err := New(t.Context(), stdinGuest, Config{MemoryLimitBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	mems := &memories{}
	r.newRunMemory = func() (experimental.MemoryAllocator, func()) {
		inner, free := newRunMemory()
		return experimental.MemoryAllocatorFunc(func(capacity, maxBytes uint64) experimental.LinearMemory {
			mem := inner.Allocate(capacity, maxBytes)
			mems.add(mem)
			return mem
		}), free
	}
	return r, mems
}

type memories struct {
	mu   sync.Mutex
	mems []experimental.LinearMemory
}

func (m *memories) add(mem experimental.LinearMemory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mems = append(m.mems, mem)
}

// check fails unless want memories were allocated and all are unmapped.
func (m *memories) check(t *testing.T, want int) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.mems) != want {
		t.Fatalf("%d memories allocated, want %d: is Run using its allocator?", len(m.mems), want)
	}
	for i, mem := range m.mems {
		mapped, ok := mem.(*mappedMemory)
		if !ok {
			t.Fatalf("memory %d is a %T, want *mappedMemory", i, mem)
		}
		if mapped.buf != nil {
			t.Fatalf("memory %d is still mapped", i)
		}
	}
}

// readerFunc is an io.Reader for the guest's stdin.
type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// Every run frees the memory it allocated, including when the guest exits or
// traps just after its context is cancelled, which wazero alone doesn't free.
func TestRunFreesMemory(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		cancel  bool // while the guest is reading stdin
		wantErr error
	}{
		{name: "exit", input: ""},
		{name: "trap", input: "t", wantErr: errors.New("gcx: ")},
		// The guest finished, so the run succeeds.
		{name: "cancelled, then exit", input: "", cancel: true},
		{name: "cancelled, then trap", input: "t", cancel: true, wantErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, mems := countingRuntime(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stdin := readerFunc(func(p []byte) (int, error) {
				if tc.cancel {
					cancel()
					// Let wazero's watcher mark the instance closed before
					// the guest carries on.
					time.Sleep(50 * time.Millisecond)
				}
				if tc.input == "" {
					return 0, io.EOF
				}
				return copy(p, tc.input), nil
			})

			_, err := r.Run(ctx, Invocation{Stdin: stdin})
			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("Run: %v", err)
			case tc.wantErr != nil && (err == nil || !errors.Is(err, tc.wantErr) && !strings.HasPrefix(err.Error(), tc.wantErr.Error())):
				t.Fatalf("Run: got %v, want %v", err, tc.wantErr)
			}
			mems.check(t, 1)
		})
	}
}

// Close stops and waits for runs in flight before freeing their memory, so
// a guest that touches its memory after Close is called doesn't crash the
// process.
func TestCloseWaitsForRuns(t *testing.T) {
	r, mems := countingRuntime(t)
	reading, release := make(chan struct{}), make(chan struct{})
	stdin := readerFunc(func(p []byte) (int, error) {
		close(reading)
		<-release
		return copy(p, "x"), nil
	})
	runErr := make(chan error, 1)
	go func() {
		_, err := r.Run(context.Background(), Invocation{Stdin: stdin})
		runErr <- err
	}()
	<-reading

	closed := make(chan error, 1)
	go func() { closed <- r.Close(context.Background()) }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned (%v) while a run was in flight", err)
	case <-time.After(100 * time.Millisecond):
	}
	// The guest now reads the byte it was given from its memory.
	close(release)
	if err := <-runErr; err != nil && !errors.Is(err, ErrClosed) {
		t.Fatalf("Run: %v, want nil or ErrClosed", err)
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	mems.check(t, 1)
	if _, err := r.Run(context.Background(), Invocation{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Run after Close: %v, want ErrClosed", err)
	}
}

// Stdin and stdout only ever see their own buffers, never guest memory,
// which is unmapped when the instance closes.
func TestRunCopiesStdio(t *testing.T) {
	r, _ := countingRuntime(t)
	var kept []byte
	stdin := readerFunc(func(p []byte) (int, error) {
		kept = p // breaks the io.Reader contract on purpose
		return 0, io.EOF
	})
	if _, err := r.Run(t.Context(), Invocation{Stdin: stdin}); err != nil {
		t.Fatal(err)
	}
	// Touching the kept buffer after the instance is gone must be safe.
	for i := range kept {
		kept[i] = 1
	}
}
