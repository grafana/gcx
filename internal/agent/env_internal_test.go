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
	for f := range typ.Fields() {
		tags[f.Name] = f.Tag.Get("env")
	}
	assert.Equal(t, map[string]string{
		"Mode":          envMode,
		"Name":          envName,
		"AIIdentity":    envAIIdentity,
		"GooseIdentity": envGooseIdentity,
	}, tags)
}
