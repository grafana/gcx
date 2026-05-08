package instrumentation_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/stretchr/testify/assert"
)

// TestInstrumentationProvider_Name verifies the provider registers with the
// expected name so gcx providers lists it correctly.
func TestInstrumentationProvider_Name(t *testing.T) {
	p := &instrumentation.InstrumentationProvider{}
	assert.Equal(t, "instrumentation", p.Name())
}

// TestInstrumentationProvider_TypedRegistrationsNil verifies that TypedRegistrations
// returns nil — no GVK is registered with the resource adapter.
func TestInstrumentationProvider_TypedRegistrationsNil(t *testing.T) {
	p := &instrumentation.InstrumentationProvider{}
	assert.Nil(t, p.TypedRegistrations(), "TypedRegistrations must return nil")
}

// TestInstrumentationProvider_CommandsNil verifies that Commands returns nil —
// the command tree is wired from cmd/gcx/root directly.
func TestInstrumentationProvider_CommandsNil(t *testing.T) {
	p := &instrumentation.InstrumentationProvider{}
	assert.Nil(t, p.Commands(), "Commands must return nil — tree is wired from root")
}

// TestInstrumentationProvider_ValidateAlwaysNil verifies that Validate returns
// nil for any input — no provider-specific config keys to validate.
func TestInstrumentationProvider_ValidateAlwaysNil(t *testing.T) {
	p := &instrumentation.InstrumentationProvider{}
	assert.NoError(t, p.Validate(nil))
	assert.NoError(t, p.Validate(map[string]string{"key": "value"}))
}

// TestInstrumentationProvider_ConfigKeysNil verifies that ConfigKeys returns nil.
func TestInstrumentationProvider_ConfigKeysNil(t *testing.T) {
	p := &instrumentation.InstrumentationProvider{}
	assert.Nil(t, p.ConfigKeys())
}
