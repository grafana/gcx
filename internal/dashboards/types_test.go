package dashboards_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafanactl/internal/dashboards"
)

func TestSnapshotResult_JSONMarshal(t *testing.T) {
	fixedTime := time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC)

	t.Run("panel_id is null when PanelID is nil", func(t *testing.T) {
		r := dashboards.SnapshotResult{
			UID:        "abc123",
			PanelID:    nil,
			FilePath:   "/tmp/abc123.png",
			Width:      1920,
			Height:     1080,
			Theme:      "dark",
			RenderedAt: fixedTime,
		}
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("unexpected marshal error: %v", err)
		}
		got := string(b)
		if !strings.Contains(got, `"panel_id":null`) {
			t.Errorf("expected panel_id to be null, got: %s", got)
		}
	})

	t.Run("all fields present when PanelID is set", func(t *testing.T) {
		panelID := 42
		r := dashboards.SnapshotResult{
			UID:        "abc123",
			PanelID:    &panelID,
			FilePath:   "/tmp/abc123-panel-42.png",
			Width:      800,
			Height:     600,
			Theme:      "light",
			RenderedAt: fixedTime,
		}
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("unexpected marshal error: %v", err)
		}
		got := string(b)
		for _, field := range []string{`"uid"`, `"panel_id"`, `"file_path"`, `"width"`, `"height"`, `"theme"`, `"rendered_at"`} {
			if !strings.Contains(got, field) {
				t.Errorf("expected field %s in JSON output, got: %s", field, got)
			}
		}
	})
}
