package loki //nolint:testpackage // white-box: exercises the unexported graphLimitHint

import (
	"strings"
	"testing"
)

func TestGraphLimitHint(t *testing.T) {
	tests := []struct {
		name     string
		limit    int
		returned int
		wantHint bool
		wantSubs []string
		wantNot  []string
	}{
		{
			name: "limit 0 discloses the backend default and promises no full range", limit: 0, returned: 100, wantHint: true,
			wantSubs: []string{"100 line(s) returned", "Loki's own default line limit may apply"},
			wantNot:  []string{"full queried range", "--limit 0 to"},
		},
		{
			name: "limit 0 hints even for a small result, since the cap is unknown", limit: 0, returned: 7, wantHint: true,
			wantSubs: []string{"7 line(s) returned"},
		},
		{
			name: "result reaching the limit reports the real count", limit: 50, returned: 50, wantHint: true,
			wantSubs: []string{"50 line(s) returned (capped by --limit)", "backend may enforce its own maximum"},
			wantNot:  []string{"--limit 0"},
		},
		{name: "result under the limit needs no hint", limit: 50, returned: 7, wantHint: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := graphLimitHint(tt.limit, tt.returned)
			if ok != tt.wantHint {
				t.Fatalf("graphLimitHint(%d, %d) ok = %v, want %v (hint %q)", tt.limit, tt.returned, ok, tt.wantHint, got)
			}
			for _, s := range tt.wantSubs {
				if !strings.Contains(got, s) {
					t.Errorf("hint %q missing %q", got, s)
				}
			}
			for _, s := range tt.wantNot {
				if strings.Contains(got, s) {
					t.Errorf("hint %q must not contain %q", got, s)
				}
			}
		})
	}
}
