---
title: Inspect SQL table constraints
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Inspect declared MySQL and PostgreSQL keys and relationships with gcx.
weight: 8
---

# Inspect SQL table constraints

Use `describe-constraints` to inspect declared MySQL and PostgreSQL primary, unique, check, and foreign-key constraints:

```shell
gcx datasources mysql describe-constraints mydb.orders -d UID
gcx datasources postgres describe-constraints public.orders -d UID -o json
```

Select the database or schema explicitly, either in the table argument or with `--database` (MySQL) or `--schema` (PostgreSQL). The SQL datasource identity must have permission to read the table's catalog metadata.

```shell
gcx datasources mysql describe-constraints orders --database mydb -d UID -o yaml
gcx datasources postgres describe-constraints orders --schema public -d UID -o wide
```

The command fetches the same metadata in every output format. Table and wide output show one row per constraint, including ordered local and referenced columns and foreign-key actions. JSON, YAML, and agent output return a `table` identity and a `constraints` array. An existing table without declared constraints returns `"constraints": []`; a missing table returns an error.

Only declared metadata is returned. The command does not infer relationships from matching column names or sampled values. MySQL CHECK constraints do not expose key-column metadata, so their `columns` array is empty. Other SQL dialects are not supported by this command yet.

Use the existing `describe-table` command to inspect columns. Its output and behavior are unchanged.
