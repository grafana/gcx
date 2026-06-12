package integrations

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// docsBaseURL is the public docs path; a slug maps to "<base><slug>/" (HTML)
// and "<base><slug>.md" (Markdown).
const docsBaseURL = "https://grafana.com/docs/grafana-cloud/monitor-infrastructure/integrations/integration-reference/integration-"

func docsURL(slug string) string         { return docsBaseURL + slug + "/" }
func docsMarkdownURL(slug string) string { return docsBaseURL + slug + ".md" }

type docsOpts struct {
	Open          bool
	URL           bool
	Full          bool
	Raw           bool
	Prerequisites bool
	Install       bool
	Config        bool
	Advanced      bool
	Kubernetes    bool
}

func (o *docsOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Open, "open", false, "Open the documentation in your browser instead of printing it")
	flags.BoolVar(&o.URL, "url", false, "Print only the documentation URL")
	flags.BoolVar(&o.Full, "full", false, "Show the full page, including advanced configuration and reference sections")
	flags.BoolVar(&o.Raw, "raw", false, "Print raw Markdown without terminal styling")
	flags.BoolVar(&o.Prerequisites, "prerequisites", false, "Show only the prerequisites (\"Before you begin\") section")
	flags.BoolVar(&o.Install, "install", false, "Show only the installation steps section")
	flags.BoolVar(&o.Config, "config", false, "Show only the simple-mode Grafana Alloy configuration snippets")
	flags.BoolVar(&o.Advanced, "advanced", false, "Show only the advanced-mode Grafana Alloy configuration snippets")
	flags.BoolVar(&o.Kubernetes, "kubernetes", false, "Show only the Kubernetes installation instructions section")
}

func newDocsCommand() *cobra.Command {
	opts := &docsOpts{}
	cmd := &cobra.Command{
		Use:   "docs <slug>",
		Short: "Show installation docs and prerequisites for an integration.",
		Long: "Show the prerequisites and installation steps for a Grafana Cloud " +
			"integration, fetched from the public documentation. By default the " +
			"advanced configuration and reference sections are omitted; use --full " +
			"to see the entire page, or --prerequisites / --install / --config / " +
			"--advanced / --kubernetes to print only that section.",
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "Show prerequisites and install steps for one Grafana Cloud integration from the public docs. Use --prerequisites, --install, --config (simple Alloy config), --advanced (advanced Alloy config), or --kubernetes to get only that section, --full for the whole page, --url for just the link.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			if !catalogHasSlug(slug) {
				return fmt.Errorf("integration %q not found; run 'gcx integrations list' to see available integrations", slug)
			}

			if opts.URL {
				fmt.Fprintln(cmd.OutOrStdout(), docsURL(slug))
				return nil
			}
			if opts.Open {
				if err := deeplink.Open(docsURL(slug)); err != nil {
					return fmt.Errorf("failed to open browser: %w", err)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Opened %s\n", docsURL(slug))
				return nil
			}

			md, err := fetchDocs(cmd.Context(), slug)
			if err != nil {
				return err
			}
			md = cleanFrontmatter(md)
			switch {
			case opts.Full:
				// whole page, untouched
			case opts.Prerequisites || opts.Install || opts.Config || opts.Advanced || opts.Kubernetes:
				var parts []string
				if opts.Prerequisites {
					if s := extractSection(md, "## before you begin"); s != "" {
						parts = append(parts, s)
					}
				}
				if opts.Install {
					if s := extractSection(md, "## install"); s != "" {
						parts = append(parts, s)
					}
				}
				if opts.Config || opts.Advanced {
					simple, advanced := splitConfig(md)
					if opts.Config && simple != "" {
						parts = append(parts, simple)
					}
					if opts.Advanced && advanced != "" {
						parts = append(parts, advanced)
					}
				}
				if opts.Kubernetes {
					if s := extractSection(md, "## kubernetes instructions"); s != "" {
						parts = append(parts, s)
					}
				}
				if len(parts) == 0 {
					return fmt.Errorf("requested section not found in docs for %q; see %s", slug, docsURL(slug))
				}
				md = strings.Join(parts, "\n\n") + docsFooter(slug)
			default:
				md = strings.TrimSpace(trimToEssentials(md)) + docsFooter(slug)
			}

			out := cmd.OutOrStdout()
			md = strings.TrimSpace(md)

			// Style only for an interactive human TTY; agents/pipes/--raw get raw Markdown.
			styled := !opts.Raw && !agent.IsAgentMode() && !terminal.IsPiped()
			if styled {
				if rendered, rerr := glamour.Render(md, "dark"); rerr == nil {
					fmt.Fprint(out, rendered)
					return nil
				}
				// Styling failed; fall through to raw Markdown.
			}
			fmt.Fprintln(out, md)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	cmd.MarkFlagsMutuallyExclusive("url", "open")
	cmd.MarkFlagsMutuallyExclusive("full", "prerequisites")
	cmd.MarkFlagsMutuallyExclusive("full", "install")
	cmd.MarkFlagsMutuallyExclusive("full", "config")
	cmd.MarkFlagsMutuallyExclusive("full", "advanced")
	cmd.MarkFlagsMutuallyExclusive("full", "kubernetes")
	return cmd
}

func catalogHasSlug(slug string) bool {
	for _, in := range curatedCatalog() {
		if in.Slug == slug {
			return true
		}
	}
	return false
}

func fetchDocs(ctx context.Context, slug string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docsMarkdownURL(slug), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/markdown")

	resp, err := httputils.NewDefaultClient(ctx).Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch docs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docs for %q are unavailable (HTTP %s); see %s", slug, resp.Status, docsURL(slug))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read docs response: %w", err)
	}
	return string(body), nil
}

// docsFooter points at the complete page; appended to trimmed/section output only.
func docsFooter(slug string) string {
	return "\n\n---\n\n_Source: " + docsURL(slug) + " — re-run with `--full` for the complete page._"
}

// extractSection returns the first H2 section whose heading (lowercased) starts
// with headingPrefix, up to the next H2, or "" if not found.
func extractSection(md, headingPrefix string) string {
	var b strings.Builder
	capturing := false
	for line := range strings.SplitSeq(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			// New H2: start if it matches, else stop.
			if !capturing && strings.HasPrefix(strings.ToLower(trimmed), headingPrefix) {
				capturing = true
			} else if capturing {
				break
			}
		}
		if capturing {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

// splitConfig divides the "Configuration snippets" section into its simple-mode
// part (heading up to "### Advanced mode") and advanced-mode part ("### Advanced
// mode" onward). Either may be "" if absent.
func splitConfig(md string) (simple, advanced string) {
	section := extractSection(md, "## configuration snippets")
	if section == "" {
		return "", ""
	}
	lines := strings.Split(section, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "### advanced mode") {
			return strings.TrimSpace(strings.Join(lines[:i], "\n")), strings.TrimSpace(strings.Join(lines[i:], "\n"))
		}
	}
	return strings.TrimSpace(section), ""
}

// cleanFrontmatter strips the leading YAML frontmatter block and the
// docs-index callout line that the published Markdown carries.
func cleanFrontmatter(md string) string {
	md = strings.TrimLeft(md, "\n")
	if strings.HasPrefix(md, "---\n") {
		if end := strings.Index(md[4:], "\n---"); end != -1 {
			md = md[4+end+len("\n---"):]
		}
	}
	var b strings.Builder
	for line := range strings.SplitSeq(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "> For a curated documentation index") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimLeft(b.String(), "\n")
}

// trimToEssentials keeps the overview, prerequisites, and install steps, dropping
// the advanced configuration and reference sections that follow them.
func trimToEssentials(md string) string {
	markers := []string{
		"## configuration snippets",
		"## kubernetes instructions",
		"## dashboards",
		"## alerts",
		"## metrics",
		"## changelog",
		"## cost",
	}
	lines := strings.Split(md, "\n")
	for i, line := range lines {
		l := strings.ToLower(strings.TrimSpace(line))
		for _, m := range markers {
			if strings.HasPrefix(l, m) {
				return strings.Join(lines[:i], "\n")
			}
		}
	}
	return md
}
