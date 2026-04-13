package version_test

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/grafana/gcx/internal/version"
	"github.com/stretchr/testify/assert"
)

func TestGet_DefaultIsSNAPSHOT(t *testing.T) {
	version.Set("")
	assert.Equal(t, "SNAPSHOT", version.Get())
}

func TestSetAndGet(t *testing.T) {
	version.Set("1.2.3")
	t.Cleanup(func() { version.Set("") })
	assert.Equal(t, "1.2.3", version.Get())
}

func TestUserAgent(t *testing.T) {
	version.Set("1.2.3")
	t.Cleanup(func() { version.Set("") })
	expected := fmt.Sprintf("gcx/1.2.3 (%s/%s)", runtime.GOOS, runtime.GOARCH)
	assert.Equal(t, expected, version.UserAgent())
}

func TestUserAgent_SNAPSHOT(t *testing.T) {
	version.Set("")
	expected := fmt.Sprintf("gcx/SNAPSHOT (%s/%s)", runtime.GOOS, runtime.GOARCH)
	assert.Equal(t, expected, version.UserAgent())
}
