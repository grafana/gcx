package skills

import (
	"errors"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontMatter struct {
	Description string `yaml:"description"`
}

// ShortDescription reads the normalized one-line description from a bundled
// skill's SKILL.md front matter. It returns "" when the skill is missing or has
// no usable description.
func ShortDescription(source fs.FS, name string) string {
	data, err := fs.ReadFile(source, path.Join(name, "SKILL.md"))
	if err != nil {
		return ""
	}
	return ShortDescriptionFromBytes(data)
}

// ShortDescriptionFromBytes extracts the normalized one-line description from
// SKILL.md content, falling back to the first non-heading body line when the
// front matter has no description.
func ShortDescriptionFromBytes(data []byte) string {
	description, err := descriptionFromFrontMatter(data)
	if err != nil {
		return normalizeDescription(fallbackDescription(data))
	}
	return normalizeDescription(description)
}

func descriptionFromFrontMatter(data []byte) (string, error) {
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", errors.New("missing front matter")
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", errors.New("unterminated front matter")
	}

	var meta skillFrontMatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &meta); err != nil {
		return "", err
	}
	return meta.Description, nil
}

func normalizeDescription(description string) string {
	return strings.Join(strings.Fields(description), " ")
}

func fallbackDescription(data []byte) string {
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "---") {
			continue
		}
		return trimmed
	}
	return ""
}
