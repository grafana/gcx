//go:build linux

package credentials

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeadlessLockedKeychainDoesNotPrompt(t *testing.T) {
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is required for an isolated Secret Service")
	}
	for _, test := range []struct {
		name            string
		loginCollection bool
		locked          bool
	}{
		{"login collection", true, true},
		{"default alias", false, true},
		{"unlocked collection", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			bus := exec.CommandContext(t.Context(), daemon, "--session", "--nofork", "--print-address=1")
			stdout, err := bus.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, bus.Start())
			t.Cleanup(func() { _ = bus.Process.Kill(); _ = bus.Wait() })
			scanner := bufio.NewScanner(stdout)
			require.True(t, scanner.Scan())
			address := scanner.Text()
			conn, err := dbus.Connect(address)
			require.NoError(t, err)
			defer conn.Close()
			props := lockedCollectionProperties{loginCollection: test.loginCollection, locked: test.locked}
			for _, path := range []dbus.ObjectPath{"/org/freedesktop/secrets", "/org/freedesktop/secrets/collection/login", "/org/freedesktop/secrets/aliases/default"} {
				require.NoError(t, conn.Export(props, path, "org.freedesktop.DBus.Properties"))
			}
			service := &pendingUnlockService{}
			require.NoError(t, conn.Export(service, "/org/freedesktop/secrets", "org.freedesktop.Secret.Service"))
			require.NoError(t, conn.Export(pendingUnlockPrompt{}, "/org/freedesktop/secrets/prompt/fixture", "org.freedesktop.Secret.Prompt"))
			_, err = conn.RequestName("org.freedesktop.secrets", dbus.NameFlagDoNotQueue)
			require.NoError(t, err)
			t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
			t.Setenv("DISPLAY", "")
			t.Setenv("WAYLAND_DISPLAY", "")
			mode := "locked"
			if !test.locked {
				mode = "unlocked"
			}
			t.Setenv("GCX_TEST_LOCKED_KEYCHAIN", mode)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			binary, err := os.Executable()
			require.NoError(t, err)
			helper := exec.CommandContext(ctx, binary, "-test.run=^TestHeadlessLockedKeychainOpenHelper$")
			output, err := helper.CombinedOutput()
			require.NoError(t, ctx.Err(), "Open waited for a Secret Service unlock prompt: %s", output)
			require.NoError(t, err, "%s", output)
			assert.Zero(t, service.unlockCalls.Load(), "a headless probe must not request an unlock")
		})
	}
}

func TestHeadlessLockedKeychainOpenHelper(t *testing.T) {
	if os.Getenv("GCX_TEST_LOCKED_KEYCHAIN") == "" {
		t.Skip("subprocess helper")
	}
	if os.Getenv("GCX_TEST_LOCKED_KEYCHAIN") == "unlocked" {
		require.NoError(t, headlessKeychainError())
		return
	}
	store := Open()
	_, err := store.Get("fixture")
	require.ErrorIs(t, err, ErrLocked)
}

type lockedCollectionProperties struct {
	loginCollection bool
	locked          bool
}

func (p lockedCollectionProperties) Get(iface, property string) (dbus.Variant, *dbus.Error) {
	if iface == "org.freedesktop.Secret.Service" && property == "Collections" {
		paths := []dbus.ObjectPath{}
		if p.loginCollection {
			paths = append(paths, "/org/freedesktop/secrets/collection/login")
		}
		return dbus.MakeVariant(paths), nil
	}
	if iface == "org.freedesktop.Secret.Collection" && property == "Locked" {
		return dbus.MakeVariant(p.locked), nil
	}
	return dbus.Variant{}, dbus.MakeFailedError(errors.New("unexpected Secret Service property"))
}

type pendingUnlockService struct{ unlockCalls atomic.Int32 }

func (s *pendingUnlockService) Unlock([]dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	s.unlockCalls.Add(1)
	return []dbus.ObjectPath{}, "/org/freedesktop/secrets/prompt/fixture", nil
}

type pendingUnlockPrompt struct{}

func (pendingUnlockPrompt) Prompt(string) *dbus.Error { return nil }

func TestDesktopKeychainSkipsHeadlessProbe(t *testing.T) {
	for _, display := range []string{"DISPLAY", "WAYLAND_DISPLAY"} {
		t.Run(display, func(t *testing.T) {
			t.Setenv("DBUS_SESSION_BUS_ADDRESS", "disabled:")
			t.Setenv("DISPLAY", "")
			t.Setenv("WAYLAND_DISPLAY", "")
			t.Setenv(display, "fixture-display")
			require.NoError(t, headlessKeychainError())
		})
	}
}
