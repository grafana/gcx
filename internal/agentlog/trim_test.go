//nolint:testpackage // needs access to unexported trimLog
package agentlog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTrimLog(t *testing.T) {
	write := func(t *testing.T, path string, n int) {
		t.Helper()
		var buf bytes.Buffer
		for i := range n {
			fmt.Fprintf(&buf, `{"i":%d}`+"\n", i)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	countLines := func(t *testing.T, path string) int {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return bytes.Count(data, []byte("\n"))
	}
	firstLine := func(t *testing.T, path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		line, _, _ := bytes.Cut(data, []byte("\n"))
		return string(line)
	}

	t.Run("no-op when under limit", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "log.jsonl")
		write(t, path, 5)
		if err := trimLog(path, 10); err != nil {
			t.Fatal(err)
		}
		if got := countLines(t, path); got != 5 {
			t.Errorf("lines=%d, want 5", got)
		}
	})

	t.Run("no-op at exactly limit", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "log.jsonl")
		write(t, path, 10)
		if err := trimLog(path, 10); err != nil {
			t.Fatal(err)
		}
		if got := countLines(t, path); got != 10 {
			t.Errorf("lines=%d, want 10", got)
		}
	})

	t.Run("trims oldest entries", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "log.jsonl")
		write(t, path, 15)
		if err := trimLog(path, 10); err != nil {
			t.Fatal(err)
		}
		if got := countLines(t, path); got != 10 {
			t.Errorf("lines=%d, want 10", got)
		}
		// Entry i=5 is the first kept (0-4 dropped).
		if got := firstLine(t, path); got != `{"i":5}` {
			t.Errorf("first line=%q, want {\"i\":5}", got)
		}
	})
}
