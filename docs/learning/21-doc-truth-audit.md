# 21 文档真实性审计：哪些已修正，哪些仍需补证

> 日期：2026-08-19。
> 目标：把 `docs/` 当前内容和仓库源码/文件树对齐，标出已修正的不实或过期内容，以及仍需要 live 环境验证的部分。

## 1. 本轮结论

本轮审计后，学习文档主线仍然成立，但 bootstrap 表述需要按当前重构更新：当前项目根目录按 `backend/`、`frontend/`、`docs/` 分离；后端主入口是 `backend/main.go`；当前 bootstrap 通过 layer registry 依次创建 Infrastructure、State、Agents、Runtime、Background；AIOps 主链路在 `backend/internal/workflow/ops`；前端 SSE/interrupt 主线在 `frontend/src/services/api.ts`、`frontend/src/store/useStore.ts` 和 `frontend/src/components/InterruptCard.tsx`。

但本轮也发现了几处必须修正的过期点：学习计划仍保留旧文档名；部分 RAG 文档引用了当前仓库不存在的 runbook；旧的 backend utility config 目录并不存在；`chat_v1.go` 的部分行号随源码变更发生漂移；bootstrap 启动链路已从单函数/Controller 构造器装配改为分层 registry + `RuntimeLayer`；`internal/agent/rca` 不能简单标成“全无用”，因为 ops diagnostic toolset 复用了 RCA tools。

## 2. 已修正内容

| 类型 | 原问题 | 当前修正 |
| --- | --- | --- |
| 学习计划目录 | `00-learning-plan.md` 仍写 `06-knowledge-rag.md`、`07-frontend-sse-ui.md`、`08-build-test-debug.md`、`09-pitfalls-and-open-questions.md` 等旧文件名 | 已改为当前实际文件：`06-tool-gateway-permissions-resume.md`、`07-checkpoint-session-memory.md`、`08-knowledge-rag-tools.md`、`09-frontend-sse-interrupts.md`、`10-build-test-local-debug.md`、`11-source-roadmap-pitfalls.md`，并补上 12–20 的第二轮专题边界 |
| 配置目录 | `00-learning-plan.md` 提到旧的 backend utility config 目录 | 已改为 `backend/utility/common/` 与 `backend/manifest/config/config.yaml` |
| RAG runbook | `08-knowledge-rag-tools.md`、`10-build-test-local-debug.md` 引用当前不存在的独立 RAG runbook | 已改为以 `backend/testdata/rag_eval_seed.jsonl`、`backend/testdata/rag_eval_gold.jsonl`、`backend/testdata/rag_eval_gold_corpus.jsonl` 和 `backend/cmd/ragctl/main_test.go` 为当前可验证证据 |
| RAG fixture 边界 | `rag_eval_gold_corpus.jsonl` 的部分 metadata 仍保留旧 runbook `source_path`，容易误以为仓库存在对应文档 | 已在 `08-knowledge-rag-tools.md`、`10-build-test-local-debug.md`、`16-hybrid-rag-eval-deep-dive.md` 增加说明：fixture 可用于离线 eval，但 metadata 里的旧路径不是当前现存文档证据 |
| Controller 行号 | `02-bootstrap-and-request-flow.md` 中 AIOpsStream/AIOpsResumeStream、resumeAgent、withSSEWorkflow、buildResumeTargetPayload 等行号仍是旧偏移 | 已按当前 `backend/internal/controller/chat/chat_v1.go` 的函数行号更新 |
| bootstrap 分层重构 | `01`、`02`、`07`、`10`、`11` 和相关 Mermaid 图仍描述 `main.go` 或 `NewV1WithHooks` 直接装配 Redis/checkpoint/runner | 已改为当前主链路：`main.go -> bootstrap.NewApplication -> LayerRegistry -> RuntimeLayer -> chat.NewV1FromDeps`，并标注 `NewV1WithHooks` 仅是兼容构造入口 |
| final report RAG 归档 | `04-ops-workflow.md` 仍写“需要 06 验证” | 已改成引用 `08-knowledge-rag-tools.md` 和 `20-final-report-archive-loop.md`，并说明本地落盘、eligibility、ops v2 Milvus/BM25 的源码证据与 live 验证边界 |
| legacy agent 边界 | `01`、`11` 对 `internal/agent/rca` / `strategy` 的表述不够精确 | 已补充：`NewRCAAgent/NewStrategyAgent` 未被 `bootstrap.NewApplication` 直接创建，但 `workflow/ops/diagnostic_toolset.go` 复用 `internal/agent/rca/tools` |
| 根目录告警手册 | `docs/告警处理手册.md` 包含日志主题地域、日志主题 ID、错误码等外部运维事实，仓库内无法证明这些配置仍然最新 | 已补充证据边界：该手册作为 runbook 文本可被仓库证明存在，但外部平台 ID/配置需以当前日志平台、监控平台和业务配置为准 |

## 3. 当前可认为属实的主线

- **前后端边界属实**：当前仓库存在 `backend/` 和 `frontend/`；旧 `Front_page` 只应作为历史命名提醒，不应作为当前路径使用。
- **启动链路属实**：`backend/main.go` 加载 env/config，调用 `bootstrap.NewApplication`，再用 `app.Runtime` 和 agent/hook 兼容字段构造 `chat.ControllerDeps`，注册 `/api/v1`，监听 6872。
- **主 Agent 装配属实**：`bootstrap.NewApplication` 当前通过 `LayerRegistry` 分层装配 dialogue、knowledge、ops；不是从 `internal/agent/rca` 和 `internal/agent/strategy` 直接启动主链路。
- **AIOps 状态机主线属实**：`incident_analysis -> diagnosis_gate -> plan -> plan_gate -> plan_approval -> execute_plan -> verify_plan -> replan_decider -> final_report` 是当前学习文档应围绕的主线。
- **RAG 边界属实**：`ragctl inspect/eval` 证明离线 BM25/fixture 流程，不证明 live Milvus + embedding + reranker + chat service 的完整线上质量。
- **前端 interrupt/resume 主线属实**：前端根据 SSE JSON 的 `type` 分发 content/step/interrupt/done/error，`InterruptCard` 组装 approved/resolved/comment/selection_value 和 interrupt_ids 后选择 chat 或 ops resume endpoint。

## 4. 仍需补证或后续新增的内容

| 优先级 | 建议新增/验证 | 原因 |
| --- | --- | --- |
| 高 | live RAG smoke 记录 | 当前文档只证明源码和离线 fixture；还没有连接真实 Milvus/Ark embedding/reranker 后的线上工具调用证据 |
| 高 | 前端浏览器 e2e 或手动 QA 截图 | 当前前端文档基于源码和 build/typecheck，尚未证明浏览器中 interrupt/resume 的完整交互体验 |
| 中 | `internal/agent/rca/tools` 与 `workflow/ops/diagnostic_toolset.go` 的专题页 | 目前只补了边界说明，后续可单独讲 RCA tools 如何作为 ops diagnostic tools 被复用 |
| 中 | 配置矩阵专题 | `manifest/config/config.yaml`、env、GoFrame config、RAG/Milvus config 的优先级可以单独整理成表 |
| 中 | 告警手册外部配置复核 | `docs/告警处理手册.md` 中日志主题地域、日志主题 ID、业务错误码需由日志平台/业务配置确认是否仍有效 |
| 低 | Mermaid 导出说明 | VS Code 默认不能直接预览 `.mmd`，可补充推荐插件或 CLI 导出 SVG/PNG 的流程 |

## 5. 审计方法

- 文件树核对：确认 `backend/`、`frontend/`、`docs/learning`、`docs/learning/diagrams`、`docs/告警处理手册.md`、`backend/testdata` 的现存文件。
- 文本扫描：搜索旧文件名、旧路径、旧 runbook、`Front_page`、`internal/agent`、RAG fixture metadata 等高风险词。
- 源码定位：用当前源码重新定位 `chat_v1.go`、`bootstrap/app.go`、`workflow/ops`、`rag`、`permissions`、`frontend` 的关键函数/入口。
- 证据边界：凡是只靠源码能证明的，标为源码证据；凡是需要真实 Redis/Milvus/Embedding/browser 的，保留为 Unknown/live 验证项。
