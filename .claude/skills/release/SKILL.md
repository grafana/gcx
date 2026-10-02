---
name: release
description: Use when a gcx release request needs stage selection, including "release patch/minor/major", release PR refreshes, or explicit publication and resumption requests.
---

# Releasing gcx

Release preparation and publication are separate invocations. Read the selected skill before acting.

| Request | Skill and stopping point |
|---|---|
| “Prepare a patch release” | [prepare-release](../prepare-release/SKILL.md): open the release PR; stop for review |
| “Refresh release PR #123” | [prepare-release](../prepare-release/SKILL.md): update artifacts and required communications; renewed review |
| “Publish approved release PR #123” | [publish-release](../publish-release/SKILL.md): merge, tag, verify publication, tap, communications handoff and announcement |
| “Resume publishing release PR #123” | [publish-release](../publish-release/SKILL.md): verify live state and finish only outstanding steps |

For a new “release patch/minor/major” request, start preparation. If an existing release PR makes the intended stage ambiguous, identify it and ask whether the user means refresh or publication **before external writes**. PR approval does not itself invoke publication.

These are repository contributor skills, not bundled end-user skills. When modifying them, use the read-only [acceptance scenarios](references/acceptance-scenarios.md); do not exercise production release actions to test the instructions.
