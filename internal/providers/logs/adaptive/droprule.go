package logs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// GlobalDropRuleSegmentID is the segment ID used for tenant-wide adaptive log drop rules.
// The drop-rules CLI commands always use this segment for now.
const GlobalDropRuleSegmentID = "__global__"

// DropRuleListQuery filters the adaptive logs drop rules list endpoint.
type DropRuleListQuery struct {
	SegmentID string
	// ExpirationFilter is one of: all, active, expired. Empty defaults to "all" in the client.
	ExpirationFilter string
}

// DropRuleBodyV1 is the version 1 JSON policy body for adaptive log drop rules
// (log-template-service pkg/droprule.PolicyBodyV1).
type DropRuleBodyV1 struct {
	DropRate        float64  `json:"drop_rate"`
	StreamSelector  string   `json:"stream_selector"`
	Levels          []string `json:"levels"`
	LogLineContains []string `json:"log_line_contains,omitempty"`
}

// DropRuleFileSpec is the YAML/JSON document for
// `gcx logs adaptive drop-rules create|update -f` (aligned with Adaptive Traces policy files).
// It matches the API create/update fields; for create, segment_id defaults to GlobalDropRuleSegmentID
// when omitted, and version defaults to 1 when omitted or zero.
//
// Version is the policy body schema version accepted by the API (only 1 today). It is not the
// rule revision returned in list/get JSON — do not bump it to "update" a rule.
type DropRuleFileSpec struct {
	Version   int            `json:"version"`
	Name      string         `json:"name"`
	Body      DropRuleBodyV1 `json:"body"`
	ExpiresAt string         `json:"expires_at,omitempty"`
	Disabled  bool           `json:"disabled,omitempty"`
	SegmentID string         `json:"segment_id,omitempty"`
}

const maxDropRuleFileSize = 10 << 20 // 10 MiB, aligned with adaptive traces policy files

// ReadDropRuleFileSpecFromReader decodes a DropRuleFileSpec from an io.Reader (YAML or JSON). Exported for testing.
func ReadDropRuleFileSpecFromReader(reader io.Reader) (*DropRuleFileSpec, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxDropRuleFileSize))
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	var spec DropRuleFileSpec
	yamlCodec := format.NewYAMLCodec()
	if err := yamlCodec.Decode(strings.NewReader(string(data)), &spec); err != nil {
		return nil, fmt.Errorf("decoding input: %w", err)
	}

	return &spec, nil
}

func validateDropRuleFileSpecCreate(s *DropRuleFileSpec) error {
	if s.Name == "" {
		return errors.New("name is required in the file")
	}
	if err := validateDropRuleFilePolicySchemaVersion(s.Version); err != nil {
		return err
	}
	if s.SegmentID != "" && s.SegmentID != GlobalDropRuleSegmentID {
		return fmt.Errorf("segment_id must be %q or omitted", GlobalDropRuleSegmentID)
	}
	return nil
}

func validateDropRuleFileSpecUpdate(s *DropRuleFileSpec) error {
	if s.Name == "" {
		return errors.New("name is required in the file")
	}
	if err := validateDropRuleFilePolicySchemaVersion(s.Version); err != nil {
		return err
	}
	return nil
}

func validateDropRuleFilePolicySchemaVersion(v int) error {
	if v == 0 || v == 1 {
		return nil
	}
	return fmt.Errorf(`"version" in the file is the policy schema version (only 1 is supported); use 1 or omit, not %d`, v)
}

func dropRuleFileSpecToCreate(s *DropRuleFileSpec) DropRule {
	ver := s.Version
	if ver == 0 {
		ver = 1
	}
	seg := GlobalDropRuleSegmentID
	if s.SegmentID != "" {
		seg = s.SegmentID
	}
	return DropRule{
		SegmentID: seg,
		Version:   ver,
		Name:      s.Name,
		Body:      s.Body,
		ExpiresAt: s.ExpiresAt,
		Disabled:  s.Disabled,
	}
}

func dropRuleFileSpecToUpdate(s *DropRuleFileSpec) DropRule {
	ver := s.Version
	if ver == 0 {
		ver = 1
	}
	return DropRule{
		Version:   ver,
		Name:      s.Name,
		Body:      s.Body,
		ExpiresAt: s.ExpiresAt,
		Disabled:  s.Disabled,
	}
}

// ValueForJSONFieldDiscovery returns a value for cmdio JSON field discovery (--json ?) on drop rule
// list/get so optional fields (expires_at, disabled) appear even when JSON omitempty would omit them.
func ValueForJSONFieldDiscovery(rules []DropRule) any {
	if len(rules) == 0 {
		return map[string]any{
			"id":         "",
			"tenant_id":  "",
			"segment_id": "",
			"version":    0,
			"name":       "",
			"body":       map[string]any{},
			"created_at": "",
			"updated_at": "",
			"expires_at": "",
			"disabled":   false,
		}
	}
	r := rules[0]
	return map[string]any{
		"id":         r.ID,
		"tenant_id":  r.TenantID,
		"segment_id": r.SegmentID,
		"version":    r.Version,
		"name":       r.Name,
		"body":       dropRuleBodyToMap(r.Body),
		"created_at": r.CreatedAt,
		"updated_at": r.UpdatedAt,
		"expires_at": r.ExpiresAt,
		"disabled":   r.Disabled,
	}
}

func dropRuleBodyToMap(b DropRuleBodyV1) map[string]any {
	data, err := json.Marshal(b)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{}
	}
	return m
}

// DropRule is an Adaptive Logs drop rule (HTTP: /adaptive-logs/drop-rules).
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type DropRule struct {
	ID        string         `json:"id,omitempty"`
	TenantID  string         `json:"tenant_id,omitempty"`
	SegmentID string         `json:"segment_id,omitempty"`
	Version   int            `json:"version"`
	Name      string         `json:"name"`
	Body      DropRuleBodyV1 `json:"body"`
	CreatedAt string         `json:"created_at,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
	ExpiresAt string         `json:"expires_at,omitempty"`
	Disabled  bool           `json:"disabled,omitempty"`
}

// GetResourceName implements adapter.ResourceNamer for TypedCRUD compatibility.
func (d DropRule) GetResourceName() string { return d.ID }

// SetResourceName implements adapter.ResourceIdentity for TypedCRUD compatibility.
func (d *DropRule) SetResourceName(name string) { d.ID = name }

// Compile-time assertion: DropRule implements adapter.ResourceIdentity.
var _ adapter.ResourceIdentity = &DropRule{}

func dropRuleCreatePayload(dr *DropRule) ([]byte, error) {
	type payload struct {
		SegmentID string         `json:"segment_id"`
		Version   int            `json:"version"`
		Name      string         `json:"name"`
		Body      DropRuleBodyV1 `json:"body"`
		ExpiresAt string         `json:"expires_at,omitempty"`
		Disabled  bool           `json:"disabled,omitempty"`
	}
	p := payload{
		SegmentID: dr.SegmentID,
		Version:   dr.Version,
		Name:      dr.Name,
		Body:      dr.Body,
		ExpiresAt: dr.ExpiresAt,
		Disabled:  dr.Disabled,
	}
	return json.Marshal(p)
}

func dropRuleUpdatePayload(dr *DropRule) ([]byte, error) {
	type payload struct {
		Version   int            `json:"version"`
		Name      string         `json:"name"`
		Body      DropRuleBodyV1 `json:"body"`
		ExpiresAt string         `json:"expires_at,omitempty"`
		// No omitempty: false must serialize so re-enabling a rule sends "disabled": false.
		Disabled bool `json:"disabled"`
	}
	p := payload{
		Version:   dr.Version,
		Name:      dr.Name,
		Body:      dr.Body,
		ExpiresAt: dr.ExpiresAt,
		Disabled:  dr.Disabled,
	}
	return json.Marshal(p)
}
