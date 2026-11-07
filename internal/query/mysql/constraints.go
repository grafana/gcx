package mysql

import "fmt"

// BuildDescribeConstraintsQuery returns the information_schema query used by
// describe-constraints. The left joins retain CHECK and other
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
  ON tc.CONSTRAINT_TYPE IN ('PRIMARY KEY', 'UNIQUE', 'FOREIGN KEY')
 AND tc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA
 AND tc.TABLE_NAME = kcu.TABLE_NAME
 AND tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
 AND ((tc.CONSTRAINT_TYPE = 'FOREIGN KEY' AND kcu.REFERENCED_TABLE_NAME IS NOT NULL)
   OR (tc.CONSTRAINT_TYPE IN ('PRIMARY KEY', 'UNIQUE') AND kcu.REFERENCED_TABLE_NAME IS NULL))
LEFT JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
  ON tc.CONSTRAINT_TYPE = 'FOREIGN KEY'
 AND tc.CONSTRAINT_SCHEMA = rc.CONSTRAINT_SCHEMA
 AND tc.TABLE_NAME = rc.TABLE_NAME
 AND tc.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
WHERE tc.TABLE_SCHEMA = '%s'
  AND tc.TABLE_NAME = '%s'
ORDER BY tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE, kcu.ORDINAL_POSITION`, EscapeSQLString(database), EscapeSQLString(table))
}
