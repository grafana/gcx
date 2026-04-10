package version

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGet_DefaultIsSNAPSHOT(t *testing.T) {
	ver = "" // reset
	assert.Equal(t, "SNAPSHOT", Get())
}

func TestSetAndGet(t *testing.T) {
	Set("1.2.3")
	t.Cleanup(func() { ver = "" })
	assert.Equal(t, "1.2.3", Get())
}

func TestUserAgent(t *testing.T) {
	Set("1.2.3")
	t.Cleanup(func() { ver = "" })
	expected := fmt.Sprintf("gcx/1.2.3 (%s/%s)", runtime.GOOS, runtime.GOARCH)
	assert.Equal(t, expected, UserAgent())
}

func TestUserAgent_SNAPSHOT(t *testing.T) {
	ver = ""
	expected := fmt.Sprintf("gcx/SNAPSHOT (%s/%s)", runtime.GOOS, runtime.GOARCH)
	assert.Equal(t, expected, UserAgent())
}
