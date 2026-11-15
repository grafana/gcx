package observation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// Capture artifacts can contain sensitive configuration and query text, so
// they are readable by the owner only.
const (
	dirPerm  = 0o700
	filePerm = 0o600
)

// ErrDestinationNotEmpty is returned when the capture destination already
// holds files. A capture never merges with, or overwrites, earlier files.
var ErrDestinationNotEmpty = errors.New("capture destination exists and is not empty")

// Write finalizes the capture at dest. Files are written into a temporary
// sibling directory, the manifest (with its inventory) is written last, and
// the directory is then renamed into place. An interrupted write therefore
// never leaves a directory at dest that looks like a finished capture.
//
// dest must not exist or must be an empty directory. On success the
// manifest's inventory is filled in.
func Write(c *Capture, dest string) error {
	dest = filepath.Clean(dest)

	destExists := false
	switch info, err := os.Stat(dest); {
	case err == nil:
		if !info.IsDir() {
			return fmt.Errorf("capture destination %s is not a directory", dest)
		}
		entries, err := os.ReadDir(dest)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("%w: %s", ErrDestinationNotEmpty, dest)
		}
		destExists = true
	case errors.Is(err, fs.ErrNotExist):
	default:
		return err
	}

	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, dirPerm); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dest)+".partial-")
	if err != nil {
		return err
	}
	finished := false
	defer func() {
		if !finished {
			_ = os.RemoveAll(tmp)
		}
	}()

	inventory := []InventoryEntry{}
	typeKeys := make([]string, 0, len(c.Objects))
	for key := range c.Objects {
		typeKeys = append(typeKeys, key)
	}
	sort.Strings(typeKeys)
	for _, typeKey := range typeKeys {
		names := make([]string, 0, len(c.Objects[typeKey]))
		for name := range c.Objects[typeKey] {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) > 0 {
			if err := os.Mkdir(filepath.Join(tmp, typeKey), dirPerm); err != nil {
				return err
			}
		}
		for _, name := range names {
			rel, err := objectPath(typeKey, name)
			if err != nil {
				return err
			}
			data, err := encode(c.Objects[typeKey][name])
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(tmp, filepath.FromSlash(rel)), data, filePerm); err != nil {
				return err
			}
			inventory = append(inventory, InventoryEntry{Type: typeKey, Name: name, Path: rel, SHA256: digest(data)})
		}
	}

	manifest := c.Manifest
	manifest.Inventory = inventory
	data, err := encode(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, ManifestFile), data, filePerm); err != nil {
		return err
	}

	if destExists {
		if err := os.Remove(dest); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	finished = true
	c.Manifest.Inventory = inventory
	return nil
}

// InvalidCaptureError reports a capture that is unfinished, altered or of an
// unknown format.
type InvalidCaptureError struct {
	Path   string
	Reason string
}

func (e *InvalidCaptureError) Error() string {
	return fmt.Sprintf("invalid capture %s: %s", e.Path, e.Reason)
}

// Load reads a finalized capture and verifies it against its inventory: every
// listed file must exist with the recorded digest, and no other files may be
// present.
func Load(dir string) (*Capture, error) {
	invalid := func(format string, args ...any) error {
		return &InvalidCaptureError{Path: dir, Reason: fmt.Sprintf(format, args...)}
	}

	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, invalid("no %s; the capture is unfinished or the path is not a capture", ManifestFile)
	}
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return nil, invalid("cannot parse %s: %v", ManifestFile, err)
	}
	if manifest.Type != ManifestType {
		return nil, invalid("unexpected manifest type %q", manifest.Type)
	}
	if manifest.SchemaVersion != SchemaVersion {
		return nil, invalid("unsupported schema version %q (this gcx reads %q)", manifest.SchemaVersion, SchemaVersion)
	}

	listed := map[string]InventoryEntry{}
	for _, entry := range manifest.Inventory {
		expected, err := objectPath(entry.Type, entry.Name)
		if err != nil || expected != entry.Path {
			return nil, invalid("inventory entry %s/%s has unexpected path %q", entry.Type, entry.Name, entry.Path)
		}
		if _, dup := listed[entry.Path]; dup {
			return nil, invalid("duplicate inventory entry %s", entry.Path)
		}
		listed[entry.Path] = entry
	}

	var extra []string
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ManifestFile {
			return nil
		}
		if _, ok := listed[rel]; !ok {
			extra = append(extra, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return nil, invalid("files not in the inventory: %v", extra)
	}

	capture := &Capture{Manifest: manifest, Objects: map[string]map[string]map[string]any{}}
	for _, entry := range manifest.Inventory {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(entry.Path)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, invalid("inventory file %s is missing", entry.Path)
		}
		if err != nil {
			return nil, err
		}
		if digest(data) != entry.SHA256 {
			return nil, invalid("file %s does not match its recorded digest", entry.Path)
		}
		var obj map[string]any
		if err := json.Unmarshal(data, &obj); err != nil {
			return nil, invalid("cannot parse %s: %v", entry.Path, err)
		}
		if capture.Objects[entry.Type] == nil {
			capture.Objects[entry.Type] = map[string]map[string]any{}
		}
		capture.Objects[entry.Type][entry.Name] = obj
	}

	return capture, nil
}

func objectPath(typeKey, name string) (string, error) {
	escaped := url.PathEscape(name)
	if name == "" || escaped == "." || escaped == ".." || typeKey == "" || typeKey == "." || typeKey == ".." || url.PathEscape(typeKey) != typeKey {
		return "", fmt.Errorf("cannot store resource %s/%s", typeKey, name)
	}
	return path.Join(typeKey, escaped+".json"), nil
}

func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
