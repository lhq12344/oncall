# Hybrid RAG Operational Runbook

## Purpose

This runbook records the operational boundary for the Hybrid RAG V1 rollout. It separates four different signals that should not be mixed during acceptance:

- Seed eval: broad CLI and fixture coverage only.
- Gold eval: retrieval-quality regression gate after manual corpus confirmation.
- Offline BM25 inspect: local file-backed BM25 visibility only.
- Live smoke: end-to-end service evidence for rewrite, embedding, BM25, fusion, rerank fallback, and tool JSON output.


## Rollout Status

| Stage | Current status | Boundary |
| --- | --- | --- |
| Stage 11 - Gold eval | Complete for offline corpus-backed V1 gate | 20 scored cases are backed by `testdata/rag_eval_gold_corpus.jsonl` and pass `ragctl eval --corpus`. |
| Stage 12 - Live smoke | Pending / dependency-blocked in current local check | Requires a running local service plus Milvus, embedding/model config, and optional reranker evidence. Current local probe found no service/Milvus listener and no Docker CLI. |
| Stage 13 - Operational boundary | Complete for V1 | Offline inspect/eval/rebuild boundaries are documented and guarded by tests. |

## Current V1 Boundary

- Agent tool inputs remain `query` and optional `top_k`.
- `knowledge_retrieve` and `ops_case_retrieve` own Hybrid RAG behavior internally.
- `ragctl inspect` is intentionally offline BM25-oriented and does not invoke a running chat service.
- `ragctl rebuild-bm25` rebuilds local BM25 indexes from normalized `rag.DocumentChunk` JSONL input.
- No command currently performs an implicit live Milvus/doc-registry scan for BM25 reconstruction.
- Runtime index/cache/eval-run folders under `.oncall/rag/` must stay uncommitted.

## Dataset Readiness

`testdata/rag_eval_seed.jsonl` is model-generated seed coverage. It can validate parser, CLI, profile routing, no-BOM fixtures, and degraded handling, but it is not retrieval-quality proof because its `expected_ids` are intentionally empty until confirmed.

`testdata/rag_eval_gold.jsonl` becomes a quality gate only when every scored case has non-empty `expected_ids` verified against actual corpus identifiers. Acceptable identifiers are actual v2 chunk IDs, legacy chunk IDs, or local final-report IDs used by the retrieval path.

`testdata/rag_eval_gold_corpus.jsonl` is the current deterministic local corpus fixture for the V1 offline gate. It lets `ragctl eval --corpus` rebuild a temporary BM25 index instead of depending on a pre-existing `.oncall/rag` runtime index. In `--corpus` mode, every selected `expected_id` must resolve to a corpus `id` or `chunk_id`; unresolved values are reported in `missing_expected_ids` and make the gate non-ready.

Shape-readiness rule:

- Shape-ready: `scored_count > 0`, `unscored_count == 0`, every selected case has non-empty `expected_ids`, and, when `--corpus` is supplied, every selected `expected_id` exists in the supplied corpus `id`/`chunk_id` set.
- Not shape-ready: empty `expected_ids`, no selected cases, a mix of scored and unscored cases, or `--corpus` expected IDs missing from the supplied corpus.
- Shape-readiness is not quality readiness. Manual corpus tracing remains required before using gold eval as an acceptance gate.

## Commands

Rebuild both local BM25 profiles from a normalized export:

```powershell
go run ./cmd/ragctl rebuild-bm25 --profile all --input .\path\to\chunks.jsonl
```

Inspect the local BM25 index only:

```powershell
go run ./cmd/ragctl inspect --profile knowledge --query "redis timeout" --top-k 5 --final-top-k 3
```

Run seed coverage eval:

```powershell
go run ./cmd/ragctl eval --dataset testdata/rag_eval_seed.jsonl --profile all
```

Run gold eval after manual expected-ID confirmation:

```powershell
go run ./cmd/ragctl eval --dataset testdata/rag_eval_gold.jsonl --profile all --corpus testdata/rag_eval_gold_corpus.jsonl
go run ./cmd/ragctl eval --dataset testdata/rag_eval_gold.jsonl --profile knowledge --corpus testdata/rag_eval_gold_corpus.jsonl
go run ./cmd/ragctl eval --dataset testdata/rag_eval_gold.jsonl --profile ops_case --corpus testdata/rag_eval_gold_corpus.jsonl
```

## Interpreting ragctl Output

### inspect

`inspect` should be treated as offline BM25 evidence when output includes:

- `inspection_mode = offline_bm25_only`
- `live_hybrid_trace = false`
- `retrieval_mode = bm25_offline`
- `rewriter = not invoked by offline CLI`

It does not prove query rewrite quality, embedding retrieval, RRF fusion, rerank sidecar behavior, Milvus availability, or agent tool invocation. The Milvus block in inspect output is config-only metadata, not a connectivity check.

### eval

`eval` reports CLI health and scoring readiness. When `quality_gate_shape_ready = false`, the command may still exit successfully, but the dataset shape is incomplete. When `quality_gate_shape_ready = true`, the CLI has verified non-empty expected_ids and, in `--corpus` mode, that those IDs exist in the supplied corpus fixture. It has still not proven live hybrid behavior or manual review quality. Reasons are reported in `degraded_reasons`, such as missing `expected_ids`, `missing_expected_ids`, no cases matching the selected profile, or offline metric-gate misses.

`retrieval_metric_gate_pass` is the offline corpus metric gate. For `--corpus` eval it requires Recall@20, MRR@3, and Top3HitRate to all equal 1 for the selected scored cases. A miss degrades the run even when `quality_gate_shape_ready=true`.

### rebuild-bm25

`rebuild-bm25 --profile all` partitions normalized chunks by source type. Unknown or missing source types default to knowledge and are reported as ambiguous instead of being silently accepted.

## Live Smoke Checklist

Live smoke checks require local service dependencies such as Milvus, embeddings, configured model access, and any optional reranker sidecar. Do not commit credentials or runtime indexes while collecting evidence.

Current local dependency probe on 2026-08-18 did not find a running service/Milvus listener on the checked ports and `docker` was unavailable in PATH. Treat Stage 12 as pending until Milvus plus the OnCall service are running and the checklist below is executed against the live tools.

- Upload a markdown knowledge file and confirm a retrievable v2 chunk with stable `doc_id`, `chunk_id`, `source_type`, and `content_hash`.
- Query a Chinese knowledge phrase through `knowledge_retrieve` and confirm structured JSON output.
- Create or reuse an ops final report and confirm `ops_case_retrieve` can return either v2/legacy Milvus results or local report fallback.
- Disable or empty BM25 and confirm retrieval degrades without crashing.
- Stop the reranker sidecar and confirm RRF fallback returns results with degraded reasons.
- Capture profile, status, latency, candidate counts, final count, degraded count, and representative results.

## Stage 11 Evidence Captured

Stage 11 is complete for the V1 offline corpus-backed gate in this repository snapshot:

- `testdata/rag_eval_gold.jsonl` contains 20 scored cases: 12 knowledge cases and 8 ops-case cases.
- Every selected gold case has non-empty `expected_ids`.
- `testdata/rag_eval_gold_corpus.jsonl` contains 20 normalized chunks with traceable metadata: 12 knowledge chunks, 4 ops_case chunks, and 4 ops_final_report chunks.
- Gold eval passed for all, knowledge, and ops_case profiles with `quality_gate_shape_ready=true`, `expected_ids_complete=true`, `retrieval_metric_gate_pass=true`, no `missing_expected_ids`, and zero unscored cases.

Do not fabricate future `expected_ids` from seed queries or model guesses; add new cases only from traceable corpus evidence.

Current minimum coverage for the manually confirmed gold set:

- Knowledge lookup.
- Ops-case lookup.
- Chinese queries.
- Pod or service references.
- Time/context rewrite scenarios.
- Expected no-hit or clarification scenarios, with acceptance semantics documented.

## Follow-Up Options

If full live hybrid inspection is needed, add a separate command mode rather than expanding offline `inspect` ambiguously. A live inspect command should show rewrite variants, embedding candidates, BM25 candidates, fused order, rerank output, final results, latency, and degraded reasons from one service-context run.
