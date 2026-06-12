package integrations

import (
	"strings"
	"testing"
)

func TestTrimToEssentials(t *testing.T) {
	md := strings.Join([]string{
		"# Redis integration",
		"",
		"Intro paragraph.",
		"",
		"## Before you begin",
		"A running Redis instance is required.",
		"",
		"## Install Redis integration",
		"1. Open Connections.",
		"",
		"## Configuration snippets for Grafana Alloy",
		"### Simple mode",
		"snippet...",
		"## Dashboards",
		"...",
	}, "\n")

	got := trimToEssentials(md)

	for _, want := range []string{"## Before you begin", "## Install Redis integration", "A running Redis instance"} {
		if !strings.Contains(got, want) {
			t.Errorf("trimmed output missing essential section %q", want)
		}
	}
	for _, drop := range []string{"Configuration snippets", "Simple mode", "## Dashboards"} {
		if strings.Contains(got, drop) {
			t.Errorf("trimmed output should not contain advanced section %q", drop)
		}
	}
}

func TestTrimToEssentials_NoMarkersKeepsAll(t *testing.T) {
	md := "# Title\n\n## Before you begin\nprereqs\n\n## Install\nsteps\n"
	if got := trimToEssentials(md); got != md {
		t.Errorf("expected unchanged content when no advanced markers present\n got: %q", got)
	}
}

func TestCleanFrontmatter(t *testing.T) {
	md := strings.Join([]string{
		"---",
		`title: "Linux Server integration"`,
		`description: "..."`,
		"---",
		"",
		"> For a curated documentation index, see [llms.txt](/llms.txt).",
		"",
		"# Linux Server integration for Grafana Cloud",
		"Body.",
	}, "\n")

	got := cleanFrontmatter(md)

	if strings.Contains(got, "title:") || strings.Contains(got, "description:") {
		t.Errorf("frontmatter not stripped:\n%s", got)
	}
	if strings.Contains(got, "For a curated documentation index") {
		t.Errorf("docs-index callout line not stripped:\n%s", got)
	}
	if !strings.HasPrefix(got, "# Linux Server integration for Grafana Cloud") {
		t.Errorf("expected content to start at the H1, got:\n%q", got)
	}
}

func TestExtractSection(t *testing.T) {
	md := strings.Join([]string{
		"# Title",
		"intro",
		"## Before you begin",
		"A running instance is required.",
		"",
		"## Install Title",
		"1. Step one.",
		"### Simple mode",
		"this subsection stays with install",
		"## Configuration snippets for Grafana Alloy",
		"advanced...",
	}, "\n")

	pre := extractSection(md, "## before you begin")
	if !strings.HasPrefix(pre, "## Before you begin") || !strings.Contains(pre, "running instance") {
		t.Errorf("prerequisites section wrong:\n%s", pre)
	}
	if strings.Contains(pre, "Install Title") {
		t.Errorf("prerequisites section bled into install:\n%s", pre)
	}

	install := extractSection(md, "## install")
	if !strings.Contains(install, "1. Step one.") || !strings.Contains(install, "Simple mode") {
		t.Errorf("install section should include steps and its ### subsection:\n%s", install)
	}
	if strings.Contains(install, "Configuration snippets") {
		t.Errorf("install section bled into advanced config:\n%s", install)
	}

	if got := extractSection(md, "## nonexistent"); got != "" {
		t.Errorf("expected empty string for missing section, got %q", got)
	}
}

func TestSplitConfig(t *testing.T) {
	md := strings.Join([]string{
		"## Configuration snippets for Grafana Alloy",
		"### Simple mode",
		"simple snippet",
		"### Logs snippets",
		"logs snippet",
		"### Advanced mode",
		"advanced snippet",
		"### Advanced logs snippets",
		"advanced logs",
		"## Dashboards",
		"dashboard stuff",
	}, "\n")

	simple, advanced := splitConfig(md)

	if !strings.Contains(simple, "Simple mode") || !strings.Contains(simple, "Logs snippets") {
		t.Errorf("simple config missing expected subsections:\n%s", simple)
	}
	if strings.Contains(simple, "Advanced mode") || strings.Contains(simple, "Dashboards") {
		t.Errorf("simple config leaked advanced/other content:\n%s", simple)
	}
	if !strings.HasPrefix(advanced, "### Advanced mode") || !strings.Contains(advanced, "Advanced logs snippets") {
		t.Errorf("advanced config wrong:\n%s", advanced)
	}
	if strings.Contains(advanced, "Dashboards") {
		t.Errorf("advanced config leaked the next H2 section:\n%s", advanced)
	}

	// No config section at all → both empty.
	if s, a := splitConfig("# Title\n## Install\nsteps\n"); s != "" || a != "" {
		t.Errorf("expected empty results when no config section, got simple=%q advanced=%q", s, a)
	}
}

func TestCatalogHasSlug(t *testing.T) {
	if !catalogHasSlug("linux-node") {
		t.Error("expected linux-node to be in the catalog")
	}
	if catalogHasSlug("definitely-not-an-integration") {
		t.Error("did not expect an unknown slug to be in the catalog")
	}
}
