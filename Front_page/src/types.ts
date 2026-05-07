export type MessageType = 'text' | 'step' | 'interrupt' | 'error' | 'user';

export interface InterruptContext {
  id: string;
  address: string;
  info: string;
  is_root_cause: boolean;
}

export interface DetailOption {
  label: string;
  value: string;
  description?: string;
}

export interface DetailRequest {
  field: string;
  question: string;
  reason?: string;
  options: DetailOption[];
}

export interface InterruptData {
  checkpoint_id: string;
  interrupt_contexts: InterruptContext[];
  message: string;
  detail_request?: DetailRequest;
  handled?: boolean;
}

export interface ChatStep {
  step: number;
  content: string;
  status: 'pending' | 'completed' | 'error';
}

export interface Message {
  id: string;
  role: 'user' | 'assistant' | 'system';
  type: MessageType;
  content: string;
  timestamp: number;
  steps?: ChatStep[];
  interrupt?: InterruptData;
}

export interface Session {
  id: string;
  title: string;
  messages: Message[];
  updatedAt: number;
}
