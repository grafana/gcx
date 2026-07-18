package org_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/org"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// Compile-time assertion that *OrgUser satisfies ResourceIdentity.
var _ adapter.ResourceIdentity = &org.OrgUser{}

func TestOrgUser_GetResourceName(t *testing.T) {
	tests := []struct {
		name     string
		user     org.OrgUser
		wantName string
	}{
		{
			name:     "numeric user id",
			user:     org.OrgUser{UserID: 42, Login: "alice"},
			wantName: "42",
		},
		{
			name:     "zero user id renders as 0",
			user:     org.OrgUser{UserID: 0},
			wantName: "0",
		},
		{
			name:     "large user id",
			user:     org.OrgUser{UserID: 1234567},
			wantName: "1234567",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.GetResourceName(); got != tt.wantName {
				t.Errorf("GetResourceName() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestOrgUser_SetResourceName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantID int
	}{
		{
			name:   "numeric id is parsed",
			input:  "42",
			wantID: 42,
		},
		{
			name:   "non-numeric leaves user id at zero value",
			input:  "alice",
			wantID: 0,
		},
		{
			name:   "empty string leaves user id at zero value",
			input:  "",
			wantID: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &org.OrgUser{}
			u.SetResourceName(tt.input)
			if u.UserID != tt.wantID {
				t.Errorf("UserID = %d, want %d", u.UserID, tt.wantID)
			}
		})
	}
}

func TestOrgUser_RoundTripIdentity(t *testing.T) {
	original := org.OrgUser{UserID: 42, Login: "alice"}
	name := original.GetResourceName()

	restored := &org.OrgUser{}
	restored.SetResourceName(name)

	if restored.UserID != original.UserID {
		t.Errorf("round-trip UserID = %d, want %d", restored.UserID, original.UserID)
	}
}
