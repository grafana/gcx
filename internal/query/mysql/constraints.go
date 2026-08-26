package mysql

import "fmt"

// BuildDescribeConstraintsQuery returns the information_schema query used by
// describe-table --include-constraints. The left joins retain CHECK and other
// constraints which do not expose a KEY_COLUMN_USAGE row; foreign-key target
// columns and referential actions are populated when available.
func BuildDescribeConstraintsQuery(database, table string) string {
	return fmt.Sprintf(`SELECT tc.CONSTRAINT_NAME AS constraint_name,
       tc.CONSTRAINT_TYPE AS constraint_type,
       kcu.ORDINAL_POSITION AS ordinal_position,
       kcu.COLUMN_NAME AS column_name,
       kcu.REFERENCED_TABLE_SCHEMA AS referenced_namespace,
       kcu.REFERENCED_TABLE_NAME AS referenced_table,
       kcu.REFERENCED_COLUMN_NAME AS referenced_column,
       rc.DELETE_RULE AS on_delete,
       rc.UPDATE_RULE AS on_update
FROM information_schema.TABLE_CONSTRAINTS tc
LEFT JOIN information_schema.KEY_COLUMN_USAGE kcu
  ON tc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA
 AND tc.TABLE_NAME = kcu.TABLE_NAME
 AND tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
LEFT JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
  ON tc.CONSTRAINT_SCHEMA = rc.CONSTRAINT_SCHEMA
 AND tc.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
WHERE tc.TABLE_SCHEMA = '%s'
  AND tc.TABLE_NAME = '%s'
ORDER BY tc.CONSTRAINT_NAME, kcu.ORDINAL_POSITION`, EscapeSQLString(database), EscapeSQLString(table))
}
