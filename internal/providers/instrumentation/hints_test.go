package instrumentation_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
)

func TestFooter_Empty(t *testing.T) {
	t.Parallel()

	var f instrumentation.Footer
	if !f.IsEmpty() {
		t.Error("new Footer should be empty")
	}
	var buf bytes.Buffer
	f.Print(&buf)
	if buf.Len() != 0 {
		t.Errorf("empty footer should print nothing, got: %q", buf.String())
	}
}

func TestFooter_StrictOrdering(t *testing.T) {
	t.Parallel()

	var f instrumentation.Footer
	// Add in "wrong" order — Print must still produce warn → note → hint.
	f.Hint("to verify", "gcx instrumentation status --cluster X")
	f.Notef("data reflects collector state.")
	f.Warnf("item X failed")

	var buf bytes.Buffer
	f.Print(&buf)
	got := buf.String()

	want := "\n" +
		"warn: item X failed\n" +
		"note: data reflects collector state.\n" +
		"hint: to verify:\n" +
		"  gcx instrumentation status --cluster X\n"
	if got != want {
		t.Errorf("ordering mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFooter_MultipleHintsSeparatedByBlank(t *testing.T) {
	t.Parallel()

	var f instrumentation.Footer
	f.Hint("to verify the cluster name", "gcx instrumentation clusters list")
	f.Hint("to check if the collector has reported", "gcx instrumentation status")

	var buf bytes.Buffer
	f.Print(&buf)
	got := buf.String()

	want := "\n" +
		"hint: to verify the cluster name:\n" +
		"  gcx instrumentation clusters list\n" +
		"\n" +
		"hint: to check if the collector has reported:\n" +
		"  gcx instrumentation status\n"
	if got != want {
		t.Errorf("multi-hint layout mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestFooter_NoDoubleBlankBetweenNoteAndHint(t *testing.T) {
	t.Parallel()

	var f instrumentation.Footer
	f.Notef("no clusters found.")
	f.Notef("data reflects collector state.")
	f.Hint("to register a cluster", "gcx instrumentation clusters setup --help")

	var buf bytes.Buffer
	f.Print(&buf)
	got := buf.String()

	// The only blank line in the output should be the leading separator.
	// No double newlines anywhere else.
	if strings.Contains(got[1:], "\n\n\n") {
		t.Errorf("unexpected double blank line: %q", got)
	}
	// Note-then-hint transition should be a single newline (no blank between).
	if !strings.Contains(got, "note: data reflects collector state.\nhint: to register a cluster:") {
		t.Errorf("note→hint should be adjacent, got: %q", got)
	}
}

func TestFooter_ConcurrentWarnsSafe(t *testing.T) {
	t.Parallel()

	var f instrumentation.Footer
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			f.Warnf("worker %d failed", n)
		}(i)
	}
	wg.Wait()

	var buf bytes.Buffer
	f.Print(&buf)
	if strings.Count(buf.String(), "warn: ") != 10 {
		t.Errorf("expected 10 warn lines; got: %q", buf.String())
	}
}
