export type MessageType = 'text' | 'step' | 'interrupt' | 'error' | 'user';

export interface InterruptContext {
  id: string;
  address: string;
  info: string;
  is_root_cause: boolean;
}

export interface BashApprovalRequest {
  command: string;
  args: string[];
  timeout: number;
  reason?: string;
  raw_command: string;
}

export interface InterruptData {
  checkpoint_id: string;
  interrupt_contexts: InterruptContext[];
  message: string;
  bash_request?: BashApprovalRequest;
  handled?: boolean;
}

export interface AIOpsStep {
  step: number;
  content: string;
  status: 'pending' | 'completed' | 'error';
}

export interface OpsStep {
  id: string;
  toolName: string;
  content: string;
  status: 'pending' | 'completed' | 'error';
  interrupt?: InterruptData;
}

export interface Message {
  id: string;
  role: 'user' | 'assistant' | 'system';
  type: MessageType;
  content: string;
  timestamp: number;
  steps?: AIOpsStep[];
  interrupt?: InterruptData;
}

export interface Session {
  id: string;
  title: string;
  messages: Message[];
  updatedAt: number;
}
