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

## Edit the complete saved workflow

Export before editing so references, assets and other saved files are retained:

```bash
gcx agent skills export custom/service-debug ./service-debug-edit
# Edit only the files that need changing in ./service-debug-edit.
gcx agent skills update custom/service-debug --from ./service-debug-edit --dry-run
gcx agent skills update custom/service-debug --from ./service-debug-edit
```

Export requires a new destination outside the managed store and copies the whole snapshot. Keep its
`.gcx-edit.json`: this records the identity and base revision. Update rejects an
export made before a newer saved change; export again into a new directory and
reapply your intended edits. Export afresh for each later editing session.

Review every added, modified and removed file in the preview. Small text changes
include unified diffs; binary, large or over-budget diffs are explicitly omitted
and retain hashes. Read those files from the saved/exported copies if needed.
The total inline diff budget is 16 KiB. Preview does not write files.

An original authored directory without export metadata is an explicit full
replacement and has no stale-edit check. Omitted files, including references,
are removed. Do not reconstruct an edit directory from `get` of SKILL.md alone.

## Inventory and removal

```bash
gcx agent skills list --source custom -o json
gcx agent skills uninstall custom/service-debug --dry-run
gcx agent skills uninstall custom/service-debug --yes
```

Removal deletes the saved copy only. Custom writes require explicit identities.
Untargeted `update`, `install --all` and `uninstall --all` operate on bundled
skills only; retirement does not delete or replace custom content.

The OS releases a writer lock when a process exits, including an abrupt kill.
If an update was interrupted between directory renames, the next explicit
mutation restores the previous snapshot or cleans up its completed backup.
Dry-run reports that recovery is needed without writing. Temporary `.import-*`
directories left by a killed process are inert; do not use them as saved skills.
This protects process interruption; it does not promise power-loss durability.

Imports accept regular local files only, reject symlinks and `.git` directories, and limit a skill to 256 files, 2 MiB per file and 16 MiB total. Invalid metadata or conflicts fail before writing a target. Fix the source and retry rather than bypassing validation.
