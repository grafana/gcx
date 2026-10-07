package auth

import "strings"

// terminalDeviceCandidates lists the device paths that may name the terminal
// behind a standard stream: the /dev/tty* entries other than /dev/tty itself,
// and the /dev/pts entries. It never lists /dev/stdin, /dev/fd/N or other
// descriptor aliases. Opening one of those would share the shell's open file
// description, and the poller's O_NONBLOCK would then stay on the shell's
// stdin after gcx exits.
func terminalDeviceCandidates(devNames, ptsPaths []string) []string {
	candidates := make([]string, 0, len(ptsPaths)+len(devNames))
	for _, path := range ptsPaths {
		if strings.HasPrefix(path, "/dev/pts/") && path != "/dev/pts/ptmx" {
			candidates = append(candidates, path)
		}
	}
	for _, name := range devNames {
		if strings.HasPrefix(name, "tty") && name != "tty" && !strings.Contains(name, "/") {
			candidates = append(candidates, "/dev/"+name)
		}
	}
	return candidates
}
