package cloudwatch_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/datasources/cloudwatch"
	cwclient "github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/stretchr/testify/assert"
)

func TestAllFramesEmpty(t *testing.T) {
	tests := []struct {
		name string
		resp *cwclient.QueryResponse
		want bool
	}{
		{
			name: "nil response",
			resp: nil,
			want: true,
		},
		{
			name: "no frames",
			resp: &cwclient.QueryResponse{Frames: nil},
			want: true,
		},
		{
			name: "single frame, empty timestamps",
			resp: &cwclient.QueryResponse{Frames: []cwclient.Frame{{Timestamps: nil}}},
			want: true,
		},
		{
			name: "single frame with data",
			resp: &cwclient.QueryResponse{Frames: []cwclient.Frame{
				{Timestamps: []time.Time{time.Unix(0, 0)}},
			}},
			want: false,
		},
		{
			name: "multi-frame, all empty",
			resp: &cwclient.QueryResponse{Frames: []cwclient.Frame{
				{Timestamps: nil},
				{Timestamps: []time.Time{}},
			}},
			want: true,
		},
		{
			name: "multi-frame, one has data",
			resp: &cwclient.QueryResponse{Frames: []cwclient.Frame{
				{Timestamps: nil},
				{Timestamps: []time.Time{time.Unix(0, 0)}},
			}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cloudwatch.AllFramesEmpty(tt.resp))
		})
	}
}

func TestMaybeEmitCrossAccountHint(t *testing.T) {
	withData := &cwclient.QueryResponse{Frames: []cwclient.Frame{
		{Timestamps: []time.Time{time.Unix(0, 0)}},
	}}
	empty := &cwclient.QueryResponse{Frames: []cwclient.Frame{{Timestamps: nil}}}
	zeroFrames := &cwclient.QueryResponse{Frames: nil}

	tests := []struct {
		name       string
		dimensions map[string]string
		accountID  string
		resp       *cwclient.QueryResponse
		wantHint   bool
	}{
		{
			name:       "fires when dims set, no account-id, all frames empty",
			dimensions: map[string]string{"InstanceId": "i-abc"},
			accountID:  "",
			resp:       empty,
			wantHint:   true,
		},
		{
			name:       "fires when zero frames are returned",
			dimensions: map[string]string{"InstanceId": "i-abc"},
			accountID:  "",
			resp:       zeroFrames,
			wantHint:   true,
		},
		{
			name:       "silent when one frame has data",
			dimensions: map[string]string{"InstanceId": "i-abc"},
			accountID:  "",
			resp:       withData,
			wantHint:   false,
		},
		{
			name:       "silent when account-id is 'all'",
			dimensions: map[string]string{"InstanceId": "i-abc"},
			accountID:  "all",
			resp:       empty,
			wantHint:   false,
		},
		{
			name:       "silent when account-id is a specific id",
			dimensions: map[string]string{"InstanceId": "i-abc"},
			accountID:  "123456789012",
			resp:       empty,
			wantHint:   false,
		},
		{
			name:       "silent when no dimensions set",
			dimensions: nil,
			accountID:  "",
			resp:       empty,
			wantHint:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cloudwatch.MaybeEmitCrossAccountHint(&buf, tt.dimensions, tt.accountID, tt.resp)
			if tt.wantHint {
				assert.Contains(t, buf.String(), "Hint: no data returned")
				assert.Contains(t, buf.String(), "--account-id all")
				assert.Contains(t, buf.String(), "list-accounts")
			} else {
				assert.Empty(t, buf.String(), "expected no hint")
			}
		})
	}
}
