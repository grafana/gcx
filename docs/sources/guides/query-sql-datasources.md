---
title: Query SQL data sources
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Execute SQL queries from a file or standard input with gcx.
weight: 7
---

# Query SQL data sources

The MySQL, PostgreSQL, ClickHouse, Athena, Microsoft SQL Server, BigQuery, and Pinot query commands accept SQL from a file with `-f` or `--filename`:

```shell
gcx datasources mysql query -d UID --filename ./query.sql -o json
gcx datasources mssql query -d UID -f ./query.sql -o json
```

Use `-` to read from standard input. With a terminal, finish input with EOF (Ctrl-D on Unix):

```shell
cat query.sql | gcx datasources postgres query -d UID -f - -o json
```

Choose one input source: a positional SQL expression, `--expr`, or `--filename`. Missing, unreadable, or empty files fail before configuration loading or datasource requests.

File input executes verbatim, including comments and line endings, without gcx's default row limit. A warning on stderr makes this explicit. Put any required `LIMIT` or `TOP` clause in the SQL itself. `--filename` cannot be combined with `--limit`, even `--limit 0`. Inline SQL keeps the existing default row limit behavior. The datasource or server can still impose its own limits.
