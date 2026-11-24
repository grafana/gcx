package pages

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"unicode/utf8"
)

// LinkFile records which draft page a local project directory mirrors and
// what it last synced, so push writes only what changed.
const LinkFile = ".gcx-page.json"

const (
	filePerm = 0o600
	dirPerm  = 0o750

	// maxPushBytes keeps one file write under the sandbox's 128 KiB limit on
	// an operation, with room for its JSON encoding.
	maxPushBytes = 120 << 10
)

// savedRoots are the project paths the sandbox saves with a page; nothing
// else survives a restart. Keep in sync with grafana-assistant-app's
// sandbox/pages/export.mjs.
var savedRoots = []string{"page.json", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "tsconfig.json", "pages.bindings.json", "pages.design.json", "AGENTS.md", "src", "packages", "public", "assets", ".agents/skills"} //nolint:gochecknoglobals // Mirrors the server's fixed list.

// errNotLinked reports a directory without LinkFile.
var errNotLinked = errors.New("not a page project directory")

// Link is the content of LinkFile.
type Link struct {
	// Grafana is the stack the page lives on.
	Grafana   string `json:"grafana"`
	ChatID    string `json:"chatId"`
	PageID    string `json:"pageId"`
	Project   string `json:"project"`
	Title     string `json:"title"`
	ReleaseID string `json:"releaseId"`
	// Files maps each synced path to the SHA-256 of what was synced.
	Files map[string]string `json:"files"`
	// Publication is the published page this draft updates, once there is one.
	Publication *Publication `json:"publication,omitempty"`
}

// Publication names a published page and the version a draft was based on.
type Publication struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// Change is a local file that differs from what was last synced.
type Change struct {
	Path    string
	Content []byte
}

// Diff is how a project directory differs from what was last synced.
type Diff struct {
	// Changed are the saved files that are new or changed, in the order to
	// write them.
	Changed []Change
	// Deleted are the synced files that are gone.
	Deleted []string
	// Ignored are local files outside the paths a page saves.
	Ignored []string
}

// Empty reports whether nothing needs to be synced.
func (d Diff) Empty() bool { return len(d.Changed)+len(d.Deleted) == 0 }

// SavedPath reports whether a path relative to a project is saved with the
// page: under a saved root, with no dot-prefixed, node_modules or dist part.
func SavedPath(rel string) bool {
	if checkPath(rel) != nil {
		return false
	}
	for _, root := range savedRoots {
		rest, ok := strings.CutPrefix(rel, root)
		if !ok || (rest != "" && rest[0] != '/') {
			continue
		}
		for part := range strings.SplitSeq(strings.TrimPrefix(rest, "/"), "/") {
			if strings.HasPrefix(part, ".") || part == "node_modules" || part == "dist" {
				return false
			}
		}
		return true
	}
	return false
}

// ReadLink reads a project directory's LinkFile.
func ReadLink(dir string) (*Link, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	link, err := readLink(root)
	if errors.Is(err, errNotLinked) {
		return nil, fmt.Errorf("%s has no %s; start a page with gcx assistant pages create --dir %s, or pull one with gcx assistant pages pull PAGE --dir %s", dir, LinkFile, dir, dir)
	}
	return link, err
}

func readLink(root *os.Root) (*Link, error) {
	data, err := root.ReadFile(LinkFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errNotLinked
	}
	if err != nil {
		return nil, err
	}
	var link Link
	if err := json.Unmarshal(data, &link); err != nil {
		return nil, fmt.Errorf("read %s: %w", LinkFile, err)
	}
	if link.ChatID == "" || link.PageID == "" || link.Project == "" {
		return nil, fmt.Errorf("%s does not name a draft page", LinkFile)
	}
	// A hand-edited link may leave out what was synced; push then sends
	// every file.
	if link.Files == nil {
		link.Files = map[string]string{}
	}
	return &link, nil
}

// WriteLink saves link in dir.
func WriteLink(dir string, link *Link) error {
	data, err := json.MarshalIndent(link, "", "  ")
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.WriteFile(LinkFile, append(data, '\n'), filePerm)
}

// WriteProject writes a page's source into dir and returns the link with
// the files it wrote. dir must be missing, empty, or a project of the same
// draft page; files that project synced and the source no longer has are
// removed.
func WriteProject(dir string, link Link, source *Source) (*Link, error) {
	names := make([]string, 0, len(source.Files))
	for name := range source.Files {
		if err := checkPath(name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	slices.Sort(names)

	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	previous, err := checkTarget(root, dir, link.PageID)
	if errors.Is(err, errNotLinked) {
		previous = &Link{}
	} else if err != nil {
		return nil, err
	}

	link.Files = make(map[string]string, len(names))
	for _, name := range names {
		if parent := path.Dir(name); parent != "." {
			if err := root.MkdirAll(parent, dirPerm); err != nil {
				return nil, err
			}
		}
		if err := root.WriteFile(name, source.Files[name], filePerm); err != nil {
			return nil, err
		}
		link.Files[name] = digest(source.Files[name])
	}
	for name := range previous.Files {
		if _, kept := link.Files[name]; kept || checkPath(name) != nil {
			continue
		}
		if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	data, err := json.MarshalIndent(link, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := root.WriteFile(LinkFile, append(data, '\n'), filePerm); err != nil {
		return nil, err
	}
	return &link, nil
}

// Changes compares dir with what link last synced.
func Changes(dir string, link *Link) (Diff, error) {
	var diff Diff
	root, err := os.OpenRoot(dir)
	if err != nil {
		return diff, err
	}
	defer root.Close()
	present := map[string]bool{}
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == "dist" || d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if name == LinkFile {
			return nil
		}
		if !SavedPath(name) {
			diff.Ignored = append(diff.Ignored, name)
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file; pages save regular files only", name)
		}
		present[name] = true
		data, err := root.ReadFile(name)
		if err != nil {
			return err
		}
		if link.Files[name] == digest(data) {
			return nil
		}
		if len(data) > maxPushBytes || !utf8.Valid(data) || slices.Contains(data, 0) {
			return fmt.Errorf("%s cannot be pushed: gcx writes text files of at most %d KiB. Shrink it, or use an SVG or text format", name, maxPushBytes>>10)
		}
		diff.Changed = append(diff.Changed, Change{Path: name, Content: data})
		return nil
	})
	if err != nil {
		return Diff{}, err
	}
	for name := range link.Files {
		if !present[name] {
			diff.Deleted = append(diff.Deleted, name)
		}
	}
	slices.Sort(diff.Deleted)
	slices.SortStableFunc(diff.Changed, func(a, b Change) int { return writeRank(a.Path) - writeRank(b.Path) })
	return diff, nil
}

// writeRank orders writes so that every build sees the files the next one
// imports: data and assets first, then modules, then the entry points.
func writeRank(name string) int {
	switch name {
	case "src/App.tsx":
		return 3
	case "src/main.tsx":
		return 4
	}
	if strings.HasPrefix(name, "src/") || strings.HasPrefix(name, "packages/") {
		return 2
	}
	return 1
}

// checkTarget accepts a directory that mirrors the same draft page, whose
// link it returns, and a missing or empty one, for which it returns
// errNotLinked.
func checkTarget(root *os.Root, dir, pageID string) (*Link, error) {
	link, err := readLink(root)
	if err == nil {
		if link.PageID != pageID {
			return nil, fmt.Errorf("%s holds another page (%s); use a new directory", dir, link.Title)
		}
		return link, nil
	}
	if !errors.Is(err, errNotLinked) {
		return nil, err
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	if len(entries) > 0 {
		return nil, fmt.Errorf("%s is not empty; use a new or empty directory", dir)
	}
	return nil, errNotLinked
}

// checkPath rejects a source path that could leave the project directory.
func checkPath(name string) error {
	if name == "" || path.IsAbs(name) || strings.Contains(name, `\`) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || name == LinkFile {
		return fmt.Errorf("unsafe page source path %q", name)
	}
	return nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
