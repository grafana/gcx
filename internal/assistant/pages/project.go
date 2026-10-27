package pages

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
)

// MetaFile records which published page and version a pulled directory came
// from, so push can update that page and detect a newer publication.
const MetaFile = ".gcx-page.json"

// Limits the Assistant applies to a page's source.
const (
	maxFiles = 256
	maxBytes = 16 << 20
)

const (
	filePerm = 0o600
	dirPerm  = 0o750
)

// errNotPulled reports a directory without MetaFile.
var errNotPulled = errors.New("not a pulled page directory")

// Meta is the content of MetaFile.
type Meta struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Version      int               `json:"version"`
	RevisionID   string            `json:"revisionId"`
	Files        []string          `json:"files"`
	SpeakerNotes map[string]string `json:"speakerNotes,omitempty"`
}

// ReadMeta reads a pulled directory's MetaFile.
func ReadMeta(dir string) (*Meta, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	meta, err := readMeta(root)
	if errors.Is(err, errNotPulled) {
		return nil, fmt.Errorf("%s has no %s; pull the page first with gcx assistant pages pull PAGE --dir %s", dir, MetaFile, dir)
	}
	return meta, err
}

func readMeta(root *os.Root) (*Meta, error) {
	data, err := root.ReadFile(MetaFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errNotPulled
	}
	if err != nil {
		return nil, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("read %s: %w", MetaFile, err)
	}
	if meta.ID == "" {
		return nil, fmt.Errorf("%s has no page id", MetaFile)
	}
	return &meta, nil
}

// WriteProject writes a page's exported source into dir with its MetaFile.
// dir must be empty, missing, or a previous pull of the same page; files that
// previous pull wrote and the new export no longer has are removed. force
// allows any other directory.
func WriteProject(dir string, entry *Entry, export *Export, force bool) (*Meta, error) {
	files := make([]string, 0, len(export.Source.Files))
	for name := range export.Source.Files {
		if err := checkPath(name); err != nil {
			return nil, err
		}
		files = append(files, name)
	}
	sort.Strings(files)

	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	previous, err := checkTarget(root, dir, entry.ID, force)
	if err != nil {
		return nil, err
	}

	for _, name := range files {
		if parent := path.Dir(name); parent != "." {
			if err := root.MkdirAll(parent, dirPerm); err != nil {
				return nil, err
			}
		}
		if err := root.WriteFile(name, export.Source.Files[name], filePerm); err != nil {
			return nil, err
		}
	}
	if previous != nil {
		kept := make(map[string]bool, len(files))
		for _, name := range files {
			kept[name] = true
		}
		for _, name := range previous.Files {
			if kept[name] || checkPath(name) != nil {
				continue
			}
			if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
		}
	}
	meta := &Meta{ID: entry.ID, Title: entry.Title, Version: entry.Version, RevisionID: entry.RevisionID, SpeakerNotes: export.SpeakerNotes}
	if err := writeMeta(root, meta, export.Source); err != nil {
		return nil, err
	}
	return meta, nil
}

// UpdateMeta records in dir that source was published as meta's version.
func UpdateMeta(dir string, meta *Meta, source Source) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return writeMeta(root, meta, source)
}

func writeMeta(root *os.Root, meta *Meta, source Source) error {
	files := make([]string, 0, len(source.Files))
	for name := range source.Files {
		files = append(files, name)
	}
	sort.Strings(files)
	meta.Files = files
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return root.WriteFile(MetaFile, append(data, '\n'), filePerm)
}

// checkTarget returns the previous pull of page id in root, or nil when root
// is empty or force lets the pull write into it.
func checkTarget(root *os.Root, dir, id string, force bool) (*Meta, error) {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil //nolint:nilnil // An empty directory has no previous pull.
	}
	meta, metaErr := readMeta(root)
	switch {
	case metaErr == nil && meta.ID == id:
		return meta, nil
	case force:
		return nil, nil //nolint:nilnil // --force writes over whatever is there.
	case metaErr == nil:
		return nil, fmt.Errorf("%s holds page %s, not %s; choose another --dir or pass --force", dir, meta.ID, id)
	default:
		return nil, fmt.Errorf("%s is not empty and was not pulled from a page; choose another --dir or pass --force", dir)
	}
}

// ReadProject reads a page's source from dir: every regular file except
// dotfiles, MetaFile and installed or built output.
func ReadProject(dir string) (Source, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Source{}, err
	}
	defer root.Close()
	files := map[string][]byte{}
	total := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || (d.IsDir() && skippedDir(d.Name())) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		data, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		files[name] = data
		total += len(data)
		return nil
	})
	if err != nil {
		return Source{}, err
	}
	if len(files) == 0 {
		return Source{}, fmt.Errorf("%s has no source files", dir)
	}
	if len(files) > maxFiles {
		return Source{}, fmt.Errorf("%s has %d files; a page holds at most %d", dir, len(files), maxFiles)
	}
	if total > maxBytes {
		return Source{}, fmt.Errorf("%s holds %d bytes of source; a page holds at most %d", dir, total, maxBytes)
	}
	return Source{Version: 1, Files: files}, nil
}

// skippedDir reports directories that hold installed or built output, never
// source.
func skippedDir(name string) bool {
	switch name {
	case "node_modules", "dist", "build":
		return true
	}
	return false
}

// checkPath rejects export paths that would land outside the project.
func checkPath(name string) error {
	clean := path.Clean(name)
	if name == "" || path.IsAbs(name) || clean != name || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(name, `\`) {
		return fmt.Errorf("the export has an unsafe path %q", name)
	}
	if name == MetaFile {
		return fmt.Errorf("the export has a reserved path %q", name)
	}
	return nil
}
