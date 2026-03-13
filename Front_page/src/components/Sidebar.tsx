import React, { useState, useRef, useEffect } from 'react';
import { useStore } from '../store/useStore';
import { 
  MessageSquare, Plus, Trash2, Edit2, Terminal, Check, 
  X as CloseIcon, AlertTriangle, PanelLeftClose, PanelLeftOpen 
} from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { motion, AnimatePresence } from 'motion/react';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export const Sidebar: React.FC = () => {
  const { 
    sessions, currentSessionId, addSession, deleteSession, 
    renameSession, setCurrentSession, theme, isSidebarOpen, toggleSidebar 
  } = useStore();
  
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editTitle, setEditTitle] = useState('');
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);
  const editInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editingId && editInputRef.current) {
      editInputRef.current.focus();
      editInputRef.current.select();
    }
  }, [editingId]);

  const handleRename = (id: string) => {
    if (editTitle.trim()) {
      renameSession(id, editTitle.trim());
    }
    setEditingId(null);
  };

  const startEditing = (e: React.MouseEvent, id: string, title: string) => {
    e.stopPropagation();
    setEditingId(id);
    setEditTitle(title);
  };

  return (
    <motion.div 
      initial={false}
      animate={{ width: isSidebarOpen ? 288 : 0, opacity: isSidebarOpen ? 1 : 0 }}
      transition={{ duration: 0.3, ease: "easeInOut" }}
      className={cn(
        "h-full flex flex-col border-r transition-all duration-300 relative z-50 overflow-hidden shrink-0",
        theme === 'dark' ? "bg-cyber-bg border-cyber-neon/20" : "bg-white/50 backdrop-blur-xl border-cyber-purple/20"
      )}
    >
      <div className="p-4 flex items-center justify-between min-w-[288px]">
        <div className="flex items-center gap-2">
          <div className="relative">
            <Terminal className={cn("w-6 h-6", theme === 'dark' ? "text-cyber-neon" : "text-cyber-purple")} />
            <motion.div 
              animate={{ opacity: [0.3, 1, 0.3] }}
              transition={{ duration: 2, repeat: Infinity }}
              className="absolute -top-1 -right-1 w-2 h-2 bg-cyber-green rounded-full shadow-[0_0_10px_#39ff14]" 
            />
          </div>
          <h1 className={cn(
            "font-display font-black tracking-tighter text-xl glitch-text",
            theme === 'dark' ? "text-cyber-neon glow-neon" : "text-cyber-purple"
          )} data-text="CYBER OPS">CYBER OPS</h1>
        </div>
        <div className="flex items-center gap-1">
          <button 
            onClick={() => addSession('新对话')}
            className={cn(
              "p-2 transition-all clip-path-corner",
              theme === 'dark' 
                ? "bg-cyber-neon/10 text-cyber-neon border border-cyber-neon/30 hover:bg-cyber-neon/20 hover:border-cyber-neon/60" 
                : "bg-cyber-purple/10 text-cyber-purple border border-cyber-purple/30 hover:bg-cyber-purple/20 hover:border-cyber-purple/60"
            )}
          >
            <Plus className="w-4 h-4" />
          </button>
          <button 
            onClick={toggleSidebar}
            className={cn(
              "p-2 transition-all clip-path-corner border",
              theme === 'dark' 
                ? "text-cyber-neon border-cyber-neon/40 bg-cyber-neon/5 shadow-[0_0_10px_rgba(0,243,255,0.2)] hover:bg-cyber-neon/20 hover:border-cyber-neon/60" 
                : "text-cyber-purple border-cyber-purple/40 bg-cyber-purple/5 shadow-[0_0_10px_rgba(139,92,246,0.2)] hover:bg-cyber-purple/20 hover:border-cyber-purple/60"
            )}
          >
            <PanelLeftClose className="w-4 h-4" />
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-2 space-y-2 no-scrollbar min-w-[288px]">
        {sessions.map((session) => (
          <div
            key={session.id}
            onClick={() => editingId !== session.id && setCurrentSession(session.id)}
            className={cn(
              "group relative p-3 cursor-pointer transition-all border clip-path-corner",
              currentSessionId === session.id
                ? (theme === 'dark' 
                    ? "bg-cyber-neon/10 border-cyber-neon/50 text-cyber-neon shadow-[0_0_15px_rgba(0,243,255,0.1)]" 
                    : "bg-cyber-purple/10 border-cyber-purple/50 text-cyber-purple")
                : (theme === 'dark'
                    ? "border-white/5 text-gray-400 hover:bg-white/5 hover:border-white/20"
                    : "border-black/5 text-gray-600 hover:bg-black/5 hover:border-black/20")
            )}
          >
            <div className="flex flex-col gap-1 overflow-hidden">
              <div className="flex items-center gap-3">
                <MessageSquare className="w-4 h-4 shrink-0" />
                {editingId === session.id ? (
                  <input
                    ref={editInputRef}
                    value={editTitle}
                    onChange={(e) => setEditTitle(e.target.value)}
                    onBlur={() => handleRename(session.id)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') handleRename(session.id);
                      if (e.key === 'Escape') setEditingId(null);
                    }}
                    className={cn(
                      "flex-1 bg-transparent border-b outline-none text-sm py-0 px-1",
                      theme === 'dark' ? "border-cyber-neon text-white" : "border-cyber-purple text-black"
                    )}
                    onClick={(e) => e.stopPropagation()}
                  />
                ) : (
                  <span className="truncate text-sm font-medium">{session.title}</span>
                )}
              </div>
              <div className="pl-7 text-[10px] opacity-40 font-mono">
                {new Date(session.updatedAt).toLocaleString('zh-CN', {
                  month: 'numeric',
                  day: 'numeric',
                  hour: '2-digit',
                  minute: '2-digit'
                })}
              </div>
            </div>
            
            {editingId !== session.id && (
              <div className="absolute right-2 top-1/2 -translate-y-1/2 hidden group-hover:flex items-center gap-1">
                <button 
                  onClick={(e) => startEditing(e, session.id, session.title)}
                  className="p-1.5 hover:bg-white/10 rounded-md transition-colors"
                  title="重命名"
                >
                  <Edit2 className="w-3.5 h-3.5" />
                </button>
                <button 
                  onClick={(e) => {
                    e.stopPropagation();
                    setDeleteConfirmId(session.id);
                  }}
                  className="p-1.5 hover:bg-red-500/20 hover:text-red-500 rounded-md transition-colors"
                  title="删除"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Delete Confirmation Modal */}
      <AnimatePresence>
        {deleteConfirmId && (
          <div className="fixed inset-0 z-[200] flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm">
            <motion.div
              initial={{ scale: 0.9, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              exit={{ scale: 0.9, opacity: 0 }}
              className={cn(
                "w-full max-w-sm p-6 rounded-3xl border-2 shadow-2xl",
                theme === 'dark' ? "bg-cyber-bg border-cyber-neon/30" : "bg-white border-cyber-purple/30"
              )}
            >
              <div className="flex items-center gap-3 mb-4 text-red-500">
                <AlertTriangle className="w-6 h-6" />
                <h3 className="font-bold text-lg">确认删除会话？</h3>
              </div>
              <p className="text-sm opacity-70 mb-6 leading-relaxed">
                此操作将永久移除该会话的所有聊天记录，且无法撤销。
              </p>
              <div className="flex gap-3">
                <button
                  onClick={() => setDeleteConfirmId(null)}
                  className="flex-1 px-4 py-2 rounded-xl border border-gray-500/30 hover:bg-gray-500/10 transition-all text-sm font-medium"
                >
                  取消
                </button>
                <button
                  onClick={() => {
                    deleteSession(deleteConfirmId);
                    setDeleteConfirmId(null);
                  }}
                  className="flex-1 px-4 py-2 rounded-xl bg-red-500 text-white hover:bg-red-600 transition-all text-sm font-bold shadow-lg shadow-red-500/20"
                >
                  确认删除
                </button>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      <div className={cn(
        "p-4 border-t text-xs opacity-50 min-w-[288px]",
        theme === 'dark' ? "border-cyber-neon/10" : "border-cyber-purple/10"
      )}>
        v1.0.5-STABLE // AIOps Engine
      </div>
    </motion.div>
  );
};
