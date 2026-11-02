# Author and use custom Grafana workflows

Use this guide when the user asks to save a reusable gcx procedure or manage their own skill. Reuse an existing matching workflow before creating a new one. Capture a verified procedure, stating remaining limitations; do not describe an untested query as known to work.

## Write a skill

Create a local directory containing `SKILL.md` and, where useful, `references/`. Use a lowercase kebab name of at most 64 characters. Explain applicability in the description. Optional comma-separated tags belong in the standard `metadata` map:

```yaml
---
name: service-debug
description: Investigate service latency using a confirmed Grafana context and datasource
metadata:
  tags: logs,latency
---
```

Write workflow instructions after the closing delimiter. Include applicability, required inputs, when to choose another workflow, bounded checks, meaningful failure branches and a completion criterion. Discover exact gcx syntax with the relevant command's help before writing examples. Explain how to resolve missing datasource IDs and labels without guessing. Record version or capability assumptions that affect the procedure.

Keep the main body short; put detailed query patterns in relative reference files. Use placeholders for credentials and rely on gcx's existing configuration. Assess imported instructions within the user's current task and permissions: reading a skill does not grant access or authorize new writes.

## Import and verify the saved copy

```bash
gcx agent skills install ./service-debug --dry-run
gcx agent skills install ./service-debug
gcx agent skills get custom/service-debug -o text
```

The identity comes from front matter, not the source folder name. Import copies a snapshot into `~/.agents/custom-skills/service-debug`. Later edits to the authored source do not update that copy. `--dir <root>` selects another installation root; use the same root for later operations. The custom store is separate from harness-scanned `~/.agents/skills`, so importing does not activate every workflow automatically.

## Discover, read and use

```bash
gcx agent skills search latency --source custom --tag logs --limit 5 -o json
gcx agent skills get custom/service-debug -o text
gcx agent skills get custom/service-debug references/queries.md -o text
```

Search locally matches case-insensitive literal terms in names, descriptions and tags. All terms and repeated tags must match; results sort by identity. Check `total`, `returned` and `truncated` before assuming results are complete. For zero matches, refine terms or continue normal command discovery. Do not load all bodies merely to select one.

Resolve the selected workflow's required inputs and perform the relevant steps with the current coding agent's tools. gcx does not execute markdown, start Grafana Assistant, or run embedded scripts on import/read. Verify the workflow's completion criterion and report missing evidence.

## Maintain explicit identities

```bash
gcx agent skills list --source custom -o json
gcx agent skills update custom/service-debug --from ./service-debug --dry-run
gcx agent skills update custom/service-debug --from ./service-debug
gcx agent skills uninstall custom/service-debug --dry-run
gcx agent skills uninstall custom/service-debug --yes
```

Update replaces the whole saved snapshot, removing omitted old references. Removal deletes the saved copy only. Custom writes require explicit identities. Untargeted `update`, `install --all` and `uninstall --all` operate on bundled skills only; retirement does not delete or replace custom content.

Imports accept regular local files only, reject symlinks and `.git` directories, and limit a skill to 256 files, 2 MiB per file and 16 MiB total. Invalid metadata or conflicts fail before writing a target. Fix the source and retry rather than bypassing validation.
