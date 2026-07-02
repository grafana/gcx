package kg_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEntityRefToken(t *testing.T) {
	ref, err := kg.ParseEntityRefToken("myapp/Service/checkout")
	require.NoError(t, err)
	assert.Equal(t, "myapp", ref.Domain)
	assert.Equal(t, "Service", ref.Type)
	assert.Equal(t, "checkout", ref.Name)
	for _, bad := range []string{"", "a/b", "a//c", "/b/c", "a/b/"} {
		_, err := kg.ParseEntityRefToken(bad)
		require.Error(t, err, "expected error for %q", bad)
	}
}

func TestParseTTL(t *testing.T) {
	cases := map[string]int64{"": -1, "1h": 3600, "30m": 1800, "7d": 604800, "0": 0, "-1h": -3600}
	for in, want := range cases {
		got, err := kg.ParseTTL(in)
		require.NoError(t, err, "input %q", in)
		assert.Equal(t, want, got, "input %q", in)
	}
	_, err := kg.ParseTTL("nonsense")
	require.Error(t, err)

	// Day-suffix inputs with trailing junk must error, not silently truncate.
	for _, bad := range []string{"1.5d", "7 d", "7days", "d"} {
		_, err := kg.ParseTTL(bad)
		require.Error(t, err, "input %q must be rejected, not truncated", bad)
	}
}

func TestValidateDomains(t *testing.T) {
	require.NoError(t, kg.ValidateWritableDomain("myapp"))
	require.Error(t, kg.ValidateWritableDomain("kg"))
	require.Error(t, kg.ValidateWritableDomain("MyApp"))
	require.NoError(t, kg.ValidateDomain("kg"))
	require.Error(t, kg.ValidateIdentifier("9bad", "type"))
	require.NoError(t, kg.ValidateIdentifier("Service", "type"))
}
