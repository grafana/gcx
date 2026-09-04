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
//
// The fields are flat rather than nesting writeOptions: loading and writing
// share only the config layer, and a load-side decision (which layer's bytes
// to read, whether that layer may read legacy keychain accounts) must not have
// to be read out of a struct named after the write. The one write parameter a
// load carries, writeLockHeldFor, is projected onto writeOptions by forWrite.
type loadOptions struct {
	// layer names the config layer being loaded ("system", "user", "local",
	// "explicit", or empty when the caller has not classified the target).
	// It decides whether the file is read through the no-symlink repository
	// reader, how the canonical source identity is derived, and whether a
	// legacy migration may read predictable per-user keychain accounts, so it
	// must be stated per target rather than inherited from an ancestor load.
	layer string

	// suppressMigrationPersistence makes a legacy config migrate in memory
	// only: it takes no migration lock, writes no file, and marks the result
	// migrationDeferred. Layered loads and pre-write reloads use it so that
	// reading never mutates a layer the caller did not select.
	suppressMigrationPersistence bool

	// explicitLegacyMigrationConsent is the canonical identity of the config
	// document the user selected through --config or GCX_CONFIG. Only the
	// high-level explicit loader mints it; the generic ExplicitConfigFile
	// Source stays a path resolver and cannot by itself authorize reads from
	// predictable legacy keychain accounts. Empty means no consent, and the
	// identity comparison in trustedLegacyKeychainSource rejects it.
	explicitLegacyMigrationConsent string

	// migrationWarnings collects the per-source in-memory migration
	// diagnostics of a layered load so the caller can collapse them into one
	// warning. Nil sends each warning straight to the warning writer or log.
	migrationWarnings *inMemoryMigrationWarningCollector

	// writeLockHeldFor names the canonical config source whose write lock the
	// caller already holds, for a write the load performs on the caller's
	// behalf: loading migrates plaintext credentials into the keychain and
	// persists legacy config migrations.
	writeLockHeldFor string
}

// forWrite projects the load's write-relevant parameters onto the options of a
// write the load performs on the caller's behalf. It reads o.layer rather than
// taking a parameter because writeConfig only consults writeOptions.layer when
// the config being written carries no source layer of its own (cfg.sourceLayer
// == ""), and a config produced by load always carries one: load's sole
// caller of forWrite runs after load has set config.sourceLayer, so that
// field wins and o.layer is never consulted for the layer decision itself.
func (o loadOptions) forWrite() writeOptions {
	return writeOptions{
		layer:            o.layer,
		writeLockHeldFor: o.writeLockHeldFor,
	}
}

// writeOptions carries the parameters of a single config write. The zero value
// is the plain caller-facing write performed by the exported Write.
type writeOptions struct {
	// layer names the config layer being written. It is only consulted when
	// the config being written carries no source layer of its own (a config
	// built in memory rather than loaded), and then decides the same things
	// it decides on the load side.
	layer string

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
