// Package vulnobs provides a client for Grafana Vulnerability Observability
// (the grafana-vulnerabilityobs-app plugin) via its plugin-proxied GraphQL
// endpoint.
package vulnobs

// Group is a vulnerability-obs team/group (tag namespace for sources).
type Group struct {
	ID   int    `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
}

// GetResourceName implements adapter.ResourceNamer for Group. Groups are not
// typed-registered (see ADR-003), but the helper is useful for output codecs
// and consistency with other domain types.
func (g Group) GetResourceName() string { return g.Name }

// CveCounts is the rollup of findings by severity for a Version.
type CveCounts struct {
	Critical int `json:"critical" yaml:"critical"`
	High     int `json:"high" yaml:"high"`
	Medium   int `json:"medium" yaml:"medium"`
	Low      int `json:"low" yaml:"low"`
}

// Integration is the upstream connection through which a Source was ingested.
type Integration struct {
	ID   int    `json:"id" yaml:"id"`
	Name string `json:"name" yaml:"name"`
	Type string `json:"type" yaml:"type"`
}

// Version is a single scanned version (tag) of a Source.
type Version struct {
	ID                 int       `json:"id" yaml:"id"`
	Tag                string    `json:"tag" yaml:"tag"`
	PublishDate        string    `json:"publishDate,omitempty" yaml:"publishDate,omitempty"`
	LowestSloRemaining int       `json:"lowestSloRemaining" yaml:"lowestSloRemaining"`
	TotalCveCounts     CveCounts `json:"totalCveCounts" yaml:"totalCveCounts"`
}

// Source is a scanned project (typically a Git repository).
//
// GetResourceName must be value-receiver to satisfy adapter.ResourceNamer
// (the TypedCRUD[T] constraint), and SetResourceName must be pointer-receiver
// to mutate the underlying field. Matches the KG provider's Rule pattern.
//
//nolint:recvcheck // ResourceIdentity requires the mixed-receiver shape:
type Source struct {
	ID          int         `json:"id" yaml:"id"`
	Name        string      `json:"name" yaml:"name"`
	Type        string      `json:"type" yaml:"type"`
	Origin      string      `json:"origin" yaml:"origin"`
	Visibility  string      `json:"visibility" yaml:"visibility"`
	Integration Integration `json:"integration" yaml:"integration"`
	Groups      []Group     `json:"groups" yaml:"groups"`
	Versions    []Version   `json:"versions" yaml:"versions"`
}

// GetResourceName satisfies adapter.ResourceIdentity. The Source name (e.g.
// "grafana/faro-web-sdk") is stable across scans, but contains a slash which
// is not a valid k8s metadata.name character — substitute with "--" for the
// resource identity while keeping the original in the spec.
//
// Value receiver is required so Source satisfies adapter.ResourceNamer used
// as the TypedCRUD[T] constraint. SetResourceName uses a pointer receiver to
// mutate the underlying field, matching the KG provider's Rule pattern.
//

func (s Source) GetResourceName() string { return resourceNameFromSource(s.Name) }

// SetResourceName implements adapter.ResourceIdentity. Restores the original
// "owner/repo" form from the metadata.name placeholder.
func (s *Source) SetResourceName(name string) { s.Name = sourceNameFromResource(name) }

// Cve is metadata about a single CVE assignment for an Issue.
type Cve struct {
	CVE       string  `json:"cve" yaml:"cve"`
	Severity  string  `json:"severity" yaml:"severity"`
	CvssScore float64 `json:"cvssScore" yaml:"cvssScore"`
	Title     string  `json:"title,omitempty" yaml:"title,omitempty"`
}

// Tool identifies the scanner that produced an Issue.
type Tool struct {
	Name string `json:"name" yaml:"name"`
}

// Issue is a single CVE finding reported by a scanner against a Version.
// Issues are sub-resources of Source.Versions[] and are not typed-registered
// (see ADR-003); upstream IssueFilters require a versionId.
type Issue struct {
	ID               int    `json:"id" yaml:"id"`
	Package          string `json:"package" yaml:"package"`
	InstalledVersion string `json:"installedVersion,omitempty" yaml:"installedVersion,omitempty"`
	FixedVersion     string `json:"fixedVersion,omitempty" yaml:"fixedVersion,omitempty"`
	Target           string `json:"target" yaml:"target"`
	SloRemaining     int    `json:"sloRemaining" yaml:"sloRemaining"`
	Tool             Tool   `json:"tool" yaml:"tool"`
	Cve              Cve    `json:"cve" yaml:"cve"`
}

// SourceFilters is the input shape for the `sources` query. See ADR-002 /
// research notes for the empirically-verified subset of fields; only the
// fields actually accepted by the upstream are present here.
type SourceFilters struct {
	GroupID        string          `json:"groupId,omitempty"`
	Name           string          `json:"name,omitempty"`
	SortBy         string          `json:"sortBy,omitempty"`
	EnabledOnly    bool            `json:"enabledOnly"`
	VersionFilters *VersionFilters `json:"versionFilters,omitempty"`
}

// VersionFilters is the nested filter on versions returned per Source.
type VersionFilters struct {
	HideK8s      bool `json:"hideK8s"`
	ShowArchived bool `json:"showArchived"`
}

// IssueFilters is the input shape for the `issues` query.
type IssueFilters struct {
	VersionID string `json:"versionId"`
}

// resourceNameFromSource turns "grafana/faro-web-sdk" into "grafana--faro-web-sdk"
// for use as a k8s metadata.name (which forbids "/").
func resourceNameFromSource(s string) string {
	out := make([]byte, 0, len(s)+1)
	for i := range len(s) {
		if s[i] == '/' {
			out = append(out, '-', '-')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// sourceNameFromResource is the inverse of resourceNameFromSource. The
// substitution is a non-injective mapping in pathological cases (a real
// repo named "foo--bar" would round-trip to "foo--bar"), so we only
// replace the first occurrence of "--" — the GitHub convention is
// exactly one slash separating owner from repo.
func sourceNameFromResource(s string) string {
	for i := range len(s) - 1 {
		if s[i] == '-' && s[i+1] == '-' {
			return s[:i] + "/" + s[i+2:]
		}
	}
	return s
}
