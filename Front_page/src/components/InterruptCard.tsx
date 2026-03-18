import React, { useEffect, useState } from 'react';
import { useStore } from '../store/useStore';
import { CheckCircle2, Copy, Loader2, Play, RotateCcw, ShieldAlert } from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { resumeChat, resumeOps } from '../services/api';
import { InterruptData } from '../types';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

interface InterruptCardProps {
  messageId?: string;
  interrupt: InterruptData;
  isOps?: boolean;
  opsStepId?: string;
}

export const InterruptCard: React.FC<InterruptCardProps> = ({ 
  interrupt,
  isOps = false,
  opsStepId
}) => {
  const {
    theme,
    currentSessionId,
    addMessage,
    updateLastMessage,
    appendStepToLastMessage,
    setLastMessageStepStatus,
    addOpsStep,
    markOpsInterruptHandled,
    updateOpsStep,
    setOpsRunning,
    setStreaming,
    setConnectionStatus
  } = useStore();
  const [isHandled, setIsHandled] = useState(Boolean(interrupt.handled));
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorText, setErrorText] = useState('');
  const [copySuccess, setCopySuccess] = useState(false);
  const [lastAction, setLastAction] = useState('');
  const [selectedValue, setSelectedValue] = useState('');
  const bashRequest = interrupt.bash_request;
  const detailRequest = interrupt.detail_request;
  const checkpointId = interrupt.checkpoint_id;
  const contexts = interrupt.interrupt_contexts || [];
  const fullCommand = bashRequest?.raw_command || [bashRequest?.command, ...(bashRequest?.args || [])].filter(Boolean).join(' ');
  const isDetailSelection = Boolean(detailRequest?.question && detailRequest?.options?.length);
  const isCommandApproval = Boolean(fullCommand);
  const approvalPurpose = isDetailSelection
    ? detailRequest?.reason?.trim() || extractInterruptPurpose(interrupt.message, contexts)
    : bashRequest?.reason?.trim() || extractInterruptPurpose(interrupt.message, contexts);
  const cardTitle = isDetailSelection ? '补充细节' : (isCommandApproval ? '执行确认' : '人工确认');

  useEffect(() => {
    setIsHandled(Boolean(interrupt.handled));
    setIsSubmitting(false);
    setErrorText('');
    setLastAction('');
    setSelectedValue('');
  }, [
    interrupt.checkpoint_id,
    interrupt.bash_request?.raw_command,
    interrupt.detail_request?.question,
    interrupt.detail_request?.field,
    interrupt.detail_request?.options?.map((item) => item.value).join('|')
  ]);

  const submitResume = async (
    actionName: string,
    payload: { approved?: boolean; resolved?: boolean; selection_value?: string }
  ) => {
    if (isSubmitting || isHandled) {
      return;
    }
    if (!checkpointId) {
      setErrorText('缺少 checkpoint_id，无法恢复执行');
      return;
    }
    if (!isOps && !currentSessionId) {
      setErrorText('缺少会话 ID，无法恢复执行');
      return;
    }

    setErrorText('');
    setCopySuccess(false);
    setLastAction(actionName);
    if (payload.selection_value) {
      setSelectedValue(payload.selection_value);
    }
    setIsSubmitting(true);
    let pausedByInterrupt = false;
    let resumedStepId = '';
    const interruptIDs = contexts
      .map((item) => item?.id)
      .filter((id): id is string => Boolean(id));
    if (interruptIDs.length === 0) {
      setIsSubmitting(false);
      setStreaming(false);
      setConnectionStatus('error');
      setErrorText('缺少 interrupt_ids，无法恢复到具体中断点');
      return;
    }

    setStreaming(true);
    setConnectionStatus('streaming');
    if (isOps) {
      setOpsRunning(true);
      if (opsStepId) {
        markOpsInterruptHandled(opsStepId, true);
        updateOpsStep(opsStepId, undefined, 'completed');
      }
    }

    if (!isOps && currentSessionId) {
      addMessage(currentSessionId, {
        role: 'assistant',
        type: 'text',
        content: ''
      });
    }

    const onContent = (content: string) => {
      if (isOps && opsStepId) {
        const normalized = (content || '').trim();
        if (!normalized) {
          return;
        }
        if (!resumedStepId) {
          resumedStepId = addOpsStep(inferOpsResumeStepTitle(normalized, actionName));
        }
        updateOpsStep(resumedStepId, content);
        return;
      }
      if (currentSessionId) {
        updateLastMessage(currentSessionId, content);
      }
    };

    const onInterrupt = (nextInterrupt: InterruptData) => {
      if (isOps && opsStepId) {
        pausedByInterrupt = true;
        if (resumedStepId) {
          updateOpsStep(resumedStepId, undefined, 'completed');
        }
        resumedStepId = addOpsStep(inferInterruptStepTitle(nextInterrupt), '', 'pending', nextInterrupt);
        return;
      }
      if (currentSessionId) {
        updateLastMessage(currentSessionId, '', undefined, nextInterrupt);
      }
    };

    const options = {
      onContent,
      onStep: (step: any) => {
        if (isOps && opsStepId) {
          if (resumedStepId) {
            updateOpsStep(resumedStepId, undefined, 'completed');
          }
          resumedStepId = addOpsStep(step?.content || inferOpsResumeStepTitle('', actionName));
          return;
        }
        if (currentSessionId) {
          appendStepToLastMessage(currentSessionId, {
            ...step,
            status: 'pending'
          });
        }
      },
      onInterrupt,
      onDone: () => {
        setStreaming(false);
        setConnectionStatus('idle');
        setIsSubmitting(false);
        setIsHandled(true);
        if (isOps && opsStepId) {
          if (resumedStepId) {
            updateOpsStep(resumedStepId, undefined, 'completed');
          }
          setOpsRunning(pausedByInterrupt);
        } else if (currentSessionId) {
          setLastMessageStepStatus(currentSessionId, 'completed');
        }
      },
      onError: (err: string) => {
        setStreaming(false);
        setConnectionStatus('error');
        setIsSubmitting(false);
        if (isOps) {
          setOpsRunning(false);
        }
        setErrorText(err || '恢复执行失败');
        if (isOps && opsStepId) {
          const targetStepId = resumedStepId || addOpsStep('流程异常');
          updateOpsStep(targetStepId, `\n\nError: ${err}`, 'error');
          return;
        }
        if (currentSessionId) {
          setLastMessageStepStatus(currentSessionId, 'error');
          updateLastMessage(currentSessionId, `\n\nError: ${err}`);
        }
      }
    };

    try {
      const requestPayload = {
        ...payload,
        interrupt_ids: interruptIDs
      };
      if (isOps) {
        await resumeOps(checkpointId, requestPayload, options);
      } else if (currentSessionId) {
        await resumeChat(currentSessionId, checkpointId, requestPayload, options);
      }
    } catch (error) {
      setIsSubmitting(false);
      setStreaming(false);
      setConnectionStatus('error');
      setErrorText(error instanceof Error ? error.message : '恢复执行失败');
    }
  };

  const handleAction = async (actionName: string, approved: boolean, resolved: boolean) => {
    return submitResume(actionName, { approved, resolved });
  };

  const handleSelection = async (label: string, value: string) => {
    return submitResume(`选择：${label}`, { selection_value: value });
  };

  const handleCopy = async () => {
    if (!fullCommand) {
      return;
    }
    try {
      await navigator.clipboard.writeText(fullCommand);
      setCopySuccess(true);
      setTimeout(() => setCopySuccess(false), 1500);
    } catch (_) {
      setErrorText('复制失败，请手动复制命令');
    }
  };

  const actionButtons = isDetailSelection ? []
    : isCommandApproval
    ? [
        { key: 'approved', label: '准许执行', approved: true, resolved: false },
        { key: 'resolved', label: '标记为已解决', approved: true, resolved: true },
        { key: 'reject', label: '拒绝请求', approved: false, resolved: false },
      ]
    : [
        { key: 'approved', label: '继续执行', approved: true, resolved: false },
        { key: 'resolved', label: '已修复完成', approved: true, resolved: true },
        { key: 'reject', label: '停止处理', approved: false, resolved: false },
      ];

  return (
    <div className={cn(
      "my-4 p-5 transition-all clip-path-corner border-2 backdrop-blur-sm",
      theme === 'dark'
        ? "bg-black/75 border-[#F59E0B]/70 shadow-[0_0_24px_rgba(245,158,11,0.2)]"
        : "bg-white/90 border-[#F59E0B]/70 shadow-[0_0_18px_rgba(245,158,11,0.18)]",
      isHandled ? "opacity-80" : "animate-in fade-in zoom-in-95 duration-500"
    )}>
      <div className="flex items-start gap-4 mb-4">
        <div className={cn(
          "p-2 rounded-lg",
          theme === 'dark'
            ? "bg-[#F59E0B]/20 text-[#F59E0B]"
            : "bg-orange-100 text-orange-700"
        )}>
          <ShieldAlert className="w-6 h-6" />
        </div>
        <div>
          <h3 className="font-display font-black text-base mb-1 tracking-tight">
            {cardTitle}
          </h3>
        </div>
      </div>

      {fullCommand && (
        <div className="mb-4 space-y-2">
          <div className="rounded-xl border border-[#F59E0B]/40 bg-black/80">
            <div className="flex items-center justify-between px-3 py-2 border-b border-[#F59E0B]/30">
              <span className="text-[10px] font-bold uppercase tracking-widest text-[#F59E0B]">待执行命令</span>
              <button
                type="button"
                onClick={handleCopy}
                disabled={isSubmitting || !fullCommand}
                className={cn(
                  "text-[10px] font-bold uppercase tracking-widest px-2 py-1 rounded border transition-all inline-flex items-center gap-1",
                  copySuccess
                    ? "border-green-500/60 text-green-400"
                    : "border-cyber-neon/40 text-cyber-neon hover:bg-cyber-neon/10"
                )}
              >
                <Copy className="w-3 h-3" />
                {copySuccess ? '已复制' : '复制'}
              </button>
            </div>
            <pre className="p-3 font-mono text-xs text-cyber-neon overflow-x-auto overflow-y-auto max-h-40 custom-scrollbar whitespace-pre-wrap break-all">
              {fullCommand}
            </pre>
          </div>
          <div className="text-xs rounded-lg border border-white/10 bg-black/50 p-3">
            <span className="opacity-60 mr-2">指令作用：</span>
            <span className="opacity-90">{approvalPurpose || 'Agent 未提供具体作用说明'}</span>
          </div>
        </div>
      )}

      {!fullCommand && approvalPurpose && (
        <div className="mb-4 space-y-2">
          <div className="text-xs rounded-lg border border-white/10 bg-black/50 p-3">
            <span className="opacity-60 mr-2">{isDetailSelection ? '补充原因：' : '确认事项：'}</span>
            <span className="opacity-90">{approvalPurpose}</span>
          </div>
        </div>
      )}

      {isDetailSelection && detailRequest && (
        <div className="mb-4 space-y-3">
          <div className="text-xs rounded-lg border border-cyber-neon/30 bg-black/60 p-3">
            <span className="opacity-60 mr-2">请选择：</span>
            <span className="opacity-95">{detailRequest.question}</span>
          </div>
        </div>
      )}

      <div className="space-y-4">
        {errorText && (
          <div className="text-xs text-red-400 bg-red-500/10 border border-red-500/30 px-3 py-2 rounded-lg">
            {errorText}
          </div>
        )}

        {isSubmitting && (
          <div className="text-xs text-cyber-neon flex items-center gap-2 font-mono">
            <Loader2 className="w-4 h-4 animate-spin" />
            {isDetailSelection ? '正在提交选择并恢复执行' : '正在提交审批并恢复执行'}（{lastAction || '处理中'}）...
          </div>
        )}

        {isHandled && !errorText && (
          <div className="text-xs text-green-400 flex items-center gap-2 font-mono">
            <CheckCircle2 className="w-4 h-4" />
            {isDetailSelection ? '已提交选择，后续流式结果将持续输出。' : '已提交审批，后续流式结果将持续输出。'}
          </div>
        )}

        {isDetailSelection && detailRequest ? (
          <div className="grid grid-cols-1 gap-3">
            {detailRequest.options.map((option) => {
              const isSelected = selectedValue === option.value;
              return (
                <button
                  key={`${detailRequest.field}-${option.value}`}
                  type="button"
                  onClick={() => handleSelection(option.label, option.value)}
                  disabled={isHandled || isSubmitting}
                  className={cn(
                    "text-left rounded-xl border px-4 py-3 transition-all",
                    "flex items-start justify-between gap-3",
                    isSelected
                      ? "border-cyber-neon bg-cyber-neon/10 text-cyber-neon"
                      : "border-white/10 bg-black/50 hover:border-cyber-neon/40 hover:bg-cyber-neon/5",
                    (isHandled || isSubmitting) && "opacity-60 cursor-not-allowed"
                  )}
                >
                  <div className="min-w-0">
                    <div className="text-xs font-bold tracking-wide">{option.label}</div>
                    {option.description && (
                      <div className="mt-1 text-[11px] opacity-70 leading-relaxed">{option.description}</div>
                    )}
                  </div>
                  <div className={cn(
                    "mt-0.5 text-[10px] font-bold uppercase tracking-widest",
                    isSelected ? "text-cyber-neon" : "opacity-40"
                  )}>
                    {isSelected ? '已选择' : '选择'}
                  </div>
                </button>
              );
            })}
          </div>
        ) : (
          <div className="grid grid-cols-3 gap-3">
            {actionButtons.map((action) => {
              const icon =
                action.key === 'approved' ? <Play className="w-4 h-4" /> :
                action.key === 'resolved' ? <CheckCircle2 className="w-4 h-4" /> :
                <RotateCcw className="w-4 h-4" />;

              const buttonClass =
                action.key === 'approved'
                  ? (theme === 'dark'
                    ? "border-green-500/40 hover:bg-green-500/15 text-green-400"
                    : "border-green-600/30 hover:bg-green-600/10 text-green-600")
                  : action.key === 'resolved'
                    ? (theme === 'dark'
                      ? "border-blue-500/40 hover:bg-blue-500/20 text-blue-300"
                      : "border-blue-600/30 hover:bg-blue-600/10 text-blue-700")
                    : (theme === 'dark'
                      ? "border-red-500/40 hover:bg-red-500/15 text-red-400"
                      : "border-red-600/30 hover:bg-red-600/10 text-red-600");

              return (
                <button
                  key={action.key}
                  onClick={() => handleAction(action.label, action.approved, action.resolved)}
                  disabled={isHandled || isSubmitting}
                  className={cn(
                    "flex flex-col items-center justify-center gap-1 p-3 rounded-xl border transition-all text-[10px] font-bold uppercase tracking-widest",
                    buttonClass,
                    (isHandled || isSubmitting) && "opacity-60 cursor-not-allowed"
                  )}
                >
                  {icon}
                  <span>{action.label}</span>
                </button>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
};

function inferOpsResumeStepTitle(content: string, actionName: string): string {
  const text = (content || '').trim();
  if (text.includes('运维技术报告') || text.includes('最终状态') || text.includes('是否已解决')) {
    return '输出最终技术报告';
  }
  if (text.includes('调用工具:')) {
    return text;
  }
  if (actionName) {
    return `审批后继续：${actionName}`;
  }
  return '继续执行';
}

function inferInterruptStepTitle(interrupt: InterruptData): string {
  if (interrupt.detail_request) {
    return '补充细节';
  }
  return interrupt.bash_request?.raw_command ? '执行确认' : '人工确认';
}

function extractInterruptPurpose(message: string, contexts: InterruptData['interrupt_contexts']): string {
  const candidates = [
    message,
    ...contexts.map((ctx) => ctx.info),
  ]
    .map((item) => (item || '').trim())
    .filter(Boolean);

  for (const candidate of candidates) {
    if (isGenericInterruptText(candidate)) {
      continue;
    }
    return candidate;
  }

  return '';
}

function isGenericInterruptText(text: string): boolean {
  const normalized = text.replace(/\s+/g, '');
  return (
    normalized.includes('流程已暂停，等待你的确认。') ||
    normalized.includes('流程已暂停，等待确认。') ||
    normalized.includes('当前流程需要人工确认后继续。')
  );
}
