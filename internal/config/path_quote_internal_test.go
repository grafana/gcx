package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuoteConfigPathForOS(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		goos string
		want string
	}{
		{"Unix spaces", "/tmp/config owner/config.yaml", "darwin", `"/tmp/config owner/config.yaml"`},
		{"Windows spaces", `C:\Program Files\gcx\config.yaml`, "windows", `'C:/Program Files/gcx/config.yaml'`},
		{"Windows dollar sign", `C:\Users\$owner\config.yaml`, "windows", `'C:/Users/$owner/config.yaml'`},
		{"Windows apostrophe", `C:\Users\O'Brien\config.yaml`, "windows", `'C:/Users/O''Brien/config.yaml'`},
		{"Windows trailing separator", `C:\Program Files\gcx\`, "windows", `'C:/Program Files/gcx/'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, quoteConfigPathForOS(tc.path, tc.goos))
		})
	}
}
