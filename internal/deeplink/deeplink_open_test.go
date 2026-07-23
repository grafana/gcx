package deeplink_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/deeplink"
)

// TestOpen_AgentModeSkipsBrowser pins the central browser guard: in agent
// mode Open must not launch anything and must report success (the URL is
// delivered as a typed stderr hint instead).
func TestOpen_AgentModeSkipsBrowser(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	if err := deeplink.Open("https://example.grafana.net/d/abc"); err != nil {
		t.Fatalf("Open() in agent mode = %v, want nil (browser skipped)", err)
	}
}

func TestOpen_RejectsNonHTTPURLsInAgentModeToo(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	if err := deeplink.Open("file:///etc/passwd"); err == nil {
		t.Fatal("Open() accepted a non-http URL")
	}
}
