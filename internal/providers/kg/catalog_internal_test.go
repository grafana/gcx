package kg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCatalogRecordType(t *testing.T) {
	for _, tt := range []struct {
		typ  string
		want bool
	}{
		{typ: "Account", want: true},
		{typ: "EntityType", want: true},
		{typ: "Env", want: true},
		{typ: "Schema", want: true},
		{typ: "TypeA"},
		{typ: "TypeB"},
		{typ: "account"},
		{typ: ""},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			assert.Equal(t, tt.want, isCatalogRecordType(tt.typ))
		})
	}
}

func TestProcessGraphSchemaLeavesOutCatalogRecordTypes(t *testing.T) {
	id := func(n int64) *int64 { return &n }
	var resp GraphSchemaResponse
	for i, name := range []string{"TypeA", "Account", "EntityType", "Env", "Schema", "TypeB"} {
		resp.Data.Entities = append(resp.Data.Entities, GraphSchemaEntity{ID: id(int64(i + 1)), Name: name})
	}
	resp.Data.Edges = []GraphSchemaEdge{
		{Source: 1, Destination: 6, Type: "TYPE_A"},
		// A relationship that touches a catalog record type is left out, so
		// no relationship prints an unnamed id.
		{Source: 1, Destination: 2, Type: "TYPE_B"},
		{Source: 3, Destination: 6, Type: "TYPE_C"},
		{Source: 4, Destination: 5, Type: "TYPE_D"},
		// An id the schema does not list keeps the id fallback.
		{Source: 1, Destination: 99, Type: "TYPE_E"},
	}

	got := processGraphSchema(resp)

	types := make([]string, 0, len(got.EntityTypes))
	for _, et := range got.EntityTypes {
		types = append(types, et.Type)
	}
	assert.Equal(t, []string{"TypeA", "TypeB"}, types)
	assert.ElementsMatch(t, []string{"TypeA --TYPE_A--> TypeB", "TypeA --TYPE_E--> id:99"}, got.Relationships)
}
