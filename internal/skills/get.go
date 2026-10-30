package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// GetResult contains bundled skill content; Get never reads local installations.
type GetResult struct {
	CatalogEntry

	Name        string   `json:"name"`
	Description string   `json:"description"`
	Path        string   `json:"path"`
	Body        string   `json:"body"`
	References  []string `json:"references"`
}

// Get reads a bundled skill or reference, explaining retirement when content is
// no longer shipped. Local files are never used as a fallback.
func Get(source fs.FS, catalogData []byte, name, reference string) (GetResult, error) {
	if err := ValidateSkillName(name); err != nil {
		return GetResult{}, err
	}
	catalog, err := LoadCatalog(catalogData)
	if err != nil {
		return GetResult{}, err
	}
	entry, ok := catalog.Skills[name]
	if !ok {
		return GetResult{}, fmt.Errorf("unknown skill %q (use 'gcx agent skills list' to see available skills)", name)
	}
	if entry.Status == Retired {
		return GetResult{}, fmt.Errorf("%s; no bundled content", LifecycleNotice{Name: name, CatalogEntry: entry})
	}

	relPath := "SKILL.md"
	if strings.TrimSpace(reference) != "" {
		relPath, err = resolveReferencePath(reference)
		if err != nil {
			return GetResult{}, err
		}
	}
	body, err := fs.ReadFile(source, path.Join(name, relPath))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return GetResult{}, fmt.Errorf("file %q not found in skill %q", relPath, name)
		}
		return GetResult{}, err
	}
	references, err := listSkillReferences(source, name)
	if err != nil {
		return GetResult{}, err
	}
	return GetResult{
		CatalogEntry: entry,
		Name:         name, Description: ShortDescription(source, name), Path: relPath,
		Body: string(body), References: references,
	}, nil
}

func resolveReferencePath(reference string) (string, error) {
	if path.IsAbs(reference) {
		return "", fmt.Errorf("invalid reference path %q: must be relative to the skill directory", reference)
	}
	cleaned := path.Clean(reference)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid reference path %q: must not escape the skill directory", reference)
	}
	return cleaned, nil
}

func listSkillReferences(source fs.FS, name string) ([]string, error) {
	refs := []string{}
	err := fs.WalkDir(source, path.Join(name, "references"), func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return fs.SkipDir
			}
			return walkErr
		}
		if !d.IsDir() {
			refs = append(refs, strings.TrimPrefix(p, name+"/"))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(refs)
	return refs, nil
}
