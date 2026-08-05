-- Active: 1774184063382@@127.0.0.1@30306

# OnCall 项目介绍（3-5 分钟面试版）

---

## 一句话定位

> OnCall 是一个智能运维 Copilot 系统。运维人员用自然语言描述问题，系统自动完成故障诊断、根因分析、修复执行，全流程可随时审批，不用担心"AI 误删生产库"。

---

## 核心技术亮点（面试重点讲）

### 亮点一：可中断恢复的工作流引擎

**核心问题**：AI 执行命令不能"发起、等待、完成"，必须能随时暂停等人工审批。

**我们的方案**：基于 Eino ADK 的 Runner + Checkpoint 机制。checkpoint_id 定位"哪次执行"，interrupt_id 定位"哪个暂停点"。用户审批后用 ResumeWithParams 精准恢复指定暂停点，其他步骤不受影响。

**为什么用 Eino**：LangChain Go 实现同等能力要多写 3-4 倍代码，没有原生流式中断支持。

---

### 亮点二：Graph-State 编排，省 90% token

**核心问题**：多 Agent 之间直接传 LLM 输出，token 消耗大。

**我们的方案**：每个 Agent 输出后解析结构化字段写入共享 IncidentState，History Rewriter 在每次 LLM 调用前用紧凑 JSON 替换完整历史。实测 token 消耗降 90%。

---

### 亮点三：Token 预算 + 双层执行门控

**Token 控制**：请求级动态裁剪，96000 上限。输出预留 8k，工具预留 20k，用户预留 4k，剩下给历史。按 turn 丢弃保持语义完整，40 轮触发压缩。

**安全门控**：Plan 层硬封禁危险命令（rm -rf /），Step 层运行时逐命令审批。解决"Plan 生成时预测 vs 执行时上下文变了"的问题。

---

## 技术选型

| 问题                             | 回答                                    |
| -------------------------------- | --------------------------------------- |
| 为什么用 Go？                    | goroutine 适合 SSE 长连接，单二进制部署 |
| 为什么用 Eino 而不是 LangChain？ | Runner 模型原生支持 Checkpoint/Resume   |
| 为什么用 SSE 而不是 WebSocket？  | 单向通信够用，HTTP LB 透明              |
| 为什么用 Zustand 而不是 Redux？  | 轻量，API 简洁，不需要 Provider wrapper |

---

## 个人负责

Agent 编排层设计，Checkpoint/Interrupt 机制实现，执行安全双层门控，Token 预算裁剪算法。

> 这个项目让我理解了 LLM Agent 的工程化挑战：如何让 AI 在生产环境中安全、可控地执行操作。
