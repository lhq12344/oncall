# Repository Guidelines

## Project Structure & Module Organization
OnCall combines a Go backend with a React/Vite frontend. The backend entry point is `main.go`. Generated GoFrame API contracts live in `api/chat/`; avoid hand-editing generated files such as `api/chat/chat.go`. Core backend code is under `internal/`: `bootstrap/` wires services, `controller/chat/` exposes SSE and resume endpoints, `context/` stores session memory and checkpoints, and `agent/` contains dialogue, ops, RCA, execution, strategy, and knowledge workflows. Shared integrations and middleware live in `utility/`. The frontend is in `Front_page/`, deployment files are in `manifest/`, and build helpers are in `hack/`.

## Build, Test, and Development Commands
- `go run main.go`: run the backend locally.
- `go test ./...`: run all Go tests.
- `go test -cover ./...`: run tests with coverage.
- `go test -v ./internal/context/...`: run focused backend tests.
- `make -f hack/hack.mk -f hack/hack-cli.mk build`: build through GoFrame tooling.
- `make -f hack/hack.mk -f hack/hack-cli.mk ctrl|dao|service`: regenerate GoFrame layers.
- `cd Front_page && npm install`: install frontend dependencies.
- `npm run dev`: start Vite on port 3000.
- `npm run lint`: run `tsc --noEmit`.
- `npm run build`: create a production frontend build.

## Coding Style & Naming Conventions
Use `gofmt` for Go. Keep packages lowercase without underscores, exported Go names in PascalCase, unexported names in camelCase, and always return errors explicitly with context. Use JSON tags on serialized structs. For TypeScript/React, use 2-space indentation, single quotes, trailing commas, strict typing, `interface` for object shapes, and avoid `any`. Components are PascalCase; variables, hooks, and zustand actions are camelCase. Keep SSE parsing centralized in `Front_page/src/services/api.ts`.

## Testing Guidelines
Go tests use `*_test.go`, table-driven cases, and names like `TestFunctionName_Scenario`; call `t.Parallel()` when safe. Add or update regression tests before refactors or interrupt/resume changes. Frontend validation relies on TypeScript linting and production build checks.

## Commit & Pull Request Guidelines
Recent history uses short messages such as `fix` and Chinese summaries. Prefer concise imperative commits with scope, for example `backend: preserve checkpoint resume state` or `frontend: render interrupt approvals`. PRs should include a summary, linked issue if applicable, tests run, screenshots for UI changes, and notes for config, schema, or approval-flow changes.

## Security & Configuration Tips
Do not commit secrets from `.env` or `manifest/config/config.yaml`. Preserve human-approval gates for high-risk operations and keep SSE event semantics compatible: `content`, `step`, `interrupt`, `error`, and `done`.
