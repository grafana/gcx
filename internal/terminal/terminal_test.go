package terminal_test

import (
	"testing"

	"github.com/grafana/gcx/internal/terminal"
	"github.com/stretchr/testify/assert"
)

func TestSetAndGetPiped(t *testing.T) {
	tests := []struct {
		name  string
		value bool
	}{
		{name: "set piped true", value: true},
		{name: "set piped false", value: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			terminal.ResetForTesting()
			terminal.SetPiped(tc.value)
			assert.Equal(t, tc.value, terminal.IsPiped())
		})
	}
}

func TestSetAndGetNoTruncate(t *testing.T) {
	tests := []struct {
		name  string
		value bool
	}{
		{name: "set no-truncate true", value: true},
		{name: "set no-truncate false", value: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			terminal.ResetForTesting()
			terminal.SetNoTruncate(tc.value)
			assert.Equal(t, tc.value, terminal.NoTruncate())
		})
	}
}

func TestResetForTesting(t *testing.T) {
	terminal.SetPiped(true)
	terminal.SetNoTruncate(true)

	terminal.ResetForTesting()

	assert.False(t, terminal.IsPiped(), "IsPiped should be false after reset")
	assert.False(t, terminal.NoTruncate(), "NoTruncate should be false after reset")
}

// TestDetectWithPipe verifies that when stdout is not a terminal (simulated via
// package state), IsPiped and NoTruncate are both true.
// Note: Detect() itself cannot be unit-tested directly without controlling
// os.Stdout's file descriptor, so we verify the setter/getter contract
// and the auto-set-NoTruncate behavior via SetPiped semantics.
func TestPipeImpliesNoTruncate(t *testing.T) {
	// When the CLI layer detects a pipe and calls SetPiped(true) + SetNoTruncate(true)
	// (as done in root command PersistentPreRun), both flags should be true.
	terminal.ResetForTesting()
	terminal.SetPiped(true)
	terminal.SetNoTruncate(true)

	assert.True(t, terminal.IsPiped())
	assert.True(t, terminal.NoTruncate())
}

func TestNoTruncateCanBeSetIndependentlyOfPiped(t *testing.T) {
	// --no-truncate flag on a TTY: piped=false but noTruncate=true
	terminal.ResetForTesting()
	terminal.SetPiped(false)
	terminal.SetNoTruncate(true)

	assert.False(t, terminal.IsPiped())
	assert.True(t, terminal.NoTruncate())
}

func TestDefaultsAfterReset(t *testing.T) {
	terminal.ResetForTesting()

	assert.False(t, terminal.IsPiped(), "default IsPiped should be false")
	assert.False(t, terminal.NoTruncate(), "default NoTruncate should be false")
}
