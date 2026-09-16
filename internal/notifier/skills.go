package notifier

import (
	"io/fs"

	skillops "github.com/grafana/gcx/internal/skills"
)

const skillsUpdateCommand = "gcx agent skills update"

// SkillsUpdateMessage returns a human-facing notification message when the
// installed skills differ from the bundled skills in the current gcx binary.
// Returns the empty string when no update is needed.
func SkillsUpdateMessage(source fs.FS, catalog []byte, root string) (string, error) {
	result, err := skillops.Update(source, catalog, root, nil, true)
	if err != nil {
		return "", err
	}
	for _, notice := range result.Notices {
		if notice.Status == skillops.Retired {
			return "Retired gcx skills are still present locally. Update reports replacements without deleting files.\nRun: " + skillsUpdateCommand, nil
		}
	}
	if result.Written == 0 && result.Overwritten == 0 {
		return "", nil
	}

	return "Installed gcx skills can be updated to match this gcx version.\nRun: " + skillsUpdateCommand, nil
}
