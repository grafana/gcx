package resources

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// InvalidSelectorError is an error that occurs when a command is invalid.
type InvalidSelectorError struct {
	Command string
	Err     string
}

func (e InvalidSelectorError) Error() string {
	return fmt.Sprintf("invalid command '%s': %s", e.Command, e.Err)
}

// Selectors is a list of resource selectors.
type Selectors []Selector

func (s Selectors) HasNamedSelectorsOnly() bool {
	if len(s) == 0 {
		return false
	}

	for _, selector := range s {
		if !selector.IsNamedSelector() {
			return false
		}
	}

	return true
}

// IsSingleTarget returns true if the selector is to get a single resource.
func (s Selectors) IsSingleTarget() bool {
	if len(s) != 1 {
		return false
	}

	return s[0].Type == FilterTypeSingle
}

// ParseSelectors parses a list of resource selector strings into a list of Selectors.
func ParseSelectors(sels []string) (Selectors, error) {
	if len(sels) == 0 {
		return []Selector{}, nil
	}

	res := make([]Selector, len(sels))

	for i, sel := range sels {
		if err := res[i].ParseString(sel); err != nil {
			return nil, err
		}
	}

	return res, nil
}

// Selector is a selector to select a resource from Grafana.
// Unlike Filter, selectors use the PartialGVK to identify the resource type,
// which only describes the target API resource partially and does not guarantee
// that the it is supported.
type Selector struct {
	Type             FilterType
	GroupVersionKind PartialGVK
	ResourceUIDs     []string
}

func (sel *Selector) IsNamedSelector() bool {
	return len(sel.ResourceUIDs) != 0
}

func (sel *Selector) String() string {
	if sel == nil {
		return ""
	}

	cmd := sel.GroupVersionKind.String()
	if len(sel.ResourceUIDs) > 0 {
		cmd += "/" + strings.Join(sel.ResourceUIDs, ",")
	}

	return cmd
}

// ParseString parses the selector from a string.
func (sel *Selector) ParseString(src string) error {
	parts := strings.Split(src, "/")

	switch len(parts) {
	case 0:
		return InvalidSelectorError{Command: src, Err: "missing resource type"}
	case 1:
		if err := sel.GroupVersionKind.ParseString(parts[0]); err != nil {
			return InvalidSelectorError{Command: src, Err: err.Error()}
		}

		sel.Type = FilterTypeAll
		sel.ResourceUIDs = []string{}

		return nil
	case 2:
		if parts[1] == "" {
			return InvalidSelectorError{Command: src, Err: "missing resource UID(s)"}
		}

		if err := sel.GroupVersionKind.ParseString(parts[0]); err != nil {
			return InvalidSelectorError{Command: src, Err: err.Error()}
		}

		uids, err := parseUIDs(parts[1])
		if err != nil {
			return InvalidSelectorError{Command: src, Err: err.Error()}
		}

		sel.ResourceUIDs = uids
		if len(sel.ResourceUIDs) > 1 {
			sel.Type = FilterTypeMultiple
		} else {
			sel.Type = FilterTypeSingle
		}

		return nil
	}

	return InvalidSelectorError{
		Command: src,
		Err:     fmt.Sprintf("invalid command '%s'", parts),
	}
}

// PartialGVK is a partial identifier of an API resource.
// Not all fields are required to be set.
// It is expected that anything that accepts a PartialGVK
// will handle the discovery of the resource based on the fields that are present.
type PartialGVK struct {
	// Group represents the API group.
	// It may or may not be set, depending on the user input.
	// It can also be in a short or long format.
	Group string

	// Version represents the API version.
	// It may or may not be set, depending on the user input.
	Version string

	// Resource is any identifier of API resource.
	// It may be one of (kind, singular, plural).
	Resource string

	// FallbackGroup retains the resource.group reading of an ambiguous parsed
	// selector. Structured group/version identifiers leave it empty to require
	// an exact match rather than reinterpreting their version as part of a group.
	FallbackGroup string
}

func (gvk *PartialGVK) String() string {
	if gvk == nil {
		return ""
	}

	var build strings.Builder

	build.WriteString(gvk.Resource)
	if gvk.Version != "" {
		build.WriteString(".")
		build.WriteString(gvk.Version)
	}
	if gvk.Group != "" {
		build.WriteString(".")
		build.WriteString(gvk.Group)
	}

	return build.String()
}

// GroupOnlyCandidate returns the alternative resource.group reading of a
// resource.version.group identifier. Discovery must try the versioned reading
// first, since a group's first label can also look like a version.
func (gvk *PartialGVK) GroupOnlyCandidate() (PartialGVK, bool) {
	if gvk.FallbackGroup == "" {
		return PartialGVK{}, false
	}
	return PartialGVK{
		Resource: gvk.Resource,
		Group:    gvk.FallbackGroup,
	}, true
}

// ParseString parses a PartialGVK from a string. For identifiers with at least
// two dots, it retains the resource.version.group reading; discovery also tries
// GroupOnlyCandidate when that reading is not supported.
func (gvk *PartialGVK) ParseString(src string) error {
	parts := strings.SplitN(src, ".", 3)

	switch len(parts) {
	case 1:
		if len(parts[0]) == 0 {
			return errors.New("must specify API resource identifier")
		}

		gvk.Group = ""
		gvk.Version = ""
		gvk.Resource = parts[0]
		gvk.FallbackGroup = ""
	case 2:
		if len(parts[0]) == 0 {
			return errors.New("must specify API resource identifier")
		}

		if len(parts[1]) == 0 {
			return errors.New("must specify API resource group")
		}

		gvk.Group = parts[1]
		gvk.Version = "" // Default version
		gvk.Resource = parts[0]
		gvk.FallbackGroup = ""
	case 3:
		if len(parts[0]) == 0 {
			return errors.New("must specify API resource identifier")
		}

		if len(parts[1]) == 0 {
			return errors.New("must specify API resource version")
		}

		if len(parts[2]) == 0 {
			return errors.New("must specify API resource group")
		}

		gvk.Group = parts[2]
		gvk.Version = parts[1]
		gvk.Resource = parts[0]
		gvk.FallbackGroup = parts[1] + "." + parts[2]
	default:
		return errors.New("invalid API resource identifier")
	}

	return nil
}

func parseUIDs(uids string) ([]string, error) {
	if uids == "" {
		return nil, errors.New("missing resource UID(s)")
	}

	res := strings.Split(uids, ",")
	if slices.Contains(res, "") {
		return nil, errors.New("missing resource UID")
	}

	return res, nil
}
