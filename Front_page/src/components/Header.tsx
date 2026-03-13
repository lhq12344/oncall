import React from 'react';
import { useStore } from '../store/useStore';
import { Sun, Moon, Zap, Activity, ChevronDown, PanelLeftOpen } from 'lucide-react';
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';
import { OpsPanel } from './OpsPanel';
import { AnimatePresence } from 'motion/react';

function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export const Header: React.FC = () => {
  const { 
    theme, toggleTheme, connectionStatus, runOps, 
    isOpsPanelOpen, setOpsPanelOpen, isOpsRunning,
    isSidebarOpen, toggleSidebar
  } = useStore();

  const [time, setTime] = React.useState(new Date());

  React.useEffect(() => {
    const timer = setInterval(() => setTime(new Date()), 1000);
    return () => clearInterval(timer);
  }, []);

  const handleOpsClick = () => {
    if (isOpsPanelOpen) {
      setOpsPanelOpen(false);
    } else {
      // If there are already steps, just open the panel. 
      // Otherwise, if it's the first time or empty, we could run a default task
      // but the user wants to choose, so let's just open the panel.
      setOpsPanelOpen(true);
      
      // If no task is running and no steps exist, we can prompt or run default
      // For now, let's just open the panel.
    }
  };

  return (
    <>
      <header className={cn(
        "h-16 border-b flex items-center justify-between px-6 transition-all relative z-50",
        theme === 'dark' ? "bg-cyber-bg/80 border-cyber-neon/20" : "bg-white/80 border-cyber-purple/20"
      )}>
        {/* Decorative Corner */}
        <div className="absolute top-0 left-0 w-2 h-2 border-t border-l border-cyber-neon opacity-40" />
        
        <div className="flex items-center gap-6">
          {!isSidebarOpen && (
            <button 
              onClick={toggleSidebar}
              className={cn(
                "p-2 transition-all clip-path-corner mr-2 border",
                theme === 'dark' 
                  ? "text-cyber-neon border-cyber-neon/40 bg-cyber-neon/5 shadow-[0_0_10px_rgba(0,243,255,0.2)] hover:bg-cyber-neon/20 hover:border-cyber-neon/60" 
                  : "text-cyber-purple border-cyber-purple/40 bg-cyber-purple/5 shadow-[0_0_10px_rgba(139,92,246,0.2)] hover:bg-cyber-purple/20 hover:border-cyber-purple/60"
              )}
            >
              <PanelLeftOpen className="w-5 h-5" />
            </button>
          )}

          <div className="flex flex-col">
            <div className="flex items-center gap-2">
              <div className={cn(
                "w-1.5 h-1.5 rounded-full animate-pulse",
                connectionStatus === 'streaming' || isOpsRunning ? "bg-cyber-green shadow-[0_0_8px_#39ff14]" :
                connectionStatus === 'error' ? "bg-red-500" : "bg-gray-500"
              )} />
              <span className="text-[9px] font-mono uppercase tracking-widest opacity-60">
                {isOpsRunning ? 'Ops Executing...' : connectionStatus === 'streaming' ? 'Receiving Stream...' : 'System Idle'}
              </span>
            </div>
            <div className="text-[8px] font-mono opacity-30 uppercase tracking-[0.2em] mt-0.5">
              Node: 0x7F // Latency: 24ms
            </div>
          </div>
 
          <div className="h-4 w-[1px] bg-white/10" />
 
          <button
            onClick={handleOpsClick}
            className={cn(
              "group relative flex items-center gap-2 px-4 py-1.5 rounded-xl text-[10px] font-display font-black transition-all border-2 overflow-hidden uppercase tracking-wider",
              theme === 'dark' 
                ? "bg-cyber-neon/10 border-cyber-neon/30 text-cyber-neon hover:border-cyber-neon/60" 
                : "bg-cyber-purple/10 border-cyber-purple/30 text-cyber-purple hover:border-cyber-purple/60"
            )}
          >
            <div className="absolute inset-0 bg-gradient-to-r from-transparent via-white/10 to-transparent -translate-x-full group-hover:animate-[shimmer_1.5s_infinite] pointer-events-none" />
            <Activity className={cn("w-3.5 h-3.5", isOpsRunning && "animate-spin")} />
            <span>AI Ops 执行中心</span>
            <ChevronDown className={cn("w-3 h-3 transition-transform duration-300", isOpsPanelOpen && "rotate-180")} />
          </button>
        </div>

        <div className="flex items-center gap-4">
          <div className="hidden md:flex flex-col items-end mr-2">
            <span className="text-[10px] font-mono font-bold text-cyber-neon glow-neon">
              {time.toLocaleTimeString('zh-CN', { hour12: false })}
            </span>
            <span className="text-[8px] font-mono opacity-30 uppercase tracking-widest">
              {time.toLocaleDateString('zh-CN')}
            </span>
          </div>

          <button
            onClick={toggleTheme}
            className={cn(
              "p-2 rounded-xl transition-all border",
              theme === 'dark' 
                ? "bg-cyber-neon/5 border-cyber-neon/20 text-cyber-neon hover:bg-cyber-neon/10" 
                : "bg-cyber-purple/5 border-cyber-purple/20 text-cyber-purple hover:bg-cyber-purple/10"
            )}
          >
            {theme === 'dark' ? <Sun className="w-5 h-5" /> : <Moon className="w-5 h-5" />}
          </button>
          
          <div className={cn(
            "flex items-center gap-2 px-3 py-1.5 rounded-xl border clip-path-corner",
            theme === 'dark' ? "bg-black/40 border-cyber-neon/20" : "bg-white/40 border-cyber-purple/20"
          )}>
            <Zap className={cn("w-4 h-4", theme === 'dark' ? "text-cyber-neon" : "text-cyber-purple")} />
            <span className="text-[10px] font-display font-black tracking-widest uppercase">AIOps_v1</span>
          </div>
        </div>
      </header>

      <AnimatePresence>
        {isOpsPanelOpen && <OpsPanel />}
      </AnimatePresence>
    </>
  );
};
