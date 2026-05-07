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

func TestGetCommit_Default(t *testing.T) {
	version.SetBuildInfo("", "")
	t.Cleanup(func() { version.SetBuildInfo("", "") })
	assert.Equal(t, "unknown", version.GetCommit())
}

func TestGetDate_Default(t *testing.T) {
	version.SetBuildInfo("", "")
	t.Cleanup(func() { version.SetBuildInfo("", "") })
	assert.Equal(t, "unknown", version.GetDate())
}

func TestSetBuildInfo(t *testing.T) {
	version.SetBuildInfo("abc1234", "2026-05-07T12:00:00Z")
	t.Cleanup(func() { version.SetBuildInfo("", "") })
	assert.Equal(t, "abc1234", version.GetCommit())
	assert.Equal(t, "2026-05-07T12:00:00Z", version.GetDate())
}

func TestInfo(t *testing.T) {
	version.Set("3.0.0")
	version.SetBuildInfo("fff9999", "2026-12-25T00:00:00Z")
	t.Cleanup(func() {
		version.Set("")
		version.SetBuildInfo("", "")
	})

	info := version.Info()
	assert.Equal(t, "3.0.0", info.Version)
	assert.Equal(t, "fff9999", info.Commit)
	assert.Equal(t, "2026-12-25T00:00:00Z", info.BuildDate)
	assert.Equal(t, runtime.Version(), info.Go)
	assert.Equal(t, runtime.GOOS, info.OS)
	assert.Equal(t, runtime.GOARCH, info.Arch)
}
