package faro

import "encoding/json"

// SessionRecordingsListResponse is the API response for listing recordings in a session.
type SessionRecordingsListResponse struct {
	Items []RecordingListItem `json:"items"`
	Page  SessionPage         `json:"page"`
}

// RecordingListItem is a single recording entry in a list response.
type RecordingListItem struct {
	ID string `json:"id"`
}

// SessionPage holds cursor-based pagination metadata.
type SessionPage struct {
	HasNext bool   `json:"hasNext"`
	Next    string `json:"next"`
}

// RecordingManifestResponse is the API response for a recording manifest.
type RecordingManifestResponse struct {
	ID        string            `json:"id"`
	SessionID string            `json:"session_id"`
	Segments  []ManifestSegment `json:"segments"`
}

// ManifestSegment describes a segment within a recording manifest.
type ManifestSegment struct {
	ID int64 `json:"id"`
}

// RecordingSegmentResponse is the API response for a single segment's events.
type RecordingSegmentResponse struct {
	RecordingID string       `json:"recording_id"`
	Events      []RRWebEvent `json:"events"`
}

// RRWebEvent preserves a single rrweb event without interpreting its fields.
type RRWebEvent = json.RawMessage
