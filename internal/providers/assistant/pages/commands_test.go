package pages //nolint:testpackage // Verify flag validation and publish audience rules on unexported options.

import (
	"strings"
	"testing"

	clientpages "github.com/grafana/gcx/internal/assistant/pages"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bound returns opts after binding its flags, as the command does, then
// applying set.
func bound[T interface{ setup(flags *pflag.FlagSet) }](opts T, set func(T)) T {
	opts.setup(pflag.NewFlagSet("test", pflag.ContinueOnError))
	set(opts)
	return opts
}

func TestPublicationOfANewPage(t *testing.T) {
	link := &clientpages.Link{Title: "Checkout health"}
	for name, tt := range map[string]struct {
		opts    publishOpts
		meta    clientpages.Metadata
		sharing *clientpages.Sharing
		err     string
	}{
		"folder": {
			opts: publishOpts{Folder: "ops", Category: "report"},
			meta: clientpages.Metadata{Title: "Checkout health", Category: "report", FolderUID: "ops"},
		},
		"unlisted with its link shared": {
			opts:    publishOpts{Unlisted: true, LinkSharing: true, Title: "Checkout"},
			meta:    clientpages.Metadata{Title: "Checkout", Category: "other", Unlisted: true},
			sharing: &clientpages.Sharing{Users: []clientpages.Share{}, LinkSharing: true},
		},
		"no audience": {err: "a new page needs --folder"},
	} {
		t.Run(name, func(t *testing.T) {
			meta, sharing, err := tt.opts.publication(link)
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.meta, meta)
			assert.Equal(t, tt.sharing, sharing)
		})
	}
}

func TestPublicationOfAPublishedPageKeepsItsAudience(t *testing.T) {
	link := &clientpages.Link{Publication: &clientpages.Publication{ID: "pub-1", Version: 3}}
	meta, sharing, err := (&publishOpts{Description: "New"}).publication(link)
	require.NoError(t, err)
	assert.Equal(t, clientpages.Metadata{Description: "New"}, meta)
	assert.Nil(t, sharing)

	_, _, err = (&publishOpts{Folder: "elsewhere"}).publication(link)
	require.ErrorContains(t, err, "already published")
}

func TestPublishFlagValidation(t *testing.T) {
	for name, tt := range map[string]struct {
		opts publishOpts
		err  string
	}{
		"folder and unlisted":     {publishOpts{Folder: "ops", Unlisted: true}, "mutually exclusive"},
		"link sharing alone":      {publishOpts{LinkSharing: true}, "requires --unlisted"},
		"unknown category":        {publishOpts{Category: "dashboard"}, "--category must be one of"},
		"too many tags":           {publishOpts{Tags: make([]string, 13)}, "at most 12"},
		"long description":        {publishOpts{Description: strings.Repeat("x", 2001)}, "--description"},
		"valid unlisted with tag": {publishOpts{Unlisted: true, LinkSharing: true, Tags: []string{"checkout"}}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			opts := bound(&publishOpts{}, func(o *publishOpts) {
				o.Folder, o.Unlisted, o.LinkSharing, o.Category, o.Tags, o.Description = tt.opts.Folder, tt.opts.Unlisted, tt.opts.LinkSharing, tt.opts.Category, tt.opts.Tags, tt.opts.Description
			})
			err := opts.Validate()
			if tt.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.err)
		})
	}
}

func TestSnapshotAndListFlagValidation(t *testing.T) {
	require.ErrorContains(t, (&snapshotOpts{Width: 100}).Validate(), "--width")
	require.ErrorContains(t, (&snapshotOpts{Width: 1200, Theme: "blue"}).Validate(), "--theme")
	require.ErrorContains(t, (&snapshotOpts{Width: 1200, Clicks: make([]string, 17)}).Validate(), "--click")
	require.NoError(t, bound(&snapshotOpts{}, func(o *snapshotOpts) { o.Theme = "light" }).Validate())

	require.ErrorContains(t, (&listOpts{Section: "all", Limit: 30}).Validate(), "--section")
	require.ErrorContains(t, (&listOpts{Limit: -1}).Validate(), "--limit")
	require.NoError(t, bound(&listOpts{}, func(o *listOpts) { o.Section = "mine" }).Validate())

	require.ErrorContains(t, (&createOpts{Title: "  "}).Validate(), "--title is required")
}
