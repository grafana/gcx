package extensions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// IndexVersion is the schema version of index.json.
const IndexVersion = 1

// ErrNotInstalled is returned when a name does not match an installed extension.
var ErrNotInstalled = errors.New("extension is not installed")

// Installed describes one installed extension as recorded in index.json.
type Installed struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	// Source is the argument the user passed to `gcx ext install`, kept so
	// `update` can re-resolve it without the user retyping it.
	Source string `json:"source"`
	// Entrypoint is the absolute path to the installed executable.
	Entrypoint string `json:"entrypoint"`
	// Interpreted marks a script extension, which is run through the OS rather
	// than assumed to be a native binary.
	Interpreted bool      `json:"interpreted,omitempty"`
	ReportUsage bool      `json:"reportUsage"`
	InstalledAt time.Time `json:"installedAt"`
}

// Index is the single file gcx owns describing every installed extension.
type Index struct {
	Version    int         `json:"version"`
	Extensions []Installed `json:"extensions"`
}

// Store is the on-disk extension root: ~/.config/gcx/extensions.
type Store struct {
	Root string
}

// DefaultStore resolves the extension root under the user's gcx config
// directory, matching the folder convention config already uses.
func DefaultStore() (*Store, error) {
	if root := os.Getenv("GCX_EXTENSIONS_DIR"); root != "" {
		return &Store{Root: root}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	return &Store{Root: filepath.Join(home, ".config", "gcx", "extensions")}, nil
}

func (s *Store) indexPath() string { return filepath.Join(s.Root, "index.json") }

// dir returns the install directory for a specific extension version.
func (s *Store) dir(name, version string) string {
	return filepath.Join(s.Root, name, version)
}

// LoadIndex reads index.json, returning an empty index when it does not exist.
func (s *Store) LoadIndex() (*Index, error) {
	data, err := os.ReadFile(s.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		return &Index{Version: IndexVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", s.indexPath(), err)
	}
	if idx.Version != IndexVersion {
		return nil, fmt.Errorf("%s has unsupported version %d (want %d)", s.indexPath(), idx.Version, IndexVersion)
	}
	return &idx, nil
}

// SaveIndex writes index.json atomically.
func (s *Store) SaveIndex(idx *Index) error {
	idx.Version = IndexVersion
	sort.Slice(idx.Extensions, func(i, j int) bool { return idx.Extensions[i].Name < idx.Extensions[j].Name })
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.indexPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.indexPath())
}

// List returns every installed extension, name-sorted.
func (s *Store) List() ([]Installed, error) {
	idx, err := s.LoadIndex()
	if err != nil {
		return nil, err
	}
	return idx.Extensions, nil
}

// Lookup returns the installed extension with the given name.
func (s *Store) Lookup(name string) (*Installed, error) {
	idx, err := s.LoadIndex()
	if err != nil {
		return nil, err
	}
	for i := range idx.Extensions {
		if idx.Extensions[i].Name == name {
			return &idx.Extensions[i], nil
		}
	}
	return nil, fmt.Errorf("%q: %w", name, ErrNotInstalled)
}

// record inserts or replaces an entry in the index.
func (s *Store) record(e Installed) error {
	idx, err := s.LoadIndex()
	if err != nil {
		return err
	}
	replaced := false
	for i := range idx.Extensions {
		if idx.Extensions[i].Name == e.Name {
			idx.Extensions[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		idx.Extensions = append(idx.Extensions, e)
	}
	return s.SaveIndex(idx)
}

// Uninstall removes an extension's files and its index entry.
func (s *Store) Uninstall(name string) error {
	idx, err := s.LoadIndex()
	if err != nil {
		return err
	}
	kept := idx.Extensions[:0]
	found := false
	for _, e := range idx.Extensions {
		if e.Name == name {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		return fmt.Errorf("%q: %w", name, ErrNotInstalled)
	}
	idx.Extensions = kept
	if err := os.RemoveAll(filepath.Join(s.Root, name)); err != nil {
		return err
	}
	return s.SaveIndex(idx)
}
