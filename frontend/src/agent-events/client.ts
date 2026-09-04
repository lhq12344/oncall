import { RUN_EVENT_SCHEMA, type RunEvent } from './types';

export interface ParsedSSEFrame {
  id?: string;
  event?: string;
  data: string;
}

export function parseSSEChunk(buffer: string): { frames: ParsedSSEFrame[]; rest: string } {
  const parts = buffer.split('\n\n');
  const rest = parts.pop() || '';
  return { frames: parts.map(parseFrame).filter((frame): frame is ParsedSSEFrame => Boolean(frame)), rest };
}

export function parseStreamData(data: string): RunEvent | undefined {
  const trimmed = data.trim();
  if (!trimmed) return undefined;
  const parsed = JSON.parse(trimmed);
  if (parsed?.version === RUN_EVENT_SCHEMA) {
    return parsed as RunEvent;
  }
  return undefined;
}

function parseFrame(raw: string): ParsedSSEFrame | undefined {
  const lines = raw.split('\n');
  let data = '';
  let id: string | undefined;
  let event: string | undefined;
  for (const line of lines) {
    if (line.startsWith('id:')) id = line.slice(3).trim();
    if (line.startsWith('event:')) event = line.slice(6).trim();
    if (line.startsWith('data:')) data += `${line.slice(5).trimStart()}\n`;
  }
  if (!data.trim()) return undefined;
  return { id, event, data: data.trimEnd() };
}
