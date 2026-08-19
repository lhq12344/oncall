# OnCall 学习笔记导航

> 目标：按“先主链路、再核心机制、最后高级主题”的顺序重新学习项目。  
> 日期：2026-08-19。

## 推荐阅读顺序

| 顺序 | 文档 | 主题 | 配套图 |
| --- | --- | --- | --- |
| 00 | `00-learning-plan.md` | 总学习计划与阶段安排 | - |
| 01 | `01-architecture-overview.md` | 架构全景、前后端边界、模块地图 | `diagrams/01-high-level-architecture.mmd`, `diagrams/02-aiops-request-flow.mmd` |
| 02 | `02-bootstrap-and-request-flow.md` | 启动流程、首次请求、Controller/Runner | `diagrams/03-bootstrap-flow.mmd`, `diagrams/04-aiops-bootstrap-request-flow.mmd` |
| 03 | `03-domain-model-glossary.md` | 领域概念随代码链路展开 | `diagrams/05-aiops-domain-state-lifecycle.mmd` |
| 04 | `04-ops-workflow.md` | AIOps 主工作流与分支 | `diagrams/06-aiops-workflow-branches.mmd` |
| 05 | `05-execution-plan-tools.md` | 执行计划、命令执行、验证与 rollback | `diagrams/07-execution-tools-data-flow.mmd` |
| 06 | `06-tool-gateway-permissions-resume.md` | 工具网关、权限、中断恢复 | `diagrams/08-tool-gateway-permission-resume-flow.mmd` |
| 07 | `07-checkpoint-session-memory.md` | Checkpoint、SessionMemory、Redis 状态边界 | `diagrams/09-checkpoint-session-memory-flow.mmd` |
| 08 | `08-knowledge-rag-tools.md` | 知识上传、Hybrid RAG、ops_case 闭环 | `diagrams/10-knowledge-rag-flow.mmd` |
| 09 | `09-frontend-sse-interrupts.md` | 前端 SSE、Zustand、InterruptCard | `diagrams/11-frontend-sse-interrupt-flow.mmd` |
| 10 | `10-build-test-local-debug.md` | 构建、测试、本地调试与验证矩阵 | `diagrams/12-build-test-debug-map.mmd` |
| 11 | `11-source-roadmap-pitfalls.md` | 源码阅读路线图与踩坑笔记 | `diagrams/13-source-roadmap-pitfalls.mmd` |

## 阅读建议

- 第一遍：只读 01、02、04、06、09，先理解请求到响应和 interrupt/resume 主链路。
- 第二遍：补 03、05、07、08，理解 Graph State、执行计划、checkpoint 和 RAG。
- 第三遍：读 10、11，把验证命令和长期阅读路线固化。

## 当前边界

- 这些笔记是源码阅读笔记，不替代 live 环境验证。
- Mermaid 图保留为 `.mmd` 源文件，可后续导出 SVG/PNG。
- 如果项目继续重构目录结构，应重新扫描源码并更新第 11 节路线图。
