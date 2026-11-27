package errortracking_test

import (
	"github.com/grafana/gcx/internal/agent"
)

func asAgent(f *fakeET) {
	agent.SetFlag(true)
	f.t.Cleanup(func() { agent.SetFlag(false) })
}
