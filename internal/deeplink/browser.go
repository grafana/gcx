package deeplink

import (
	"os/exec"
	"runtime"
)

// openURL opens a URL in the user's default browser.
// The command is started in the background (fire-and-forget).
//
//nolint:noctx // exec.Command is intentional here - Start() returns immediately and we don't want a context killing the browser process.
func openURL(url string) error {
	name, args := browserCommand(runtime.GOOS, url)
	return exec.Command(name, args...).Start()
}

func browserCommand(goos string, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}
