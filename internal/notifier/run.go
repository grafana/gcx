package notifier

import (
	"fmt"
	"io"
	"io/fs"
	"time"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	skillops "github.com/grafana/gcx/internal/skills"
)

const (
	SkillsCheckKey        = "skills_update_notice"
	DefaultCheckInterval  = 24 * time.Hour
	DisableNotifierEnvVar = "GCX_NO_UPDATE_NOTIFIER"
)

// MaybeNotifySkills runs the default skills notifier check and writes a message
// to dst only when installed gcx skills can be updated. The check is throttled
// via persisted state; repeated calls within the interval are silent.
func MaybeNotifySkills(dst io.Writer) error {
	statePath, err := StatePath()
	if err != nil {
		return err
	}
	root, err := skillops.ResolveInstallRoot("")
	if err != nil {
		return err
	}

	return maybeNotifySkillsAt(claudeplugin.SkillsFS(), dst, statePath, root, time.Now(), DefaultCheckInterval)
}

func maybeNotifySkillsAt(source fs.FS, dst io.Writer, statePath, root string, now time.Time, interval time.Duration) error {
	state, err := LoadState(statePath)
	if err != nil {
		return err
	}
	if !ShouldRun(state, SkillsCheckKey, now, interval) {
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
