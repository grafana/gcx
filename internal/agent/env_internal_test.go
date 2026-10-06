package agent

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A changed input must remain in the generated environment reference.
func TestEnvTagsMatchResolvedNames(t *testing.T) {
	typ := reflect.TypeFor[Env]()
	tags := make(map[string]string, typ.NumField())
	inputs := EnvironmentVariables()
	for f := range typ.Fields() {
		tags[f.Name] = f.Tag.Get("env")
		assert.Contains(t, inputs, tags[f.Name], "test helpers must clear every documented control")
	}
	assert.Equal(t, map[string]string{
		"Mode":          envMode,
		"Name":          envName,
		"AIIdentity":    envAIIdentity,
		"GooseIdentity": envGooseIdentity,
	}, tags)
}

// Native markers and explicit inputs must use the same labels.
func TestNativeHarnessLabelsAreSupported(t *testing.T) {
	for _, marker := range append(harnessEnvVars, harnessSessionVars...) {
		t.Run(marker.envVar, func(t *testing.T) {
			assert.Equal(t, marker.name, supportedName(marker.name))
		})
	}
}
