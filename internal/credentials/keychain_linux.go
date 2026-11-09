package credentials

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/godbus/dbus/v5"
)

func headlessKeychainError() error {
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return normalizeKeyringError(err)
	}
	defer conn.Close()
	service := conn.Object("org.freedesktop.secrets", "/org/freedesktop/secrets")
	var value dbus.Variant
	if err := service.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.Secret.Service", "Collections").Store(&value); err != nil {
		return normalizeKeyringError(err)
	}
	var collections []dbus.ObjectPath
	if err := value.Store(&collections); err != nil {
		return err
	}
	collection := dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	if !slices.Contains(collections, collection) {
		collection = "/org/freedesktop/secrets/aliases/default"
	}
	if err := conn.Object("org.freedesktop.secrets", collection).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.Secret.Collection", "Locked").Store(&value); err != nil {
		return normalizeKeyringError(err)
	}
	var locked bool
	if err := value.Store(&locked); err != nil {
		return err
	}
	if locked {
		// go-keyring unlocks before reading, then waits without a deadline for
		// a graphical prompt that a headless session cannot answer.
		return fmt.Errorf("%w: the Secret Service collection cannot be unlocked in a headless session", ErrLocked)
	}
	return nil
}
