package local

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GroupResourcesByKindTitle plans title-based paths for an entire pull before
// writing. Directories match GroupResourcesByKind. Every case-insensitive
// collision gets a name suffix; residual collisions get a numeric suffix.
// The result is independent of input order and does not change resource identity.
func GroupResourcesByKindTitle(extension string, plurals map[string]string, all []*resources.Resource) FileNamer {
	defaultNamer := GroupResourcesByKind(extension, plurals)
	type entry struct {
		resource *resources.Resource
		path     string
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
		if slug := titleSlug(title); slug != "" {
			path = filepath.Join(kindDir(resource, plurals), slug+"."+extension)
		}
		entries = append(entries, entry{resource: resource, path: path})
		counts[strings.ToLower(path)]++
	}

	// Add the resource name to every initial collision, including collisions
	// with fallback filenames. Windows device names are reserved even with an
	// extension, so give those a suffix too.
	reserved := make(map[string]int, len(entries))
	for i := range entries {
		e := &entries[i]
		stem := strings.TrimSuffix(filepath.Base(e.path), "."+extension)
		if counts[strings.ToLower(e.path)] > 1 || windowsDeviceName(stem) {
			if windowsDeviceName(stem) {
				stem = adapter.SlugifyName(stem)
			}
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
	paths := make(map[resources.ResourceRef]string, len(entries))
	for _, e := range entries {
		path := e.path
		if reserved[strings.ToLower(path)] > 1 {
			stem := strings.TrimSuffix(path, "."+extension)
			for n := 1; ; n++ {
				path = fmt.Sprintf("%s-%d.%s", stem, n, extension)
				if reserved[strings.ToLower(path)] == 0 {
					reserved[strings.ToLower(path)] = 1
					break
				}
			}
		}
		paths[e.resource.Ref()] = path
	}

	return func(resource *resources.Resource) (string, error) {
		if path, ok := paths[resource.Ref()]; ok {
			return path, nil
		}
		return defaultNamer(resource)
	}
}

func titleSlug(title string) string {
	slug := adapter.SlugifyName(title)
	// SlugifyName uses "resource" as its empty-input sentinel. Preserve a real
	// title such as "Resource", while falling back for punctuation or CJK only.
	if slug == "resource" && !strings.Contains(strings.ToLower(title), "resource") {
		return ""
	}
	return strings.TrimRight(slug[:min(len(slug), 100)], "-")
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
