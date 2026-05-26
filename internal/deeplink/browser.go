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
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
