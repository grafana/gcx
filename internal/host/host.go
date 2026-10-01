// Package host is the only place gcx touches the host process: the
// filesystem, environment, stdio, argv, subprocesses, network listeners and
// signals. Every function takes a context.
//
// By default each function delegates to the os, os/exec, net or os/signal
// equivalent. When the context carries a [Sandbox] (gcx embedded in another
// process, possibly serving many tenants concurrently) host access is refused
// with [ErrUnavailable], and env, stdio and argv are served from the sandbox
// instead of the process.
//
// A lint rule forbids calling the underlying APIs anywhere else, so any code
// path that reaches the host is sandbox-aware by construction.
package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// ErrUnavailable is returned (wrapped) by host operations attempted inside a
// [Sandbox].
//
// Filesystem errors additionally satisfy errors.Is(err, fs.ErrNotExist), so
// code that treats a missing file as "use defaults" behaves as if the
// filesystem were empty, while code that requires a file fails with an
// explanatory message.
var ErrUnavailable = errors.New("not available when gcx is embedded")

// unavailable is the error inside filesystem *fs.PathErrors from a sandbox.
type unavailable struct{}

func (unavailable) Error() string { return ErrUnavailable.Error() }
func (unavailable) Unwrap() error { return ErrUnavailable }

func (unavailable) Is(target error) bool {
	return target == fs.ErrNotExist
}

// Sandbox isolates a single embedded gcx invocation from the host process.
// It must not be shared between concurrent invocations.
type Sandbox struct {
	// Args is the argv reported to commands (e.g. for pagination hints),
	// including the program name at index 0.
	Args []string
	// Env is the complete environment visible to the invocation. The process
	// environment is never consulted.
	Env map[string]string
	// Stdin is read by commands that accept "-" as a file argument. Nil
	// behaves as an empty reader.
	Stdin io.Reader
	// Stdout and Stderr receive the invocation's output. Nil discards.
	Stdout, Stderr io.Writer
}

type sandboxKey struct{}

// WithSandbox returns a context whose host operations are confined to sb.
func WithSandbox(ctx context.Context, sb *Sandbox) context.Context {
	return context.WithValue(ctx, sandboxKey{}, sb)
}

func sandbox(ctx context.Context) *Sandbox {
	sb, _ := ctx.Value(sandboxKey{}).(*Sandbox)
	return sb
}

// Sandboxed reports whether ctx confines host access to a [Sandbox].
func Sandboxed(ctx context.Context) bool {
	return sandbox(ctx) != nil
}

func pathErr(op, path string) error {
	return &fs.PathError{Op: op, Path: path, Err: unavailable{}}
}

func opErr(op string) error {
	return fmt.Errorf("%s: %w", op, ErrUnavailable)
}

// Environment.

// Getenv mirrors [os.Getenv].
func Getenv(ctx context.Context, key string) string {
	v, _ := LookupEnv(ctx, key)
	return v
}

// LookupEnv mirrors [os.LookupEnv].
func LookupEnv(ctx context.Context, key string) (string, bool) {
	if sb := sandbox(ctx); sb != nil {
		v, ok := sb.Env[key]
		return v, ok
	}
	return os.LookupEnv(key)
}

// Environ mirrors [os.Environ].
func Environ(ctx context.Context) []string {
	sb := sandbox(ctx)
	if sb == nil {
		return os.Environ()
	}
	env := make([]string, 0, len(sb.Env))
	for k, v := range sb.Env {
		env = append(env, k+"="+v)
	}
	slices.Sort(env)
	return env
}

// Args mirrors [os.Args].
func Args(ctx context.Context) []string {
	if sb := sandbox(ctx); sb != nil {
		return sb.Args
	}
	return os.Args
}

// Stdio.

// Stdin returns the invocation's standard input.
func Stdin(ctx context.Context) io.Reader {
	if sb := sandbox(ctx); sb != nil {
		if sb.Stdin == nil {
			return strings.NewReader("")
		}
		return sb.Stdin
	}
	return os.Stdin
}

// Stdout returns the invocation's standard output.
func Stdout(ctx context.Context) io.Writer {
	if sb := sandbox(ctx); sb != nil {
		if sb.Stdout == nil {
			return io.Discard
		}
		return sb.Stdout
	}
	return os.Stdout
}

// Stderr returns the invocation's standard error.
func Stderr(ctx context.Context) io.Writer {
	if sb := sandbox(ctx); sb != nil {
		if sb.Stderr == nil {
			return io.Discard
		}
		return sb.Stderr
	}
	return os.Stderr
}

// StdinFile returns the process stdin as an *os.File, for callers that need a
// file descriptor (terminal checks, Stat). Sandboxed contexts never have one.
func StdinFile(ctx context.Context) (*os.File, error) {
	if Sandboxed(ctx) {
		return nil, opErr("stdin")
	}
	return os.Stdin, nil
}

// StdoutFile returns the process stdout as an *os.File. Sandboxed contexts
// never have one.
func StdoutFile(ctx context.Context) (*os.File, error) {
	if Sandboxed(ctx) {
		return nil, opErr("stdout")
	}
	return os.Stdout, nil
}

// StderrFile returns the process stderr as an *os.File. Sandboxed contexts
// never have one.
func StderrFile(ctx context.Context) (*os.File, error) {
	if Sandboxed(ctx) {
		return nil, opErr("stderr")
	}
	return os.Stderr, nil
}
