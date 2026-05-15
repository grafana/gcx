package vulnobs

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/fail"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// usageError builds a DetailedError with ExitCode=2 (usage/validation),
// matching gcx's convention so that bad input yields a single, actionable
// message and a non-1 exit code per DESIGN.md.
func usageError(summary, details string) error {
	code := fail.ExitUsageError
	return &fail.DetailedError{
		Summary:  summary,
		Details:  details,
		ExitCode: &code,
	}
}

// newVulnobsCommand returns the root `vulnobs` cobra command.
func newVulnobsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vulnobs",
		Short: "Inspect Grafana Vulnerability Observability data (read-only).",
		Long: `Inspect Grafana Vulnerability Observability data (read-only).

The vulnerability-obs API is read-only from clients; mutations happen on the
server as scanners ingest findings. This command tree exposes groups,
projects (sources), and CVE findings (issues) through the same plugin-proxy
GraphQL endpoint the Grafana UI uses.

Source data is also available through the unified resources tier:

    gcx resources list sources.vulnobs.grafana.app
`,
	}

	cmd.AddCommand(
		newGroupsCommand(loader),
		newProjectsCommand(loader),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// groups
// ---------------------------------------------------------------------------

type groupsListOpts struct {
	IO cmdio.Options
}

func (o *groupsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &GroupTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newGroupsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "Manage vulnerability-obs groups (read-only).",
	}
	opts := &groupsListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all vulnerability-obs groups.",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClientFromLoader(cmd, loader)
			if err != nil {
				return err
			}
			groups, err := client.Groups(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), groups)
		},
	}
	opts.setup(listCmd.Flags())
	cmd.AddCommand(listCmd)
	return cmd
}

// ---------------------------------------------------------------------------
// projects
// ---------------------------------------------------------------------------

type projectsListOpts struct {
	IO           cmdio.Options
	Group        string
	Sort         string
	First        int
	ShowArchived bool
	IncludeK8s   bool
}

func (o *projectsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ProjectTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Group, "group", "", "Filter to one group (name or numeric id)")
	flags.StringVar(&o.Sort, "sort", "CRITICALS_DESC", "Sort order: CRITICALS_DESC, HIGHS_DESC, SLO_ASC")
	flags.IntVar(&o.First, "first", 30, "Maximum number of projects to return")
	flags.BoolVar(&o.ShowArchived, "show-archived", false, "Include archived sources")
	flags.BoolVar(&o.IncludeK8s, "include-k8s", false, "Include k8s-scan versions")
}

type projectsListIssuesOpts struct {
	IO         cmdio.Options
	Repo       string
	Tag        string
	Severities string
}

func (o *projectsListIssuesOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &IssueTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Repo, "repo", "", "Resolve from repo (owner/name); alternative to positional versionId")
	flags.StringVar(&o.Tag, "tag", "main", "Tag to resolve when --repo is used")
	flags.StringVar(&o.Severities, "severity", "", "Comma-separated severities to include (CRITICAL,HIGH,...)")
}

func newProjectsCommand(loader RESTConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Manage vulnerability-obs projects (sources) and their findings.",
	}

	listOpts := &projectsListOpts{}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List projects with CVE counts.",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   `gcx vulnobs projects list --group feO11y -o json`,
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := listOpts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClientFromLoader(cmd, loader)
			if err != nil {
				return err
			}
			groupID, err := client.ResolveGroupID(cmd.Context(), listOpts.Group)
			if err != nil {
				return err
			}
			sources, _, err := client.Projects(cmd.Context(), ProjectsOptions{
				GroupID:      groupID,
				SortBy:       listOpts.Sort,
				First:        listOpts.First,
				ShowArchived: listOpts.ShowArchived,
				HideK8s:      !listOpts.IncludeK8s,
			})
			if err != nil {
				return err
			}
			return listOpts.IO.Encode(cmd.OutOrStdout(), sources)
		},
	}
	listOpts.setup(listCmd.Flags())

	listIssuesOpts := &projectsListIssuesOpts{}
	listIssuesCmd := &cobra.Command{
		Use:   "list-issues [<versionId>]",
		Short: "List CVE findings for a project version (sub-resource).",
		Long: `List CVE findings for a project version.

Issues are sub-resources of a Source's Version: every query requires a
versionId. Pass it either as a positional argument or resolve it from
--repo (and optionally --tag, default "main").

Examples:

  gcx vulnobs projects list-issues 10355
  gcx vulnobs projects list-issues --repo grafana/faro-web-sdk
  gcx vulnobs projects list-issues --repo grafana/faro-web-sdk --tag v2.6.3 \
      --severity CRITICAL,HIGH
`,
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   `gcx vulnobs projects list-issues --repo grafana/faro-web-sdk --severity CRITICAL,HIGH -o json`,
		},
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := listIssuesOpts.IO.Validate(); err != nil {
				return err
			}
			var versionID string
			switch {
			case len(args) == 1 && listIssuesOpts.Repo != "":
				return usageError("vulnobs: conflicting inputs",
					"pass either a positional <versionId> or --repo, not both")
			case len(args) == 1:
				versionID = args[0]
			case listIssuesOpts.Repo != "":
				// fall through; resolved below.
			default:
				return usageError("vulnobs: missing input",
					"a versionId or --repo is required")
			}

			client, err := newClientFromLoader(cmd, loader)
			if err != nil {
				return err
			}
			if versionID == "" {
				versionID, err = client.ResolveVersion(cmd.Context(), listIssuesOpts.Repo, listIssuesOpts.Tag)
				if err != nil {
					return err
				}
			}
			issues, err := client.Issues(cmd.Context(), versionID)
			if err != nil {
				return err
			}
			if listIssuesOpts.Severities != "" {
				wanted := map[string]bool{}
				for s := range strings.SplitSeq(listIssuesOpts.Severities, ",") {
					wanted[strings.ToUpper(strings.TrimSpace(s))] = true
				}
				filtered := issues[:0]
				for _, iss := range issues {
					if wanted[strings.ToUpper(iss.Cve.Severity)] {
						filtered = append(filtered, iss)
					}
				}
				issues = filtered
			}
			return listIssuesOpts.IO.Encode(cmd.OutOrStdout(), issues)
		},
	}
	listIssuesOpts.setup(listIssuesCmd.Flags())

	cmd.AddCommand(listCmd, listIssuesCmd)
	return cmd
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newClientFromLoader(cmd *cobra.Command, loader RESTConfigLoader) (*Client, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, err
	}
	return NewClient(cfg)
}

// ---------------------------------------------------------------------------
// Table codecs
// ---------------------------------------------------------------------------

// GroupTableCodec renders groups as a table.
type GroupTableCodec struct{}

func (c *GroupTableCodec) Format() format.Format { return "table" }
func (c *GroupTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
func (c *GroupTableCodec) Encode(w io.Writer, v any) error {
	groups, ok := v.([]Group)
	if !ok {
		return fmt.Errorf("vulnobs: GroupTableCodec: unexpected type %T", v)
	}
	t := style.NewTable("ID", "NAME")
	for _, g := range groups {
		t.Row(strconv.Itoa(g.ID), g.Name)
	}
	return t.Render(w)
}

// ProjectTableCodec renders projects (sources) as a table.
type ProjectTableCodec struct{}

func (c *ProjectTableCodec) Format() format.Format { return "table" }
func (c *ProjectTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
func (c *ProjectTableCodec) Encode(w io.Writer, v any) error {
	sources, ok := v.([]Source)
	if !ok {
		return fmt.Errorf("vulnobs: ProjectTableCodec: unexpected type %T", v)
	}
	t := style.NewTable("REPO", "TAG", "CRIT", "HIGH", "MED", "LOW", "SLO")
	for _, s := range sources {
		latest := latestVersion(s)
		if latest == nil {
			t.Row(s.Name, "-", "0", "0", "0", "0", "")
			continue
		}
		t.Row(
			s.Name,
			latest.Tag,
			strconv.Itoa(latest.TotalCveCounts.Critical),
			strconv.Itoa(latest.TotalCveCounts.High),
			strconv.Itoa(latest.TotalCveCounts.Medium),
			strconv.Itoa(latest.TotalCveCounts.Low),
			strconv.Itoa(latest.LowestSloRemaining),
		)
	}
	return t.Render(w)
}

func latestVersion(s Source) *Version {
	var best *Version
	for i := range s.Versions {
		v := &s.Versions[i]
		if best == nil || v.PublishDate > best.PublishDate {
			best = v
		}
	}
	return best
}

// IssueTableCodec renders issues as a table.
type IssueTableCodec struct{}

func (c *IssueTableCodec) Format() format.Format { return "table" }
func (c *IssueTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
func (c *IssueTableCodec) Encode(w io.Writer, v any) error {
	issues, ok := v.([]Issue)
	if !ok {
		return fmt.Errorf("vulnobs: IssueTableCodec: unexpected type %T", v)
	}
	t := style.NewTable("SEV", "CVSS", "CVE", "PACKAGE", "INSTALLED", "FIXED", "TARGET", "TOOL", "SLO")
	for _, iss := range issues {
		t.Row(
			iss.Cve.Severity,
			strconv.FormatFloat(iss.Cve.CvssScore, 'f', -1, 64),
			iss.Cve.CVE,
			iss.Package,
			fallback(iss.InstalledVersion, "-"),
			fallback(iss.FixedVersion, "-"),
			iss.Target,
			iss.Tool.Name,
			strconv.Itoa(iss.SloRemaining),
		)
	}
	return t.Render(w)
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
