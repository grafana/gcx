package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	appversion "github.com/grafana/gcx/internal/version"
)

const (
	latestReleaseURL    = "https://api.github.com/repos/grafana/gcx/releases/latest"
	releaseTagURLPrefix = "https://github.com/grafana/gcx/releases/tag/"
	versionCheckTimeout = 2 * time.Second
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// VersionUpdateMessage fetches the latest released gcx version and returns a
// notification message when it is newer than currentVersion.
func VersionUpdateMessage(ctx context.Context, client *http.Client, url, currentVersion string) (string, error) {
	if client == nil {
		return "", errors.New("version update client is required")
	}

	current, ok := parseNotifyVersion(currentVersion)
	if !ok {
		return "", nil
	}

	release, err := fetchLatestRelease(ctx, client, url)
	if err != nil {
		return "", err
	}
	latest, ok := parseNotifyVersion(release.TagName)
	if !ok {
		return "", nil
	}
	if !latest.GreaterThan(current) {
		return "", nil
	}

	upgradeURL := release.HTMLURL
	if upgradeURL == "" {
		upgradeURL = releaseTagURLPrefix + release.TagName
	}

	return fmt.Sprintf("A new gcx version is available: %s (current: %s)\nUpgrade: %s", release.TagName, currentVersion, upgradeURL), nil
}

func fetchLatestRelease(ctx context.Context, client *http.Client, url string) (githubRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, versionCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return githubRelease{}, fmt.Errorf("create latest release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", appversion.UserAgent())

	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return githubRelease{}, fmt.Errorf("fetch latest release: HTTP %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode latest release: %w", err)
	}
	return release, nil
}

func parseNotifyVersion(v string) (*semver.Version, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" || strings.EqualFold(v, "SNAPSHOT") || strings.EqualFold(v, "(devel)") || strings.Contains(v, " ") {
		return nil, false
	}

	version, err := semver.NewVersion(v)
	if err != nil {
		return nil, false
	}
	return version, true
}
