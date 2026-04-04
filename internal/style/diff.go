package style

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderDiff writes a colorized unified diff to w.
// name identifies the resource. before and after are YAML strings.
// When styling is disabled, outputs standard unified diff text.
func RenderDiff(w io.Writer, name, before, after string) {
	styled := IsStylingEnabled()

	var addStyle, delStyle, mutedStyle lipgloss.Style
	if styled {
		addStyle = lipgloss.NewStyle().Foreground(ColorSuccess)
		delStyle = lipgloss.NewStyle().Foreground(ColorError)
		mutedStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	}

	render := func(prefix, line string, s lipgloss.Style) string {
		text := prefix + line
		if styled {
			return s.Render(text)
		}
		return text
	}

	// Header.
	hdrA := "--- a/" + name
	hdrB := "+++ b/" + name
	if styled {
		fmt.Fprintln(w, mutedStyle.Render(hdrA))
		fmt.Fprintln(w, mutedStyle.Render(hdrB))
	} else {
		fmt.Fprintln(w, hdrA)
		fmt.Fprintln(w, hdrB)
	}

	beforeLines := splitLines(before)
	afterLines := splitLines(after)

	// Simple LCS-based diff.
	lcs := longestCommonSubsequence(beforeLines, afterLines)

	bi, ai, li := 0, 0, 0
	for li < len(lcs) {
		// Emit deletions (lines in before not in LCS).
		for bi < len(beforeLines) && beforeLines[bi] != lcs[li] {
			fmt.Fprintln(w, render("-", beforeLines[bi], delStyle))
			bi++
		}
		// Emit additions (lines in after not in LCS).
		for ai < len(afterLines) && afterLines[ai] != lcs[li] {
			fmt.Fprintln(w, render("+", afterLines[ai], addStyle))
			ai++
		}
		// Emit context line (common).
		fmt.Fprintln(w, render(" ", lcs[li], mutedStyle))
		bi++
		ai++
		li++
	}
	// Remaining deletions.
	for bi < len(beforeLines) {
		fmt.Fprintln(w, render("-", beforeLines[bi], delStyle))
		bi++
	}
	// Remaining additions.
	for ai < len(afterLines) {
		fmt.Fprintln(w, render("+", afterLines[ai], addStyle))
		ai++
	}
}

// splitLines splits s into lines, handling the trailing newline gracefully.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty string from a final newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// longestCommonSubsequence returns the LCS of two string slices.
func longestCommonSubsequence(a, b []string) []string {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			switch {
			case a[i-1] == b[j-1]:
				dp[i][j] = dp[i-1][j-1] + 1
			case dp[i-1][j] >= dp[i][j-1]:
				dp[i][j] = dp[i-1][j]
			default:
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to recover the LCS.
	result := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		switch {
		case a[i-1] == b[j-1]:
			result = append(result, a[i-1])
			i--
			j--
		case dp[i-1][j] >= dp[i][j-1]:
			i--
		default:
			j--
		}
	}
	// Reverse.
	for l, r := 0, len(result)-1; l < r; l, r = l+1, r-1 {
		result[l], result[r] = result[r], result[l]
	}
	return result
}
