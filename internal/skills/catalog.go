package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Status describes a skill's release lifecycle, not its installation or ownership.
type Status string

const (
	Active     Status = "active"
	Deprecated Status = "deprecated"
	Retired    Status = "retired"
)

// CatalogEntry survives removal of the corresponding bundled skill directory.
type CatalogEntry struct {
	Status      Status `yaml:"status" json:"status"`
	Replacement string `yaml:"replacement,omitempty" json:"replacement,omitempty"`
	Message     string `yaml:"message,omitempty" json:"message,omitempty"`
}

// Catalog records all current and retired skills shipped by gcx.
type Catalog struct {
	Skills map[string]CatalogEntry `yaml:"skills"`
}

// LoadCatalog decodes embedded release metadata and validates names and statuses
// used for local targeting. Bundle consistency is checked by TestBundledCatalog,
// not here: packaging errors must not disable uninstalling cataloged skills.
func LoadCatalog(data []byte) (Catalog, error) {
	var catalog Catalog
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode skills catalog: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Catalog{}, errors.New("skills catalog must contain exactly one YAML document")
	}
	if catalog.Skills == nil {
		return Catalog{}, errors.New("skills catalog must contain a skills map")
	}
	for name, entry := range catalog.Skills {
		if err := ValidateSkillName(name); err != nil {
			return Catalog{}, err
		}
		switch entry.Status {
		case Active, Deprecated, Retired:
		default:
			return Catalog{}, fmt.Errorf("invalid status %q for skill %q: use active, deprecated, or retired", entry.Status, name)
		}
	}
	return catalog, nil
}

// SkillState reconciles release metadata with the selected local installation.
// Known records catalog membership, not ownership. Present also covers incomplete
// installations without SKILL.md. A failed stat reports absence, as in the legacy
// installation check; it does not abort inventory or unrelated operations.
type SkillState struct {
	CatalogEntry

	Name             string `json:"name"`
	Known            bool   `json:"known"`
	ShortDescription string `json:"short_description"`
	Installed        bool   `json:"installed"`
	Present          bool   `json:"present"`
}

// Reconcile is read-only. It includes missing catalog entries and uncataloged
// local directories, allowing every command to make the same targeting decision.
func Reconcile(source fs.FS, data []byte, root string) ([]SkillState, error) {
	catalog, err := LoadCatalog(data)
	if err != nil {
		return nil, err
	}
	skillsDir := filepath.Join(root, "skills")
	local, err := os.ReadDir(skillsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	names := make(map[string]struct{}, len(catalog.Skills)+len(local))
	for name := range catalog.Skills {
		names[name] = struct{}{}
	}
	for _, entry := range local {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			names[entry.Name()] = struct{}{}
		}
	}
	states := make([]SkillState, 0, len(names))
	for name := range names {
		entry, known := catalog.Skills[name]
		state := SkillState{Name: name, Known: known, CatalogEntry: entry}
		if entry.Status == Active || entry.Status == Deprecated {
			state.ShortDescription = ShortDescription(source, name)
		}
		localPath := filepath.Join(skillsDir, name)
		info, err := os.Lstat(localPath)
		state.Present = err == nil
		if state.Present && (info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			info, err := os.Stat(filepath.Join(localPath, "SKILL.md"))
			state.Installed = err == nil && info.Mode().IsRegular()
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Name < states[j].Name })
	return states, nil
}

// ListResult contains bundled skills and locally present retired skills.
type ListResult struct {
	Skills     []SkillState `json:"skills"`
	SkillCount int          `json:"skill_count"`
}

// List omits unmanaged skills and absent retirements from the user-facing list.
func List(source fs.FS, catalog []byte, root string) (ListResult, error) {
	states, err := Reconcile(source, catalog, root)
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Skills: make([]SkillState, 0, len(states))}
	for _, state := range states {
		if !state.Known || (state.Status == Retired && !state.Present) {
			continue
		}
		result.Skills = append(result.Skills, state)
	}
	result.SkillCount = len(result.Skills)
	return result, nil
}

// LifecycleNotice is included in operation results, not only stderr, so agents
// can discover retirement and replacements from the result alone.
type LifecycleNotice struct {
	CatalogEntry

	Name string `json:"name"`
}

func (n LifecycleNotice) String() string {
	message := fmt.Sprintf("skill %q is %s", n.Name, n.Status)
	if n.Replacement != "" {
		message += "; replacement: " + n.Replacement
	}
	if n.Message != "" {
		message += "; " + n.Message
	}
	return message
}

// ValidateSkillName rejects paths before they reach installation or removal.
func ValidateSkillName(name string) error {
	if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\\x00") || filepath.Base(name) != name {
		return fmt.Errorf("invalid skill name %q: must be a plain name without path separators (e.g. gcx, manage-dashboards)", name)
	}
	return nil
}
