package main

import (
	"strings"
	"testing"
)

func TestPackageName(t *testing.T) {
	tests := []struct {
		label string
		want  string
	}{
		{"runtime.selectgo", "runtime."},
		{"github.com/grafana/gcx/internal/query.Run", "github.com/grafana/gcx/internal/query."},
		{"std::vec::Vec::push", "std"},
		{"total", "total"},
	}
	for _, tc := range tests {
		if got := packageName(tc.label); got != tc.want {
			t.Errorf("packageName(%q): got %q, want %q", tc.label, got, tc.want)
		}
	}
}

// Frames from one package must land on one colour, which is the only reason the
// palette is keyed by hash rather than by index.
func TestFrameColorIsStablePerPackage(t *testing.T) {
	a := frameColor("github.com/grafana/gcx/internal/query.Run")
	b := frameColor("github.com/grafana/gcx/internal/query.Stop")
	if a != b {
		t.Errorf("same package coloured differently: %v vs %v", a, b)
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		value int64
		unit  string
		want  string
	}{
		{10810000000, "nanoseconds", "10.81s"},
		{1500000, "nanoseconds", "1.5ms"},
		{800, "nanoseconds", "800ns"},
		{2048, "bytes", "2.0KiB"},
		{512, "bytes", "512B"},
		{7, "count", "7"},
	}
	for _, tc := range tests {
		if got := formatValue(tc.value, tc.unit); got != tc.want {
			t.Errorf("formatValue(%d, %q): got %q, want %q", tc.value, tc.unit, got, tc.want)
		}
	}
}

func TestCentreTruncate(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"abc", 5, "abc  "},
		{"abcdefgh", 6, "ab..gh"},
		{"abcdefgh", 2, "ab"},
		{"abc", 0, ""},
	}
	for _, tc := range tests {
		if got := centreTruncate(tc.in, tc.width); got != tc.want {
			t.Errorf("centreTruncate(%q, %d): got %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}

// renderIcicle must respect the zoom root: a frame outside it never reaches the
// output, and the zoomed frame fills the width.
func TestRenderIcicleHonoursZoom(t *testing.T) {
	f, err := parseFlamegraph([]byte(testPayload), "nanoseconds")
	if err != nil {
		t.Fatalf("parseFlamegraph: %v", err)
	}
	n := &nav{width: 40}
	n.moveDeeper(f)

	full := renderIcicle(f, n, 40, 4, "")
	if !strings.Contains(full, "b") {
		t.Error("unzoomed render should include frame b")
	}

	n.zoomIn(f)
	zoomed := renderIcicle(f, n, 40, 4, "")
	lines := strings.Split(zoomed, "\n")
	if len(lines) == 0 {
		t.Fatal("no rows rendered")
	}
	if strings.Contains(stripANSI(lines[0]), "b") {
		t.Errorf("zoomed render leaked frame b: %q", stripANSI(lines[0]))
	}
	if got := len(stripANSI(lines[0])); got != 40 {
		t.Errorf("zoom root should fill the width: got %d cells, want 40", got)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
