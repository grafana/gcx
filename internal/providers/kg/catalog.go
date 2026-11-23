package kg

// The graph keeps its own catalog records next to observed entities, as
// nodes with their own labels, and the API marks them in no other way. A node
// pattern without a label matches them, the entity-count endpoint counts them,
// and the schema endpoint can list their types. isCatalogRecordType names their
// types; it is the package's only list of catalog type names.

// isCatalogRecordType reports whether t is the type, or node label, of the
// graph's own catalog records rather than of observed entities.
// processGraphSchema leaves these types out of `kg meta schema`, the diagnose
// entity overview does not count them, and `kg graph query` warns about rows
// that hold them (see catalogRowsWarning).
func isCatalogRecordType(t string) bool {
	switch t {
	case "Account", "EntityType", "Env", "Schema":
		return true
	default:
		return false
	}
}
