//go:build !linux && !darwin

package credentials

func headlessKeychainError() error { return nil }
