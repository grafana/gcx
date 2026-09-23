package gfc_test

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/pkg/gfc"
)

func TestFormatError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       []byte
		wantStatus int
		wantSubstr string
	}{
		{
			name:       "json error field",
			status:     400,
			body:       []byte(`{"error":"bad request"}`),
			wantStatus: 400,
			wantSubstr: "bad request",
		},
		{
			name:       "json message field",
			status:     404,
			body:       []byte(`{"message":"not found"}`),
			wantStatus: 404,
			wantSubstr: "not found",
		},
		{
			name:       "json with traceID",
			status:     500,
			body:       []byte(`{"error":"internal","traceID":"abc123"}`),
			wantStatus: 500,
			wantSubstr: "traceID abc123",
		},
		{
			name:       "plain text body",
			status:     502,
			body:       []byte("bad gateway"),
			wantStatus: 502,
			wantSubstr: "bad gateway",
		},
		{
			name:       "empty body",
			status:     503,
			body:       nil,
			wantStatus: 503,
			wantSubstr: "status 503",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gfc.FormatError(tt.status, tt.body)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var httpErr *gfc.HTTPStatusError
			if !errors.As(err, &httpErr) {
				t.Fatalf("expected HTTPStatusError, got %T", err)
			}
			if httpErr.HTTPStatusCode() != tt.wantStatus {
				t.Errorf("status = %d, want %d", httpErr.HTTPStatusCode(), tt.wantStatus)
			}
			if got := err.Error(); !contains(got, tt.wantSubstr) {
				t.Errorf("error = %q, want substring %q", got, tt.wantSubstr)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && containsAt(s, substr)
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
