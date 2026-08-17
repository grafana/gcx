package azuremonitor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateKQLArg(t *testing.T) {
	require.NoError(t, validateKQLArg("AppRequests | take 10"))

	for _, kql := range []string{"", "   ", "\t\n"} {
		err := validateKQLArg(kql)
		require.Error(t, err, "%q", kql)
		assert.Contains(t, err.Error(), "KQL query must not be empty")
	}
}
