package faro

import (
	"encoding/json"
	"time"
)

// SessionRecordingsListResponse is the API response for listing recordings in a session.
type SessionRecordingsListResponse struct {
	SessionID string              `json:"session_id"`
	Items     []RecordingListItem `json:"items"`
	Page      SessionPage         `json:"page"`
}

// RecordingListItem is a single recording entry in a list response.
type RecordingListItem struct {
	ID      string    `json:"id"`
	Status  string    `json:"status"`
	StartTS time.Time `json:"start_ts"`
	EndTS   time.Time `json:"end_ts"`
}

// SessionPage holds cursor-based pagination metadata.
type SessionPage struct {
	HasNext    bool   `json:"hasNext"`
	Limit      int64  `json:"limit"`
	Next       string `json:"next"`
	TotalItems int64  `json:"totalItems"`
}

// RecordingManifestResponse is the API response for a recording manifest.
type RecordingManifestResponse struct {
	ID                string             `json:"id"`
	SessionID         string             `json:"session_id"`
	Status            string             `json:"status"`
	StartTS           time.Time          `json:"start_ts"`
	EndTS             time.Time          `json:"end_ts"`
	Segments          []ManifestSegment  `json:"segments"`
	InactivityPeriods []InactivityPeriod `json:"inactivity_periods"`
}

// ManifestSegment describes a segment within a recording manifest.
type ManifestSegment struct {
	ID                int64  `json:"id"`
	StartOffsetMs     int64  `json:"start_offset_ms"`
	EndOffsetMs       int64  `json:"end_offset_ms"`
	RequiresSegmentID *int64 `json:"requires_segment_id,omitempty"`
}

// InactivityPeriod represents a gap in recording activity.
type InactivityPeriod struct {
	StartOffsetMs int64 `json:"start_offset_ms"`
	EndOffsetMs   int64 `json:"end_offset_ms"`
}

// RecordingSegmentResponse is the API response for a single segment's events.
type RecordingSegmentResponse struct {
	ID          string       `json:"id"`
	RecordingID string       `json:"recording_id"`
	Events      []RRWebEvent `json:"events"`
}

// RRWebEvent is a single rrweb event within a segment.
type RRWebEvent struct {
	Type      int             `json:"type"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}
