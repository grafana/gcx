package config

import (
	"bytes"
	"fmt"
)

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

	// sourceSnapshot, when set, freezes the bytes the load must read for one
	// specific config path, instead of reading that path from disk. A caller
	// that already inspected a config document — layered preflight, the
	// targeted --file write selection, the login mutation guard — hands the
	// exact bytes it approved to the load, so a rewrite between the inspection
	// and the load cannot make the load act on different content than the one
	// that was preflighted.
	//
	// It is a pointer so that "no snapshot" stays distinguishable from "a
	// snapshot of an empty file", and it is per-target: a load derived from
	// another load's options must not silently reuse its snapshot. Set it with
	// withSourceSnapshot and read it with snapshotFor, never directly — both
	// clone, and snapshotFor enforces the path binding below.
	sourceSnapshot *configSnapshot

	// keychainPolicy, when set, is the already-resolved credential-storage
	// decision this load must obey instead of deriving one from the bytes it
	// reads.
	//
	// It is the one field here that is deliberately *inherited* rather than
	// stated per target. Every other field describes one config document; the
	// keychain policy describes the process: it is resolved once from all
	// trusted layers plus GCX_KEYCHAIN, and a per-layer load that re-derived
	// it from its own bytes would reach a different answer than the load it
	// belongs to — a user layer holding "keychain: off" would not stop the
	// system layer's load from opening the real keychain and migrating that
	// layer's plaintext into it. So loadLayered sets it on the parent options
	// before the per-iteration copy, and every derived load carries it.
	//
	// It is a pointer so that "no policy resolved yet" (resolve one) stays
	// distinguishable from a zero-valued policy. Set it with
	// withKeychainPolicy and read it with resolvedKeychainPolicy.
	keychainPolicy *keychainPolicy
}

// withKeychainPolicy returns a copy of the options bound to an already-resolved
// credential-storage policy, so every load derived from them obeys that
// decision rather than re-deriving one from the bytes it happens to read.
func (o loadOptions) withKeychainPolicy(policy keychainPolicy) loadOptions {
	o.keychainPolicy = &policy
	return o
}

// resolvedKeychainPolicy returns the policy these options were bound to, and
// whether one was bound at all.
func (o loadOptions) resolvedKeychainPolicy() (keychainPolicy, bool) {
	if o.keychainPolicy == nil {
		return keychainPolicy{}, false
	}
	return *o.keychainPolicy, true
}

// configSnapshot is a frozen copy of one config document, bound to the path it
// was taken from. The binding is the point: bytes alone cannot say which file
// they came from, and a snapshot of one config satisfying the load of another
// would feed a load content it never preflighted.
type configSnapshot struct {
	path     string
	contents []byte
}

// withSourceSnapshot returns a copy of the options that reads path from the
// given bytes rather than from disk. The contents are cloned so a later
// mutation of the caller's buffer cannot change what the load sees.
func (o loadOptions) withSourceSnapshot(path string, contents []byte) loadOptions {
	o.sourceSnapshot = &configSnapshot{path: path, contents: bytes.Clone(contents)}
	return o
}

// snapshotFor returns the frozen bytes for path, if these options carry a
// snapshot taken from exactly that path.
//
// A snapshot bound to a different path is not a snapshot of this one: honoring
// it would load one config document's content as another's. Like the write
// lock identity in writeLockCovers, the claim "I already have these bytes" is
// only meaningful together with what they are bytes of.
func (o loadOptions) snapshotFor(path string) ([]byte, bool) {
	if o.sourceSnapshot == nil || o.sourceSnapshot.path != path {
		return nil, false
	}
	return bytes.Clone(o.sourceSnapshot.contents), true
}

// forWrite projects the load's write-relevant parameters onto the options of a
// write the load performs on the caller's behalf. The source snapshot is not
// among them: it says what a load must read, and no write consults it.
//
// It reads o.layer rather than taking a parameter because writeConfig only
// consults writeOptions.layer when the config being written carries no source
// layer of its own (cfg.sourceLayer == ""), and a config produced by load
// always carries one: load's sole caller of forWrite runs after load has set
// config.sourceLayer, so that field wins and o.layer is never consulted for
// the layer decision itself.
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
