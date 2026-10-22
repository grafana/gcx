# RFC 025: Providers declare table columns, with typed adapters for wrapper and singleton payloads

**Created**: 2026-09-08
**Status**: accepted
**Bead**: none
**Supersedes**: none

## Context

RFC 008 moved the cloud CLI's providers into gcx. Each provider brought its own code for printing tables.

That left a lot of copies. The provider packages hold 174 codec types today. 152
of them have the same `Decode` method, which only returns an error. 68 have the
same `Format` method, which returns `"wide"` or `"table"`. Around 116 of them do
the same job: take a list, pick some columns, print a table. The only part that
really differs is the columns.

Copies drift. Two files that should print the same thing stop matching, and a
fix to one is not a fix to the others. The narrow and wide versions of a single
table drift too, because they are written as two separate blocks of code.

This is a result of how RFC 008 ported providers, not a break from it. RFC 008 kept each provider's clients and types as they were, and rendering was ported the same way.

## Decision

A provider declares its columns. It does not write a codec.

```go
func PipelineTable() cmdio.Table[Pipeline] {
	return cmdio.Table[Pipeline]{
		Columns: []cmdio.Column[Pipeline]{
			{Header: "ID", Content: func(p Pipeline) string { return p.ID }},
			{Header: "NAME", Content: func(p Pipeline) string { return p.Name }},
			{Header: "MATCHERS", Visible: cmdio.WideOnly, Content: func(p Pipeline) string {
				return strings.Join(p.Matchers, ", ")
			}},
		},
	}
}
```

The pieces:

- `Column.Header` is the heading. `Column.Content` turns one row into one cell.
  Cells are strings, because that is all the table builder takes.
- `Column.Visible` says where a column appears. The default is both the narrow
  and the wide table. `WideOnly` means wide only. `NarrowOnly` means the narrow
  table only.
- `Table.Empty` prints something when the list is empty. Leave it unset to print
  the headings and no rows.
- `RegisterTable` registers the table under `"table"` and `"wide"`.
  `RegisterTableAs` does the same but lets the caller name the narrow one, which
  matters because some commands call theirs `"text"`.

Two rules fall out of this:

**Narrow means "not wide".** It is not a named format. Some commands register
their narrow table as `"table"` and some as `"text"`. A column should not have
to know which, so `NarrowOnly` matches anything that is not `"wide"`.

**One list of columns, not two.** The wide table is the narrow one plus the
columns marked `WideOnly`. Because there is a single list, the two cannot drift
apart. Where a column needs different content in each, write it twice under the
same heading, once `NarrowOnly` and once `WideOnly`.

Codecs made this way only encode. They do not decode. Their `Format` method
returns whatever name they were registered under.

### Golden files are required

Every migration must add golden files, and must capture them **from the old
codec, before changing it**. Then migrate, and run the tests again without
regenerating. If they still pass, the output did not change.

Capturing after the change proves nothing. That is the easy mistake to make.

This is what makes a large diff reviewable. A reviewer can trust that output is
unchanged instead of reading every column by hand.

### What this does not cover

Some codecs stay hand-written. Each is excluded for a reason, so the reason can
be checked again later.

| Excluded | Why |
| --- | --- |
| ~26 codecs that do more than pick columns | They draw trees, group and total rows, or print several tables at once. A list of columns cannot say that. |
| 6 `graph`, 1 `mermaid`, 1 `dot` | They do not draw tables at all. They may share a shape with each other, which would be its own piece of work. |
| 2 codecs in `irm` | They set column widths worked out from the terminal size and from the data. The shared table has no way to say that. |
| 9 places using `tabwriter` directly | They never used the shared table builder, so there is nothing to swap. |
| text codecs | Printing one line is a different job. It needs its own small shared type. |

### Typed payload adapters

29 of the 116 table codecs are handed something other than a plain list. Usually
it is a wrapper like `{"items": [...]}`, which is the shape JSON output has to
keep. The table wants the rows; JSON wants the wrapper.

A separate `output.TableAdapter[P, T]` adapts wrapper and singleton payloads
to table rows:

```go
table := output.TableAdapter[Result, Item]{
    Table: itemTable(),
    Rows: func(result Result) ([]Item, error) {
        return result.Items, nil
    },
}
opts.RegisterCustomCodec("table", table.Codec("table"))
// Every format receives the complete result, including list-level metadata.
return opts.Encode(w, result)
```

`P` is the complete payload type; `T` is one row. The adapter checks the payload
type and runs `Rows` only when its codec is selected. Extraction errors return
before rendering. The underlying table still owns columns, visibility and empty
output; its codec supplies `Format` and `Decode`. JSON/YAML codecs receive the
original payload without extraction. A singleton adapter returns a one-element
slice without changing that object's machine-readable shape.

Slice-based tables continue to use `Table[T]` directly. Register adapted codecs
under the command's existing format names explicitly, including `wide` where it
is already supported. The adapter does not change defaults or add formats.

### Rejected: naming formats per column

An earlier version let a column list the format names it appeared under. Nothing
in the codebase varies that way. Every table has exactly two sets of columns,
narrow and wide. Naming formats per column also broke as soon as a command
registered its narrow table as `"text"`.

### Rejected: two booleans

`WideOnly bool` plus `TableOnly bool` allows both to be set at once, which means
nothing. A single field cannot be in two states.

## Consequences

**Better:**

- One place decides how a table is drawn. A fix lands everywhere.
- The narrow and wide tables cannot drift apart.
- Each migration deletes a type assertion, a `Format` method and a `Decode` stub.
- Golden files make a big diff quick to review.
- A new provider writes a list of columns, not a codec.

**Worse:**

- Commands handed a wrapper or singleton declare a typed row extraction function
  alongside their table, while passing the same payload to every output format.
- Some commands call their narrow table `"table"` and some call it `"text"`.
  This decision works around that split. It does not fix it.

**Still to do:**

- Move the remaining table codecs over, one provider per pull request. Delete
  the old type in the same change, so nobody copies it.
- Give text codecs the same treatment. They are a smaller and simpler job.
- Once the sweep is done, add this as a rule in `CONSTITUTION.md` and enforce it
  with a test. Not before. A test cannot yet tell a codec that should have been
  migrated from one that is meant to stay hand-written, and there are still too
  many of the first kind for the rule to be true.
