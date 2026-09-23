package notifier

import (
	"io/fs"
	"strings"

	skillops "github.com/grafana/gcx/internal/skills"
)

const skillsUpdateCommand = "gcx agent skills update"

// SkillsUpdateMessage reports pending content updates and retired installations
// independently, with an explicit action for each. It returns an empty string
// when neither condition needs attention.
func SkillsUpdateMessage(source fs.FS, catalog []byte, root string) (string, error) {
	result, err := skillops.Update(source, catalog, root, nil, true)
	if err != nil {
		return "", err
	}
	var messages []string
	if result.Written > 0 || result.Overwritten > 0 {
		messages = append(messages, "Installed gcx skills can be updated to match this gcx version.\nRun: "+skillsUpdateCommand)
	}
	var retired []string
	for _, notice := range result.Notices {
		if notice.Status == skillops.Retired {
			retired = append(retired, notice.Name)
		}
	}
	if len(retired) > 0 {
		messages = append(messages, "Retired gcx skills are still present locally.\nRun: gcx agent skills uninstall "+strings.Join(retired, " "))
	}
	return strings.Join(messages, "\n\n"), nil
}
