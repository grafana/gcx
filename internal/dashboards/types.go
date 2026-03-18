package dashboards

import "time"

// SnapshotResult holds metadata about a rendered dashboard snapshot.
// It is used for both JSON output (agent mode) and table output (human mode).
type SnapshotResult struct {
	UID        string    `json:"uid"`
	PanelID    *int      `json:"panel_id"`
	FilePath   string    `json:"file_path"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	Theme      string    `json:"theme"`
	RenderedAt time.Time `json:"rendered_at"`
	FileSize   int64     `json:"-"`
}
