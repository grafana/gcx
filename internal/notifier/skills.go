package notifier

import (
	"io/fs"

	skillops "github.com/grafana/gcx/internal/skills"
)

const skillsUpdateCommand = "gcx skills update"

// SkillsCheckResult summarizes whether installed gcx-managed skills would be
// updated by the current bundled skill set.
type SkillsCheckResult struct {
	NeedsUpdate bool
	Result      skillops.InstallResult
}

// CheckSkillsUpdate previews `gcx skills update` against the given install root.
// It never writes files.
func CheckSkillsUpdate(source fs.FS, root string) (SkillsCheckResult, error) {
	result, err := skillops.Update(source, root, nil, true)
	if err != nil {
		return SkillsCheckResult{}, err
	}

	return SkillsCheckResult{
		NeedsUpdate: result.Written > 0 || result.Overwritten > 0,
		Result:      result,
	}, nil
}

// SkillsUpdateMessage returns a human-facing notification message when the
// installed skills differ from the bundled skills in the current gcx binary.
// Returns the empty string when no update is needed.
func SkillsUpdateMessage(source fs.FS, root string) (string, error) {
	check, err := CheckSkillsUpdate(source, root)
	if err != nil {
		return "", err
	}
	if !check.NeedsUpdate {
		return "", nil
	}

	return "Installed gcx skills can be updated to match this gcx version.\nRun: " + skillsUpdateCommand, nil
}
