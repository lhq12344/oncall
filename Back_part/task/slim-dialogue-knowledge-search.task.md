# Slim Dialogue Knowledge Search Tasks

Source plan: `.claude/PRPs/plans/slim-to-dialogue-knowledge-search.plan.md`

Goal: slim OnCall to dialogue + knowledge base + web search, remove AIOps UI/API
paths, and add local middleware startup.

## Task 1: Slim DialogueAgent Tools

- Remove ops case, bash approval, Kubernetes, and Prometheus tools from active tool registration.
- Keep intent analysis, detail selection, knowledge retrieve, and web search.
- Remove obsolete ops config fields when no longer used.
- Validate dialogue package build.

## Task 2: Slim Bootstrap Initialization

- Remove OpsIntegration, OpsAgent, and pod log shipper initialization.
- Remove log sync and ops config fields from app/bootstrap structs.
- Keep dialogue, knowledge, embedder, Redis, DB, and Elasticsearch fallback paths where applicable.

## Task 3: Slim main.go

- Stop reading removed Prometheus, Kubernetes, and log sync config.
- Remove removed fields from `bootstrap.Config`.
- Remove `app.OpsAgent` from controller construction.
- Remove obsolete Prometheus startup log line.

## Task 4: Slim Controller

- Remove ops runner and ops agent fields.
- Remove `AIOpsStream`, `AIOpsResumeStream`, and `Monitoring`.
- Remove helpers used only by AIOps streaming.
- Preserve chat stream/resume, interrupt handling, file upload, and helpers still used by chat.

## Task 5: Slim API Structs

- Delete AIOps stream/resume request and response structs.
- Delete monitoring response structs.
- Preserve chat stream/resume, upload, and interrupt structs.

## Task 6: Slim Frontend Header

- Remove OpsPanel import and rendering.
- Remove AI Ops button and click handler.
- Remove ops-running status branch.
- Clean unused icons/imports.

## Task 7: Slim Frontend Store

- Remove ops panel state, ops steps, current ops task, and ops running flag.
- Remove ops action methods and helper functions.
- Keep message step merging if chat messages still use `AIOpsStep`.
- Remove deleted fields from persisted partial state.

## Task 8: Slim Frontend API Service

- Remove `streamOps` and `resumeOps`.
- Keep stream chat, resume chat, upload, and knowledge APIs.
- Keep types still used by chat stream callbacks.

## Task 9: Slim Frontend Types

- Remove `OpsStep` if only used by removed OpsPanel/store.
- Preserve `AIOpsStep` when chat message step rendering still depends on it.

## Task 10: Middleware Docker Compose

- Create `deploy/docker-compose.middleware.yml`.
- Redis 7 on host `127.0.0.1:31029`.
- Milvus standalone on host `127.0.0.1:31953`.
- Include required etcd and minio services.
- Use named volumes and avoid exposing etcd/minio unless needed.

## Task 11: dev.sh

- Create executable `scripts/dev.sh`.
- Commands: `start`, `stop`, `restart`, `status`, `logs`.
- Store local PIDs/logs under `.run/`.
- Keep startup idempotent and report occupied ports clearly.

## Acceptance

- AIOps routes are no longer bound.
- Header no longer renders AI Ops controls.
- Chat, upload, knowledge, and web-search flows remain intact.
- `go build ./...` passes.
- Frontend lint/build passes when dependencies are installed.
