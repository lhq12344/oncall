import React, { useEffect, useState, useRef } from 'react';
import { useStore } from '../store/useStore';
import { Send, Paperclip, Loader2, Terminal } from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { fetchSlashCommands, uploadFile } from '../services/api';
import { SlashCommandInfo } from '../types';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

const FALLBACK_COMMANDS: SlashCommandInfo[] = [
  { name: 'help', aliases: ['h', '?'], description: '列出所有斜杠命令', argument_hint: '[command]', type: 'local', source: 'builtin' },
  { name: 'commands', description: '显示斜杠命令加载来源与警告', type: 'local', source: 'builtin' },
  { name: 'status', aliases: ['s'], description: '显示 OnCall 运行状态', type: 'local', source: 'builtin' },
  { name: 'session', description: '显示当前会话概览', type: 'local', source: 'builtin' },
  { name: 'memory', description: '显示最近会话记忆摘要', argument_hint: '[list]', type: 'local', source: 'builtin' },
  { name: 'review', description: '审查当前代码变更', argument_hint: '[focus]', type: 'prompt', source: 'builtin' },
  { name: 'diagnose', aliases: ['diag'], description: '诊断故障症状', argument_hint: '<symptom>', type: 'prompt', source: 'builtin' },
  { name: 'ops', aliases: ['incident', 'aiops'], description: '触发完整 AI 运维处置', argument_hint: '<incident>', type: 'ops_workflow', source: 'builtin' },
  { name: 'k8s', aliases: ['pods'], description: '只读检查 Kubernetes 状态', argument_hint: '[resource] [-n namespace]', type: 'prompt', source: 'builtin' },
  { name: 'metrics', aliases: ['prom'], description: '查询 Prometheus 指标', argument_hint: '<query>', type: 'prompt', source: 'builtin' },
  { name: 'logs', aliases: ['last-error', 'errors'], description: '查询最近错误日志', argument_hint: '[query|error] [time_range]', type: 'prompt', source: 'builtin' },
  { name: 'cases', description: '检索历史故障案例', argument_hint: '<query>', type: 'prompt', source: 'builtin' },
  { name: 'clear', description: '清空当前前端会话', type: 'client_action', source: 'builtin' },
];

export const InputArea: React.FC = () => {
  const { theme, currentSessionId, addSession, setStreaming, setConnectionStatus, isStreaming, sendMessage, addMessage } = useStore();
  const [input, setInput] = useState('');
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [slashCommands, setSlashCommands] = useState<SlashCommandInfo[]>(FALLBACK_COMMANDS);
  const [selectedSlashIndex, setSelectedSlashIndex] = useState(0);
  const [dismissedSlashInput, setDismissedSlashInput] = useState('');
  const fileInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let cancelled = false;
    fetchSlashCommands()
      .then((commands) => {
        if (!cancelled && commands.length > 0) {
          setSlashCommands(commands);
        }
      })
      .catch(() => {
        // Static fallback keeps slash UX available when backend discovery is unavailable.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const slashQuery = input.startsWith('/') && !/\s/.test(input) ? input.slice(1).toLowerCase() : null;
  const slashSuggestions = slashQuery === null || dismissedSlashInput === input
    ? []
    : slashCommands
        .filter((cmd) => {
          const aliases = cmd.aliases || [];
          return cmd.name.toLowerCase().startsWith(slashQuery)
            || aliases.some((alias) => alias.toLowerCase().startsWith(slashQuery));
        })
        .slice(0, 8);
  const activeSlashIndex = Math.min(selectedSlashIndex, Math.max(slashSuggestions.length - 1, 0));

  const completeSlashCommand = (command: SlashCommandInfo) => {
    setInput(`/${command.name} `);
    setSelectedSlashIndex(0);
  };

  const handleSend = async () => {
    if (!input.trim() || isStreaming) return;

    let sessionId = currentSessionId;
    if (!sessionId) {
      sessionId = addSession(input.slice(0, 20) + '...');
    }

    const userQuestion = input;
    setInput('');
    
    await sendMessage(sessionId, userQuestion);
  };

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setUploading(true);
    setUploadProgress(20);

    try {
      const result = await uploadFile(file);
      setUploadProgress(100);
      
      let sessionId = currentSessionId;
      if (!sessionId) sessionId = addSession('Knowledge Upload');

      addMessage(sessionId, {
        role: 'system',
        type: 'text',
        content: `已关联知识库：${result.data.fileName} (${(result.data.fileSize / 1024).toFixed(2)} KB)`,
      });
    } catch (err) {
      alert('Upload failed: ' + err);
    } finally {
      setTimeout(() => {
        setUploading(false);
        setUploadProgress(0);
      }, 1000);
    }
  };

  return (
    <div className="p-6 relative">
      <div className={cn(
        "relative max-w-4xl mx-auto transition-all p-2 clip-path-corner",
        theme === 'dark' ? "bg-black/60 border-2 border-cyber-neon/30 focus-within:border-cyber-neon/60" : "bg-white/60 border-2 border-cyber-purple/30 focus-within:border-cyber-purple/60"
      )}>
        {/* Technical Corner Accents */}
        <div className="absolute top-0 left-0 w-4 h-4 border-t-2 border-l-2 border-cyber-neon opacity-40" />
        <div className="absolute bottom-0 right-0 w-4 h-4 border-b-2 border-r-2 border-cyber-neon opacity-40" />
        
        {/* Signal Bar */}
        <div className="absolute -top-1 left-10 flex gap-0.5">
          {[1, 2, 3, 4, 5].map(i => (
            <div key={i} className={cn("w-1 h-2 rounded-full", i <= 4 ? "bg-cyber-neon" : "bg-white/10")} />
          ))}
          <span className="text-[8px] font-mono ml-2 opacity-40 uppercase tracking-widest">Signal: Stable</span>
        </div>

        {uploading && (
          <div className="absolute -top-12 left-0 right-0 flex items-center gap-3 px-4 py-2 rounded-xl glass animate-in fade-in slide-in-from-bottom-2">
            <Loader2 className="w-4 h-4 animate-spin text-cyber-neon" />
            <span className="text-xs font-mono">Uploading Knowledge... {uploadProgress}%</span>
            <div className="flex-1 h-1 bg-white/10 rounded-full overflow-hidden">
              <div 
                className="h-full bg-cyber-neon transition-all duration-300" 
                style={{ width: `${uploadProgress}%` }}
              />
            </div>
          </div>
        )}

        <div className="flex items-end gap-2">
          <input
            type="file"
            ref={fileInputRef}
            onChange={handleFileUpload}
            className="hidden"
            accept=".txt,.md"
          />
          <button
            onClick={() => fileInputRef.current?.click()}
            disabled={isStreaming || uploading}
            className={cn(
              "p-3 rounded-xl transition-all",
              theme === 'dark' ? "hover:bg-cyber-neon/10 text-cyber-neon" : "hover:bg-cyber-purple/10 text-cyber-purple"
            )}
          >
            <Paperclip className="w-5 h-5" />
          </button>

          <textarea
            value={input}
            onChange={(e) => {
              setInput(e.target.value);
              setDismissedSlashInput('');
              setSelectedSlashIndex(0);
            }}
            onKeyDown={(e) => {
              if (slashSuggestions.length > 0) {
                if (e.key === 'ArrowDown') {
                  e.preventDefault();
                  setSelectedSlashIndex((idx) => (idx + 1) % slashSuggestions.length);
                  return;
                }
                if (e.key === 'ArrowUp') {
                  e.preventDefault();
                  setSelectedSlashIndex((idx) => (idx - 1 + slashSuggestions.length) % slashSuggestions.length);
                  return;
                }
                if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
                  e.preventDefault();
                  completeSlashCommand(slashSuggestions[activeSlashIndex]);
                  return;
                }
                if (e.key === 'Escape') {
                  e.preventDefault();
                  setDismissedSlashInput(input);
                  setSelectedSlashIndex(0);
                  return;
                }
              }
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            placeholder="输入运维指令或咨询问题..."
            className="flex-1 bg-transparent border-none outline-none py-3 px-2 resize-none max-h-40 min-h-[44px] text-sm"
            rows={1}
          />

          {slashSuggestions.length > 0 && (
            <div className="absolute left-16 right-16 bottom-20 z-30 rounded-2xl border border-cyber-neon/20 bg-black/90 shadow-2xl shadow-cyber-neon/10 backdrop-blur-xl overflow-hidden">
              {slashSuggestions.map((cmd, index) => {
                const selected = index === activeSlashIndex;
                const hint = cmd.argument_hint ? ` ${cmd.argument_hint}` : '';
                return (
                  <button
                    key={cmd.name}
                    type="button"
                    onMouseDown={(event) => {
                      event.preventDefault();
                      completeSlashCommand(cmd);
                    }}
                    className={cn(
                      'w-full flex items-center gap-3 px-4 py-3 text-left transition-colors',
                      selected ? 'bg-cyber-neon/15 text-cyber-neon' : 'hover:bg-white/5'
                    )}
                  >
                    <Terminal className="w-4 h-4 shrink-0" />
                    <div className="min-w-0 flex-1">
                      <div className="font-mono text-sm truncate">
                        /{cmd.name}<span className="opacity-50">{hint}</span>
                      </div>
                      <div className="text-xs opacity-60 truncate">{cmd.description}</div>
                    </div>
                    <span className="text-[10px] uppercase tracking-widest opacity-40">{cmd.type}</span>
                  </button>
                );
              })}
            </div>
          )}

          <button
            onClick={handleSend}
            disabled={!input.trim() || isStreaming}
            className={cn(
              "p-3 rounded-xl transition-all",
              !input.trim() || isStreaming
                ? "opacity-20 cursor-not-allowed"
                : (theme === 'dark' ? "bg-cyber-neon text-black glow-neon" : "bg-cyber-purple text-white")
            )}
          >
            {isStreaming ? <Loader2 className="w-5 h-5 animate-spin" /> : <Send className="w-5 h-5" />}
          </button>
        </div>
      </div>
      <p className="text-center text-[10px] mt-3 opacity-30 font-mono uppercase tracking-widest">
        Secure Channel // End-to-End Encrypted // AI-Ops Node 0x7F
      </p>
    </div>
  );
};
