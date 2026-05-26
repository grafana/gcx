package deeplink

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// openURL opens a URL in the user's default browser.
func openURL(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "open", url).Start()
	case "windows":
		return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.CommandContext(ctx, "xdg-open", url).Start()
	}
}
