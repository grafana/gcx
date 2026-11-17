package config

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/stretchr/testify/require"
)

const benchmarkLoadsPerProcess = 20

// Each helper has its own OS file descriptors. Pipes schedule all operations.
func TestLayeredConfigProcessHelper(t *testing.T) {
	if os.Getenv("GCX_LOAD_PROCESS_HELPER") == "" {
		return
	}
	runLayeredConfigProcessHelper(t)
	// Omit the test runner's PASS output after the helper releases its locks.
	os.Exit(0)
}

func runLayeredConfigProcessHelper(t *testing.T) {
	t.Helper()
	mode := os.Getenv("GCX_LOAD_PROCESS_HELPER")
	path := os.Getenv("GCX_LOAD_PROCESS_PATH")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch mode {
	case "measure":
		fmt.Fprintln(os.Stdout, "ready")
		_, err := bufio.NewReader(os.Stdin).ReadString('\n')
		require.NoError(t, err)
		samples := make([]int64, benchmarkLoadsPerProcess)
		for i := range samples {
			started := time.Now()
			var lock *flock.Flock
			if os.Getenv("GCX_LOAD_PROCESS_LOCK_ALL") == "true" {
				identity, identityErr := canonicalConfigSource(path)
				require.NoError(t, identityErr)
				lockPath, pathErr := configLockFile(identity)
				require.NoError(t, pathErr)
				lock = flock.New(lockPath)
				held, lockErr := lock.TryLockContext(ctx, 100*time.Millisecond)
				require.NoError(t, lockErr)
				require.True(t, held)
			}
			_, loadErr := LoadLayered(ctx, "")
			if lock != nil {
				require.NoError(t, lock.Unlock())
			}
			require.NoError(t, loadErr)
			samples[i] = time.Since(started).Nanoseconds()
		}
		require.NoError(t, json.NewEncoder(os.Stdout).Encode(samples))
	case "read":
		_, err := LoadLayered(ctx, "")
		require.NoError(t, err)
	case "write":
		cfg, err := Load(ctx, ExplicitConfigFile(path))
		require.NoError(t, err)
		cfg.CurrentContext = "b"
		require.NoError(t, Write(ctx, ExplicitConfigFile(path), cfg))
	case "hold", "probe":
		identity, err := canonicalConfigSource(path)
		require.NoError(t, err)
		lockPath, err := configLockFile(identity)
		require.NoError(t, err)
		lock := flock.New(lockPath)
		held, err := lock.TryLock()
		require.NoError(t, err)
		fmt.Fprintln(os.Stdout, held)
		if held {
			defer func() { _ = lock.Unlock() }()
		}
		if mode == "hold" {
			require.True(t, held)
			_, err = bufio.NewReader(os.Stdin).ReadString('\n')
			require.NoError(t, err)
		}
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

func configProcessCommand(t *testing.T, mode, path string) *exec.Cmd {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestLayeredConfigProcessHelper$")
	cmd.Env = append(os.Environ(), "GCX_LOAD_PROCESS_HELPER="+mode, "GCX_LOAD_PROCESS_PATH="+path)
	return cmd
}

func runConfigProcess(t *testing.T, mode, path string) string {
	t.Helper()
	output, err := configProcessCommand(t, mode, path).CombinedOutput()
	require.NoError(t, err, "%s", output)
	return string(output)
}

type processLoadLogger struct {
	logging.Logger

	path   string
	onLoad func()
}

func (l *processLoadLogger) WithContext(context.Context) logging.Logger { return l }
func (l *processLoadLogger) With(...any) logging.Logger                 { return l }
func (l *processLoadLogger) Debug(message string, args ...any) {
	if message != "Loading config" {
		return
	}
	for _, arg := range args {
		if attr, ok := arg.(slog.Attr); ok && attr.Key == "filename" && attr.Value.String() == l.path {
			l.onLoad()
			return
		}
	}
}

func TestLoadLayeredProcessWriterConflict(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(strconv.FormatBool(canceled), func(t *testing.T) {
			f := newLayeredMigrationFixture(t)
			t.Setenv("GCX_KEYCHAIN", "off")
			writeLayeredMigrationFixture(t, f.user, "version: 1\ncontexts:\n  a: {}\n  b: {}\ncurrent-context: a\n")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			attempts := 0
			logger := &processLoadLogger{Logger: &boundTestLogger{}, path: f.user}
			logger.onLoad = func() {
				attempts++
				if attempts == 1 {
					runConfigProcess(t, "write", f.user)
					if canceled {
						cancel()
					}
				} else {
					require.Equal(t, "false\n", runConfigProcess(t, "probe", f.user), "fresh fallback must hold the existing writer lock")
				}
			}
			loaded, err := LoadLayered(logging.Context(ctx, logger), "")
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, 1, attempts)
			} else {
				require.NoError(t, err)
				require.Equal(t, "b", loaded.CurrentContext)
				require.Equal(t, 2, attempts)
			}
			require.Equal(t, "true\n", runConfigProcess(t, "probe", f.user), "load must release its writer lock")
		})
	}
}

func TestLoadLayeredProcessStableReadWhileWriterLocked(t *testing.T) {
	f := newLayeredMigrationFixture(t)
	t.Setenv("GCX_KEYCHAIN", "off")
	writeLayeredMigrationFixture(t, f.user, "version: 1\ncontexts:\n  a: {}\ncurrent-context: a\n")
	cmd := configProcessCommand(t, "hold", f.user)
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	output, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = input.Close(); _ = cmd.Process.Kill() })
	ready, err := bufio.NewReader(output).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "true\n", ready)
	// A child reader must finish before the parent permits the writer to unlock.
	runConfigProcess(t, "read", f.user)
	_, err = io.WriteString(input, "release\n")
	require.NoError(t, err)
	require.NoError(t, cmd.Wait())
}

func TestLoadLayeredProcessReadDuringSlowOAuthRefresh(t *testing.T) {
	f := newLayeredMigrationFixture(t)
	t.Setenv("GCX_KEYCHAIN", "off")
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			close(entered)
			<-release
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"token":"gat_new","refresh_token":"gar_new","expires_at":"2099-01-01T00:00:00Z","refresh_expires_at":"2099-02-01T00:00:00Z"}}`)
		}
	}))
	defer server.Close()
	// Ensure failure cleanup unblocks the handler before server.Close.
	defer unblock()
	writeLayeredMigrationFixture(t, f.user, fmt.Sprintf(`version: 1
stacks:
  a:
    grafana:
      server: %q
      proxy-endpoint: %q
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
contexts:
  a:
    stack: a
current-context: a
`, server.URL, server.URL))
	cfg, err := Load(t.Context(), ExplicitConfigFile(f.user))
	require.NoError(t, err)
	rc, err := NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["a"])
	require.NoError(t, err)
	rc.WireTokenPersistence(t.Context(), ExplicitConfigFile(f.user), "a", "a", []ConfigSource{{Path: f.user, Type: "user"}})
	done := make(chan error, 1)
	go func() {
		req, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/protected", nil)
		if requestErr != nil {
			done <- requestErr
			return
		}
		response, requestErr := (&http.Client{Transport: rc.WrapTransport(http.DefaultTransport)}).Do(req)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- requestErr
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("refresh did not start")
	}
	require.Equal(t, "false\n", runConfigProcess(t, "probe", f.user), "refresh must hold the writer lock during network I/O")
	runConfigProcess(t, "read", f.user)
	unblock()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("refresh did not finish")
	}
}

func BenchmarkLoadLayeredConcurrentReaders(b *testing.B) {
	root := b.TempDir()
	b.Setenv("HOME", root)
	b.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	b.Setenv("XDG_CONFIG_DIRS", filepath.Join(root, "system"))
	b.Setenv(ConfigFileEnvVar, "")
	b.Setenv("GCX_KEYCHAIN", "off")
	b.Chdir(root)
	path := filepath.Join(root, ".config", StandardConfigFolder, StandardConfigFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("version: 1\ncontexts:\n  a: {}\ncurrent-context: a\n"), 0o600); err != nil {
		b.Fatal(err)
	}
	binary, err := os.Executable()
	require.NoError(b, err)
	// Start with: go test ./internal/config -run '^$' -bench '^BenchmarkLoadLayeredConcurrentReaders$' -benchtime=1x
	for _, readers := range []int{8, 32} {
		for _, lockAll := range []bool{false, true} {
			b.Run(fmt.Sprintf("readers-%d/lock-all-%v", readers, lockAll), func(b *testing.B) {
				var samples []int64
				var batchDuration time.Duration
				for range b.N {
					batchSamples, elapsed := measureConfigProcessBatch(b, binary, path, readers, lockAll)
					samples = append(samples, batchSamples...)
					batchDuration += elapsed
				}
				slices.Sort(samples)
				b.ReportMetric(float64(samples[len(samples)/2]), "p50-ns/load")
				b.ReportMetric(float64(samples[(len(samples)-1)*95/100]), "p95-ns/load")
				b.ReportMetric(float64(samples[len(samples)-1]), "max-ns/load")
				b.ReportMetric(float64(batchDuration.Nanoseconds())/float64(b.N), "ns/batch")
				b.ReportMetric(float64(len(samples)), "loads")
			})
		}
	}
}

// The pipe barrier excludes process startup from each load and batch metric.
func measureConfigProcessBatch(b *testing.B, binary, path string, readers int, lockAll bool) ([]int64, time.Duration) {
	b.Helper()
	ctx, cancel := context.WithTimeout(b.Context(), 15*time.Second)
	defer cancel()
	type readerProcess struct {
		cmd    *exec.Cmd
		input  io.WriteCloser
		output *bufio.Reader
		stderr bytes.Buffer
	}
	processes := make([]readerProcess, readers)
	defer func() {
		for _, process := range processes {
			if process.input != nil {
				_ = process.input.Close()
			}
			if process.cmd != nil && process.cmd.Process != nil {
				_ = process.cmd.Process.Kill()
			}
		}
	}()
	for i := range processes {
		process := &processes[i]
		process.cmd = exec.CommandContext(ctx, binary, "-test.run=^TestLayeredConfigProcessHelper$")
		process.cmd.Env = append(os.Environ(), "GCX_LOAD_PROCESS_HELPER=measure", "GCX_LOAD_PROCESS_PATH="+path, "GCX_LOAD_PROCESS_LOCK_ALL="+strconv.FormatBool(lockAll))
		process.cmd.Stderr = &process.stderr
		input, err := process.cmd.StdinPipe()
		require.NoError(b, err)
		process.input = input
		output, err := process.cmd.StdoutPipe()
		require.NoError(b, err)
		process.output = bufio.NewReader(output)
		require.NoError(b, process.cmd.Start())
	}
	for i := range processes {
		ready, err := processes[i].output.ReadString('\n')
		require.NoError(b, err)
		require.Equal(b, "ready\n", ready)
	}
	started := time.Now()
	for _, process := range processes {
		_, err := io.WriteString(process.input, "go\n")
		require.NoError(b, err)
	}
	samples := make([]int64, 0, readers*benchmarkLoadsPerProcess)
	for i := range processes {
		process := &processes[i]
		var readerSamples []int64
		require.NoError(b, json.NewDecoder(process.output).Decode(&readerSamples))
		require.NoError(b, process.cmd.Wait(), "%s", process.stderr.String())
		require.Len(b, readerSamples, benchmarkLoadsPerProcess)
		samples = append(samples, readerSamples...)
	}
	return samples, time.Since(started)
}
