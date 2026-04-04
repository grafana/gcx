package style

import "math"

//nolint:gochecknoglobals
var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline renders a sequence of values as a compact sparkline string
// using Unicode block characters. Works in both styled and unstyled modes.
func Sparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}

	lo, hi := values[0], values[0]
	for _, v := range values[1:] {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}

	rng := hi - lo
	if rng == 0 {
		rng = 1
	}

	result := make([]rune, len(values))
	for i, v := range values {
		normalized := (v - lo) / rng
		idx := max(0, int(math.Round(normalized*float64(len(sparkBlocks)-1))))
		idx = min(idx, len(sparkBlocks)-1)
		result[i] = sparkBlocks[idx]
	}

	return string(result)
}
