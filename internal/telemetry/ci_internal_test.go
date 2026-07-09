package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectCI(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		wantProvider string
		wantIsCI     bool
	}{
		{
			name: "no CI env",
		},
		{
			name:         "github actions",
			env:          map[string]string{"GITHUB_ACTIONS": "true"},
			wantProvider: "github_actions",
			wantIsCI:     true,
		},
		{
			name:         "specific provider wins over generic CI var",
			env:          map[string]string{"GITLAB_CI": "true", "CI": "true"},
			wantProvider: "gitlab",
			wantIsCI:     true,
		},
		{
			name:         "generic CI var only",
			env:          map[string]string{"CI": "true"},
			wantProvider: "unknown",
			wantIsCI:     true,
		},
		{
			name: "CI=false is not CI",
			env:  map[string]string{"CI": "false"},
		},
		{
			name:         "non-boolean signature value counts as set",
			env:          map[string]string{"JENKINS_URL": "https://jenkins.internal/"},
			wantProvider: "jenkins",
			wantIsCI:     true,
		},
		{
			name:         "BUILD_NUMBER falls back to unknown",
			env:          map[string]string{"BUILD_NUMBER": "42"},
			wantProvider: "unknown",
			wantIsCI:     true,
		},
		{
			name:         "first matching provider in table order wins",
			env:          map[string]string{"GITHUB_ACTIONS": "true", "DRONE": "true"},
			wantProvider: "github_actions",
			wantIsCI:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			provider, isCI := detectCI(fakeGetenv(tc.env))
			assert.Equal(t, tc.wantProvider, provider)
			assert.Equal(t, tc.wantIsCI, isCI)
		})
	}
}
