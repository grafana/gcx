package local

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GroupResourcesByKindTitle plans title-based paths against both the pull and
// existing destination files. Only files with the same identity may be replaced.
func GroupResourcesByKindTitle(root, extension string, plurals map[string]string, all []*resources.Resource, warn io.Writer) (FileNamer, error) {
	existing, err := readDestinationFiles(root)
	if err != nil {
		return nil, fmt.Errorf("could not plan title filenames: %w", err)
	}
	byIdentity := map[fileIdentity][]string{}
	for _, files := range existing {
		for _, file := range files {
			byIdentity[file.identity] = append(byIdentity[file.identity], file.path)
		}
	}
	for _, paths := range byIdentity {
		slices.Sort(paths)
	}
	collides := func(path string, resource *resources.Resource) bool {
		for _, file := range existing[strings.ToLower(path)] {
			if file.identity != identityOf(&resource.Object) {
				return true
			}
		}
		return false
	}

	defaultNamer := GroupResourcesByKind(extension, plurals)
	type entry struct {
		resource  *resources.Resource
		path      string
		fromTitle bool
	}
	entries := make([]entry, 0, len(all))
	counts := make(map[string]int, len(all))
	for _, resource := range all {
		path, err := defaultNamer(resource)
		if err != nil {
			// Leave invalid resources to the default namer at write time,
			// preserving FSWriter's stop/continue and error callbacks.
			continue
		}
		title, _, _ := unstructured.NestedString(resource.Object.Object, "spec", "title")
		slug := titleSlug(title)
		if slug != "" {
			path = filepath.Join(kindDir(resource, plurals), slug+"."+extension)
		}
		entries = append(entries, entry{resource: resource, path: path, fromTitle: slug != ""})
		counts[strings.ToLower(path)]++
	}

	// Add the resource name to every initial collision, including collisions
	// with fallback filenames and existing files owned by another identity.
	// Only title-derived stems rewrite Windows device names.
	reserved := make(map[string]int, len(entries))
	for i := range entries {
		e := &entries[i]
		stem := strings.TrimSuffix(filepath.Base(e.path), "."+extension)
		if counts[strings.ToLower(e.path)] > 1 || collides(e.path, e.resource) || (e.fromTitle && windowsDeviceName(stem)) {
			stem = strings.TrimRight(stem[:min(len(stem), 100)], "-")
			// Bound and sanitize the suffix as well as the title: long names
			// and provider IDs must not introduce unsafe filename components.
			suffix := adapter.SlugifyName(e.resource.Name())
			suffix = suffix[:min(len(suffix), 100)]
			e.path = filepath.Join(filepath.Dir(e.path), stem+"-"+suffix+"."+extension)
		}
		reserved[strings.ToLower(e.path)]++
	}

	// A suffixed path can itself equal another title (or differ only in case).
	// Reserve every planned path before allocating numeric suffixes, so a
	// generated path cannot take the filename of a later entry.
	slices.SortFunc(entries, func(a, b entry) int {
		return cmp.Compare(a.resource.Ref(), b.resource.Ref())
	})
	paths := make(map[fileIdentity]string, len(entries))
	for _, e := range entries {
		path := e.path
		if reserved[strings.ToLower(path)] > 1 || collides(path, e.resource) {
			stem := strings.TrimSuffix(path, "."+extension)
			for n := 1; ; n++ {
				path = fmt.Sprintf("%s-%d.%s", stem, n, extension)
				if reserved[strings.ToLower(path)] == 0 && !collides(path, e.resource) {
					reserved[strings.ToLower(path)] = 1
					break
				}
			}
		}
		// Reuse the existing spelling on case-sensitive filesystems too.
		if files := existing[strings.ToLower(path)]; len(files) > 0 {
			actual := files[0].path
			for _, file := range files {
				if file.path == path {
					actual = path
					break
				}
			}
			path = actual
		}
		identity := identityOf(&e.resource.Object)
		paths[identity] = path
		if warn != nil {
			for _, previous := range byIdentity[identity] {
				if previous != path {
					output.EmitWarn(warn, fmt.Sprintf("stale file %q has the same resource identity as pull target %q; remove stale files before pushing", filepath.Join(root, previous), filepath.Join(root, path)))
				}
			}
		}
	}

	return func(resource *resources.Resource) (string, error) {
		if path, ok := paths[identityOf(&resource.Object)]; ok {
			return path, nil
		}
		return defaultNamer(resource)
	}, nil
}

func titleSlug(title string) string {
	slug := slugOrEmpty(title)
	return strings.TrimRight(slug[:min(len(slug), 100)], "-")
}

// slugOrEmpty preserves the empty result so titles can fall back to metadata.name.
func slugOrEmpty(value string) string {
	var slug strings.Builder
	separator := false
	for _, r := range value {
		r = unicode.ToLower(r)
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if separator && slug.Len() > 0 {
				slug.WriteByte('-')
			}
			slug.WriteRune(r)
			separator = false
		} else {
			separator = true
		}
	}
	return slug.String()
}

// fileIdentity compares the fields in the manifest, independently of filenames.
type fileIdentity struct {
	apiVersion string
	kind       string
	namespace  string
	name       string
}

func identityOf(obj *unstructured.Unstructured) fileIdentity {
	return fileIdentity{obj.GetAPIVersion(), obj.GetKind(), obj.GetNamespace(), obj.GetName()}
}

type destinationFile struct {
	path     string
	identity fileIdentity
}

func readDestinationFiles(root string) (map[string][]destinationFile, error) {
	files := map[string][]destinationFile{}
	codecs := format.Codecs()
	resolved, err := filepath.EvalSymlinks(root)
	if os.IsNotExist(err) {
		return files, nil
	}
	if err != nil {
		return nil, err
	}
	root = resolved
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			info, statErr := os.Stat(path)
			if statErr == nil && info.IsDir() {
				return fmt.Errorf("cannot inventory symlinked destination directory %q", path)
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file := destinationFile{path: rel}
		// Reserve directories, symlinks, and undecodable files without an owner.
		if entry.Type().IsRegular() {
			file.identity, err = readFileIdentity(directory, rel, codecs)
			if err != nil {
				return err
			}
		}
		key := strings.ToLower(rel)
		files[key] = append(files[key], file)
		return nil
	})
	return files, err
}

func readFileIdentity(directory *os.Root, path string, codecs map[format.Format]format.Codec) (fileIdentity, error) {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if ext == "yml" {
		ext = "yaml"
	}
	codec := codecs[format.Format(ext)]
	if codec == nil {
		return fileIdentity{}, nil
	}
	data, err := directory.ReadFile(path)
	if err != nil {
		return fileIdentity{}, err
	}
	var obj unstructured.Unstructured
	if codec.Decode(bytes.NewReader(data), &obj) == nil {
		return identityOf(&obj), nil
	}
	// Undecodable files remain reserved without an owner.
	return fileIdentity{}, nil
}

func windowsDeviceName(stem string) bool {
	// A device name before a dot is reserved too (e.g. CON.backup.json).
	base, _, _ := strings.Cut(strings.ToLower(stem), ".")
	switch base {
	case "con", "prn", "aux", "nul":
		return true
	default:
		return len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9'
	}
}
