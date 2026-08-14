package terminal

import (
	"os"
	"strings"
)

// sshSessionEnvVars are the environment variables that OpenSSH sets in an
// interactive session on the remote host.
var sshSessionEnvVars = []string{ //nolint:gochecknoglobals
	"SSH_CONNECTION",
	"SSH_CLIENT",
	"SSH_TTY",
}

// IsRemoteSession reports whether gcx runs in an SSH session.
//
// The OAuth callback server listens on the loopback address of the host that
// runs gcx. A browser on a different computer cannot open that address, so the
// login flow needs either a forwarded port or the manual paste mode.
//
// The check reads the environment, so it reports true inside a detached tmux or
// screen session that inherited a stale SSH_CONNECTION, SSH_CLIENT, or SSH_TTY
// from the session that created it. That result is harmless: the paste route
// only adds a second way to finish the login, the callback server still works,
// and the user sees one note too many.
//
// The result is deliberately not cached: callers read it at most twice per
// process, and tests set the environment with t.Setenv.
func IsRemoteSession() bool {
	for _, name := range sshSessionEnvVars {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	return false
}
