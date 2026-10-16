//go:build !linux

package credentials

func headlessKeychainError() error { return nil }
