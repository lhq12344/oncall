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
  const { theme, currentSessionId, updateLastMessage, updateOpsStep, setStreaming, setConnectionStatus } = useStore();
  const [comment, setComment] = useState('');
  const [isHandled, setIsHandled] = useState(Boolean(interrupt.handled));
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorText, setErrorText] = useState('');
  const [copySuccess, setCopySuccess] = useState(false);
  const [lastAction, setLastAction] = useState('');
  const bashRequest = interrupt.bash_request;
  const checkpointId = interrupt.checkpoint_id;
  const contexts = interrupt.interrupt_contexts || [];
  const fullCommand = bashRequest?.raw_command || [bashRequest?.command, ...(bashRequest?.args || [])].filter(Boolean).join(' ');

  useEffect(() => {
    setIsHandled(Boolean(interrupt.handled));
    setIsSubmitting(false);
    setErrorText('');
    setLastAction('');
  }, [interrupt.checkpoint_id, interrupt.message, interrupt.bash_request?.raw_command]);

  const handleAction = async (actionName: string, approved: boolean, resolved: boolean) => {
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
    setIsSubmitting(true);
    setStreaming(true);
    setConnectionStatus('streaming');

    const onContent = (content: string) => {
      if (isOps && opsStepId) {
        updateOpsStep(opsStepId, content);
        return;
      }
      if (currentSessionId) {
        updateLastMessage(currentSessionId, content);
      }
    };

    const onInterrupt = (nextInterrupt: InterruptData) => {
      if (isOps && opsStepId) {
        updateOpsStep(opsStepId, undefined, undefined, nextInterrupt);
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
          updateOpsStep(opsStepId, `\n${step?.content || ''}`);
          return;
        }
        if (currentSessionId) {
          updateLastMessage(currentSessionId, '', [step]);
        }
      },
      onInterrupt,
      onDone: () => {
        setStreaming(false);
        setConnectionStatus('idle');
        setIsSubmitting(false);
        setIsHandled(true);
        if (isOps && opsStepId) {
          updateOpsStep(opsStepId, undefined, 'completed');
        }
      },
      onError: (err: string) => {
        setStreaming(false);
        setConnectionStatus('error');
        setIsSubmitting(false);
        setErrorText(err || '恢复执行失败');
        if (isOps && opsStepId) {
          updateOpsStep(opsStepId, `\n\nError: ${err}`, 'error');
          return;
        }
        if (currentSessionId) {
          updateLastMessage(currentSessionId, `\n\nError: ${err}`);
        }
      }
    };

    try {
      const payload = { approved, resolved, comment: comment.trim() };
      if (isOps) {
        await resumeOps(checkpointId, payload, options);
      } else if (currentSessionId) {
        await resumeChat(currentSessionId, checkpointId, payload, options);
      }
    } catch (error) {
      setIsSubmitting(false);
      setStreaming(false);
      setConnectionStatus('error');
      setErrorText(error instanceof Error ? error.message : '恢复执行失败');
    }
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

  const actionButtons = [
    { key: 'approved', label: '准许执行', approved: true, resolved: false },
    { key: 'resolved', label: '标记为已解决', approved: true, resolved: true },
    { key: 'reject', label: '拒绝请求', approved: false, resolved: false },
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
            💡 动作请求：执行系统命令
          </h3>
          <p className="text-xs opacity-80 whitespace-pre-wrap break-words">
            {interrupt.message}
          </p>
          <p className="text-[10px] mt-2 opacity-60 font-mono">
            Checkpoint: {checkpointId}
          </p>
        </div>
      </div>

      {bashRequest && (
        <div className="mb-4 space-y-2">
          <div className="rounded-xl border border-[#F59E0B]/40 bg-black/80">
            <div className="flex items-center justify-between px-3 py-2 border-b border-[#F59E0B]/30">
              <span className="text-[10px] font-bold uppercase tracking-widest text-[#F59E0B]">Command Preview</span>
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
            <span className="opacity-60 mr-2">执行原因：</span>
            <span className="opacity-90">{bashRequest.reason || 'Agent 未提供具体原因'}</span>
          </div>
        </div>
      )}

      {!bashRequest && contexts.length > 0 && (
        <div className="mb-4 space-y-2">
          {contexts.map((ctx, i) => (
            <div key={i} className="text-xs font-mono p-2 bg-black/20 rounded border border-white/5">
              <span className="text-cyber-neon">[{ctx.address}]</span> {ctx.info}
            </div>
          ))}
        </div>
      )}

      <div className="space-y-4">
        <div>
          <textarea
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            disabled={isHandled || isSubmitting}
            placeholder="可选备注（如审批理由、人工处理说明）"
            className={cn(
              "w-full px-3 py-2 rounded-xl text-sm min-h-[80px] transition-all outline-none border",
              theme === 'dark' 
                ? "bg-black/50 border-cyber-neon/20 focus:border-cyber-neon/50" 
                : "bg-white border-cyber-purple/20 focus:border-cyber-purple/50"
            )}
          />
        </div>

        {errorText && (
          <div className="text-xs text-red-400 bg-red-500/10 border border-red-500/30 px-3 py-2 rounded-lg">
            {errorText}
          </div>
        )}

        {isSubmitting && (
          <div className="text-xs text-cyber-neon flex items-center gap-2 font-mono">
            <Loader2 className="w-4 h-4 animate-spin" />
            正在提交审批并恢复执行（{lastAction || '处理中'}）...
          </div>
        )}

        {isHandled && !errorText && (
          <div className="text-xs text-green-400 flex items-center gap-2 font-mono">
            <CheckCircle2 className="w-4 h-4" />
            已提交审批，后续流式结果将持续输出。
          </div>
        )}

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
      </div>
    </div>
  );
};
