package kg

// The graph keeps records about itself next to its other entities, and the
// entity-count endpoint counts them like any other type. `kg stats` and
// the diagnose entity overview count these catalog records apart, and
// `kg meta schema` does not list their types.
//
// Most catalog types can be read from the graph: the schema does not list the
// types of the records that describe the schema itself, so a counted type that
// the schema does not list is a catalog type. The graph's account and
// environment records are the exception: the schema lists their types like
// any other, and the API marks them in no other way, so isGraphRecordType
// names them. It is the package's only list of catalog type names.

// isGraphRecordType reports whether t is the type of the graph's account or
// environment records. processGraphSchema leaves these types out of
// `kg meta schema`, and isCatalogType counts them as catalog types.
func isGraphRecordType(t string) bool {
	switch t {
	case "Account", "Env":
		return true
	default:
		return false
	}
}

// listedSchemaTypes returns the entity types that `kg meta schema` lists for
// the given schema response.
func listedSchemaTypes(resp GraphSchemaResponse) map[string]bool {
	result := processGraphSchema(resp)
	listed := make(map[string]bool, len(result.EntityTypes))
	for _, et := range result.EntityTypes {
		listed[et.Type] = true
	}
	return listed
}

// isCatalogType reports whether a counted entity type holds the graph's own
// catalog records: a type that listed (the types `kg meta schema` lists for
// the same window, see listedSchemaTypes) does not contain. Account and
// environment record types are catalog types either way. A nil listed means
// the schema could not be read; then only those two are.
func isCatalogType(t string, listed map[string]bool) bool {
	if isGraphRecordType(t) {
		return true
	}
	return listed != nil && !listed[t]
}
