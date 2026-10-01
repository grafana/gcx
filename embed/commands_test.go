package embed_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/embed"
	"github.com/grafana/gcx/internal/host"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// leafCommands returns the command path (without "gcx") of every runnable
// command in the tree.
func leafCommands() [][]string {
	var leaves [][]string
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		if c.Runnable() && !c.HasAvailableSubCommands() {
			leaves = append(leaves, slices.Clone(path))
		}
		for _, sub := range c.Commands() {
			walk(sub, append(path, sub.Name()))
		}
	}
	walk(root.Command("test"), nil)
	return leaves
}

// TestEveryCommandIsSafeEmbedded runs every gcx command, read-only and with
// placeholder arguments, against a server that answers every request. This
// is the backstop for gcx having no deny-list: whatever a command does, it
// must not send a write, hang, or touch the host.
func TestEveryCommandIsSafeEmbedded(t *testing.T) {
	if testing.Short() {
		t.Skip("runs every gcx command")
	}
	dirs := isolateHost(t)

	var mu sync.Mutex
	writes := map[string][]string{} // command -> write requests seen
	reached := map[string]bool{}    // commands that sent at least one request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reached[r.Header.Get("X-Test-Command")] = true
		mu.Unlock()
		// Anything arriving here that needs more than read access bypassed
		// the access guard; the classification itself is tested in host.
		if host.RequiredAccess(r.Method, r.URL.Path) > host.AccessRead {
			mu.Lock()
			cmd := r.Header.Get("X-Test-Command")
			writes[cmd] = append(writes[cmd], r.Method+" "+r.URL.Path)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	type run struct{ command string }
	leaves := leafCommands()
	runs := make([]run, 0, 3*len(leaves))
	for _, path := range leaves {
		base := strings.Join(path, " ")
		runs = append(runs, run{base}, run{base + " x --force"}, run{base + " x y --force"})
	}

	// Anything reaching the process's own stdio would corrupt an embedder's
	// output, e.g. an MCP server speaking JSON-RPC on stdout.
	leaked := captureProcessStdio(t)

	var hungMu sync.Mutex
	var hung []string
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, r := range runs {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = embed.Run(ctx, r.command, embed.Options{
					Grafana: embed.Grafana{
						URL: srv.URL, Token: "token", StackID: 1,
						Headers: map[string]string{"X-Test-Command": r.command},
					},
					Cloud: &embed.Cloud{Token: "cloud-token", APIURL: srv.URL},
				})
			}()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				hungMu.Lock()
				hung = append(hung, r.command)
				hungMu.Unlock()
			}
		})
	}
	wg.Wait()

	assert.Empty(t, leaked(), "commands wrote to the process's stdout or stderr")
	assert.Empty(t, hung, "commands that ignored cancellation")
	for cmd, reqs := range writes {
		assert.Fail(t, fmt.Sprintf("read-only %q sent writes", cmd), "%v", reqs)
	}
	assertNothingWritten(t, dirs)
	t.Logf("ran %d invocations of %d commands; %d reached the server", len(runs), len(runs)/3, len(reached))
}

// captureProcessStdio redirects os.Stdout and os.Stderr until the returned
// function is called, which restores them and returns what was written.
func captureProcessStdio(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	var buf bytes.Buffer
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(copied)
	}()
	restored := false
	restore := func() string {
		if !restored {
			restored = true
			os.Stdout, os.Stderr = oldOut, oldErr
			_ = w.Close()
			<-copied
		}
		return buf.String()
	}
	t.Cleanup(func() { restore() })
	return restore
}
