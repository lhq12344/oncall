# Phase 13 Live Verification Runbook

Date: 2026-09-04

This runbook converts the environment-limited Phase 13 gates into explicit live
checks. The repository-local gates are covered by `backend/hack/phase13-verify.ps1`;
this runbook covers the production-like services that cannot be proven by unit
tests alone.

On Windows hosts without `gcc`, `backend/hack/phase13-verify.ps1` automatically
attempts a WSL race-test fallback. The fallback expects the repository to be
visible under `/mnt/<drive>/...`, then runs `go test -race ./...` with
`GOCACHE=/tmp/oncall-race-cache` and `GOFLAGS=-p=2`.
If the current host cannot start WSL but another terminal or CI runner can run
the race gate, generate a structured proof with
`backend/hack/phase13-race-evidence.ps1` and pass it back to the local verifier
with `-ExternalRaceEvidencePath`.

## Required Environment

Set all variables before running the live gate:

```powershell
$env:ONCALL_LIVE_BASE_URL = "http://127.0.0.1:6872"
$env:ONCALL_LIVE_CHAT_STREAM = "/api/v1/chat_stream"
$env:ONCALL_LIVE_SSE_PRESSURE = "1"
$env:ONCALL_LIVE_SSE_CLIENTS = "4"
$env:ONCALL_LIVE_SSE_RECONNECTS = "2"
$env:ONCALL_LIVE_REDIS_ADDR = "127.0.0.1:6379"
$env:ONCALL_LIVE_ELASTICSEARCH_ADDR = "127.0.0.1:9200"
$env:ONCALL_LIVE_MILVUS_ADDR = "127.0.0.1:19530"
$env:ONCALL_LIVE_KUBERNETES_ADDR = "127.0.0.1:6443"
$env:ONCALL_LIVE_COZELOOP_ADDR = "127.0.0.1:18082"

# Official CozeLoop SaaS is also supported for connectivity checks. The SaaS
# endpoint is validated by TCP reachability because it does not expose the local
# deployment's /ping route.
# $env:ONCALL_LIVE_COZELOOP_ADDR = "https://api.coze.cn"

# Optional backend model overrides for live validation. The checked-in default
# model config now targets DeepSeek's OpenAI-compatible endpoint.
$env:ONCALL_CHAT_BASE_URL = "https://api.deepseek.com"
$env:ONCALL_CHAT_MODEL = "deepseek-v4-flash"
$env:ONCALL_CHAT_API_KEY = "<redacted>"
```

`ONCALL_LIVE_BASE_URL` must point at a running OnCall backend. The SSE check
posts to `ONCALL_LIVE_CHAT_STREAM` when set, or `/api/v1/chat_stream` by
default, and requires a `text/event-stream` response that emits
`oncall.event/v1` without legacy sentinel frames.
`ONCALL_LIVE_SSE_PRESSURE=1` enables the pressure/reconnect drill. Optional
`ONCALL_LIVE_SSE_CLIENTS` and `ONCALL_LIVE_SSE_RECONNECTS` control the workload;
defaults are 4 clients and 2 reconnect attempts per client.

## Commands

From `backend/` on Windows:

```powershell
.\hack\phase13-live-verify.cmd
```

From any shell with PowerShell available:

```powershell
pwsh ./backend/hack/phase13-live-verify.ps1
```

If Windows lacks `gcc` and the verifier cannot start WSL from the current host,
run the race proof from a terminal that can access WSL, then feed the generated
JSON back into the verifier:

```powershell
.\hack\phase13-race-evidence.cmd `
  -EvidencePath .\.gocache\phase13-race-evidence.json `
  -WslDistro Ubuntu-24.04

.\hack\phase13-verify.ps1 `
  -ExternalRaceEvidencePath .\.gocache\phase13-race-evidence.json `
  -EvidencePath .\.gocache\phase13-local-final-current.json
```

The live script records each live gate separately in the evidence JSON:

- `live_sse_endpoint`
- `live_sse_pressure`
- `live_dependency_redis`
- `live_dependency_elasticsearch`
- `live_dependency_milvus`
- `live_dependency_kubernetes`
- `live_dependency_cozeloop`

If one endpoint fails, the script continues collecting the remaining live gates,
records earlier successful gates as `complete`, stores all failures in
`failed_gates`, and exits non-zero after the full pass. Operators can resume
from the failing live dependency instead of re-triaging the entire environment.

For partial evidence collection, use `-AllowPartial`. Partial runs are not a
production cutover pass; they only document which live checks could run.

```powershell
.\hack\phase13-live-verify.cmd -AllowPartial
```

## Production Evidence File

The remaining production-only gates are validated from a structured evidence
file instead of free-form notes. Copy
`docs/architecture/phase13-production-evidence-template.json` to
`docs/architecture/phase13-production-evidence.json`, fill the real evidence,
then run:

```powershell
.\hack\phase13-production-evidence-init.cmd `
  -EvidencePath ..\docs\architecture\phase13-production-evidence.json
```

The init command only scaffolds the evidence file and may prefill already-known
connectivity endpoints from environment variables. It keeps production-only
proof fields incomplete until the real drills are performed.

After each manual production drill, update the same evidence file with the
structured updater instead of editing JSON by hand. Example:

```powershell
.\hack\phase13-production-evidence-update.cmd `
  -Gate dependency `
  -Component redis `
  -ObservedDegraded `
  -UnrelatedCapabilitiesAvailable `
  -EvidenceRef "operator log / screenshot / trace reference"
```

After filling the real drill results, run:

```powershell
.\hack\phase13-production-evidence-verify.cmd `
  -ProductionEvidencePath ..\docs\architecture\phase13-production-evidence.json `
  -EvidencePath .\.gocache\phase13-production-evidence-verification.json
```

The script validates these four gates:

- `live_optional_dependency_fault_injection`: Redis, Elasticsearch, Milvus,
  Kubernetes, and CozeLoop drills must each show degraded behavior and unrelated
  capabilities remaining available.
- `live_cozeloop_trace`: one real trace must include model, RAG, Tool, Workflow,
  error, token, and cost span categories.
- `live_mutation_approval`: the backend/UI path must allow a matching approval
  and reject stale plan revision, changed target, changed args, and changed
  snapshot hash.
- `live_data_flywheel_publish`: a canary knowledge item must pass staging eval,
  publish to canary, be retrieved by live hybrid RAG, and preserve rollback
  version evidence.

## Status Summary

Use the status summary after local, live, or production evidence runs to get the
current remaining item count from one command:

```powershell
.\hack\phase13-status.cmd -EvidencePath .\.gocache\phase13-status-current.json
```

The summary reads the latest local evidence, granular live evidence, and
production evidence verification output, then emits `phase13.status/v1` with
`open_count` and `open_items`.

For a local Windows + WSL CozeLoop deployment, the live check validates `/ping`
and accepts either the app `pong` response or the Coze Loop UI HTML served by
the bridge. For official CozeLoop SaaS, use `https://api.coze.cn`; the verifier
checks endpoint reachability without assuming a `/ping` route. Full trace
inspection remains a separate production evidence gate.

## Pass Criteria

- All required environment variables are set unless intentionally running with
  `-AllowPartial`.
- The live chat stream endpoint returns `2xx`, `text/event-stream`, and at least
  one successful terminal `oncall.event/v1` frame. `error` and `run.failed`
  frames fail the live gate even when the transport protocol is valid.
- A 2026-09-04 Windows live run against `deepseek-v4-flash` returned
  `run.started`, `message.token`, and `run.completed` frames from
  `/api/v1/chat_stream`; the full live gate still requires the CozeLoop endpoint
  below.
- The pressure/reconnect drill is explicitly enabled and completes all client
  attempts without legacy frames or missing versioned events. Reconnect attempts
  use the previous response's actual SSE `id:` value as `Last-Event-ID`.
- The live stream contains no legacy `[DONE]`, `[ERROR]`, or Go map command
  payloads.
- Redis, Elasticsearch, Milvus, Kubernetes, and CozeLoop endpoints are reachable
  by TCP from the verification host. Local CozeLoop deployments additionally
  validate `/ping`; official CozeLoop SaaS validates endpoint reachability.
- The repository-local verification script still passes separately, including
  CI race testing through `.github/workflows/phase13-verification.yml`.

## Remaining Manual Evidence

The live harness proves connectivity and protocol shape. Full production signoff
still needs operator evidence for these scenarios:

- SSE long-connection pressure and reconnect soak testing.
- Deliberate Redis, Elasticsearch, Milvus, Kubernetes, and CozeLoop outage
  injection, confirming unrelated capabilities degrade instead of failing.
- CozeLoop trace inspection for one complete answer, including model, RAG, Tool,
  Workflow, error, token, and cost spans.
- Live mutation approval drill against the running backend/UI path, confirming
  changed plan revision, snapshot hash, tool target, or arguments invalidates
  old approvals at the transport boundary.
- Live data flywheel drill against the running hybrid RAG stack, confirming a
  canary-published item is retrievable after publish.
