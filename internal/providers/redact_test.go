package providers_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
)

func TestRedactSecrets(t *testing.T) {
	sloProvider := &mockProvider{
		name: "slo",
		configKeys: []providers.ConfigKey{
			{Name: "token", Secret: true},
			{Name: "url", Secret: false},
		},
	}

	tests := []struct {
		name           string
		input          map[string]map[string]string
		registered     []providers.Provider
		expectedOutput map[string]map[string]string
	}{
		{
			name:           "nil input is a no-op",
			input:          nil,
			registered:     []providers.Provider{sloProvider},
			expectedOutput: nil,
		},
		{
			name:       "empty registry redacts all values for all providers",
			registered: []providers.Provider{},
			input: map[string]map[string]string{
				"slo": {"token": "secret-value", "url": "https://slo.example.com"},
			},
			expectedOutput: map[string]map[string]string{
				"slo": {"token": "**REDACTED**", "url": "**REDACTED**"},
			},
		},
		{
			name:       "unknown provider: all non-empty values redacted",
			registered: []providers.Provider{sloProvider},
			input: map[string]map[string]string{
				"unknown": {"key1": "val1", "key2": "val2"},
			},
			expectedOutput: map[string]map[string]string{
				"unknown": {"key1": "**REDACTED**", "key2": "**REDACTED**"},
			},
		},
		{
			name:       "known provider: secret key redacted, non-secret key preserved",
			registered: []providers.Provider{sloProvider},
			input: map[string]map[string]string{
				"slo": {"token": "my-token", "url": "https://slo.example.com"},
			},
			expectedOutput: map[string]map[string]string{
				"slo": {"token": "**REDACTED**", "url": "https://slo.example.com"},
			},
		},
		{
			name:       "known provider: undeclared key redacted",
			registered: []providers.Provider{sloProvider},
			input: map[string]map[string]string{
				"slo": {"token": "my-token", "url": "https://slo.example.com", "extra": "value"},
			},
			expectedOutput: map[string]map[string]string{
				"slo": {"token": "**REDACTED**", "url": "https://slo.example.com", "extra": "**REDACTED**"},
			},
		},
		{
			name:       "empty values are left empty regardless of secret flag",
			registered: []providers.Provider{sloProvider},
			input: map[string]map[string]string{
				"slo": {"token": "", "url": ""},
			},
			expectedOutput: map[string]map[string]string{
				"slo": {"token": "", "url": ""},
			},
		},
		{
			name:       "mixed: known and unknown providers in same map",
			registered: []providers.Provider{sloProvider},
			input: map[string]map[string]string{
				"slo":     {"token": "slo-token", "url": "https://slo.example.com"},
				"unknown": {"apikey": "secret"},
			},
			expectedOutput: map[string]map[string]string{
				"slo":     {"token": "**REDACTED**", "url": "https://slo.example.com"},
				"unknown": {"apikey": "**REDACTED**"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			providers.RedactSecrets(tc.input, tc.registered)
			assert.Equal(t, tc.expectedOutput, tc.input)
		})
	}
}
