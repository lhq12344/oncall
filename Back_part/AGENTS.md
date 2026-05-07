# Repository Guidelines

## Project Structure & Module Organization
OnCall is a GoFrame + Eino ADK multi-agent operations system with a Go backend and a React/Vite frontend.
- `main.go` is the backend entry point; `cmd/` is currently reserved and should not be treated as an app entry.
- `api/chat/v1/` holds editable API contracts; `api/chat/chat.go` is generated.
- `internal/` contains backend application code: `cmd/`, `controller/`, `logic/`, `model/`, and service wiring.
- `internal/controller/` is the protocol boundary: validate request parameters, call services/runners, emit SSE, and return API results only.
- `internal/legacy/opsworkflow/` contains legacy AIOps workflow code; keep dialogue, knowledge, and operations responsibilities separated.
- `utility/` contains shared infrastructure such as middleware, clients, and helpers.
- `manifest/` stores runtime config and Kubernetes scripts; treat config values as sensitive.
- The frontend lives at sibling path `../Front_page/`, not under `Back_part/`.
- `.run/` stores local runtime logs, PIDs, and build artifacts; do not commit generated runtime output.

## Build, Test, and Development Commands
- `go run main.go` starts the backend locally.
- `go test ./...` runs all Go tests and should be used after backend changes.
- `make -f hack/hack.mk -f hack/hack-cli.mk build` builds through the GoFrame hack targets.
- `make -f hack/hack.mk -f hack/hack-cli.mk ctrl|dao|service` regenerates GoFrame artifacts when contracts or models change.
- `./scripts/dev.sh start|restart|status|logs|stop` manages local backend/frontend/middleware when available.
- `cd ../Front_page && npm install` installs frontend dependencies.
- `cd ../Front_page && npm run dev` starts Vite on port `3100` by default.
- `cd ../Front_page && npm run build` creates a production frontend build.
- `cd ../Front_page && npm run lint` runs TypeScript checks with `tsc --noEmit`.

## Coding Style & Naming Conventions
Use `gofmt` for Go code and keep package names short, lowercase, and domain-oriented. Keep controllers thin: validate parameters, orchestrate services, stream SSE, and persist state only through the service layer. Do not mix dialogue, knowledge, and operations agent responsibilities. For frontend code, use TypeScript/React components in PascalCase and keep shared API parsing in `../Front_page/src/services/api.ts`.

## Testing Guidelines
Place Go tests beside implementation files using `*_test.go` and descriptive names such as `TestSessionMemoryTrim`. For SSE or interrupt/resume work, validate both the interrupt trigger path and resume path. Preserve SSE event semantics: `content`, `step`, `interrupt`, `error`, and `done`. If a change affects generated API contracts, update `api/chat/v1/` first, regenerate when required, and verify controller/frontend parsing stays consistent.

## Commit & Pull Request Guidelines
Recent history uses short Chinese commit subjects, often `fix`; prefer clearer intent-based subjects such as `修复 SSE 中断恢复状态丢失`. Commit messages should follow the Lore protocol when requested by the active workflow: intent line first, then useful trailers such as `Constraint:`, `Rejected:`, `Confidence:`, `Scope-risk:`, `Tested:`, and `Not-tested:`. PRs should include a concise summary, affected backend/frontend paths, test evidence, linked issues when available, and screenshots or recordings for UI changes.

## Security & Configuration Tips
Treat `manifest/config/config.yaml`, `.env`, DSNs, API keys, and provider credentials as sensitive. Do not paste secrets into logs, examples, PR descriptions, generated docs, or test fixtures. Prefer environment variables for local overrides such as backend ports, API base URLs, and optional Eino skill directories.
