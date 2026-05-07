---
name: visionloop-phase-workflow
description: Use this skill when starting any new task on the VisionLoop project, when continuing work on the current phase, or when about to mark a TODO.md item complete. Enforces the project workflow: read TODO.md to identify the current phase, check spec sections in docs/spec/visionloop-spec.md and project rules in CLAUDE.md, output a plan citing relevant sections, wait for owner approval before writing code, write tests as exit criteria, then update TODO.md in the same PR. Triggers when the user says "next task", "continue", "work on phase N", or references TODO items.
---

# VisionLoop Phase Workflow

Enforce the development workflow for VisionLoop tasks.

## Pre-flight checklist (before any code)

1. **Read `TODO.md`** — identify the current phase and the
   specific ⏳ item being worked on. Confirm the item is in the
   current phase, not a future one. Do not work on future phases.

2. **Read relevant spec section** in
   `docs/spec/visionloop-spec.md`. Cite the section number
   in your plan.

3. **Check `CLAUDE.md`** for project rules that apply to this
   task (SQL access, multi-tenancy, auth, testing requirements).

4. **Check `docs/adr/`** for prior decisions on this topic.

## Plan output format

Before writing any code, output exactly this structure and
wait for owner approval:

```
Task: [item name from TODO.md]

Context
Phase: [N]
Spec section: §[N.N] [section title]
Relevant ADRs: [list, or "none"]
CLAUDE.md rules that apply: [list]

Plan
[step]
[step]
[step]

Files to create/modify
path/to/file — [purpose]

Tests required (exit criteria)
[test description]

Open questions (if any)
[wait for owner answer before proceeding]

Awaiting approval before implementation.
```

## During implementation

- Make minimal diffs. Match existing style.
- All SQL via sqlc — never raw query strings.
- Preserve org_id scoping in every tenant-scoped query.
- Write tests in the same PR as the feature.

## Before declaring done

1. Run all tests in the affected service. CI must be green.
2. Update `TODO.md` — move ⏳ → ✅ for the completed item.
3. If an architectural decision was made, draft an ADR in `docs/adr/`.
4. Reference the spec section in the commit message:
   `feat(api): description (spec §X.Y)`.

## Anti-patterns to avoid

- Working ahead into a future phase without explicit owner approval
- Skipping the plan step "to save time"
- Marking ⏳ → ✅ before tests pass and CI is green
- Adding raw SQL because "it's simpler"
- Inferring intent on ambiguous requests instead of asking
