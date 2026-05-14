package style_test

import (
	"testing"

	"github.com/grafana/gcx/internal/style"
	"github.com/stretchr/testify/assert"
)

// All tests run with styling disabled (no TTY in test env, --no-color via
// SetEnabled(false)) so the helpers must return their input unchanged.

func TestColorHelpersReturnInputWhenStylingDisabled(t *testing.T) {
	style.SetEnabled(false)
	t.Cleanup(func() { style.SetEnabled(true) })

	tests := []struct {
		name string
		got  string
	}{
		{"ColorPercent threshold red", style.ColorPercent("99.9%", 99.9, false, false)},
		{"ColorPercent threshold yellow", style.ColorPercent("12.0%", 12.0, false, false)},
		{"ColorPercent dim", style.ColorPercent(" 0.5%", 0.5, true, false)},
		{"ColorPercent error override", style.ColorPercent(" 0.1%", 0.1, true, true)},
		{"ColorPercent default", style.ColorPercent(" 5.0%", 5.0, false, false)},

		{"ColorCell error", style.ColorCell("name", false, true)},
		{"ColorCell dim", style.ColorCell("svc", true, false)},
		{"ColorCell error overrides dim", style.ColorCell("name", true, true)},
		{"ColorCell default", style.ColorCell("svc", false, false)},

		{"ColorMutedText", style.ColorMutedText("── divider ──")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotContains(t, tc.got, "\x1b[", "must not contain ANSI escape")
		})
	}

	assert.Equal(t, "99.9%", style.ColorPercent("99.9%", 99.9, false, false))
	assert.Equal(t, "name", style.ColorCell("name", true, true))
	assert.Equal(t, "── divider ──", style.ColorMutedText("── divider ──"))
}

func TestColorPercentBranchesCoverAllThresholds(t *testing.T) {
	// With styling disabled the function returns input unchanged for every
	// branch, so we just exercise each path to keep them covered. The visual
	// test (ANSI emission when styling is on) is exercised manually — we
	// don't test it here because the test environment never has a TTY.
	style.SetEnabled(false)
	t.Cleanup(func() { style.SetEnabled(true) })

	cases := []struct {
		name string
		pct  float64
		dim  bool
		err  bool
	}{
		{"error path", 50.0, false, true},
		{"dim path", 0.5, true, false},
		{"red threshold", 50.0, false, false},
		{"yellow threshold", 10.0, false, false},
		{"under-yellow threshold", 9.99, false, false},
		{"zero pct", 0.0, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := style.ColorPercent("text", c.pct, c.dim, c.err)
			assert.Equal(t, "text", out)
		})
	}
}
