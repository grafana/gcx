package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const snapshotOnDiskConfig = `version: 1
contexts:
  ondisk: {}
current-context: ondisk
`

const snapshotFrozenConfig = `version: 1
contexts:
  frozen: {}
current-context: frozen
`

// TestLoadSourceSnapshotIsBoundToItsPath pins the path binding of the load
// source snapshot.
//
// A snapshot lets a caller that already inspected a config document hand the
// exact bytes it approved to the load, so a concurrent rewrite cannot make the
// load act on content nobody preflighted. The bytes alone do not say which
// file they came from, so the snapshot carries its path and the load only
// honors it for that path. Dropping the binding would let a snapshot taken
// from one layer satisfy the load of another — feeding a load content from a
// different config document entirely — which is the same class of bug as a
// write lock held for one source authorizing a write to another.
//
// The three cases are observably different because the frozen bytes and the
// bytes on disk name different contexts.
func TestLoadSourceSnapshotIsBoundToItsPath(t *testing.T) {
	tests := []struct {
		name string
		// snapshot builds the load options under test from the path being
		// loaded and an unrelated config path.
		snapshot    func(targetPath, otherPath string) loadOptions
		wantContext string
	}{
		{
			name:        "no snapshot reads the file from disk",
			snapshot:    func(_, _ string) loadOptions { return loadOptions{} },
			wantContext: "ondisk",
		},
		{
			name: "snapshot taken from the loaded path replaces the disk read",
			snapshot: func(target, _ string) loadOptions {
				return loadOptions{}.withSourceSnapshot(target, []byte(snapshotFrozenConfig))
			},
			wantContext: "frozen",
		},
		{
			name: "snapshot taken from another path is ignored",
			snapshot: func(_, other string) loadOptions {
				return loadOptions{}.withSourceSnapshot(other, []byte(snapshotFrozenConfig))
			},
			wantContext: "ondisk",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withFakeKeychain(t)
			targetPath := writeTestConfig(t, snapshotOnDiskConfig)
			otherPath := writeTestConfig(t, snapshotFrozenConfig)
			require.NotEqual(t, targetPath, otherPath)

			cfg, err := load(t.Context(), ExplicitConfigFile(targetPath), tc.snapshot(targetPath, otherPath))
			require.NoError(t, err)

			assert.Equal(t, tc.wantContext, cfg.CurrentContext)
			require.NotNil(t, cfg.Contexts[tc.wantContext])
		})
	}
}

// TestLoadSourceSnapshotDoesNotLeakBetweenDerivedLoads pins the value
// semantics that make the snapshot a per-target option.
//
// loadLayered derives one option value per source and sets the frozen bytes on
// that copy. Setting them on the shared value instead would carry one layer's
// snapshot into the next layer's load, so withSourceSnapshot must return a
// copy and leave its receiver untouched.
func TestLoadSourceSnapshotDoesNotLeakBetweenDerivedLoads(t *testing.T) {
	base := loadOptions{layer: "user"}

	derived := base.withSourceSnapshot("/layer-a.yaml", []byte(snapshotFrozenConfig))

	_, ok := base.snapshotFor("/layer-a.yaml")
	assert.False(t, ok, "deriving a load must not attach the snapshot to the shared options")
	contents, ok := derived.snapshotFor("/layer-a.yaml")
	require.True(t, ok)
	assert.Equal(t, snapshotFrozenConfig, string(contents))
}

// TestMigrationRejectsSourceChangedAfterPreflight pins the second reader of
// the load source snapshot, in migrateLegacyConfig.
//
// A legacy migration is decided from bytes read before the migration lock is
// held. Under the lock the file is read again, and the frozen bytes are what
// makes the comparison meaningful: they are the content the caller preflighted
// and consented to migrate. If the snapshot never reaches the migration, the
// comparison silently degrades into "the file equals itself" and gcx migrates
// and rewrites a document it never inspected.
//
// The stale snapshot here stands in for a rewrite between preflight and load:
// the load is told to migrate one legacy document while a different legacy
// document is on disk.
func TestMigrationRejectsSourceChangedAfterPreflight(t *testing.T) {
	withFakeKeychain(t)
	const onDiskLegacy = `contexts:
  ondisk:
    grafana:
      server: https://ondisk.example
current-context: ondisk
`
	const preflightedLegacy = `contexts:
  preflighted:
    grafana:
      server: https://preflighted.example
current-context: preflighted
`
	path := writeTestConfig(t, onDiskLegacy)
	require.True(t, isLegacyConfig([]byte(onDiskLegacy)))
	require.True(t, isLegacyConfig([]byte(preflightedLegacy)))

	// Persistence stays enabled: the re-read under the migration lock only
	// happens for a migration that is allowed to write.
	opts := loadOptions{}.withSourceSnapshot(path, []byte(preflightedLegacy))
	_, err := load(t.Context(), ExplicitConfigFile(path), opts)

	require.Error(t, err)
	require.ErrorContains(t, err, "changed after migration preflight")
	require.ErrorContains(t, err, "no config files or credentials were changed")

	// The error's promise must hold: nothing was migrated, replaced, or backed up.
	onDisk, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, onDiskLegacy, string(onDisk))
	_, statErr := os.Stat(path + legacyBackupSuffix)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}
