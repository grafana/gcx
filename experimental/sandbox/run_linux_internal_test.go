//go:build linux

package sandbox

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/experimental"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
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

// check fails unless the one run's memory was allocated and is unmapped.
func (m *memories) check(t *testing.T) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.mems) != 1 {
		t.Fatalf("%d memories allocated, want 1: is Run using its allocator?", len(m.mems))
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

// release unblocks a stdin reader that waits on c. Its cleanup releases the
// reader if the test fails first, and runs before countingRuntime's, whose
// Close would otherwise wait for that run forever.
type release struct {
	c     chan struct{}
	close func()
}

func releaser(t *testing.T) release {
	t.Helper()
	c := make(chan struct{})
	r := release{c: c, close: sync.OnceFunc(func() { close(c) })}
	t.Cleanup(r.close)
	return r
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
			mems.check(t)
		})
	}
}

// Close stops and waits for runs in flight before freeing their memory, so
// a guest that touches its memory after Close is called doesn't crash the
// process.
func TestCloseWaitsForRuns(t *testing.T) {
	r, mems := countingRuntime(t)
	reading, release := make(chan struct{}), releaser(t)
	stdin := readerFunc(func(p []byte) (int, error) {
		close(reading)
		<-release.c
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
	// The guest now reads the byte it was given from its memory, and exits
	// with status 0: a run that finishes anyway returns its result.
	release.close()
	if err := <-runErr; err != nil {
		t.Fatalf("Run: %v, want nil", err)
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	mems.check(t)
	if _, err := r.Run(context.Background(), Invocation{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Run after Close: %v, want ErrClosed", err)
	}
}

// Close cancels runs in flight, which return ErrClosed.
func TestCloseCancelsRuns(t *testing.T) {
	r, mems := countingRuntime(t)
	reading := make(chan struct{})
	stdin := readerFunc(func(p []byte) (int, error) {
		close(reading)
		return copy(p, "s"), nil // spin until stopped
	})
	runErr := make(chan error, 1)
	go func() {
		_, err := r.Run(context.Background(), Invocation{Stdin: stdin})
		runErr <- err
	}()
	<-reading

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-runErr; !errors.Is(err, ErrClosed) {
		t.Fatalf("Run: %v, want ErrClosed", err)
	}
	mems.check(t)
}

// When its ctx is done before the runs stop, Close gives up and leaves their
// memory alone, and a later Close finishes the job.
func TestCloseGivesUpWhenCtxDone(t *testing.T) {
	r, mems := countingRuntime(t)
	reading, release := make(chan struct{}), releaser(t)
	stdin := readerFunc(func(p []byte) (int, error) {
		close(reading)
		<-release.c // ignores cancellation, like a stuck pipe
		return 0, io.EOF
	})
	runErr := make(chan error, 1)
	go func() {
		_, err := r.Run(context.Background(), Invocation{Stdin: stdin})
		runErr <- err
	}()
	<-reading

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := r.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close: %v, want context.DeadlineExceeded", err)
	}
	mems.mu.Lock()
	mapped, ok := mems.mems[0].(*mappedMemory)
	freed := !ok || mapped.buf == nil
	mems.mu.Unlock()
	if freed {
		t.Fatal("Close freed the memory of a run still in flight")
	}
	if _, err := r.Run(t.Context(), Invocation{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Run after Close: %v, want ErrClosed", err)
	}

	release.close()
	<-runErr
	if err := r.Close(t.Context()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	mems.check(t)
}

// Stdin and stdout only ever see their own buffers, never guest memory,
// which is unmapped when the instance closes.
func TestRunCopiesStdio(t *testing.T) {
	r, _ := countingRuntime(t)
	var keptIn, keptOut []byte
	// Both break the io.Reader and io.Writer contracts on purpose.
	stdin := readerFunc(func(p []byte) (int, error) {
		keptIn = p
		return copy(p, "x"), nil
	})
	stdout := writerFunc(func(p []byte) (int, error) {
		keptOut = p
		return len(p), nil
	})
	if _, err := r.Run(t.Context(), Invocation{Stdin: stdin, Stdout: stdout}); err != nil {
		t.Fatal(err)
	}
	if string(keptOut) != "x" {
		t.Fatalf("stdout got %q, want %q", keptOut, "x")
	}
	// Touching the kept buffers after the instance is gone must be safe.
	for _, kept := range [][]byte{keptIn, keptOut} {
		for i := range kept {
			kept[i] = 1
		}
	}
}

// writerFunc is an io.Writer for the guest's stdout.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// Files reach wazero unwrapped, so it can poll and stat them, and a wrapped
// reader keeps its Poll.
func TestStdioWrapping(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if copyStdin(f) != io.Reader(f) || copyOutput(f) != io.Writer(f) {
		t.Fatal("an *os.File was wrapped")
	}
	p, ok := copyStdin(pollReader{}).(experimentalsys.Pollable)
	if !ok {
		t.Fatal("a Pollable reader lost its Poll")
	}
	if ready, _ := p.Poll(experimentalsys.POLLIN, 0); ready {
		t.Fatal("Poll wasn't forwarded")
	}
	if _, ok := copyStdin(strings.NewReader("")).(copyReader); !ok {
		t.Fatal("a plain reader wasn't copied")
	}
}

// pollReader is never ready.
type pollReader struct{ io.Reader }

func (pollReader) Poll(experimentalsys.Pflag, int32) (bool, experimentalsys.Errno) { return false, 0 }
