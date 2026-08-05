package terminal_test

import (
	"testing"

	"github.com/grafana/gcx/internal/terminal"
	"github.com/stretchr/testify/assert"
)

// TestIsRemoteSession pins the SSH detection used to decide whether the OAuth
// callback address is reachable from the user's browser. It uses t.Setenv, so
// it must not run in parallel.
func TestIsRemoteSession(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{
			name: "no ssh variables",
			env:  map[string]string{},
			want: false,
		},
		{
			name: "ssh connection set",
			env:  map[string]string{"SSH_CONNECTION": "10.0.0.1 51234 10.0.0.2 22"},
			want: true,
		},
		{
			name: "ssh client set",
			env:  map[string]string{"SSH_CLIENT": "10.0.0.1 51234 22"},
			want: true,
		},
		{
			name: "ssh tty set",
			env:  map[string]string{"SSH_TTY": "/dev/pts/0"},
			want: true,
		},
		{
			name: "empty values",
			env:  map[string]string{"SSH_CONNECTION": "", "SSH_CLIENT": "", "SSH_TTY": ""},
			want: false,
		},
		{
			name: "whitespace only values",
			env:  map[string]string{"SSH_CONNECTION": "   ", "SSH_TTY": "\t"},
			want: false,
		},
		{
			name: "one populated among blanks",
			env:  map[string]string{"SSH_CONNECTION": " ", "SSH_TTY": "/dev/pts/3"},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
				t.Setenv(name, "")
			}
			for name, value := range tc.env {
				t.Setenv(name, value)
			}

			assert.Equal(t, tc.want, terminal.IsRemoteSession())
		})
	}
}
