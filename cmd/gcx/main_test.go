package main

import "testing"

func TestParsePseudoVersion(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		wantCommit string
		wantDate   string
	}{
		{
			name:       "valid pseudo-version",
			version:    "v0.1.1-0.20260401105553-2fbda4a2dd27",
			wantCommit: "2fbda4a",
			wantDate:   "2026-04-01T10:55:53Z",
		},
		{
			name:       "pseudo-version with +dirty suffix",
			version:    "v0.1.1-0.20260401105553-2fbda4a2dd27+dirty",
			wantCommit: "2fbda4a",
			wantDate:   "2026-04-01T10:55:53Z",
		},
		{
			name:       "pseudo-version with +incompatible suffix",
			version:    "v2.0.1-0.20260401105553-2fbda4a2dd27+incompatible",
			wantCommit: "2fbda4a",
			wantDate:   "2026-04-01T10:55:53Z",
		},
		{
			name:       "tagged version",
			version:    "v1.0.0",
			wantCommit: "",
			wantDate:   "",
		},
		{
			name:       "pre-release tagged version",
			version:    "v1.0.0-rc.1",
			wantCommit: "",
			wantDate:   "",
		},
		{
			name:       "devel",
			version:    "(devel)",
			wantCommit: "",
			wantDate:   "",
		},
		{
			name:       "empty string",
			version:    "",
			wantCommit: "",
			wantDate:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCommit, gotDate := parsePseudoVersion(tt.version)
			if gotCommit != tt.wantCommit {
				t.Errorf("commit = %q, want %q", gotCommit, tt.wantCommit)
			}
			if gotDate != tt.wantDate {
				t.Errorf("date = %q, want %q", gotDate, tt.wantDate)
			}
		})
	}
}
