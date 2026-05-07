# Repository Guidelines

## Project Structure & Module Organization
OnCall is a GoFrame + Eino ADK multi-agent operations system with a React/Vite frontend.
- `main.go` is the backend entry point; `cmd/` is currently reserved and should not be treated as an app entry.
- `api/chat/v1/` holds editable API contracts; `api/chat/chat.go` is generated.
- `internal/` contains backend application code: `bootstrap/`, `controller/`, `context/`, `agent/`, and `ai/`.
- `utility/` contains shared infrastructure such as Redis, MySQL, Elasticsearch, middleware, and token/memory helpers.
- `Front_page/` contains the React UI, SSE client code, and approval components.
- `manifest/` stores runtime config and Kubernetes scripts; `test/` is reserved for broader tests.

## Build, Test, and Development Commands
- `go run main.go` starts the backend locally.
- `go test ./...` runs all Go tests and should be used after backend changes.
- `make -f hack/hack.mk -f hack/hack-cli.mk build` builds through the GoFrame hack targets.
- `make -f hack/hack.mk -f hack/hack-cli.mk ctrl|dao|service` regenerates GoFrame artifacts when contracts or models change.
- `cd Front_page && npm install` installs frontend dependencies.
- `cd Front_page && npm run dev` starts Vite on port `3000`.
- `cd Front_page && npm run build` creates a production frontend build.
- `cd Front_page && npm run lint` runs TypeScript checks with `tsc --noEmit`.

## Coding Style & Naming Conventions
Use `gofmt` for Go code and keep package names short, lowercase, and domain-oriented. Keep controllers thin: validate parameters, orchestrate services, stream SSE, and persist state only. Do not mix dialogue, knowledge, and operations agent responsibilities. For frontend code, use TypeScript/React components in PascalCase and keep shared API parsing in `Front_page/src/services/api.ts`.

## Testing Guidelines
Place Go tests beside implementation files using `*_test.go` and descriptive names such as `TestSessionMemoryTrim`. For SSE or interrupt/resume work, validate both the interrupt trigger path and resume path. Preserve SSE event semantics: `content`, `step`, `interrupt`, `error`, and `done`.

## Commit & Pull Request Guidelines
Recent history uses short Chinese commit subjects, often `fix`; prefer clearer intent-based subjects such as `修复 SSE 中断恢复状态丢失`. PRs should include a concise summary, affected backend/frontend paths, test evidence, linked issues when available, and screenshots or recordings for UI changes.

## Security & Configuration Tips
Treat `manifest/config/config.yaml` as sensitive because it may contain API keys or DSNs. Do not paste secrets into logs, examples, PR descriptions, or generated documentation.
