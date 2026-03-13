import React, { useState, useRef } from 'react';
import { useStore } from '../store/useStore';
import { Send, Paperclip, Loader2, X } from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { streamChat, uploadFile } from '../services/api';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export const InputArea: React.FC = () => {
  const { theme, currentSessionId, addSession, setStreaming, setConnectionStatus, isStreaming, sendMessage, addMessage } = useStore();
  const [input, setInput] = useState('');
  const [uploading, setUploading] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(0);
  const fileInputRef = useRef<HTMLInputElement>(null);

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
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            placeholder="输入运维指令或咨询问题..."
            className="flex-1 bg-transparent border-none outline-none py-3 px-2 resize-none max-h-40 min-h-[44px] text-sm"
            rows={1}
          />

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
