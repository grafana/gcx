package output_test

import (
	"bytes"
	"encoding/json"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// oncallUser mirrors the shape of `gcx irm oncall users list`: the fields a
// caller wants live under spec, not at the top level.
type oncallUser struct {
	Spec struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	} `json:"spec"`
}

func newOnCallUsers(names ...string) []oncallUser {
	users := make([]oncallUser, len(names))
	for i, name := range names {
		users[i].Spec.Username = name
		users[i].Spec.Email = name + "@example.com"
	}
	return users
}

// omitemptyItem holds a field that a row leaves out when it is empty. The
// type still declares the field, so selection must keep it.
type omitemptyItem struct {
	Name string   `json:"name"`
	Tags []string `json:"tags,omitempty"`
}

// nestedOmitemptyItem puts the same field one level down, so the walk into
// the field type is exercised as well.
type nestedOmitemptyItem struct {
	Spec struct {
		Tags []string `json:"tags,omitempty"`
	} `json:"spec"`
}

// twoNameItem declares the same leaf name under two paths, so the suggestion
// must name both.
type twoNameItem struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Name string `json:"name"`
	} `json:"spec"`
}

// arrayItem holds its values inside an array, which field selection cannot
// walk into.
type arrayItem struct {
	Rows []struct {
		Value string `json:"value"`
	} `json:"rows"`
}

// TestLeafNameInsteadOfPathFails is the regression test for the reported
// defect. `--json username,email` returned one null per row and no error, so
// a script that searched the result found nothing and reported zero.
func TestLeafNameInsteadOfPathFails(t *testing.T) {
	codec := cmdio.NewFieldSelectCodec([]string{"username", "email"})

	var buf bytes.Buffer
	err := codec.Encode(&buf, newOnCallUsers("ward", "dafydd"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown field(s) in --json: username, email")
	assert.Contains(t, err.Error(), "Did you mean spec.username for username; spec.email for email?")
	assert.Contains(t, err.Error(), "--json list")
	assert.Empty(t, buf.String(), "a rejected selection must write nothing")
}

// TestDottedPathSucceeds is the other half: the correct paths still work.
func TestDottedPathSucceeds(t *testing.T) {
	codec := cmdio.NewFieldSelectCodec([]string{"spec.username", "spec.email"})

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, newOnCallUsers("ward")))

	var got []map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "ward", got[0]["spec.username"])
	assert.Equal(t, "ward@example.com", got[0]["spec.email"])
}

func TestAbsentFieldSelection(t *testing.T) {
	tests := []struct {
		name    string
		fields  []string
		value   any
		wantErr string
		// wantOK marks the cases that must still encode.
		wantOK bool
	}{
		{
			name:    "an empty result set still answers from the item type",
			fields:  []string{"username"},
			value:   []oncallUser{},
			wantErr: "unknown field(s) in --json: username. Did you mean spec.username?",
		},
		{
			name:   "a path only some objects carry is kept",
			fields: []string{"a", "b"},
			value: []map[string]any{
				{"a": 1},
				{"b": 2},
			},
			wantOK: true,
		},
		{
			name:   "a path that exists and holds null is kept",
			fields: []string{"a"},
			value:  []map[string]any{{"a": nil}},
			wantOK: true,
		},
		{
			name:   "an omitempty field that no row emits is kept",
			fields: []string{"name", "tags"},
			value:  []omitemptyItem{{Name: "a"}, {Name: "b"}},
			wantOK: true,
		},
		{
			name:   "an omitempty field under a nested path is kept",
			fields: []string{"spec.tags"},
			value:  []nestedOmitemptyItem{{}},
			wantOK: true,
		},
		{
			name:   "an empty object declares no type, so the path is kept",
			fields: []string{"anything"},
			value:  map[string]any{},
			wantOK: true,
		},
		{
			name:   "a dynamic map declares no type, so the path is kept",
			fields: []string{"a", "missing"},
			value:  []map[string]any{{"a": 1}, {"a": 2}},
			wantOK: true,
		},
		{
			name:    "a path the item type does not declare fails",
			fields:  []string{"bogus"},
			value:   []omitemptyItem{{Name: "a"}},
			wantErr: "unknown field(s) in --json: bogus",
		},
		{
			name:    "a dotted path the caller wrote gets no candidate",
			fields:  []string{"spec.nope"},
			value:   []oncallUser{{}},
			wantErr: "unknown field(s) in --json: spec.nope. Run --json list",
		},
		{
			name:    "each absent name keeps its own candidates",
			fields:  []string{"username", "bogus"},
			value:   newOnCallUsers("ward"),
			wantErr: "unknown field(s) in --json: username, bogus. Did you mean spec.username for username?",
		},
		{
			name:    "a leaf name that matches two paths names both",
			fields:  []string{"name"},
			value:   []twoNameItem{{}},
			wantErr: "Did you mean metadata.name, spec.name?",
		},
		{
			name:    "a path that continues past an array names --jq",
			fields:  []string{"rows.value"},
			value:   []arrayItem{{}},
			wantErr: "--json cannot reach a value inside an array: rows.value. Use --jq",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := cmdio.NewFieldSelectCodec(tt.fields)
			var buf bytes.Buffer
			err := codec.Encode(&buf, tt.value)

			if tt.wantOK {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestValidatorAcceptsDeclaredPaths covers the validator route: it must
// accept an omitempty field and a nested path that the type declares, and it
// must name the real path for a leaf name.
func TestValidatorAcceptsDeclaredPaths(t *testing.T) {
	validator := cmdio.MakeFieldValidator(nestedOmitemptyItem{})
	require.NotNil(t, validator)

	require.NoError(t, validator([]string{"spec.tags"}))

	err := validator([]string{"tags"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Did you mean spec.tags?")
}

// TestValidatorRejectsPathInsideArray covers the validator route for a path
// that continues past an array. The message must name --jq, because --json
// cannot reach such a value.
func TestValidatorRejectsPathInsideArray(t *testing.T) {
	validator := cmdio.MakeFieldValidator(arrayItem{})
	require.NotNil(t, validator)

	err := validator([]string{"rows.value"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--json cannot reach a value inside an array: rows.value")
	assert.Contains(t, err.Error(), "--jq")
}

// TestEmptyTypedListRejectsUnknownPath is the `gcx datasources list --json
// bogus` case against an empty result set. The item type is reachable through
// the envelope, so the answer must not depend on how many rows the server
// returned.
func TestEmptyTypedListRejectsUnknownPath(t *testing.T) {
	type datasource struct {
		UID  string `json:"uid"`
		Name string `json:"name"`
	}
	// The command emits an empty array, never null, for an empty page.
	envelope := struct {
		Datasources []datasource `json:"datasources"`
	}{Datasources: []datasource{}}

	codec := cmdio.NewFieldSelectCodec([]string{"bogus"})
	var buf bytes.Buffer
	err := codec.Encode(&buf, &envelope)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown field(s) in --json: bogus")
	assert.Empty(t, buf.String(), "a rejected selection must write nothing")

	// The same flag must be accepted for a declared path, empty set or not.
	okCodec := cmdio.NewFieldSelectCodec([]string{"uid"})
	buf.Reset()
	require.NoError(t, okCodec.Encode(&buf, &envelope))
	assert.JSONEq(t, `{"datasources":[]}`, buf.String())
}

// TestAbsentFieldSelectionOnSingleObject covers the single-object branches,
// which take a different route through Encode than the list branches.
func TestAbsentFieldSelectionOnSingleObject(t *testing.T) {
	codec := cmdio.NewFieldSelectCodec([]string{"username"})

	var buf bytes.Buffer
	err := codec.Encode(&buf, oncallUser{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Did you mean spec.username?")
}

// TestEmptyUnstructuredSliceKeepsEveryPath is the list counterpart of
// TestUnstructuredSingleObjectKeepsUnsetPath. An unstructured object declares
// no field set — its only field is an untagged map — so a slice of them must
// reject nothing, even when the slice is empty. `gcx irm oncall users list`
// and the synthetic monitoring list commands take this route.
func TestEmptyUnstructuredSliceKeepsEveryPath(t *testing.T) {
	codec := cmdio.NewFieldSelectCodec([]string{"spec.username"})

	for name, value := range map[string]any{
		"an empty slice": []unstructured.Unstructured{},
		"a nil slice":    []unstructured.Unstructured(nil),
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, value))
		})
	}
}

// TestUnstructuredSingleObjectKeepsUnsetPath is the regression test for the
// single-resource route of `gcx resources get`. The route encodes one
// unstructured item, which declares no type, so an unset omitempty path such
// as spec.tags must keep its null and exit 0. The list route keeps it too,
// because some other object carries the key.
func TestUnstructuredSingleObjectKeepsUnsetPath(t *testing.T) {
	codec := cmdio.NewFieldSelectCodec([]string{"spec.tags"})

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "untagged"},
		"spec":     map[string]any{"title": "Untagged"},
	}}))

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Contains(t, got, "spec.tags")
	assert.Nil(t, got["spec.tags"])
}
