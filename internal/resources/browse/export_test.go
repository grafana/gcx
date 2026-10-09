package browse

import tea "charm.land/bubbletea/v2"

// IsBrowserMsg reports whether msg is one of the browser's own background
// results, so test harnesses can feed those back and drop unrelated ones.
func IsBrowserMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case typesLoadedMsg, objectsLoadedMsg, schemasLoadedMsg, openedMsg:
		return true
	}
	return false
}
