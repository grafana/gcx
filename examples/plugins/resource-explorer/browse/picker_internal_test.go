package browse

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		pattern, target string
		want            bool
	}{
		{"", "anything", true},
		{"dash", "dashboards", true},
		{"dshb", "dashboards", true},
		{"DASH", "dashboards.v1.dashboard.grafana.app", true},
		{"sm chk", "Check checks.v1alpha1.syntheticmonitoring", false}, // space is literal
		{"chksyn", "Check checks.v1alpha1.syntheticmonitoring", true},
		{"zz", "dashboards", false},
		{"sdrawhsad", "dashboards", false}, // order matters
	}
	for _, tc := range tests {
		t.Run(tc.pattern+"/"+tc.target, func(t *testing.T) {
			_, ok := fuzzyMatch(tc.pattern, tc.target)
			assert.Equal(t, tc.want, ok)
		})
	}
}

func TestFuzzyMatch_Ranking(t *testing.T) {
	// Contiguous and word-boundary matches outrank scattered ones.
	contiguous, ok := fuzzyMatch("folder", "Folder folders.v1.folder.grafana.app")
	require.True(t, ok)
	scattered, ok := fuzzyMatch("folder", "Fooled orders.v1.x.grafana.app")
	require.True(t, ok)
	assert.Greater(t, contiguous, scattered)
}

type testItem string

func (i testItem) Title() string       { return string(i) }
func (i testItem) Description() string { return "" }
func (i testItem) FilterValue() string { return string(i) }

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func newTestPicker(items ...string) *picker {
	p := newPicker("t")
	p.setSize(40, 10)
	pi := make([]pickerItem, 0, len(items))
	for _, i := range items {
		pi = append(pi, testItem(i))
	}
	p.setItems(pi)
	return &p
}

func selectedTitle(t *testing.T, p *picker) string {
	t.Helper()
	it, ok := p.selected()
	require.True(t, ok)
	return it.Title()
}

func TestPicker_Navigation(t *testing.T) {
	p := newTestPicker("a", "b", "c")
	assert.Equal(t, "a", selectedTitle(t, p))
	p.update(keyMsg("down"))
	p.update(keyMsg("j"))
	assert.Equal(t, "c", selectedTitle(t, p))
	p.update(keyMsg("down")) // clamps at the end
	assert.Equal(t, "c", selectedTitle(t, p))
	p.update(keyMsg("k"))
	assert.Equal(t, "b", selectedTitle(t, p))
}

func TestPicker_FilterLifecycle(t *testing.T) {
	p := newTestPicker("dashboards", "folders", "dashboard-snapshots")

	p.update(keyMsg("/"))
	require.True(t, p.settingFilter())
	for _, r := range "dsnap" {
		p.update(keyMsg(string(r)))
	}
	assert.Equal(t, "dashboard-snapshots", selectedTitle(t, p))
	assert.Contains(t, p.view(), "1/3")

	for range 4 {
		p.update(keyMsg("backspace"))
	}
	assert.Len(t, p.matches, 3, `"d" matches every item`)

	// j and k are filter text while typing, not navigation.
	p.update(keyMsg("a"))
	assert.Len(t, p.matches, 2, `"da" drops folders`)
	p.update(keyMsg("j"))
	assert.Empty(t, p.matches)
	p.update(keyMsg("backspace"))

	p.update(keyMsg("enter"))
	assert.False(t, p.settingFilter())
	assert.True(t, p.filterApplied())

	p.resetFilter()
	assert.False(t, p.filterApplied())
	assert.Len(t, p.matches, 3)
}

func TestPicker_EscWhileTypingCancels(t *testing.T) {
	p := newTestPicker("a", "b")
	p.update(keyMsg("/"))
	p.update(keyMsg("b"))
	p.update(keyMsg("esc"))
	assert.False(t, p.settingFilter())
	assert.False(t, p.filterApplied())
	assert.Len(t, p.matches, 2)
}

func TestPicker_NoMatches(t *testing.T) {
	p := newTestPicker("a", "b")
	p.update(keyMsg("/"))
	p.update(keyMsg("z"))
	_, ok := p.selected()
	assert.False(t, ok)
}

func TestPicker_ScrollKeepsCursorVisible(t *testing.T) {
	items := make([]string, 50)
	for i := range items {
		items[i] = string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	p := newTestPicker(items...)
	for range 30 {
		p.update(keyMsg("down"))
	}
	assert.Contains(t, p.view(), selectedTitle(t, p))
	assert.LessOrEqual(t, len(strings.Split(p.view(), "\n")), 10)
}
