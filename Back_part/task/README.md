# Task Catalog

This directory collects implementation and maintenance task lists extracted from
the project plans and current dialogue-agent work.

These files are task documents, not Eino skills. Keep runtime source files in
their package locations, and use this folder only for planning/checklist
material.

## Archived Tests

All backend `*_test.go` files have been moved under `task/testdata/` with their
original relative paths preserved. They are intentionally under `testdata`
because many are same-package tests that reference unexported functions; moving
them into a normal Go package and changing imports would either fail to compile
or require exporting production-only APIs. `go test ./...` ignores `testdata`,
so these files are retained as the single archived copy rather than active
tests.

## Tasks

| File | Source | Purpose |
| --- | --- | --- |
| `dialogue-skill-filesystem-backend.task.md` | `internal/logic/agent/dialogue/skill_filesystem_backend.go` and test | Maintain the read-only Eino skill filesystem backend. |
| `eino-recursive-agent-refactor.task.md` | `Back_part/.claude/PRPs/plans/eino-recursive-agent-refactor.plan.md` | Recursive agent architecture and persistence refactor tasks. |
| `slim-dialogue-knowledge-search.task.md` | `.claude/PRPs/plans/slim-to-dialogue-knowledge-search.plan.md` | Product slimming and local middleware startup tasks. |
| `three-agent-dialogue-graph.task.md` | `.claude/PRPs/plans/completed/three-agent-dialogue-graph.plan.md` | Historical three-agent graph refactor checklist. |
