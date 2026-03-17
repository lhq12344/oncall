# SSE (Server-Sent Events) 技术详解

> 本文档详细解析 SSE 在 OnCall 项目中的作用、原理、适用性及具体实现。

---

## 一、SSE 是什么？

### 1.1 定义

**SSE (Server-Sent Events)** 是 HTML5 规范中的一种浏览器标准技术，允许服务器向客户端单向推送实时数据。

- **协议**：基于 HTTP 的长连接
- **格式**：文本格式，每条消息以 `data: ` 开头，以 `\n\n` 结束
- **特点**：单向通信（服务器 → 客户端），自动重连，简单易用

### 1.2 与 WebSocket 的区别

| 特性 | SSE | WebSocket |
|------|-----|-----------|
| 通信方向 | 单向（服务器 → 客户端） | 双向 |
| 协议 | HTTP/1.1+ | 独立协议 |
| 连接数 | 浏览器限制 6 个同域连接 | 无限制 |
| 实现复杂度 | 简单（文本格式） | 较复杂（二进制/帧） |
| 适用场景 | 实时通知、日志流、AI 流式输出 | 即时通讯、游戏、协同编辑 |

**OnCall 项目选择 SSE 的原因**：
- 只需服务器向客户端推送流式数据（AI 生成内容、工具调用步骤、中断事件）
- 无需客户端向服务器发送大量数据
- 实现简单，浏览器原生支持

---

## 二、SSE 在 OnCall 项目中的作用

### 2.1 核心作用

SSE 在 OnCall 项目中承担 **流式通信管道** 的角色，实现以下功能：

1. **实时流式输出**：AI 生成内容逐字推送，避免用户长时间等待
2. **工具调用进度展示**：展示 RCA、Execution 等 Agent 的工具调用步骤
3. **中断事件通知**：高风险命令触发人工审批时，实时推送中断事件
4. **错误实时反馈**：执行过程中的错误实时推送到前端
5. **会话结束通知**：流式传输结束时的完成信号

### 2.2 项目中的 SSE 端点

OnCall 项目提供 4 个 SSE 端点：

| 端点 | 作用 | 参数 |
|------|------|------|
| `/api/v1/chat_stream` | 对话流式输出 | `id` (sessionID), `question` |
| `/api/v1/chat_resume_stream` | 对话中断恢复 | `checkpoint_id`, `interrupt_ids`, `approved`, `resolved`, `comment` |
| `/api/v1/ai_ops_stream` | 运维工作流流式输出 | 无参数（固定诊断） |
| `/api/v1/ai_ops_resume_stream` | 运维工作流中断恢复 | `checkpoint_id`, `interrupt_ids`, `approved`, `resolved`, `comment` |

---

## 三、SSE 为什么适用于当前项目？

### 3.1 技术匹配性

| 项目需求 | SSE 优势 |
|----------|----------|
| AI 生成内容需要实时展示 | 流式推送，逐字显示，提升用户体验 |
| 工具调用过程需要透明化 | 可推送 `step` 事件，展示执行进度 |
| 高风险操作需要人工审批 | 可推送 `interrupt` 事件，触发前端审批卡片 |
| 长时间运行的工作流 | 长连接保持，避免轮询开销 |
| 多种事件类型（content/step/interrupt/done） | 文本格式灵活，可自定义事件类型 |

### 3.2 与其他方案对比

| 方案 | 优点 | 缺点 | 适用性 |
|------|------|------|--------|
| **SSE（当前选择）** | 简单、浏览器原生、自动重连 | 单向通信 | ✅ 完美匹配 |
| **WebSocket** | 双向通信、低延迟 | 实现复杂、需要额外协议 | ❌ 过度设计 |
| **HTTP 轮询** | 简单 | 高延迟、高开销 | ❌ 体验差 |
| **Long Polling** | 比轮询高效 | 服务器资源占用高 | ❌ 不如 SSE |

---

## 四、SSE 在项目中的具体实现

### 4.1 后端实现（Go + GoFrame）

#### 4.1.1 SSE 初始化

```go
// internal/controller/chat/chat_v1.go

func setupSSE(ctx context.Context) (*ghttp.Request, error) {
    r := g.RequestFromCtx(ctx)
    if r == nil {
        return nil, fmt.Errorf("failed to get request from context")
    }
    // 设置响应头
    r.Response.Header().Set("Content-Type", "text/event-stream")  // 关键：MIME 类型
    r.Response.Header().Set("Cache-Control", "no-cache")           // 禁止缓存
    r.Response.Header().Set("Connection", "keep-alive")            // 保持连接
    r.Response.Header().Set("X-Accel-Buffering", "no")             // 禁用 Nginx 缓冲
    r.Response.WriteHeader(200)
    r.Response.Flush()
    return r, nil
}
```

#### 4.1.2 数据写入

```go
// internal/controller/chat/chat_v1.go

func writeSSEData(r *ghttp.Request, data string) {
    if r == nil {
        return
    }
    // 规范化换行符
    data = strings.ReplaceAll(data, "\r\n", "\n")
    data = strings.ReplaceAll(data, "\r", "\n")
    
    // 按行写入（SSE 规范）
    lines := strings.Split(data, "\n")
    for _, line := range lines {
        r.Response.Write(fmt.Sprintf("data: %s\n", line))
    }
    
    // 事件结束符
    r.Response.Write("\n")
    
    // 立即刷新到客户端
    r.Response.Flush()
}
```

#### 4.1.3 事件类型定义

OnCall 项目定义了 5 种 SSE 事件类型：

| 事件类型 | 格式 | 用途 |
|----------|------|------|
| **content** | `{"type":"content","content":"..."}` | AI 生成的文本内容 |
| **step** | `{"type":"step","step":N,"content":"..."}` | 工具调用步骤进度 |
| **interrupt** | `{"type":"interrupt","checkpoint_id":"...","message":"..."}` | 中断等待人工审批 |
| **error** | `{"type":"error","content":"..."}` | 错误信息 |
| **done** | `{"type":"done"}` 或 `[DONE]` | 流结束信号 |

#### 4.1.4 完整流式输出示例

```go
// AIOpsStream 方法（简化版）

func (c *ControllerV1) AIOpsStream(ctx context.Context, req *v1.AIOpsStreamReq) (*v1.AIOpsStreamRes, error) {
    // 1. 初始化 SSE
    r, err := setupSSE(ctx)
    if err != nil {
        return nil, err
    }
    
    // 2. 运行 Runner
    iter := c.opsStreamRunner.Run(ctx, messages, adk.WithCheckPointID(checkpointID))
    
    // 3. 遍历事件并推送
    for {
        event, ok := iter.Next()
        if !ok {
            break
        }
        
        if event.Err != nil {
            // 推送错误事件
            writeSSEData(r, fmt.Sprintf("{\"type\":\"error\",\"content\":%q}", event.Err.Error()))
            return nil, nil
        }
        
        // 推送 content 事件
        if content != "" {
            writeSSEData(r, fmt.Sprintf("{\"type\":\"content\",\"content\":%q}", content))
        }
        
        // 推送 step 事件（工具调用）
        if toolCall != nil {
            writeSSEData(r, fmt.Sprintf("{\"type\":\"step\",\"step\":%d,\"content\":%q}", stepNum, "调用工具: "+call.Function.Name))
        }
        
        // 推送 interrupt 事件（人工审批）
        if interruptInfo != nil {
            payload := buildInterruptPayload(checkpointID, interruptInfo)
            payloadBytes, _ := json.Marshal(payload)
            writeSSEData(r, string(payloadBytes))
        }
    }
    
    // 4. 推送结束事件
    writeSSEData(r, "{\"type\":\"done\"}")
    
    return &v1.AIOpsStreamRes{}, nil
}
```

### 4.2 前端实现（React + TypeScript）

#### 4.2.1 SSE 消费逻辑

```typescript
// Front_page/src/services/api.ts

async function streamRequest(url: string, body: any, options: StreamOptions) {
    const { onContent, onStep, onInterrupt, onError, onDone } = options;
    
    const response = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });
    
    const reader = response.body?.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    
    while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        
        buffer += decoder.decode(value, { stream: true });
        const parts = buffer.split('\n\n');  // SSE 事件分隔符
        buffer = parts.pop() || '';
        
        for (const part of parts) {
            // 提取 data 内容
            const dataContent = part.split('\n')
                .filter(l => l.startsWith('data: '))
                .map(l => l.slice(6))
                .join('\n')
                .trim();
            
            // 处理 [DONE] 文本回退
            if (dataContent === '[DONE]') {
                onDone?.();
                return;
            }
            
            // 解析 JSON 事件
            try {
                const json = JSON.parse(dataContent);
                switch(json.type) {
                    case 'content':   onContent(json.content); break;
                    case 'step':      onStep?.(json); break;
                    case 'interrupt': onInterrupt?.(mapInterruptData(json)); break;
                    case 'done':      onDone?.(); return;
                    case 'error':     onError?.(json.content); return;
                }
            } catch (e) {
                // 非 JSON，作为普通文本处理
                onContent(dataContent);
            }
        }
    }
}
```

#### 4.2.2 中断审批卡片

```typescript
// Front_page/src/components/InterruptCard.tsx

// 用户点击审批按钮后，恢复 SSE 流
const handleAction = async (actionName: string, approved: boolean, resolved: boolean) => {
    const payload = {
        approved,
        resolved,
        interrupt_ids: interruptIDs
    };
    
    // 调用恢复接口，建立新的 SSE 连接
    if (isOps) {
        await resumeOps(checkpointId, payload, options);
    } else {
        await resumeChat(currentSessionId, checkpointId, payload, options);
    }
};
```

---

## 五、面试回答话术

### 问题 1：SSE 是什么？为什么选择 SSE 而不是 WebSocket？

**回答：**

> **SSE (Server-Sent Events)** 是 HTML5 标准中的单向通信技术，允许服务器向客户端实时推送数据。它基于 HTTP 长连接，使用文本格式（`data: \n\n`）传输数据。
>
> **为什么选择 SSE 而不是 WebSocket？**
> 1. **单向通信足够**：我们的项目只需服务器向客户端推送 AI 生成内容、工具调用步骤、中断事件，无需客户端向服务器发送大量数据。
> 2. **实现简单**：SSE 是浏览器原生支持的标准，前端只需 `EventSource` API，后端只需设置响应头并按格式写入数据。
> 3. **自动重连**：SSE 内置自动重连机制，网络中断后会自动恢复连接。
> 4. **浏览器连接限制**：浏览器对同域 WebSocket 连接数无限制，但对 SSE 有 6 个连接限制。我们的项目每个会话只需 1 个 SSE 连接，完全够用。

### 问题 2：SSE 在 OnCall 项目中是如何使用的？

**回答：**

> OnCall 项目使用 SSE 实现流式通信，主要场景包括：
>
> 1. **对话流式输出**（`/api/v1/chat_stream`）：
>    - 推送 AI 生成的文本内容（`content` 事件）
>    - 推送工具调用步骤（`step` 事件）
>    - 推送中断事件（`interrupt` 事件，触发人工审批）
>
> 2. **运维工作流流式输出**（`/api/v1/ai_ops_stream`）：
>    - 推送 RCA Agent 的分析进度
>    - 推送 Execution Agent 的命令执行步骤
>    - 推送最终技术报告
>
> 3. **中断恢复**（`/api/v1/chat_resume_stream` 和 `/api/v1/ai_ops_resume_stream`）：
>    - 用户在前端审批后，通过 SSE 恢复流式执行
>    - 继续推送后续内容
>
> **后端实现**：
> - 使用 GoFrame 的 `ghttp.Request` 设置 SSE 响应头
> - 使用 `writeSSEData` 函数按格式写入数据
> - 使用 `Flush()` 立即刷新到客户端
>
> **前端实现**：
> - 使用 `fetch` API 获取响应流
> - 使用 `TextDecoder` 解码数据
> - 按 `\n\n` 分隔事件，解析 JSON 或文本内容

### 问题 3：SSE 如何处理中断和恢复？

**回答：**

> **中断流程**：
> 1. 当 `ExecuteStepTool` 检测到高风险命令时，调用 `tool.Interrupt()` 挂起工作流
> 2. 后端通过 SSE 推送 `interrupt` 事件，包含 `checkpoint_id` 和中断信息
> 3. 前端 `InterruptCard` 组件渲染审批卡片，展示待执行命令
>
> **恢复流程**：
> 1. 用户在前端点击审批按钮（准许执行/拒绝/标记已解决）
> 2. 前端 POST 到恢复接口（`/chat_resume_stream` 或 `/ai_ops_resume_stream`）
> 3. 后端 `runner.ResumeWithParams()` 恢复执行，工具内 `tool.GetResumeContext()` 取回用户决策
> 4. 建立新的 SSE 连接，继续推送后续内容
>
> **关键点**：
> - `checkpoint_id` 用于标识中断点，支持任意位置恢复
> - `interrupt_ids` 用于标识具体的中断上下文
> - 恢复后，SSE 继续推送 `content`、`step`、`interrupt` 等事件

### 问题 4：SSE 的性能优化有哪些？

**回答：**

> 1. **禁用缓冲**：设置 `X-Accel-Buffering: no`，禁用 Nginx 缓冲，确保数据实时推送
> 2. **立即刷新**：每次写入数据后调用 `Flush()`，避免数据积压
> 3. **文本格式**：SSE 使用文本格式，比二进制协议更轻量
> 4. **自动重连**：SSE 内置重连机制，无需额外实现
> 5. **连接复用**：每个会话只需 1 个 SSE 连接，避免过多连接开销

---

## 六、代码落点

| 功能 | 文件路径 | 核心函数 |
|------|----------|----------|
| SSE 初始化 | `internal/controller/chat/chat_v1.go` | `setupSSE()` |
| 数据写入 | `internal/controller/chat/chat_v1.go` | `writeSSEData()` |
| 对话流式输出 | `internal/controller/chat/chat_v1.go` | `ChatStream()` |
| 运维工作流流式输出 | `internal/controller/chat/chat_v1.go` | `AIOpsStream()` |
| 中断恢复 | `internal/controller/chat/chat_v1.go` | `ChatResumeStream()`, `AIOpsResumeStream()` |
| 前端 SSE 消费 | `Front_page/src/services/api.ts` | `streamRequest()` |
| 中断审批卡片 | `Front_page/src/components/InterruptCard.tsx` | `InterruptCard` 组件 |

---

## 七、总结

SSE 在 OnCall 项目中扮演着 **流式通信管道** 的关键角色，实现了 AI 生成内容的实时展示、工具调用进度的透明化、高风险操作的人工审批等功能。相比 WebSocket，SSE 更简单、更适合单向通信场景，是流式 AI 应用的理想选择。
