package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pmezard/go-difflib/difflib"
)

const customEditFile = ".gcx-edit.json"

type customEdit struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

// CustomFileChange describes a byte-level change. Large or binary files retain
// hashes even when a bounded textual diff cannot be included.
type CustomFileChange struct {
	Path        string `json:"path"`
	Action      string `json:"action"`
	BeforeHash  string `json:"before_hash,omitempty"`
	AfterHash   string `json:"after_hash,omitempty"`
	Diff        string `json:"diff,omitempty"`
	DiffOmitted bool   `json:"diff_omitted,omitempty"`
}

func snapshotRevision(snapshot customSnapshot) string {
	paths := make([]string, 0, len(snapshot.files))
	for p := range snapshot.files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	h := sha256.New()
	for _, p := range paths {
		// Length framing makes the hash unambiguous, including binary contents.
		fmt.Fprintf(h, "%d:%s%d:", len(p), p, len(snapshot.files[p]))
		_, _ = h.Write(snapshot.files[p]) // hash.Hash.Write never returns an error.
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

func customChanges(before, after map[string][]byte) []CustomFileChange {
	paths := make([]string, 0, len(before)+len(after))
	for p := range before {
		paths = append(paths, p)
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	changes := []CustomFileChange{}
	budget := 16 << 10
	for _, p := range paths {
		old, oldExists := before[p]
		next, nextExists := after[p]
		if oldExists && nextExists && bytes.Equal(old, next) {
			continue
		}
		change := CustomFileChange{Path: p, Action: "modified"}
		if oldExists {
			change.BeforeHash = fmt.Sprintf("sha256:%x", sha256.Sum256(old))
		} else {
			change.Action = "added"
		}
		if nextExists {
			change.AfterHash = fmt.Sprintf("sha256:%x", sha256.Sum256(next))
		} else {
			change.Action = "removed"
		}
		change.DiffOmitted = true
		// Bound quadratic diff work as well as the resulting output size.
		if len(old)+len(next) <= 32<<10 && bytes.Count(old, []byte{'\n'})+bytes.Count(next, []byte{'\n'}) <= 1000 &&
			utf8.Valid(old) && utf8.Valid(next) && !bytes.ContainsRune(old, 0) && !bytes.ContainsRune(next, 0) {
			diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
				A: customDiffLines(old), B: customDiffLines(next),
				FromFile: "before/" + p, ToFile: "after/" + p, Context: 3,
			})
			if err == nil && len(diff) <= budget {
				change.Diff, change.DiffOmitted = diff, false
				budget -= len(diff)
			}
		}
		changes = append(changes, change)
	}
	return changes
}

func previewCustomUpdate(store *os.Root, result *CustomMutation, snapshot customSnapshot, directory string) error {
	if err := requireManaged(store, snapshot.name); err != nil {
		return err
	}
	previous, err := readCustom(result.Path)
	if err != nil {
		return err
	}
	if previous.name != snapshot.name {
		return errors.New("stored skill name does not match its custom identity")
	}
	result.BeforeRevision = snapshotRevision(previous)
	result.Changes = customChanges(previous.files, snapshot.files)
	result.Changed = result.BeforeRevision != result.Revision
	if !result.Changed {
		return nil
	}
	return checkCustomEdit(directory, result.ID, result.BeforeRevision)
}

func customDiffLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	lines := strings.SplitAfter(string(data), "\n")
	last := len(lines) - 1
	if lines[last] == "" {
		return lines[:last]
	}
	lines[last] += "\n\\ No newline at end of file\n"
	return lines
}

func checkCustomEdit(directory, id, revision string) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(customEditFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil // An explicit replacement source has no exported base revision.
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return errors.New("invalid custom skill edit metadata; export a fresh workspace")
	}
	file, err := root.Open(customEditFile)
	if err != nil {
		return err
	}
	defer file.Close()
	var edit customEdit
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	if err := decoder.Decode(&edit); err != nil {
		return fmt.Errorf("invalid custom skill edit metadata: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF || edit.ID != id || edit.Revision != revision {
		return fmt.Errorf("custom skill changed since export, or the workspace targets another identity; export %s again and reapply your edits", id)
	}
	return nil
}

// ExportCustom copies the whole saved snapshot to a new edit directory. The
// revision manifest protects a later update against silently losing newer edits.
func ExportCustom(root, id, directory string, dryRun bool) (CustomMutation, error) {
	name, err := customName(id)
	if err != nil {
		return CustomMutation{}, err
	}
	store, err := customStore(root, false)
	if err != nil {
		return CustomMutation{}, err
	}
	defer store.Close()
	if err := checkCustomReady(store, name); err != nil {
		return CustomMutation{}, err
	}
	if err := requireManaged(store, name); err != nil {
		return CustomMutation{}, err
	}
	snapshot, err := readCustom(filepath.Join(root, customDirectory, name))
	if err != nil {
		return CustomMutation{}, err
	}
	if snapshot.name != name {
		return CustomMutation{}, errors.New("stored skill name does not match its custom identity")
	}
	if strings.TrimSpace(directory) == "" {
		return CustomMutation{}, errors.New("provide a new export directory")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return CustomMutation{}, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return CustomMutation{}, err
	}
	storePath, err := filepath.EvalSymlinks(store.Name())
	if err != nil {
		return CustomMutation{}, err
	}
	storePath, err = filepath.Abs(storePath)
	if err != nil {
		return CustomMutation{}, err
	}
	relative, err := filepath.Rel(storePath, filepath.Join(parent, filepath.Base(directory)))
	if err != nil {
		return CustomMutation{}, err
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return CustomMutation{}, errors.New("export destination must be outside the managed custom skills store")
	}
	if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
		return CustomMutation{}, errors.New("export destination already exists or cannot be inspected; choose a new directory")
	}
	result := CustomMutation{ID: id, Path: directory, Changed: true, DryRun: dryRun, Revision: snapshotRevision(snapshot)}
	if dryRun {
		return result, nil
	}
	// Mkdir refuses existing directories/symlinks, including a concurrent export.
	if err := os.Mkdir(directory, 0o700); err != nil {
		return CustomMutation{}, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(directory) // Clean up only this export's new directory.
		}
	}()
	destination, err := os.OpenRoot(directory)
	if err != nil {
		return CustomMutation{}, err
	}
	defer destination.Close()
	for p, data := range snapshot.files {
		if err := destination.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return CustomMutation{}, err
		}
		if err := destination.WriteFile(p, data, 0o600); err != nil {
			return CustomMutation{}, err
		}
	}
	manifest, err := json.Marshal(customEdit{ID: id, Revision: result.Revision})
	if err != nil {
		return CustomMutation{}, err
	}
	if err := destination.WriteFile(customEditFile, manifest, 0o600); err != nil {
		return CustomMutation{}, err
	}
	complete = true
	return result, nil
}
