package dashboards

import (
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// folderAnnotation holds the UID of the folder a dashboard or folder
	// belongs to. An empty or missing value means the root ("General") level.
	folderAnnotation = "grafana.app/folder"

	// updatedTimestampAnnotation is set by Grafana when a resource is updated.
	updatedTimestampAnnotation = "grafana.app/updatedTimestamp"
)

// dashboardTree is the document `gcx dashboards tree` produces. Without
// folder arguments it is the whole instance: top-level folders plus the
// dashboards that are not in any folder. With folder arguments, Folders holds
// the selected folders, each with its subtree, and Dashboards is empty.
type dashboardTree struct {
	Folders    []*treeFolder   `json:"folders"`
	Dashboards []treeDashboard `json:"dashboards"`

	// label is the text root line for a whole-instance tree.
	label string
	// selected reports whether Folders are explicit selections, which the
	// text codec renders as separate trees.
	selected bool
}

// treeFolder is one folder in the hierarchy.
type treeFolder struct {
	Name    string    `json:"name"`
	Title   string    `json:"title"`
	Created time.Time `json:"created,omitzero"`
	Updated time.Time `json:"updated,omitzero"`
	// Unreadable marks a folder that dashboards reference but that is missing
	// from the folder list, usually because the caller cannot read it. Its
	// title is the folder UID.
	Unreadable bool `json:"unreadable,omitempty"`
	// Truncated marks a folder whose contents were left out by --level.
	Truncated  bool            `json:"truncated,omitempty"`
	Folders    []*treeFolder   `json:"folders"`
	Dashboards []treeDashboard `json:"dashboards"`

	// path is the slash-separated title path from the root.
	path string
}

// treeDashboard is a dashboard leaf in the hierarchy.
type treeDashboard struct {
	Name    string    `json:"name"`
	Title   string    `json:"title"`
	Created time.Time `json:"created,omitzero"`
	Updated time.Time `json:"updated,omitzero"`
}

// folderIndex maps folder UIDs to nodes in a built tree.
type folderIndex map[string]*treeFolder

// buildDashboardTree arranges dashboards under the folder hierarchy described
// by folders and returns the root node plus an index of every folder by UID.
// Every folder is included, even one without dashboards, so the result
// mirrors the folder structure in Grafana. Children keep the order of the
// input lists until sortTree is applied.
//
// Folders whose parent is not in the list, or whose ancestry forms a cycle,
// are attached to the root. A dashboard that references a folder missing from
// the list (for example one the caller cannot read) is grouped under an
// unreadable placeholder folder titled with the folder UID, so no dashboard
// is dropped.
func buildDashboardTree(dashboards, folders []unstructured.Unstructured) (*treeFolder, folderIndex) {
	root := newTreeFolder("", "")
	index := make(folderIndex, len(folders))
	parents := make(map[string]string, len(folders))

	for _, f := range folders {
		name := f.GetName()
		title := nestedString(f.Object, "spec", "title")
		if title == "" {
			title = name
		}
		node := newTreeFolder(name, title)
		node.Created, node.Updated = objectTimes(f)
		index[name] = node
		parents[name] = f.GetAnnotations()[folderAnnotation]
	}

	for _, f := range folders {
		name := f.GetName()
		parent := root
		if p, ok := index[parents[name]]; ok && !inFolderCycle(name, parents) {
			parent = p
		}
		parent.Folders = append(parent.Folders, index[name])
	}

	for _, d := range dashboards {
		title := nestedString(d.Object, "spec", "title")
		if title == "" {
			title = d.GetName()
		}
		dash := treeDashboard{Name: d.GetName(), Title: title}
		dash.Created, dash.Updated = objectTimes(d)

		parent := root
		if uid := d.GetAnnotations()[folderAnnotation]; uid != "" {
			node, ok := index[uid]
			if !ok {
				node = newTreeFolder(uid, uid)
				node.Unreadable = true
				index[uid] = node
				root.Folders = append(root.Folders, node)
			}
			parent = node
		}
		parent.Dashboards = append(parent.Dashboards, dash)
	}

	root.setPaths("")
	return root, index
}

// newTreeFolder returns a folder with non-nil child slices, so JSON and YAML
// output show empty folders as [] rather than null.
func newTreeFolder(name, title string) *treeFolder {
	return &treeFolder{Name: name, Title: title, Folders: []*treeFolder{}, Dashboards: []treeDashboard{}}
}

func (n *treeFolder) setPaths(prefix string) {
	for _, f := range n.Folders {
		f.path = joinTreePath(prefix, f.Title)
		f.setPaths(f.path)
	}
}

func joinTreePath(prefix, title string) string {
	if prefix == "" {
		return title
	}
	return prefix + "/" + title
}

// objectTimes returns when an object was created and last updated. The update
// time comes from Grafana's updatedTimestamp annotation, then the newest
// managed-fields entry, then the creation time.
// Both are in UTC; zero when the object carries no timestamps.
func objectTimes(obj unstructured.Unstructured) (time.Time, time.Time) {
	created := utcOrZero(obj.GetCreationTimestamp().Time)
	if ts, err := time.Parse(time.RFC3339, obj.GetAnnotations()[updatedTimestampAnnotation]); err == nil {
		return created, ts.UTC()
	}
	updated := created
	for _, mf := range obj.GetManagedFields() {
		if mf.Time != nil && mf.Time.After(updated) {
			updated = mf.Time.UTC()
		}
	}
	return created, updated
}

func utcOrZero(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC()
}

// inFolderCycle reports whether following parent links from name leads back
// to a folder already visited. Grafana does not allow such cycles; the check
// keeps a malformed response from hiding folders.
func inFolderCycle(name string, parents map[string]string) bool {
	seen := map[string]bool{}
	for cur := name; cur != ""; cur = parents[cur] {
		if seen[cur] {
			return true
		}
		seen[cur] = true
	}
	return false
}

// findFolder resolves a folder argument, trying in order:
//
//  1. a folder UID;
//  2. a path of folder titles from the root separated by slashes, such as
//     "Infrastructure/Ceph" (titles may themselves contain slashes), matched
//     exactly and then ignoring case;
//  3. the title of a single folder anywhere in the tree, matched exactly and
//     then ignoring case.
//
// When a title matches several folders, findFolder returns no folder and the
// paths of the candidates.
func findFolder(root *treeFolder, index folderIndex, arg string) (*treeFolder, []string) {
	if node, ok := index[arg]; ok {
		return node, nil
	}
	path := strings.Trim(arg, "/")
	if path == "" {
		return nil, nil
	}
	for _, fold := range []bool{false, true} {
		if node := findFolderPath(root, path, fold); node != nil {
			return node, nil
		}
	}
	for _, fold := range []bool{false, true} {
		var matches []*treeFolder
		for _, f := range index {
			if f.Title == path || (fold && strings.EqualFold(f.Title, path)) {
				matches = append(matches, f)
			}
		}
		switch len(matches) {
		case 0:
			continue
		case 1:
			return matches[0], nil
		default:
			paths := make([]string, 0, len(matches))
			for _, m := range matches {
				paths = append(paths, m.path)
			}
			slices.Sort(paths)
			return nil, paths
		}
	}
	return nil, nil
}

func findFolderPath(node *treeFolder, path string, fold bool) *treeFolder {
	equal := func(a, b string) bool { return a == b }
	if fold {
		equal = strings.EqualFold
	}
	for _, child := range node.Folders {
		if equal(path, child.Title) {
			return child
		}
		if len(path) > len(child.Title) && path[len(child.Title)] == '/' && equal(path[:len(child.Title)], child.Title) {
			if found := findFolderPath(child, path[len(child.Title)+1:], fold); found != nil {
				return found
			}
		}
	}
	return nil
}

// folderSuggestions returns up to three folder paths that contain arg,
// ignoring case, to help after a failed lookup.
func folderSuggestions(index folderIndex, arg string) []string {
	needle := strings.ToLower(strings.Trim(arg, "/"))
	var out []string
	for _, f := range index {
		if needle != "" && strings.Contains(strings.ToLower(f.path), needle) {
			out = append(out, f.path)
		}
	}
	slices.Sort(out)
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// ---------------------------------------------------------------------------
// selection: filtering and sorting
// ---------------------------------------------------------------------------

// treeSelection holds the options that decide which entries the tree
// contains. They apply to every output format.
type treeSelection struct {
	// level is the maximum depth to descend; 0 means unlimited.
	level       int
	foldersOnly bool
	include     *globMatcher
	exclude     *globMatcher
	// matchFolders applies include and exclude to folder titles too. A folder
	// matching include is shown with all its contents.
	matchFolders bool
	// prune drops folders left without dashboards or subfolders.
	prune bool
}

// filter returns a filtered copy of node's contents. depth is the depth of
// node's children, starting at 1.
func (s treeSelection) filter(node *treeFolder, depth int, includeAll bool) *treeFolder {
	out := *node
	out.Folders = []*treeFolder{}
	out.Dashboards = []treeDashboard{}

	if s.level > 0 && depth > s.level {
		out.Truncated = len(node.Folders) > 0 || (!s.foldersOnly && len(node.Dashboards) > 0)
		return &out
	}

	if !s.foldersOnly {
		for _, d := range node.Dashboards {
			if s.exclude.matches(d.Title) {
				continue
			}
			if !includeAll && s.include != nil && !s.include.matches(d.Title) {
				continue
			}
			out.Dashboards = append(out.Dashboards, d)
		}
	}

	for _, child := range node.Folders {
		if s.matchFolders && s.exclude.matches(child.Title) {
			continue
		}
		childAll := includeAll || (s.matchFolders && s.include.matches(child.Title))
		filtered := s.filter(child, depth+1, childAll)
		if s.prune && !filtered.Truncated && len(filtered.Folders) == 0 && len(filtered.Dashboards) == 0 {
			continue
		}
		out.Folders = append(out.Folders, filtered)
	}
	return &out
}

// treeSortKey selects the order of folders and dashboards.
type treeSortKey string

const (
	sortByName    treeSortKey = "name"
	sortByVersion treeSortKey = "version"
	sortByMtime   treeSortKey = "mtime"
	sortByCtime   treeSortKey = "ctime"
	sortByNone    treeSortKey = "none"
)

// sortTree orders folders and dashboards at every level of the tree. Ties
// fall back to the title and then the name, so the order is stable.
func sortTree(n *treeFolder, key treeSortKey, reverse bool) {
	if key != sortByNone {
		slices.SortStableFunc(n.Folders, func(a, b *treeFolder) int {
			return compareTreeEntries(key, a.Title, a.Name, a.Created, a.Updated, b.Title, b.Name, b.Created, b.Updated)
		})
		slices.SortStableFunc(n.Dashboards, func(a, b treeDashboard) int {
			return compareTreeEntries(key, a.Title, a.Name, a.Created, a.Updated, b.Title, b.Name, b.Created, b.Updated)
		})
	}
	if reverse {
		slices.Reverse(n.Folders)
		slices.Reverse(n.Dashboards)
	}
	for _, f := range n.Folders {
		sortTree(f, key, reverse)
	}
}

func compareTreeEntries(key treeSortKey, titleA, nameA string, createdA, updatedA time.Time, titleB, nameB string, createdB, updatedB time.Time) int {
	switch key {
	case sortByMtime:
		if c := updatedA.Compare(updatedB); c != 0 {
			return c
		}
	case sortByCtime:
		if c := createdA.Compare(createdB); c != 0 {
			return c
		}
	case sortByVersion:
		if c := naturalCompare(strings.ToLower(titleA), strings.ToLower(titleB)); c != 0 {
			return c
		}
	}
	if c := strings.Compare(strings.ToLower(titleA), strings.ToLower(titleB)); c != 0 {
		return c
	}
	if c := strings.Compare(titleA, titleB); c != 0 {
		return c
	}
	return strings.Compare(nameA, nameB)
}
