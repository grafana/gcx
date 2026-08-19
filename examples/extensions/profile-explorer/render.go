package main

import (
	"fmt"
	"math/bits"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// packageColors is @grafana/flamegraph's packageColors palette, so a function
// lands on the same colour here as it does in the Grafana flamegraph panel.
var packageColors = [24]string{
	"#df8b53", "#e0ad6c", "#68b7cf", "#59c0a3", "#6897ca", "#8982c9",
	"#eba8e6", "#ffe175", "#b7dbab", "#f4d598", "#4e92f9", "#f9ba8f",
	"#f29191", "#82b5d8", "#e5a8e2", "#aea2e0", "#9ac48a", "#f2c96d",
	"#65c5db", "#f9934e", "#ea6460", "#5195ce", "#d683ce", "#806eb7",
}

// murmur3 is the 32-bit MurmurHash3 the Grafana flamegraph keys its palette on.
func murmur3(key string, seed uint32) uint32 {
	const c1, c2 = 0xcc9e2d51, 0x1b873593
	b := []byte(key)
	h1 := seed

	blocks := len(b) / 4
	i := 0
	for range blocks {
		k1 := uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16 | uint32(b[i+3])<<24
		i += 4
		k1 = bits.RotateLeft32(k1*c1, 15) * c2
		h1 ^= k1
		h1 = bits.RotateLeft32(h1, 13)
		h1 = h1*5 + 0xe6546b64
	}

	var k1 uint32
	switch len(b) & 3 {
	case 3:
		k1 ^= uint32(b[i+2]) << 16
		fallthrough
	case 2:
		k1 ^= uint32(b[i+1]) << 8
		fallthrough
	case 1:
		k1 ^= uint32(b[i])
	}
	h1 ^= bits.RotateLeft32(k1*c1, 15) * c2

	h1 ^= uint32(len(b))
	h1 ^= h1 >> 16
	h1 *= 0x85ebca6b
	h1 ^= h1 >> 13
	h1 *= 0xc2b2ae35
	h1 ^= h1 >> 16
	return h1
}

// packageName extracts the package a symbol belongs to, mirroring
// @grafana/flamegraph's getPackageName so sibling frames from one package share
// a colour.
func packageName(label string) string {
	for _, ext := range []string{".php", ".py", ".rb"} {
		if pos := strings.Index(label, ext); pos >= 0 {
			if slash := strings.LastIndex(label[:pos], "/"); slash >= 0 {
				return label[:slash+1]
			}
		}
	}
	if pos := strings.Index(label, "::"); pos >= 0 {
		return label[:pos]
	}
	if colon := strings.Index(label, ":"); colon >= 0 {
		path := strings.TrimPrefix(label[:colon], "./node_modules/")
		if slash := strings.Index(path, "/"); slash >= 0 {
			return path[:slash]
		}
		return path
	}
	if slash := strings.LastIndex(label, "/"); slash >= 0 {
		if dot := strings.Index(label[slash+1:], "."); dot >= 0 {
			return label[:slash+1+dot+1]
		}
		return label[:slash+1]
	}
	trimmed := label
	if p := strings.Index(trimmed, "("); p >= 0 {
		trimmed = trimmed[:p]
	}
	if dot := strings.Index(trimmed, "."); dot >= 0 {
		return label[:dot+1]
	}
	return label
}

func frameColor(label string) lipgloss.Color {
	return lipgloss.Color(packageColors[murmur3(packageName(label), 0)%uint32(len(packageColors))])
}

// centreTruncate fits a label into width cells, trimming from the middle so
// that both the package and the function name stay readable.
func centreTruncate(s string, width int) string {
	r := []rune(s)
	if width <= 0 {
		return ""
	}
	if len(r) <= width {
		return s + strings.Repeat(" ", width-len(r))
	}
	if width <= 3 {
		return string(r[:width])
	}
	keep := width - 2
	left := keep / 2
	right := keep - left
	return string(r[:left]) + ".." + string(r[len(r)-right:])
}

// formatValue renders a sample count in the profile's own unit.
func formatValue(v int64, unit string) string {
	switch unit {
	case "nanoseconds":
		return formatDuration(float64(v))
	case "bytes":
		return formatBytes(float64(v))
	default:
		return fmt.Sprintf("%d", v)
	}
}

func formatDuration(ns float64) string {
	switch {
	case ns < 1_000:
		return fmt.Sprintf("%.0fns", ns)
	case ns < 1_000_000:
		return fmt.Sprintf("%.1fµs", ns/1_000)
	case ns < 1_000_000_000:
		return fmt.Sprintf("%.1fms", ns/1_000_000)
	case ns < 60_000_000_000:
		return fmt.Sprintf("%.2fs", ns/1_000_000_000)
	default:
		return fmt.Sprintf("%.1fmin", ns/60_000_000_000)
	}
}

func formatBytes(b float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f%s", b, units[i])
	}
	return fmt.Sprintf("%.1f%s", b, units[i])
}

func percent(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

// renderIcicle draws the flamegraph as an icicle chart, zoom root at the top,
// one terminal row per stack level. Frames narrower than a cell are dropped.
func renderIcicle(f *flame, n *nav, width, height int, search string) string {
	rootX, rootW := n.rootBounds(f)
	if width <= 0 || height <= 0 || rootW <= 0 {
		return ""
	}
	term := strings.ToLower(search)

	var out strings.Builder
	rows := 0
	for lvl := n.rootLevel; lvl < len(f.levels) && rows < height; lvl++ {
		var row strings.Builder
		cursor := 0
		for idx, fr := range f.levels[lvl].frames {
			if fr.Total <= 0 || fr.X+fr.Total <= rootX || fr.X >= rootX+rootW {
				continue
			}
			start := int((fr.X - rootX) * int64(width) / rootW)
			end := int((fr.X + fr.Total - rootX) * int64(width) / rootW)
			start = clamp(start, 0, width)
			end = clamp(end, 0, width)
			if end <= start || start < cursor {
				continue
			}
			if start > cursor {
				row.WriteString(strings.Repeat(" ", start-cursor))
			}
			label := f.name(fr)
			style := lipgloss.NewStyle().Background(frameColor(label)).Foreground(lipgloss.Color("#000000"))
			switch {
			case lvl == n.selLevel && idx == n.selFrame:
				style = style.Reverse(true).Bold(true)
			case term != "" && strings.Contains(strings.ToLower(label), term):
				style = style.Underline(true).Bold(true)
			}
			row.WriteString(style.Render(centreTruncate(label, end-start)))
			cursor = end
		}
		out.WriteString(row.String())
		out.WriteString("\n")
		rows++
	}
	return strings.TrimRight(out.String(), "\n")
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
