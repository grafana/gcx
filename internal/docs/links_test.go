package docs_test

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/docs"
)

// TestAllURLsAreMarkdown asserts every registry URL is a well-formed https
// grafana.com/docs link ending in .md. This guards the core invariant of the
// package: agents must always be pointed at the Markdown rendering of a doc.
func TestAllURLsAreMarkdown(t *testing.T) {
	seen := map[string]bool{}
	for _, raw := range docs.All() {
		u, err := url.Parse(raw)
		if err != nil {
			t.Errorf("not a valid URL: %q: %v", raw, err)
			continue
		}
		if u.Scheme != "https" {
			t.Errorf("URL must use https: %q", raw)
		}
		if u.Host != "grafana.com" {
			t.Errorf("URL must be on grafana.com: %q", raw)
		}
		if !strings.HasPrefix(u.Path, "/docs/") {
			t.Errorf("URL must be under /docs/: %q", raw)
		}
		if !strings.HasSuffix(u.Path, ".md") {
			t.Errorf("URL must end in .md so agents fetch Markdown: %q", raw)
		}
		if seen[raw] {
			t.Errorf("duplicate URL in registry: %q", raw)
		}
		seen[raw] = true
	}
}

// TestAllContainsCloudAPI guards against the constant being defined but left
// out of All() — the shape test above only checks what All() returns.
func TestAllContainsCloudAPI(t *testing.T) {
	if !slices.Contains(docs.All(), docs.CloudAPI) {
		t.Errorf("docs.CloudAPI (%q) is missing from docs.All()", docs.CloudAPI)
	}
}

// TestAllNamedIsSubsetOfAll asserts every agent-facing named link is part of
// the full validity set, and that names are unique and non-empty.
func TestAllNamedIsSubsetOfAll(t *testing.T) {
	all := docs.All()

	seenNames := map[string]bool{}
	for i, l := range docs.AllNamed() {
		if l.Name == "" {
			t.Errorf("entry %d has empty name (url %q)", i, l.URL)
		}
		if !slices.Contains(all, l.URL) {
			t.Errorf("AllNamed entry %q (%q) is missing from All()", l.Name, l.URL)
		}
		if seenNames[l.Name] {
			t.Errorf("duplicate name in registry: %q", l.Name)
		}
		seenNames[l.Name] = true
	}
}
