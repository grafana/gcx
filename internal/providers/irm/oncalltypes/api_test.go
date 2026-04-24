package oncalltypes_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/irm/oncalltypes"
)

func TestWithStartedAfter(t *testing.T) {
	t.Parallel()

	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	cfg := oncalltypes.ApplyListOpts([]oncalltypes.ListOption{oncalltypes.WithStartedAfter(ts)})
	if cfg.StartedAfter == nil || !cfg.StartedAfter.Equal(ts) {
		t.Errorf("expected StartedAfter=%v, got %v", ts, cfg.StartedAfter)
	}
}

func TestWithStartedAfter_NotSetByDefault(t *testing.T) {
	t.Parallel()

	cfg := oncalltypes.ApplyListOpts(nil)
	if cfg.StartedAfter != nil {
		t.Errorf("expected StartedAfter=nil, got %v", cfg.StartedAfter)
	}
}
