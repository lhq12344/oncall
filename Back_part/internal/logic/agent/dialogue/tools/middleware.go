package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// approvalToolNames 需要中断门控的工具名称集合。
var approvalToolNames = map[string]struct{}{
	"bash_execute_with_approval": {},
	"request_detail_selection":   {},
}

// ApprovalMiddleware 为 bash_execute_with_approval 和 request_detail_selection 工具提供中断门控。
// 工具本身只负责执行，中断/恢复逻辑完全在此 middleware 内处理。
type ApprovalMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	Logger *zap.Logger
}

func (m *ApprovalMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	if _, needsApproval := approvalToolNames[tCtx.Name]; !needsApproval {
		return endpoint, nil
	}
	toolName := tCtx.Name

	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		wasInterrupted, _, storedArgs := tool.GetInterruptState[string](ctx)

		if !wasInterrupted {
			interruptInfo, err := buildInterruptInfo(toolName, args)
			if err != nil {
				return "", fmt.Errorf("failed to build interrupt info for %s: %w", toolName, err)
			}
			return "", tool.StatefulInterrupt(ctx, interruptInfo, args)
		}

		isTarget, hasData, resumeData := tool.GetResumeContext[map[string]any](ctx)
		if !isTarget || !hasData {
			interruptInfo, err := buildInterruptInfo(toolName, storedArgs)
			if err != nil {
				return "", fmt.Errorf("failed to rebuild interrupt info for %s: %w", toolName, err)
			}
			return "", tool.StatefulInterrupt(ctx, interruptInfo, storedArgs)
		}

		return handleResumeResult(ctx, toolName, storedArgs, resumeData, endpoint, opts)
	}, nil
}

func (m *ApprovalMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	if _, needsApproval := approvalToolNames[tCtx.Name]; !needsApproval {
		return endpoint, nil
	}
	toolName := tCtx.Name

	return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		wasInterrupted, _, storedArgs := tool.GetInterruptState[string](ctx)

		if !wasInterrupted {
			interruptInfo, err := buildInterruptInfo(toolName, args)
			if err != nil {
				return nil, fmt.Errorf("failed to build interrupt info for %s: %w", toolName, err)
			}
			return nil, tool.StatefulInterrupt(ctx, interruptInfo, args)
		}

		isTarget, hasData, resumeData := tool.GetResumeContext[map[string]any](ctx)
		if !isTarget || !hasData {
			interruptInfo, err := buildInterruptInfo(toolName, storedArgs)
			if err != nil {
				return nil, fmt.Errorf("failed to rebuild interrupt info for %s: %w", toolName, err)
			}
			return nil, tool.StatefulInterrupt(ctx, interruptInfo, storedArgs)
		}

		result, err := handleResumeResult(ctx, toolName, storedArgs, resumeData, nil, nil)
		if err != nil {
			return nil, err
		}
		return singleChunkReader(result), nil
	}, nil
}

// buildInterruptInfo 根据工具名和 JSON 参数构建对应的中断信息结构。
func buildInterruptInfo(toolName, argsJSON string) (any, error) {
	switch toolName {
	case "bash_execute_with_approval":
		// 使用与 InvokableRun 相同的宽容解析器，兼容 LLM 可能输出的多对象 JSON 流和空负载。
		in, err := parseBashApprovalArgs(argsJSON)
		if err != nil {
			return nil, fmt.Errorf("invalid bash args: %w", err)
		}
		timeout := in.Timeout
		if timeout <= 0 {
			timeout = defaultBashTimeoutSeconds
		}
		return &BashApprovalInterruptInfo{
			Command: strings.TrimSpace(in.Command),
			Args:    in.Args,
			Script:  strings.TrimSpace(in.Script),
			Timeout: timeout,
			Reason:  strings.TrimSpace(in.Reason),
		}, nil

	case "request_detail_selection":
		type selArgs struct {
			Field    string                  `json:"field"`
			Question string                  `json:"question"`
			Reason   string                  `json:"reason"`
			Options  []DetailSelectionOption `json:"options"`
		}
		var in selArgs
		if err := json.Unmarshal([]byte(argsJSON), &in); err != nil {
			return nil, fmt.Errorf("invalid detail selection args: %w", err)
		}
		return &DetailSelectionInterruptInfo{
			Field:    strings.TrimSpace(in.Field),
			Question: strings.TrimSpace(in.Question),
			Reason:   strings.TrimSpace(in.Reason),
			Options:  in.Options,
		}, nil

	default:
		return map[string]any{"tool_name": toolName, "args": argsJSON}, nil
	}
}

// handleResumeResult 根据工具名和审批数据决定执行结果。
// bash_execute_with_approval: 审批通过则调用 endpoint 执行命令；拒绝则返回拒绝消息。
// request_detail_selection: 从 resumeData 提取选择值直接构造结果，不调用 endpoint。
func handleResumeResult(
	ctx context.Context,
	toolName, storedArgs string,
	resumeData map[string]any,
	endpoint adk.InvokableToolCallEndpoint,
	opts []tool.Option,
) (string, error) {
	switch toolName {
	case "bash_execute_with_approval":
		approved, resolved, comment := parseBashApprovalDecision(resumeData)
		// 解析存储的参数以恢复命令上下文（Command、Args、Timeout），保持结果结构与原工具一致。
		storedIn, parseErr := parseBashApprovalArgs(storedArgs)
		if parseErr != nil {
			storedIn = bashApprovalArgs{}
		}
		if storedIn.Timeout <= 0 {
			storedIn.Timeout = defaultBashTimeoutSeconds
		}
		if resolved {
			result := BashExecuteResult{
				Approved: true, Resolved: true, Executed: false, Success: true,
				Command: storedIn.Command, Args: storedIn.Args, Timeout: storedIn.Timeout,
				Comment: comment,
			}
			return marshalBashExecuteResult(result)
		}
		if !approved {
			result := BashExecuteResult{
				Approved: false, Executed: false, Success: false,
				Command: storedIn.Command, Args: storedIn.Args, Timeout: storedIn.Timeout,
				Error: "command execution rejected by user", ExitCode: -1, Comment: comment,
			}
			return marshalBashExecuteResult(result)
		}
		if endpoint == nil {
			return "", fmt.Errorf("endpoint is nil for bash tool resume")
		}
		return endpoint(ctx, storedArgs, opts...)

	case "request_detail_selection":
		type selArgs struct {
			Field    string                  `json:"field"`
			Question string                  `json:"question"`
			Options  []DetailSelectionOption `json:"options"`
		}
		var in selArgs
		if err := json.Unmarshal([]byte(storedArgs), &in); err != nil {
			return "", fmt.Errorf("failed to parse stored detail selection args: %w", err)
		}
		selectionValue := parseDetailSelectionValue(resumeData)
		selectedOption, ok := findDetailSelectionOption(in.Options, selectionValue)
		if !ok {
			return "", fmt.Errorf("invalid or missing selection_value: %q", selectionValue)
		}
		result := DetailSelectionResult{
			Field:         in.Field,
			Question:      in.Question,
			SelectedValue: selectedOption.Value,
			SelectedLabel: selectedOption.Label,
		}
		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal detail selection result: %w", err)
		}
		return string(out), nil

	default:
		return "", fmt.Errorf("unknown approval tool: %s", toolName)
	}
}

// singleChunkReader 创建只含一个 chunk 的 StreamReader[string]。
func singleChunkReader(msg string) *schema.StreamReader[string] {
	r, w := schema.Pipe[string](1)
	_ = w.Send(msg, nil)
	w.Close()
	return r
}

// SafeToolMiddleware 包装所有工具调用，将普通 error 转为字符串结果，防止工具错误中断 Agent ReAct 循环。
// 唯一例外：interrupt rerun error 必须原样透传（由 compose.IsInterruptRerunError 判断）。
type SafeToolMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func (m *SafeToolMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	_ *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, args, opts...)
		if err != nil {
			if _, ok := compose.IsInterruptRerunError(err); ok {
				return "", err
			}
			return fmt.Sprintf("[tool error] %v", err), nil
		}
		return result, nil
	}, nil
}

func (m *SafeToolMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	_ *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		sr, err := endpoint(ctx, args, opts...)
		if err != nil {
			if _, ok := compose.IsInterruptRerunError(err); ok {
				return nil, err
			}
			return singleChunkReader(fmt.Sprintf("[tool error] %v", err)), nil
		}
		return safeWrapReader(sr), nil
	}, nil
}

// safeWrapReader 将 StreamReader 中的非 EOF error 转为字符串 chunk（中断错误除外）。
func safeWrapReader(sr *schema.StreamReader[string]) *schema.StreamReader[string] {
	r, w := schema.Pipe[string](64)
	go func() {
		defer w.Close()
		defer sr.Close()
		for {
			chunk, err := sr.Recv()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				if _, ok := compose.IsInterruptRerunError(err); ok {
					_ = w.Send("", err)
					return
				}
				_ = w.Send(fmt.Sprintf("\n[tool error] %v", err), nil)
				return
			}
			_ = w.Send(chunk, nil)
		}
	}()
	return r
}
