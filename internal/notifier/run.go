package notifier

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	skillops "github.com/grafana/gcx/internal/skills"
)

const (
	SkillsCheckKey       = "skills_update_notice"
	VersionCheckKey      = "gcx_version_notice"
	DefaultCheckInterval = 24 * time.Hour
)

// MaybeNotifySkills runs the default skills notifier check and writes a message
// to dst only when installed gcx skills can be updated. The check is throttled
// via persisted state; repeated calls within the interval are silent.
func MaybeNotifySkills(dst io.Writer) error {
	root, err := skillops.ResolveInstallRoot("")
	if err != nil {
		return err
	}

	return maybeNotifySkillsAt(claudeplugin.SkillsFS(), dst, StatePath(), root, time.Now())
}

// MaybeNotifyVersion runs the default gcx version update check. Network errors
// are treated as silent misses so notification checks never affect CLI commands.
func MaybeNotifyVersion(ctx context.Context, dst io.Writer, currentVersion string) error {
	return maybeNotifyVersionAt(ctx, dst, StatePath(), currentVersion, time.Now(), http.DefaultClient, latestReleaseURL)
}

func maybeNotifySkillsAt(source fs.FS, dst io.Writer, statePath, root string, now time.Time) error {
	state, err := LoadState(statePath)
	if err != nil {
		return err
	}
	if !ShouldRun(state, SkillsCheckKey, now, DefaultCheckInterval) {
		return nil
	}

	msg, err := SkillsUpdateMessage(source, root)
	if err != nil {
		return err
	}

	MarkRan(&state, SkillsCheckKey, now)
	if err := SaveState(statePath, state); err != nil {
		return err
	}
	if msg == "" {
		return nil
	}

	_, err = fmt.Fprintln(dst, msg)
	return err
}

func maybeNotifyVersionAt(ctx context.Context, dst io.Writer, statePath, currentVersion string, now time.Time, client *http.Client, url string) error {
	state, err := LoadState(statePath)
	if err != nil {
		return err
	}
	if !ShouldRun(state, VersionCheckKey, now, DefaultCheckInterval) {
		return nil
	}

	msg, err := VersionUpdateMessage(ctx, client, url, currentVersion)
	if err != nil {
		return nil //nolint:nilerr // Version lookup is non-critical UX; do not fail CLI commands.
	}

	MarkRan(&state, VersionCheckKey, now)
	if err := SaveState(statePath, state); err != nil {
		return err
	}
	if msg == "" {
		return nil
	}

	_, err = fmt.Fprintln(dst, msg)
	return err
}
