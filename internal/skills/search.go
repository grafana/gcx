package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ValidateSource keeps existing list defaults separate from opt-in discovery.
func ValidateSource(source string) error {
	if source != "bundled" && source != "custom" && source != "all" {
		return fmt.Errorf("invalid skill source %q: use bundled, custom, or all", source)
	}
	return nil
}

// ListSelected returns metadata only. Custom content is never silently adopted
// from unrelated harness directories; only explicit gcx imports are included.
func ListSelected(source fs.FS, catalog []byte, root, selection string) (ListResult, error) {
	if err := ValidateSource(selection); err != nil {
		return ListResult{}, err
	}
	result := ListResult{Skills: []SkillState{}}
	if selection != "custom" {
		bundled, err := List(source, catalog, root)
		if err != nil {
			return ListResult{}, err
		}
		for _, entry := range bundled.Skills {
			entry.ID, entry.Source = entry.Name, "bundled"
			result.Skills = append(result.Skills, entry)
		}
	}
	if selection != "bundled" {
		custom, err := listCustom(root)
		if err != nil {
			return ListResult{}, err
		}
		result.Skills = append(result.Skills, custom...)
	}
	slices.SortFunc(result.Skills, func(a, b SkillState) int { return strings.Compare(a.ID, b.ID) })
	result.SkillCount = len(result.Skills)
	return result, nil
}

func listCustom(root string) ([]SkillState, error) {
	result := []SkillState{}
	store, err := customStore(root, false)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer store.Close()
	entries, err := fs.ReadDir(store.FS(), ".")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if name, ok := strings.CutPrefix(entry.Name(), ".previous-"); ok {
			if err := checkCustomReady(store, name); err != nil {
				return nil, err
			}
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if err := requireManaged(store, entry.Name()); err != nil {
			return nil, err
		}
		snapshot, err := readCustom(filepath.Join(root, customDirectory, entry.Name()))
		if err != nil {
			return nil, err
		}
		if snapshot.name != entry.Name() {
			return nil, fmt.Errorf("custom skill directory %q disagrees with its declared name", entry.Name())
		}
		result = append(result, SkillState{
			Name: snapshot.name, ID: "custom/" + snapshot.name, Source: "custom", Tags: snapshot.tags,
			ShortDescription: snapshot.description, Installed: true, Present: true,
		})
	}
	return result, nil
}

// SearchResult discloses the full match count when the requested limit cuts the
// metadata list. No skill body is returned by discovery.
type SearchResult struct {
	Skills    []SkillState `json:"skills"`
	Returned  int          `json:"returned"`
	Total     int          `json:"total"`
	Limit     int          `json:"limit"`
	Truncated bool         `json:"truncated"`
}

// Search matches all whitespace-separated terms and all supplied tags locally,
// case-insensitively. It sorts by identity and performs no semantic/model calls.
func Search(source fs.FS, catalog []byte, root, selection, query string, tags []string, limit int) (SearchResult, error) {
	if strings.TrimSpace(query) == "" || limit < 0 {
		return SearchResult{}, errors.New("provide a nonempty search query and a nonnegative --limit (0 returns all matches)")
	}
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			return SearchResult{}, errors.New("--tag must not be empty")
		}
	}
	list, err := ListSelected(source, catalog, root, selection)
	if err != nil {
		return SearchResult{}, err
	}
	result := SearchResult{Skills: []SkillState{}, Limit: limit}
	for _, entry := range list.Skills {
		haystack := strings.ToLower(strings.Join([]string{entry.ID, entry.ShortDescription, strings.Join(entry.Tags, " ")}, " "))
		matches := true
		for term := range strings.FieldsSeq(strings.ToLower(query)) {
			matches = matches && strings.Contains(haystack, term)
		}
		for _, tag := range tags {
			matches = matches && slices.Contains(entry.Tags, strings.ToLower(strings.TrimSpace(tag)))
		}
		if !matches {
			continue
		}
		result.Total++
		if limit == 0 || len(result.Skills) < limit {
			result.Skills = append(result.Skills, entry)
		}
	}
	result.Returned = len(result.Skills)
	result.Truncated = result.Total > result.Returned
	return result, nil
}
