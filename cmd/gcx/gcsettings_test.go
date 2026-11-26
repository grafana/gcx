package main

import (
	"math"
	"testing"
)

func TestGCSettings(t *testing.T) {
	tests := []struct {
		name        string
		gogc, limit string
		wantPercent *int
		wantLimit   *int64
		wantErr     bool
	}{
		{name: "unset leaves both"},
		{name: "percent", gogc: "400", wantPercent: new(400)},
		{name: "gc off", gogc: "off", wantPercent: new(-1)},
		{name: "bad percent means the default", gogc: "lots", wantPercent: new(100)},
		{name: "bytes", limit: "1048576", wantLimit: new(int64(1 << 20))},
		{name: "B suffix", limit: "512B", wantLimit: new(int64(512))},
		{name: "MiB", limit: "384MiB", wantLimit: new(int64(384 << 20))},
		{name: "GiB", limit: "2GiB", wantLimit: new(int64(2 << 30))},
		{name: "limit off", limit: "off", wantLimit: new(int64(math.MaxInt64))},
		{name: "unknown unit", limit: "384MB", wantErr: true},
		{name: "negative", limit: "-1", wantErr: true},
		{name: "overflow", limit: "9999999999TiB", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			percent, limit, err := gcSettings(tc.gogc, tc.limit)
			if (err != nil) != tc.wantErr {
				t.Fatalf("gcSettings(%q, %q) error = %v, wantErr %v", tc.gogc, tc.limit, err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !equalPtr(percent, tc.wantPercent) {
				t.Errorf("percent = %v, want %v", deref(percent), deref(tc.wantPercent))
			}
			if !equalPtr(limit, tc.wantLimit) {
				t.Errorf("limit = %v, want %v", deref(limit), deref(tc.wantLimit))
			}
		})
	}
}

func equalPtr[T comparable](a, b *T) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
