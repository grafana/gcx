package deeplink

// BrowserCommand exposes browserCommand for black-box tests.
func BrowserCommand(goos string, url string) (string, []string) {
	return browserCommand(goos, url)
}
