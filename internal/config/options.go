package config

import "fmt"

// This file holds the explicit parameter structs for the config load and write
// paths. Historically these parameters travelled as context values, which made
// them invisible in every signature they crossed and impossible to reason
// about locally: a caller could not tell which decisions a nested Load or Write
// would inherit from an ancestor. Only genuinely ambient, request-scoped CLI
// identity (the selected context name, the --config file, and the warning
// writer) belongs in a context.Context; a parameter that changes what a
// function does belongs in its signature.
//
// The exported entry points (Load, Write, LoadLayered, LoadForWrite) keep their
// signatures — they have hundreds of call sites across the CLI — and delegate
// to unexported workhorses that take these structs. Intra-package callers use
// the workhorses and state their options explicitly.

// loadOptions carries the parameters of a single config load. The zero value
// is the plain caller-facing load performed by the exported Load.
type loadOptions struct {
	// write carries the options for a write the load performs on the caller's
	// behalf: loading migrates plaintext credentials into the keychain and
	// persists legacy config migrations. Nesting rather than duplicating the
	// write options keeps a single definition of what a write needs to know.
	write writeOptions
}

// writeOptions carries the parameters of a single config write. The zero value
// is the plain caller-facing write performed by the exported Write.
type writeOptions struct {
	// writeLockHeldFor names the canonical config source whose write lock the
	// caller already holds, so the write must not acquire it again. Empty
	// means no lock is held and the write takes its own. Config write locks
	// are per-source (configLockFile derives the lock file from the canonical
	// source identity), so the identity is part of the claim: "a lock is
	// held" is only meaningful together with what it is held for.
	writeLockHeldFor string
}

// writeLockCovers reports whether the caller-held write lock protects a write
// to sourceIdentity, letting the write skip acquiring the flock itself.
//
// A lock held for a different source protects nothing here: the two writes
// take different lock files and can interleave freely. Treating that as
// "locked" would write the target without mutual exclusion, so it is an
// error rather than a silent pass.
func (o writeOptions) writeLockCovers(sourceIdentity string) (bool, error) {
	switch o.writeLockHeldFor {
	case "":
		return false, nil
	case sourceIdentity:
		return true, nil
	default:
		return false, fmt.Errorf(
			"refusing to write config without mutual exclusion: the write lock is held for %q but this write targets %q",
			o.writeLockHeldFor, sourceIdentity)
	}
}
