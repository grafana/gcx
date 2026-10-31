package postgres

import "fmt"

// BuildDescribeConstraintsQuery returns a PostgreSQL catalog query for
// describe-table --include-constraints. pg_constraint's conkey/confkey
// arrays, unnested with ordinality, preserve the local and referenced column
// order for composite keys. The left lateral joins retain CHECK constraints,
// whose conkey is NULL.
func BuildDescribeConstraintsQuery(schema, table string) string {
	return fmt.Sprintf(`SELECT c.conname AS constraint_name,
       CASE c.contype
         WHEN 'p' THEN 'PRIMARY KEY'
         WHEN 'u' THEN 'UNIQUE'
         WHEN 'f' THEN 'FOREIGN KEY'
         WHEN 'c' THEN 'CHECK'
         WHEN 'x' THEN 'EXCLUDE'
         ELSE c.contype::text
       END AS constraint_type,
       cols.ordinality AS ordinal_position,
       src_att.attname AS column_name,
       ref_ns.nspname AS referenced_namespace,
       ref_cls.relname AS referenced_table,
       ref_att.attname AS referenced_column,
       CASE c.confdeltype
         WHEN 'a' THEN 'NO ACTION'
         WHEN 'r' THEN 'RESTRICT'
         WHEN 'c' THEN 'CASCADE'
         WHEN 'n' THEN 'SET NULL'
         WHEN 'd' THEN 'SET DEFAULT'
         ELSE NULL
       END AS on_delete,
       CASE c.confupdtype
         WHEN 'a' THEN 'NO ACTION'
         WHEN 'r' THEN 'RESTRICT'
         WHEN 'c' THEN 'CASCADE'
         WHEN 'n' THEN 'SET NULL'
         WHEN 'd' THEN 'SET DEFAULT'
         ELSE NULL
       END AS on_update
FROM pg_constraint c
JOIN pg_class src_cls ON src_cls.oid = c.conrelid
JOIN pg_namespace src_ns ON src_ns.oid = src_cls.relnamespace
LEFT JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS cols(attnum, ordinality) ON TRUE
LEFT JOIN pg_attribute src_att
  ON src_att.attrelid = c.conrelid
 AND src_att.attnum = cols.attnum
LEFT JOIN pg_class ref_cls ON ref_cls.oid = c.confrelid
LEFT JOIN pg_namespace ref_ns ON ref_ns.oid = ref_cls.relnamespace
LEFT JOIN LATERAL unnest(c.confkey) WITH ORDINALITY AS ref_cols(attnum, ordinality)
  ON ref_cols.ordinality = cols.ordinality
LEFT JOIN pg_attribute ref_att
  ON ref_att.attrelid = c.confrelid
 AND ref_att.attnum = ref_cols.attnum
WHERE src_ns.nspname = '%s'
  AND src_cls.relname = '%s'
ORDER BY c.conname, cols.ordinality`, EscapeSQLString(schema), EscapeSQLString(table))
}
