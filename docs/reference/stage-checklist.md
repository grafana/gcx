# Stage Completion Checklist

Use this template when completing each implementation stage.

## Pre-Work

- [ ] Previous stage is closed in beads
- [ ] Stage feature task is assigned and set to `in_progress`
- [ ] Read the stage description (PLAN.md, spec, or design doc)
- [ ] Identify files to create/modify

## Implementation

- [ ] All code changes implemented per spec
- [ ] Tests written and passing
- [ ] `make build` passes
- [ ] `make tests` passes
- [ ] `make lint` passes

## Documentation

- [ ] Updated docs per [doc-maintenance.md](doc-maintenance.md) rules
- [ ] New design docs created for any non-trivial decisions
- [ ] DESIGN.md updated if package map or architecture changed
- [ ] README.md updated if CLI usage changed

## Completion

- [ ] Git commit with Title/What/Why format
- [ ] Beads task closed: `bd close gcx-experiments-N`
- [ ] `bd dolt push` — sync beads to remote
- [ ] Next stage is now unblocked
- [ ] Any follow-up tasks created as beads issues
