import { InterruptData, AIOpsStep, SlashCommandInfo, CommandAction } from '../types';
import { parseSSEChunk, parseStreamData } from '../agent-events/client';
import { createInitialAgentEventState, reduceRunEvent } from '../agent-events/reducer';
import type { AgentEventState } from '../agent-events/types';

const BASE_URL = 'http://127.0.0.1:6872/api/v1';

export async function uploadFile(file: File) {
  const formData = new FormData();
  formData.append('file', file);

  const response = await fetch(`${BASE_URL}/upload`, {
    method: 'POST',
    body: formData,
  });

  if (!response.ok) {
    throw new Error('Upload failed');
  }

  return response.json();
}

interface StreamOptions {
  onContent: (content: string) => void;
  onStep?: (step: AIOpsStep) => void;
  onInterrupt?: (interrupt: InterruptData) => void;
  onCommandAction?: (action: CommandAction) => void;
  onError?: (error: string) => void;
  onDone?: () => void;
}

export async function fetchSlashCommands(): Promise<SlashCommandInfo[]> {
  const response = await fetch(`${BASE_URL}/slash_commands`);
  if (!response.ok) {
    throw new Error(`HTTP error! status: ${response.status}`);
  }
  const payload = await response.json();
  const commands = payload?.data?.commands ?? payload?.commands ?? [];
  return Array.isArray(commands) ? commands : [];
}

export async function streamChat(
  sessionId: string,
  question: string,
  options: StreamOptions
) {
  return streamRequest(`${BASE_URL}/chat_stream`, { id: sessionId, question }, options);
}

export async function resumeChat(
  sessionId: string,
  checkpointId: string,
  data: { approved?: boolean; resolved?: boolean; comment?: string; interrupt_ids?: string[]; selection_value?: string },
  options: StreamOptions
) {
  return streamRequest(`${BASE_URL}/chat_resume_stream`, {
    id: sessionId,
    checkpoint_id: checkpointId,
    ...data
  }, options);
}

export async function streamOps(options: StreamOptions) {
  return streamRequest(`${BASE_URL}/ai_ops_stream`, {}, options);
}

export async function resumeOps(
  checkpointId: string,
  data: { approved?: boolean; resolved?: boolean; comment?: string; interrupt_ids?: string[]; selection_value?: string },
  options: StreamOptions
) {
  return streamRequest(`${BASE_URL}/ai_ops_resume_stream`, {
    checkpoint_id: checkpointId,
    ...data
  }, options);
}

async function streamRequest(url: string, body: any, options: StreamOptions) {
  const { onContent, onStep, onInterrupt, onCommandAction, onError, onDone } = options;

  try {
    const response = await fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(body),
    });

    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }

    const reader = response.body?.getReader();
    if (!reader) throw new Error('No reader available');

    const decoder = new TextDecoder();
    let buffer = '';
    let eventState = createInitialAgentEventState();

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const parsed = parseSSEChunk(buffer);
      buffer = parsed.rest;

      for (const frame of parsed.frames) {
        try {
          const parsedData = parseStreamData(frame.data);
          if (!parsedData) continue;
          const previous = eventState;
          eventState = reduceRunEvent(eventState, parsedData);
          emitStateDelta(previous, eventState, options);
        } catch (e) {
          onError?.(e instanceof Error ? e.message : String(e));
        }
      }
    }
  } catch (error) {
    onError?.(error instanceof Error ? error.message : String(error));
  }
}

function emitStateDelta(previous: AgentEventState, next: AgentEventState, options: StreamOptions) {
  if (next.content.length > previous.content.length) {
    options.onContent(next.content.slice(previous.content.length));
  }
  for (const step of next.steps.slice(previous.steps.length)) {
    options.onStep?.(step);
  }
  for (const interrupt of next.interrupts.slice(previous.interrupts.length)) {
    options.onInterrupt?.(interrupt);
  }
  for (const action of next.commandActions.slice(previous.commandActions.length)) {
    options.onCommandAction?.(action);
  }
  for (const error of next.errors.slice(previous.errors.length)) {
    options.onError?.(error);
  }
  if (previous.status !== 'completed' && next.status === 'completed') {
    options.onDone?.();
  }
}
