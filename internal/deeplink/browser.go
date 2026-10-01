package deeplink

import (
	"context"
	"runtime"

	"github.com/grafana/gcx/internal/host"
)

// openURL opens a URL in the user's default browser.
// The command is started in the background (fire-and-forget), detached from
// ctx's cancellation so that finishing the command does not kill the browser.
func openURL(ctx context.Context, url string) error {
	name, args := browserCommand(runtime.GOOS, url)
	cmd, err := host.Command(context.WithoutCancel(ctx), name, args...)
	if err != nil {
		return err
	}
	return cmd.Start()
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
