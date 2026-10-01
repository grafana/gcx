package notifier

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"time"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/grafana/gcx/internal/host"
	skillops "github.com/grafana/gcx/internal/skills"
)

const (
	SkillsCheckKey       = "skills_update_notice"
	VersionCheckKey      = "gcx_version_notice"
	DefaultCheckInterval = 24 * time.Hour
)

// MaybeNotifySkills runs the default skills notifier check and writes a message
// to dst when installed gcx skills can be updated or retired copies remain. The check is throttled
// via persisted state; repeated calls within the interval are silent.
func MaybeNotifySkills(ctx context.Context, dst io.Writer) error {
	root, err := skillops.ResolveInstallRoot(ctx, "")
	if err != nil {
		return err
	}

	return maybeNotifySkillsAt(ctx, claudeplugin.SkillsFS(), claudeplugin.SkillsCatalog(), dst, StatePath(ctx), root, time.Now())
}

// MaybeNotifyVersion runs the default gcx version update check. Network errors
// are treated as silent misses so notification checks never affect CLI commands.
func MaybeNotifyVersion(ctx context.Context, dst io.Writer, currentVersion string) error {
	return maybeNotifyVersionAt(ctx, dst, StatePath(ctx), currentVersion, time.Now(), host.DefaultClient(), latestReleaseURL)
}

func maybeNotifySkillsAt(ctx context.Context, source fs.FS, catalog []byte, dst io.Writer, statePath, root string, now time.Time) error {
	state, err := LoadState(ctx, statePath)
	if err != nil {
		return err
	}
	if !ShouldRun(state, SkillsCheckKey, now, DefaultCheckInterval) {
		return nil
	}

	msg, err := SkillsUpdateMessage(ctx, source, catalog, root)
	if err != nil {
		return err
	}

	MarkRan(&state, SkillsCheckKey, now)
	if err := SaveState(ctx, statePath, state); err != nil {
		return err
	}
	if msg == "" {
		return nil
	}

	_, err = fmt.Fprintln(dst, msg)
	return err
}

func maybeNotifyVersionAt(ctx context.Context, dst io.Writer, statePath, currentVersion string, now time.Time, client *http.Client, url string) error {
	state, err := LoadState(ctx, statePath)
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
	if err := SaveState(ctx, statePath, state); err != nil {
		return err
	}
	if msg == "" {
		return nil
	}

	_, err = fmt.Fprintln(dst, msg)
	return err
}
