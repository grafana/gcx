package dashboards

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

const defaultTreeTimeFormat = "2006-01-02 15:04"

// treeRender holds the options that only change how the text tree looks.
type treeRender struct {
	fullPath    bool
	flat        bool
	classify    bool
	forceColor  bool
	noColor     bool
	showDate    bool
	dateCreated bool
	timeFormat  string
	showUID     bool
	skipReport  bool
	filesFirst  bool
	fileLimit   int
	foldersOnly bool
}

// dashboardTreeTextCodec renders a dashboardTree for people, like tree(1).
type dashboardTreeTextCodec struct {
	opts *treeRender
}

func (c *dashboardTreeTextCodec) Format() format.Format { return "text" }

func (c *dashboardTreeTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *dashboardTreeTextCodec) Encode(w io.Writer, value any) error {
	tree, ok := value.(*dashboardTree)
	if !ok || tree == nil {
		return fmt.Errorf("invalid data type for tree text codec: expected *dashboardTree, got %T", value)
	}

	var opts treeRender
	if c.opts != nil {
		opts = *c.opts
	}
	if opts.timeFormat == "" {
		opts.timeFormat = defaultTreeTimeFormat
	}

	tw := &treeWriter{opts: opts, color: !opts.noColor && (opts.forceColor || style.IsStylingEnabled())}
	if tree.selected {
		for _, f := range tree.Folders {
			tw.measure(f)
		}
	} else {
		tw.measure(&treeFolder{Folders: tree.Folders, Dashboards: tree.Dashboards})
	}

	if tree.selected {
		for i, f := range tree.Folders {
			if i > 0 {
				tw.line("")
			}
			tw.line(tw.folderName(f, f.path))
			tw.children(f, "")
		}
	} else {
		label := tree.label
		if label == "" {
			label = "."
		}
		tw.line(tw.style(lipgloss.NewStyle().Bold(true), label))
		tw.children(&treeFolder{Folders: tree.Folders, Dashboards: tree.Dashboards}, "")
	}

	if !opts.skipReport {
		tw.line("")
		tw.line(tw.report())
	}

	_, err := io.WriteString(w, tw.out.String())
	return err
}

// treeWriter accumulates the rendered tree.
type treeWriter struct {
	opts     treeRender
	color    bool
	out      strings.Builder
	uidWidth int
	folders  int
	dashes   int
}

// measure records the widest UID among the entries that will be printed
// under node, so the metadata column lines up.
func (tw *treeWriter) measure(node *treeFolder) {
	for _, d := range node.Dashboards {
		tw.uidWidth = max(tw.uidWidth, len(d.Name))
	}
	for _, f := range node.Folders {
		tw.uidWidth = max(tw.uidWidth, len(f.Name))
		if n := len(f.Folders) + len(f.Dashboards); tw.opts.fileLimit == 0 || n <= tw.opts.fileLimit {
			tw.measure(f)
		}
	}
}

func (tw *treeWriter) line(s string) {
	tw.out.WriteString(s)
	tw.out.WriteByte('\n')
}

func (tw *treeWriter) style(s lipgloss.Style, text string) string {
	if !tw.color || text == "" {
		return text
	}
	return s.Render(text)
}

func (tw *treeWriter) muted(text string) string {
	return tw.style(lipgloss.NewStyle().Foreground(style.ColorMuted), text)
}

// children writes the contents of node, one line per entry.
func (tw *treeWriter) children(node *treeFolder, prefix string) {
	type entry struct {
		folder *treeFolder
		dash   *treeDashboard
	}
	entries := make([]entry, 0, len(node.Folders)+len(node.Dashboards))
	addFolders := func() {
		for _, f := range node.Folders {
			entries = append(entries, entry{folder: f})
		}
	}
	addDashboards := func() {
		for i := range node.Dashboards {
			entries = append(entries, entry{dash: &node.Dashboards[i]})
		}
	}
	if tw.opts.filesFirst {
		addDashboards()
		addFolders()
	} else {
		addFolders()
		addDashboards()
	}

	for i, e := range entries {
		connector, childPrefix := "├── ", "│   "
		if i == len(entries)-1 {
			connector, childPrefix = "└── ", "    "
		}
		lead := tw.muted(prefix + connector)
		if tw.opts.flat {
			lead, childPrefix = "", ""
		}

		if e.dash != nil {
			tw.dashes++
			name := e.dash.Title
			if tw.opts.fullPath {
				name = joinTreePath(node.path, e.dash.Title)
			}
			tw.line(lead + tw.meta(e.dash.Name, e.dash.Created, e.dash.Updated) + name)
			continue
		}

		f := e.folder
		tw.folders++
		name := f.Title
		if tw.opts.fullPath {
			name = f.path
		}
		text := lead + tw.meta(f.Name, f.Created, f.Updated) + tw.folderName(f, name)

		if n := len(f.Folders) + len(f.Dashboards); tw.opts.fileLimit > 0 && n > tw.opts.fileLimit {
			tw.line(text + tw.muted(fmt.Sprintf("  [%d entries exceed the file limit, not opening folder]", n)))
			continue
		}
		tw.line(text)
		tw.children(f, prefix+childPrefix)
	}
}

// folderName styles a folder name: bold in the primary color, or muted with
// a note when the folder could not be read.
func (tw *treeWriter) folderName(f *treeFolder, name string) string {
	if tw.opts.classify {
		name += "/"
	}
	if f.Unreadable {
		return tw.style(lipgloss.NewStyle().Foreground(style.ColorMuted).Italic(true), name) + tw.muted(" (folder not readable)")
	}
	return tw.style(lipgloss.NewStyle().Foreground(style.ColorPrimary).Bold(true), name)
}

// meta renders the bracketed UID and date column that precedes a name when
// --uid or --date is set, like the inode and date columns of tree(1).
func (tw *treeWriter) meta(uid string, created, updated time.Time) string {
	var parts []string
	if tw.opts.showUID {
		parts = append(parts, fmt.Sprintf("%-*s", tw.uidWidth, uid))
	}
	if tw.opts.showDate {
		ts := updated
		if tw.opts.dateCreated {
			ts = created
		}
		date := strings.Repeat(" ", len(time.Time{}.Format(tw.opts.timeFormat)))
		if !ts.IsZero() {
			date = ts.UTC().Format(tw.opts.timeFormat)
		}
		parts = append(parts, date)
	}
	if len(parts) == 0 {
		return ""
	}
	return tw.muted("["+strings.Join(parts, "  ")+"]") + "  "
}

func (tw *treeWriter) report() string {
	folders := plural(tw.folders, "folder", "folders")
	if tw.opts.foldersOnly {
		return folders
	}
	return folders + ", " + plural(tw.dashes, "dashboard", "dashboards")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
