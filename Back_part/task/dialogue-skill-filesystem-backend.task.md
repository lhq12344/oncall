# Dialogue Skill Filesystem Backend Tasks

Source files:
- `internal/logic/agent/dialogue/skill_filesystem_backend.go`
- `internal/logic/agent/dialogue/skill_filesystem_backend_test.go`
- `internal/logic/agent/dialogue/agent.go`

Do not move these Go files into `task/`. They are runtime package code used by
`newDialogueSkillMiddleware`; this task file only records the maintenance
checklist.

## Task 1: Preserve Read-Only Backend Behavior

- `Read` reads only files inside the configured skill root.
- Relative paths resolve under the skill root.
- Absolute paths are accepted only when still inside the root.
- `Write` and `Edit` return unsupported errors.
- `LsInfo` and `GrepRaw` stay unsupported unless Eino middleware requires them.
- Default read offset is `1`; default limit is `2000`.

## Task 2: Preserve Path Confinement

- Reject empty paths.
- Normalize paths with `filepath.Abs` and `filepath.Clean`.
- Use `filepath.Rel(root, absPath)` to reject `..`, `../...`, and absolute rel results.
- Regression cases:
  - `../secret.txt` is rejected.
  - `filepath.Join(root, "..", "secret.txt")` is rejected.
  - `root/<skill>/SKILL.md` remains readable.

## Task 3: Preserve Skill Discovery

- Empty glob base path defaults to backend root.
- `*/SKILL.md` discovers immediate skill files.
- Glob results are sorted.
- Every glob match is resolved through `resolvePath` before stat.

## Task 4: Preserve Middleware Integration

- Empty `SkillsDir` disables skill middleware without error.
- Missing path or non-directory path disables with a warning.
- Valid directory uses `skill.NewBackendFromFilesystem` with:
  - `Backend: newReadOnlySkillFilesystemBackend(absSkillsDir)`
  - `BaseDir: absSkillsDir`

## Verification

```bash
go test ./internal/logic/agent/dialogue/skill_filesystem_backend.go ./internal/logic/agent/dialogue/skill_filesystem_backend_test.go
```
