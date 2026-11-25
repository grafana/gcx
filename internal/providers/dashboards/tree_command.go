package dashboards

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/dashboards/descriptor"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// treeLister is the subset of the dynamic client used by the tree command.
type treeLister interface {
	List(ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)
}

type treeOpts struct {
	IO         cmdio.Options
	APIVersion string

	render treeRender

	level        int
	foldersOnly  bool
	pattern      string
	exclude      string
	ignoreCase   bool
	matchFolders bool
	prune        bool

	sortBy       string
	sortModified bool
	sortCreated  bool
	unsorted     bool
	reverse      bool

	// Set by Validate.
	selection treeSelection
	sortKey   treeSortKey
}

func (o *treeOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &dashboardTreeTextCodec{opts: &o.render})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.APIVersion, "api-version", "", "Dashboard API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")

	// Listing: these decide what the tree contains, in every output format.
	flags.BoolVarP(&o.foldersOnly, "folders-only", "d", false, "List folders only")
	flags.IntVarP(&o.level, "level", "L", 0, "Descend at most this many folder levels (0 means no limit)")
	flags.StringVarP(&o.pattern, "pattern", "P", "", "List only dashboards whose title matches the wildcard pattern (*, ?, [...], and | between alternatives)")
	flags.StringVarP(&o.exclude, "exclude", "I", "", "Do not list dashboards whose title matches the wildcard pattern")
	flags.BoolVar(&o.ignoreCase, "ignore-case", false, "Ignore case when matching --pattern and --exclude")
	flags.BoolVar(&o.matchFolders, "match-folders", false, "Match --pattern and --exclude against folder titles too; a folder that matches --pattern is listed with all its contents")
	flags.BoolVar(&o.prune, "prune", false, "Leave out folders that end up empty, for example after --pattern")

	// Sorting.
	flags.StringVar(&o.sortBy, "sort", string(sortByName), "Sort by name, version (numbers in titles by value), mtime, ctime, or none")
	flags.BoolVarP(&o.sortModified, "sort-modified", "t", false, "Sort by last modification time, oldest first (--sort mtime)")
	flags.BoolVarP(&o.sortCreated, "sort-created", "c", false, "Sort by creation time, oldest first (--sort ctime); --date then shows the creation date")
	flags.BoolVarP(&o.unsorted, "unsorted", "U", false, "Keep the order the server returns (--sort none)")
	flags.BoolVarP(&o.reverse, "reverse", "r", false, "Reverse the sort order")
	flags.BoolVar(&o.render.filesFirst, "files-first", false, "List dashboards before subfolders")

	// Display: text output only.
	flags.BoolVarP(&o.render.fullPath, "full-path", "f", false, "Print the full folder path of every entry")
	flags.BoolVarP(&o.render.flat, "flat", "i", false, "Print entries without indentation lines (with -f, a flat list of paths)")
	flags.BoolVarP(&o.render.classify, "classify", "F", false, "Append / to folder names")
	flags.BoolVarP(&o.render.forceColor, "color", "C", false, "Always use color, even when output is not a terminal")
	flags.BoolVarP(&o.render.noColor, "no-color", "n", false, "Disable color output")
	flags.BoolVarP(&o.render.showDate, "date", "D", false, "Print the last modification date (UTC) of every entry")
	flags.StringVar(&o.render.timeFormat, "time-format", defaultTreeTimeFormat, "Go time layout for --date (times are in UTC)")
	flags.BoolVar(&o.render.showUID, "uid", false, "Print the UID of every entry")
	flags.IntVar(&o.render.fileLimit, "file-limit", 0, "Do not open folders with more than this many entries (0 means no limit)")
	flags.BoolVar(&o.render.skipReport, "skip-report", false, "Do not print the folder and dashboard count at the end")
}

func (o *treeOpts) Validate() error {
	if o.level < 0 {
		return fmt.Errorf("--level must be >= 0, got %d", o.level)
	}
	if o.render.fileLimit < 0 {
		return fmt.Errorf("--file-limit must be >= 0, got %d", o.render.fileLimit)
	}
	if o.render.timeFormat == "" {
		return errors.New("--time-format must not be empty")
	}

	switch {
	case o.sortModified:
		o.sortKey = sortByMtime
	case o.sortCreated:
		o.sortKey = sortByCtime
	case o.unsorted:
		o.sortKey = sortByNone
	default:
		o.sortKey = treeSortKey(o.sortBy)
	}
	switch o.sortKey {
	case sortByName, sortByVersion, sortByMtime, sortByCtime, sortByNone:
	default:
		return fmt.Errorf("--sort must be one of name, version, mtime, ctime, none, got %q", o.sortBy)
	}
	o.render.dateCreated = o.sortKey == sortByCtime
	o.render.foldersOnly = o.foldersOnly

	var include, exclude *globMatcher
	if o.pattern != "" {
		m, err := newGlobMatcher(o.pattern, o.ignoreCase)
		if err != nil {
			return fmt.Errorf("--pattern: %w", err)
		}
		include = m
	}
	if o.exclude != "" {
		m, err := newGlobMatcher(o.exclude, o.ignoreCase)
		if err != nil {
			return fmt.Errorf("--exclude: %w", err)
		}
		exclude = m
	}
	o.selection = treeSelection{
		level:        o.level,
		foldersOnly:  o.foldersOnly,
		include:      include,
		exclude:      exclude,
		matchFolders: o.matchFolders,
		prune:        o.prune,
	}

	return o.IO.Validate()
}

// newTreeCommand returns the `dashboards tree` subcommand.
func newTreeCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &treeOpts{}

	cmd := &cobra.Command{
		Use:   "tree [folder...]",
		Short: "Show dashboards in their folder hierarchy",
		Long: `Show dashboards nested under their folders, like tree(1).

With no folder argument the tree covers the whole Grafana instance. Pass one
or more folders to show only their subtrees. A folder is a UID or a path of
folder titles, such as "Infrastructure/Ceph", or the title of a single
folder anywhere in the tree, such as "Ceph". Titles are matched exactly
first, then ignoring case.

The tree always includes every dashboard and folder the caller can read,
across all pages. Folders without dashboards are listed. A dashboard in a
folder the caller cannot read is listed under that folder's UID.

The flags follow tree(1): -d, -L, -P, -I, --prune, and --match-folders choose
what the tree contains, and also apply to JSON and YAML output. -t, -c, -U,
-r, and --sort set the order; tree's -v is --sort version here, because -v is
gcx's --verbose. -f, -i, -F, -C, -n, -D, --uid, and --file-limit only change
the text output. JSON and YAML contain the nested structure with names,
titles, and creation and modification times.`,
		Example: `  # Show every folder and dashboard
  gcx dashboards tree

  # Show one folder, by title path, by unique title, or by UID
  gcx dashboards tree Infrastructure/Ceph
  gcx dashboards tree ceph
  gcx dashboards tree <folder-uid>

  # Show only the folder structure, two levels deep
  gcx dashboards tree -d -L 2

  # Find dashboards by title and hide folders without a match
  gcx dashboards tree -P '*Ceph*|*OSD*' --ignore-case --prune

  # Show the most recently changed dashboards last, with their dates
  gcx dashboards tree -tD

  # Print one full path per line, for scripts
  gcx dashboards tree -fi --skip-report

  # Get the hierarchy as nested JSON
  gcx dashboards tree -o json`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return gcxerrors.NewCommandUsageError(cmd, "", err)
			}

			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := dynamic.NewDefaultNamespacedClient(cfg)
			if err != nil {
				return err
			}

			dashDesc, err := descriptor.Resolve(ctx, cfg, opts.APIVersion)
			if err != nil {
				return err
			}
			folderDesc, err := descriptor.ResolveFolders(ctx, cfg)
			if err != nil {
				return err
			}

			dashboards, folders, err := listTreeObjects(ctx, client, dashDesc, folderDesc)
			if err != nil {
				return err
			}

			tree, err := assembleDashboardTree(dashboards, folders, args, opts)
			if err != nil {
				return err
			}
			tree.label = cfg.GrafanaURL

			return opts.IO.Encode(cmd.OutOrStdout(), tree)
		},
	}

	opts.setup(cmd.Flags())
	cmd.MarkFlagsMutuallyExclusive("sort", "sort-modified", "sort-created", "unsorted")
	cmd.MarkFlagsMutuallyExclusive("color", "no-color")

	return cmd
}

// listTreeObjects fetches every dashboard and every folder. A zero limit
// makes the client follow every continue token.
func listTreeObjects(ctx context.Context, client treeLister, dashDesc, folderDesc resources.Descriptor) ([]unstructured.Unstructured, []unstructured.Unstructured, error) {
	dashboards, err := client.List(ctx, dashDesc, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	folders, err := client.List(ctx, folderDesc, metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("listing folders: %w", err)
	}
	return dashboards.Items, folders.Items, nil
}

// assembleDashboardTree builds the hierarchy, selects the requested folders,
// and applies the listing and sorting options.
func assembleDashboardTree(dashboards, folders []unstructured.Unstructured, args []string, opts *treeOpts) (*dashboardTree, error) {
	root, index := buildDashboardTree(dashboards, folders)

	if len(args) == 0 {
		filtered := opts.selection.filter(root, 1, false)
		sortTree(filtered, opts.sortKey, opts.reverse)
		return &dashboardTree{Folders: filtered.Folders, Dashboards: filtered.Dashboards}, nil
	}

	tree := &dashboardTree{Folders: []*treeFolder{}, Dashboards: []treeDashboard{}, selected: true}
	for _, arg := range args {
		node, ambiguous := findFolder(root, index, arg)
		if len(ambiguous) > 0 {
			return nil, ambiguousFolderError(arg, ambiguous)
		}
		if node == nil {
			return nil, folderNotFoundError(index, arg)
		}
		filtered := opts.selection.filter(node, 1, false)
		sortTree(filtered, opts.sortKey, opts.reverse)
		tree.Folders = append(tree.Folders, filtered)
	}
	return tree, nil
}

func ambiguousFolderError(arg string, paths []string) error {
	suggestions := make([]string, 0, len(paths))
	for _, path := range paths {
		suggestions = append(suggestions, "gcx dashboards tree "+strconv.Quote(path))
	}
	return gcxerrors.DetailedError{
		Summary:     fmt.Sprintf("Folder title %q matches %d folders", arg, len(paths)),
		Details:     "Pass the folder path or UID instead.",
		Suggestions: suggestions,
	}
}

func folderNotFoundError(index folderIndex, arg string) error {
	paths := folderSuggestions(index, arg)
	suggestions := make([]string, 0, len(paths)+1)
	for _, path := range paths {
		suggestions = append(suggestions, "gcx dashboards tree "+strconv.Quote(path))
	}
	suggestions = append(suggestions, "List all folders with: gcx dashboards tree -d")

	return gcxerrors.DetailedError{
		Summary:     fmt.Sprintf("Folder %q not found", arg),
		Details:     "Pass a folder UID or a path of folder titles, such as Infrastructure/Ceph.",
		Suggestions: suggestions,
	}
}
