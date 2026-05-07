import React, { useEffect, useState } from 'react';
import { useStore } from '../store/useStore';
import { CheckCircle2, Loader2, ShieldAlert } from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { resumeChat } from '../services/api';
import { InterruptData } from '../types';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

interface InterruptCardProps {
  interrupt: InterruptData;
}

export const InterruptCard: React.FC<InterruptCardProps> = ({ 
  interrupt
}) => {
  const {
    theme,
    currentSessionId,
    addMessage,
    updateLastMessage,
    appendStepToLastMessage,
    setLastMessageStepStatus,
    setStreaming,
    setConnectionStatus
  } = useStore();
  const [isHandled, setIsHandled] = useState(Boolean(interrupt.handled));
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorText, setErrorText] = useState('');
  const [lastAction, setLastAction] = useState('');
  const [selectedValue, setSelectedValue] = useState('');
  const detailRequest = interrupt.detail_request;
  const commandApproval = interrupt.command_approval;
  const checkpointId = interrupt.checkpoint_id;
  const contexts = interrupt.interrupt_contexts || [];
  const isDetailSelection = Boolean(detailRequest?.question && detailRequest?.options?.length);
  const approvalPurpose = detailRequest?.reason?.trim() || commandApproval?.reason?.trim() || extractInterruptPurpose(interrupt.message, contexts);
  const cardTitle = isDetailSelection ? '补充细节' : '等待确认';

  useEffect(() => {
    setIsHandled(Boolean(interrupt.handled));
    setIsSubmitting(false);
    setErrorText('');
    setLastAction('');
    setSelectedValue('');
  }, [
    interrupt.checkpoint_id,
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
    if (!currentSessionId) {
      setErrorText('缺少会话 ID，无法恢复执行');
      return;
    }

    setErrorText('');
    setLastAction(actionName);
    if (payload.selection_value) {
      setSelectedValue(payload.selection_value);
    }
    setIsSubmitting(true);
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

    if (currentSessionId) {
      addMessage(currentSessionId, {
        role: 'assistant',
        type: 'text',
        content: ''
      });
    }

    const onContent = (content: string) => {
      if (currentSessionId) {
        updateLastMessage(currentSessionId, content);
      }
    };

    const onInterrupt = (nextInterrupt: InterruptData) => {
      if (currentSessionId) {
        updateLastMessage(currentSessionId, '', undefined, nextInterrupt);
      }
    };

    const options = {
      onContent,
      onStep: (step: any) => {
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
        if (currentSessionId) {
          setLastMessageStepStatus(currentSessionId, 'completed');
        }
      },
      onError: (err: string) => {
        setStreaming(false);
        setConnectionStatus('error');
        setIsSubmitting(false);
        setErrorText(err || '恢复执行失败');
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
      if (currentSessionId) {
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

      {approvalPurpose && (
        <div className="mb-4 space-y-2">
          <div className="text-xs rounded-lg border border-white/10 bg-black/50 p-3">
            <span className="opacity-60 mr-2">{isDetailSelection ? '补充原因：' : '确认事项：'}</span>
            <span className="opacity-90">{approvalPurpose}</span>
          </div>
        </div>
      )}

      {!isDetailSelection && commandApproval && (
        <div className="mb-4 space-y-2 text-xs rounded-lg border border-white/10 bg-black/50 p-3 font-mono">
          <div>
            <span className="opacity-60 mr-2">命令：</span>
            <span className="opacity-95 break-all">{commandApproval.command}</span>
          </div>
          {commandApproval.args && commandApproval.args.length > 0 && (
            <div>
              <span className="opacity-60 mr-2">参数：</span>
              <span className="opacity-95 break-all">{commandApproval.args.join(' ')}</span>
            </div>
          )}
          {commandApproval.script && (
            <div>
              <span className="opacity-60 mr-2">脚本：</span>
              <span className="opacity-95 whitespace-pre-wrap break-all">{commandApproval.script}</span>
            </div>
          )}
          {typeof commandApproval.timeout === 'number' && (
            <div>
              <span className="opacity-60 mr-2">超时：</span>
              <span className="opacity-95">{commandApproval.timeout}s</span>
            </div>
          )}
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
            {isDetailSelection ? '正在提交选择并恢复执行' : '正在提交确认并恢复执行'}（{lastAction || '处理中'}）...
          </div>
        )}

        {isHandled && !errorText && (
          <div className="text-xs text-green-400 flex items-center gap-2 font-mono">
            <CheckCircle2 className="w-4 h-4" />
            {isDetailSelection ? '已提交选择，后续流式结果将持续输出。' : '已提交确认，后续流式结果将持续输出。'}
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
          <button
            type="button"
            onClick={() => handleAction('继续执行', true, false)}
            disabled={isHandled || isSubmitting}
            className={cn(
              "w-full flex items-center justify-center gap-2 p-3 rounded-xl border transition-all text-[10px] font-bold uppercase tracking-widest",
              theme === 'dark'
                ? "border-green-500/40 hover:bg-green-500/15 text-green-400"
                : "border-green-600/30 hover:bg-green-600/10 text-green-600",
              (isHandled || isSubmitting) && "opacity-60 cursor-not-allowed"
            )}
          >
            <CheckCircle2 className="w-4 h-4" />
            <span>继续执行</span>
          </button>
        )}
      </div>
    </div>
  );
};

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
