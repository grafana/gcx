package notifier

import (
	"io/fs"

	skillops "github.com/grafana/gcx/internal/skills"
)

const skillsUpdateCommand = "gcx skills update"

// SkillsUpdateMessage returns a human-facing notification message when the
// installed skills differ from the bundled skills in the current gcx binary.
// Returns the empty string when no update is needed.
func SkillsUpdateMessage(source fs.FS, root string) (string, error) {
	result, err := skillops.Update(source, root, nil, true)
	if err != nil {
		return "", err
	}
	if result.Written == 0 && result.Overwritten == 0 {
		return "", nil
	}

	return "Installed gcx skills can be updated to match this gcx version.\nRun: " + skillsUpdateCommand, nil
}
