import React, { useEffect, useRef } from 'react';
import { useStore } from '../store/useStore';
import { 
  X, Minus, Terminal, CheckCircle2, AlertCircle, 
  Loader2, Play, RotateCcw, MessageCircle, ChevronRight,
  Activity, Cpu, Shield, HardDrive
} from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { motion, AnimatePresence } from 'motion/react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { resumeOps } from '../services/api';
import { InterruptCard } from './InterruptCard';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export const OpsPanel: React.FC = () => {
  const { 
    theme, isOpsPanelOpen, setOpsPanelOpen, opsSteps, 
    currentOpsTask, isOpsRunning, updateOpsStep, runOps 
  } = useStore();
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [opsSteps]);

  if (!isOpsPanelOpen) return null;

  return (
    <motion.div
      initial={{ y: -100, opacity: 0 }}
      animate={{ y: 0, opacity: 1 }}
      exit={{ y: -100, opacity: 0 }}
      className={cn(
        "fixed top-0 left-1/2 -translate-x-1/2 w-[90%] max-w-5xl z-[100] mt-4",
        "backdrop-blur-xl border-2 overflow-hidden shadow-[0_20px_50px_rgba(0,0,0,0.5)] clip-path-corner",
        theme === 'dark' ? "bg-black/80 border-cyber-neon/30" : "bg-white/80 border-cyber-purple/30"
      )}
    >
      {/* Technical Corner Accents */}
      <div className="absolute top-0 left-0 w-8 h-8 border-t-2 border-l-2 border-cyber-neon opacity-40" />
      <div className="absolute bottom-0 right-0 w-8 h-8 border-b-2 border-r-2 border-cyber-neon opacity-40" />

      {/* Header */}
      <div className={cn(
        "px-6 py-4 border-b flex items-center justify-between",
        theme === 'dark' ? "border-white/10 bg-white/5" : "border-black/10 bg-black/5"
      )}>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2 px-3 py-1 rounded-full bg-cyber-neon/10 border border-cyber-neon/20">
            <Activity className="w-3.5 h-3.5 text-cyber-neon animate-pulse" />
            <span className="text-[10px] font-display font-black text-cyber-neon uppercase tracking-widest">AI Ops Active</span>
          </div>
          <h2 className="text-xs font-display font-bold opacity-80 uppercase tracking-wider">
            <span className="opacity-40 mr-2">Task:</span>
            {currentOpsTask || 'System Diagnostic'}
          </h2>
        </div>
        <div className="flex items-center gap-2">
          {!isOpsRunning && opsSteps.length > 0 && (
            <button 
              onClick={() => runOps(currentOpsTask || 'MySQL 性能诊断')}
              className="flex items-center gap-2 px-3 py-1 rounded-lg bg-cyber-neon/10 border border-cyber-neon/30 text-cyber-neon text-[10px] font-bold uppercase tracking-widest hover:bg-cyber-neon/20 transition-all"
            >
              <RotateCcw className="w-3 h-3" />
              重新执行
            </button>
          )}
          <button 
            onClick={() => setOpsPanelOpen(false)}
            className="p-2 hover:bg-white/10 rounded-lg transition-colors opacity-40 hover:opacity-100"
          >
            <Minus className="w-4 h-4" />
          </button>
          <button 
            onClick={() => setOpsPanelOpen(false)}
            className="p-2 hover:bg-red-500/20 hover:text-red-500 rounded-lg transition-colors opacity-40 hover:opacity-100"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Content */}
      <div 
        ref={scrollRef}
        className="max-h-[70vh] overflow-y-auto p-6 space-y-4 scroll-smooth no-scrollbar"
      >
        <AnimatePresence initial={false}>
          {opsSteps.length === 0 && !isOpsRunning && (
            <div className="py-20 text-center">
              <Terminal className="w-12 h-12 mx-auto mb-4 opacity-20" />
              <p className="text-sm font-mono opacity-40 mb-6">等待指令输入...</p>
              <button 
                onClick={() => runOps('MySQL 性能诊断')}
                className="px-6 py-2 rounded-xl bg-cyber-neon text-black font-display font-black text-xs uppercase tracking-widest hover:scale-105 transition-transform shadow-[0_0_20px_rgba(0,243,255,0.4)]"
              >
                开始系统诊断
              </button>
            </div>
          )}
          
          {opsSteps.map((step, idx) => (
            <motion.div
              key={step.id}
              initial={{ x: -20, opacity: 0 }}
              animate={{ x: 0, opacity: 1 }}
              transition={{ duration: 0.4, ease: "easeOut" }}
              className={cn(
                "rounded-2xl border transition-all duration-500 overflow-hidden",
                step.status === 'error' ? "bg-red-500/5 border-red-500/20" : 
                step.status === 'completed' ? "bg-white/5 border-white/10" : 
                "bg-cyber-neon/5 border-cyber-neon/30 shadow-[0_0_15px_rgba(0,243,255,0.05)]"
              )}
            >
              {/* Step Header */}
              <div className={cn(
                "px-4 py-3 flex items-center justify-between border-b",
                step.status === 'error' ? "border-red-500/10 bg-red-500/5" : 
                step.status === 'completed' ? "border-white/5 bg-white/5" : 
                "border-cyber-neon/10 bg-cyber-neon/5"
              )}>
                <div className="flex items-center gap-3">
                  <div className={cn(
                    "w-6 h-6 rounded-lg flex items-center justify-center border",
                    step.status === 'completed' ? "bg-green-500/20 border-green-500 text-green-500" :
                    step.status === 'error' ? "bg-red-500/20 border-red-500 text-red-500" :
                    "bg-cyber-neon/20 border-cyber-neon text-cyber-neon"
                  )}>
                    {step.status === 'completed' ? <CheckCircle2 className="w-3.5 h-3.5" /> :
                     step.status === 'error' ? <AlertCircle className="w-3.5 h-3.5" /> :
                     <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-[10px] font-mono opacity-40 uppercase tracking-widest">Step {idx + 1}</span>
                    <span className="text-xs font-bold tracking-tight">调用工具: {step.toolName}</span>
                  </div>
                </div>
                {step.status === 'pending' && (
                  <div className="flex gap-1">
                    <div className="w-1 h-1 rounded-full bg-cyber-neon animate-ping" />
                    <div className="w-1 h-1 rounded-full bg-cyber-neon animate-ping [animation-delay:0.2s]" />
                    <div className="w-1 h-1 rounded-full bg-cyber-neon animate-ping [animation-delay:0.4s]" />
                  </div>
                )}
              </div>

              {/* Step Content */}
              <div className="p-4">
                <div className="prose prose-sm dark:prose-invert max-w-none font-mono text-xs leading-relaxed opacity-90">
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>
                    {step.content || (step.status === 'pending' ? '正在执行分析...' : '')}
                  </ReactMarkdown>
                </div>

                {/* Error Actions */}
                {step.status === 'error' && (
                  <div className="mt-4 flex gap-2">
                    <button 
                      onClick={() => runOps(currentOpsTask)}
                      className="px-4 py-1.5 rounded-lg bg-red-500/20 border border-red-500/40 text-red-400 text-[10px] font-bold uppercase tracking-widest hover:bg-red-500/30 transition-all flex items-center gap-2"
                    >
                      <RotateCcw className="w-3 h-3" />
                      重试任务
                    </button>
                  </div>
                )}

                {/* Interrupt UI */}
                {step.interrupt && (
                  step.interrupt.bash_request ? (
                    <InterruptCard
                      interrupt={step.interrupt}
                      isOps
                      opsStepId={step.id}
                    />
                  ) : (
                    <div className="mt-4 p-4 rounded-xl bg-black/40 border border-cyber-orange/30 animate-in fade-in zoom-in-95 duration-500">
                      <div className="flex items-start gap-3 mb-4">
                        <RotateCcw className="w-5 h-5 text-cyber-orange mt-1" />
                        <div>
                          <div className="text-xs font-bold text-cyber-orange uppercase tracking-widest mb-1">需要人工确认 // INTERRUPT</div>
                          <p className="text-xs opacity-80">{step.interrupt.message}</p>
                        </div>
                      </div>
                      
                      <div className="flex gap-2">
                        <button 
                          onClick={() => handleResume(
                            step.id,
                            step.interrupt!.checkpoint_id,
                            true,
                            false,
                            (step.interrupt?.interrupt_contexts || []).map((item) => item.id).filter(Boolean)
                          )}
                          className="flex-1 py-2 rounded-lg bg-green-500/20 border border-green-500/40 text-green-400 text-[10px] font-bold uppercase tracking-widest hover:bg-green-500/30 transition-all"
                        >
                          继续执行
                        </button>
                        <button 
                          onClick={() => handleResume(
                            step.id,
                            step.interrupt!.checkpoint_id,
                            true,
                            true,
                            (step.interrupt?.interrupt_contexts || []).map((item) => item.id).filter(Boolean)
                          )}
                          className="flex-1 py-2 rounded-lg bg-blue-600 text-white text-[10px] font-bold uppercase tracking-widest hover:bg-blue-500 transition-all shadow-[0_0_15px_rgba(37,99,235,0.3)]"
                        >
                          已修复完成
                        </button>
                      </div>
                    </div>
                  )
                )}
              </div>
            </motion.div>
          ))}
        </AnimatePresence>
      </div>

      {/* Footer / Status Bar */}
      <div className={cn(
        "px-6 py-3 border-t flex items-center justify-between text-[10px] font-mono uppercase tracking-widest opacity-40",
        theme === 'dark' ? "bg-white/5" : "bg-black/5"
      )}>
        <div className="flex items-center gap-4">
          <span>Node: 0x7F_Ops</span>
          <span>Status: {isOpsRunning ? 'Executing...' : 'Idle'}</span>
        </div>
        <div>
          {new Date().toLocaleTimeString()} // Secure_Channel
        </div>
      </div>
    </motion.div>
  );
};

async function handleResume(stepId: string, checkpointId: string, approved: boolean, resolved: boolean, interruptIDs: string[]) {
  const { updateOpsStep, setStreaming, setConnectionStatus } = useStore.getState();
  
  setStreaming(true);
  setConnectionStatus('streaming');

  const options = {
    onContent: (content: string) => updateOpsStep(stepId, content),
    onStep: (step: any) => {
      // Handle nested steps if needed, but for now we just append content
    },
    onInterrupt: (interrupt: any) => updateOpsStep(stepId, undefined, undefined, interrupt),
    onDone: () => {
      setStreaming(false);
      setConnectionStatus('idle');
      updateOpsStep(stepId, undefined, 'completed');
    },
    onError: (err: string) => {
      setStreaming(false);
      setConnectionStatus('error');
      updateOpsStep(stepId, `\n\nError: ${err}`, 'error');
    }
  };

  await resumeOps(checkpointId, { approved, resolved, interrupt_ids: interruptIDs }, options);
}
