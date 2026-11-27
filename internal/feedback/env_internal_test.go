package feedback

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The Env struct tag is what the docs generator publishes; Endpoint reads the
// constant. If they drift, the docs advertise a variable that does nothing.
func TestEnvTagMatchesResolvedName(t *testing.T) {
	field, ok := reflect.TypeFor[Env]().FieldByName("Endpoint")
	assert.True(t, ok)
	assert.Equal(t, envEndpoint, field.Tag.Get("env"))
}
