/**
 * @license
 * SPDX-License-Identifier: Apache-2.0
 */

import React, { useEffect } from 'react';
import { useStore } from './store/useStore';
import { Sidebar } from './components/Sidebar';
import { Header } from './components/Header';
import { ChatArea } from './components/ChatArea';
import { InputArea } from './components/InputArea';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export default function App() {
  const { theme, sessions, currentSessionId, setCurrentSession, addSession, isRehydrated } = useStore();
  const initialized = React.useRef(false);

  useEffect(() => {
    if (theme === 'dark') {
      document.documentElement.classList.add('dark');
      document.documentElement.classList.remove('light');
    } else {
      document.documentElement.classList.add('light');
      document.documentElement.classList.remove('dark');
    }
  }, [theme]);

  useEffect(() => {
    if (!isRehydrated || initialized.current) return;

    if (!currentSessionId) {
      if (sessions.length > 0) {
        setCurrentSession(sessions[0].id);
      } else {
        addSession('新对话');
      }
      initialized.current = true;
    }
  }, [isRehydrated, currentSessionId, sessions.length, setCurrentSession, addSession]);

  return (
    <div className={cn(
      "flex h-screen w-full overflow-hidden font-sans transition-colors duration-500 relative scanlines",
      theme === 'dark' ? "bg-cyber-bg text-white" : "bg-slate-50 text-slate-900"
    )}>
      {/* Background Image Layer */}
      <div className="fixed inset-0 pointer-events-none z-0">
        <img 
          src="https://images.unsplash.com/photo-1605142859862-978be7eba909?auto=format&fit=crop&q=80&w=2070" 
          alt="Cyberpunk City" 
          className={cn(
            "w-full h-full object-cover transition-all duration-1000",
            theme === 'dark' ? "opacity-20 scale-105" : "opacity-5 grayscale-[0.8]"
          )}
          referrerPolicy="no-referrer"
        />
        <div className={cn(
          "absolute inset-0 transition-colors duration-500 cyber-grid",
          theme === 'dark' 
            ? "bg-gradient-to-b from-cyber-bg/40 via-cyber-bg/80 to-cyber-bg" 
            : "bg-gradient-to-b from-white/40 via-white/80 to-white"
        )} />
      </div>

      {/* Background Effects */}
      {theme === 'dark' && (
        <div className="fixed inset-0 pointer-events-none overflow-hidden z-[1]">
          <div className="absolute top-[-10%] left-[-10%] w-[40%] h-[40%] bg-cyber-neon/5 blur-[120px] rounded-full" />
          <div className="absolute bottom-[-10%] right-[-10%] w-[40%] h-[40%] bg-cyber-purple/5 blur-[120px] rounded-full" />
          <div className="absolute inset-0 bg-[url('https://grainy-gradients.vercel.app/noise.svg')] opacity-20 brightness-100 contrast-150 pointer-events-none" />
        </div>
      )}

      <div className="relative z-10 flex w-full h-full">
        <Sidebar />
        
        <main className="flex-1 flex flex-col relative">
          <Header />
          <ChatArea />
          <InputArea />
        </main>
      </div>
    </div>
  );
}
