package config

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/require"
)

const writeLockTestConfig = `
version: 1
contexts:
  default: {}
current-context: default
`

// TestWriteLockIdentityGovernsWriteLockSkip pins the rule that a caller-held
// config write lock only authorizes writes to the source it was taken for.
//
// Config write locks are per-source: configLockFile derives the lock file from
// the canonical source identity. A caller that holds the lock for source A and
// then writes source B is writing without mutual exclusion, so the write must
// refuse rather than skip locking.
//
// Each case holds a foreign flock on the *target's* write lock, which makes the
// three outcomes observably different: covered writes proceed straight through
// it, uncovered writes block on it, and mismatched writes are rejected before
// reaching it.
func TestWriteLockIdentityGovernsWriteLockSkip(t *testing.T) {
	tests := []struct {
		name string
		// heldFor selects which source identity the caller claims to hold the
		// write lock for; "" means it claims none.
		heldFor         func(targetIdentity, otherIdentity string) string
		lockWaitTimeout time.Duration
		wantErrContains string
		wantWritten     bool
	}{
		{
			name:            "lock held for the target skips acquisition",
			heldFor:         func(target, _ string) string { return target },
			lockWaitTimeout: time.Second,
			wantWritten:     true,
		},
		{
			name:            "no lock held acquires the target lock",
			heldFor:         func(_, _ string) string { return "" },
			lockWaitTimeout: 200 * time.Millisecond,
			wantErrContains: "lock config for write",
			wantWritten:     false,
		},
		{
			name:            "lock held for another source is rejected",
			heldFor:         func(_, other string) string { return other },
			lockWaitTimeout: time.Second,
			wantErrContains: "the write lock is held for",
			wantWritten:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targetPath := writeTestConfig(t, writeLockTestConfig)
			otherPath := writeTestConfig(t, writeLockTestConfig)

			targetIdentity, err := canonicalConfigSourceForLayer(targetPath, "")
			require.NoError(t, err)
			otherIdentity, err := canonicalConfigSourceForLayer(otherPath, "")
			require.NoError(t, err)
			require.NotEqual(t, targetIdentity, otherIdentity)

			target := ExplicitConfigFile(targetPath)
			cfg, err := load(t.Context(), target, loadOptions{})
			require.NoError(t, err)

			// A change that must reach disk only if the write is allowed.
			cfg.SetContext("written", true, Context{})
			cfg.Resolve()

			before, err := os.ReadFile(targetPath)
			require.NoError(t, err)

			// Stand in for another process that holds the target's write lock.
			targetLockPath, err := configLockFile(targetIdentity)
			require.NoError(t, err)
			foreign := flock.New(targetLockPath)
			locked, err := foreign.TryLock()
			require.NoError(t, err)
			require.True(t, locked, "another test must not hold the target write lock")
			t.Cleanup(func() { _ = foreign.Unlock() })

			ctx, cancel := context.WithTimeout(t.Context(), tc.lockWaitTimeout)
			defer cancel()

			writeErr := write(ctx, target, cfg, writeOptions{
				writeLockHeldFor: tc.heldFor(targetIdentity, otherIdentity),
			})

			if tc.wantErrContains == "" {
				require.NoError(t, writeErr)
			} else {
				require.Error(t, writeErr)
				require.ErrorContains(t, writeErr, tc.wantErrContains)
			}

			after, err := os.ReadFile(targetPath)
			require.NoError(t, err)
			if tc.wantWritten {
				require.NotEqual(t, string(before), string(after), "write should have replaced the config")
				require.Contains(t, string(after), "written")
			} else {
				require.Equal(t, string(before), string(after), "config must be unchanged when the write is not allowed")
			}
		})
	}
}
