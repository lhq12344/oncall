import { InterruptData, ChatStep, InterruptContext, DetailOption, DetailRequest, CommandApprovalData } from '../types';

const DEFAULT_BACKEND_PORT = '6872';
const API_BASE_PATH = '/api/v1';
const BASE_URL = resolveApiBaseUrl();

function resolveApiBaseUrl() {
  const configured = getViteEnv('VITE_API_BASE_URL')?.trim();
  if (configured) {
    return resolveConfiguredApiBaseUrl(configured);
  }

  const backendPort = resolveBackendPort();
  if (typeof window === 'undefined') {
    return `http://127.0.0.1:${backendPort}${API_BASE_PATH}`;
  }

  const protocol = window.location.protocol === 'https:' ? 'https:' : 'http:';
  const hostname = window.location.hostname || '127.0.0.1';
  return `${protocol}//${hostname}:${backendPort}${API_BASE_PATH}`;
}

function getViteEnv(key: string) {
  return (import.meta as unknown as { env?: Record<string, string | undefined> }).env?.[key];
}

function resolveBackendPort() {
  const configured = getViteEnv('VITE_BACKEND_PORT')?.trim();
  return configured || DEFAULT_BACKEND_PORT;
}

function resolveConfiguredApiBaseUrl(value: string) {
  const trimmed = trimTrailingSlash(value);
  if (typeof window === 'undefined') {
    return trimmed;
  }

  const pageHostname = window.location.hostname;
  if (!pageHostname || isLoopbackHost(pageHostname)) {
    return trimmed;
  }

  try {
    const url = new URL(trimmed);
    if (!isLoopbackHost(url.hostname)) {
      return trimmed;
    }
    url.hostname = pageHostname;
    if (!url.port) {
      url.port = resolveBackendPort();
    }
    url.protocol = window.location.protocol === 'https:' ? 'https:' : 'http:';
    if (url.pathname === '/' || url.pathname === '') {
      url.pathname = API_BASE_PATH;
    }
    return trimTrailingSlash(url.toString());
  } catch {
    return trimmed;
  }
}

function isLoopbackHost(hostname: string) {
  return hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '::1' || hostname === '[::1]';
}

function trimTrailingSlash(value: string) {
  return value.replace(/\/+$/, '');
}

export async function uploadFile(file: File) {
  const formData = new FormData();
  formData.append('file', file);

  const response = await fetch(`${BASE_URL}/upload`, {
    method: 'POST',
    body: formData,
  });

  const payload = await readJsonResponse<UploadFileResponse>(response);
  if (!response.ok) {
    throw new Error(payload?.message || 'Upload failed');
  }

  if (!payload?.data) {
    throw new Error(payload?.message || 'Upload failed: empty response');
  }

  return payload;
}

interface UploadFileData {
  fileName: string;
  filePath: string;
  fileSize: number;
}

interface UploadFileResponse {
  message?: string;
  data?: UploadFileData | null;
}

async function readJsonResponse<T>(response: Response): Promise<T | null> {
  const text = await response.text();
  if (!text.trim()) {
    return null;
  }
  try {
    return JSON.parse(text) as T;
  } catch {
    return null;
  }
}

interface StreamOptions {
  onContent: (content: string) => void;
  onStep?: (step: ChatStep) => void;
  onInterrupt?: (interrupt: InterruptData) => void;
  onError?: (error: string) => void;
  onDone?: () => void;
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

async function streamRequest(url: string, body: any, options: StreamOptions) {
  const { onContent, onStep, onInterrupt, onError, onDone } = options;

  try {
    const response = await fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(body),
    });

    if (!response.ok) {
      throw new Error(await formatHTTPError(response));
    }

    const reader = response.body?.getReader();
    if (!reader) throw new Error('No reader available');

    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      
      const parts = buffer.split('\n\n');
      buffer = parts.pop() || '';

      for (const part of parts) {
        const lines = part.split('\n');
        const dataLines: string[] = [];

        for (const line of lines) {
          if (!line.startsWith('data:')) {
            continue;
          }
          if (line.startsWith('data: ')) {
            dataLines.push(line.slice(6));
            continue;
          }
          dataLines.push(line.slice(5));
        }

        if (dataLines.length === 0) {
          continue;
        }

        // SSE multi-line data semantics: join each data line with "\n"
        // to preserve model-emitted whitespace (including trailing newlines).
        const eventData = dataLines.join('\n');
        const controlPayload = eventData.trim();

        // Handle [DONE]
        if (controlPayload === '[DONE]') {
          onDone?.();
          return;
        }

        // Handle [ERROR]
        if (controlPayload.startsWith('[ERROR]')) {
          onError?.(controlPayload.slice(7).trim());
          return;
        }

        // Try parsing as JSON
        try {
          const json = JSON.parse(controlPayload);

          if (json.type === 'done') {
            onDone?.();
            return;
          }

          if (json.type === 'error') {
            onError?.(json.content || 'Unknown error');
            return;
          }

          if (json.type === 'interrupt') {
            onInterrupt?.(mapInterruptData(json));
            continue;
          }

          if (json.type === 'step') {
            onStep?.({
              step: json.step,
              content: json.content,
              status: 'completed'
            });
            continue;
          }

          if (json.type === 'content') {
            onContent(String(json.content ?? ''));
            continue;
          }

          // If it's JSON but not a recognized type, maybe it's just content?
          // Or just ignore it if it doesn't match our protocol
        } catch {
          // Not JSON, treat as raw text and preserve whitespace as-is.
          onContent(eventData);
        }
      }
    }
  } catch (error) {
    onError?.(formatStreamError(error, url));
  }
}

function formatStreamError(error: unknown, url: string) {
  if (error instanceof TypeError && error.message === 'Failed to fetch') {
    return `Failed to fetch ${url}. Backend is unreachable from this browser origin; check that port ${resolveBackendPort()} is running or set VITE_API_BASE_URL.`;
  }
  return error instanceof Error ? error.message : String(error);
}

async function formatHTTPError(response: Response) {
  const text = await response.text();
  if (!text.trim()) {
    return `HTTP error! status: ${response.status}`;
  }

  try {
    const payload = JSON.parse(text);
    const message = payload?.message || payload?.error || payload?.data?.message;
    if (typeof message === 'string' && message.trim()) {
      return message.trim();
    }
  } catch {
    // Fall back to the raw response body below.
  }

  return text.trim();
}

function mapInterruptData(raw: any): InterruptData {
  const checkpoint_id = typeof raw?.checkpoint_id === 'string' ? raw.checkpoint_id : '';
  const message = typeof raw?.message === 'string' ? raw.message : '';
  const interrupt_contexts = normalizeInterruptContexts(raw?.interrupt_contexts);
  const detail_request = extractDetailRequest(raw);
  const interrupt_data = raw?.interrupt_data;
  const command_approval = extractCommandApproval(raw);

  return {
    checkpoint_id,
    message,
    interrupt_contexts,
    detail_request,
    command_approval,
    interrupt_data
  };
}

function normalizeInterruptContexts(input: any): InterruptContext[] {
  if (!Array.isArray(input)) {
    return [];
  }
  return input
    .filter(Boolean)
    .map((item) => ({
      id: typeof item.id === 'string' ? item.id : '',
      address: typeof item.address === 'string' ? item.address : '',
      info: typeof item.info === 'string' ? item.info : '',
      is_root_cause: Boolean(item.is_root_cause)
    }));
}

function extractDetailRequest(raw: any): DetailRequest | undefined {
  const structuredCandidates = [
    raw?.detail_request,
    raw?.interrupt_data,
    raw?.data
  ];
  for (const candidate of structuredCandidates) {
    const parsed = parseDetailRequestFromUnknown(candidate);
    if (parsed) {
      return parsed;
    }
  }
  return undefined;
}

function extractCommandApproval(raw: any): CommandApprovalData | undefined {
  const structuredCandidates = [
    raw?.command_approval,
    raw?.interrupt_data,
    raw?.data
  ];
  for (const candidate of structuredCandidates) {
    const parsed = parseCommandApprovalFromUnknown(candidate);
    if (parsed) {
      return parsed;
    }
  }
  return undefined;
}

function parseCommandApprovalFromUnknown(input: unknown): CommandApprovalData | undefined {
  if (!input || typeof input !== 'object') {
    return undefined;
  }

  const value = input as Record<string, any>;
  const command = normalizeOptionalString(value.command);
  if (!command) {
    return undefined;
  }
  return {
    command,
    args: normalizeStringArray(value.args),
    script: normalizeOptionalString(value.script),
    timeout: normalizeOptionalNumber(value.timeout),
    reason: normalizeOptionalString(value.reason)
  };
}

function parseDetailRequestFromUnknown(input: unknown): DetailRequest | undefined {
  if (!input || typeof input !== 'object') {
    return undefined;
  }

  const value = input as Record<string, any>;
  const field = normalizeOptionalString(value.field);
  const question = normalizeOptionalString(value.question);
  const reason = normalizeOptionalString(value.reason);
  const options = normalizeDetailOptions(value.options);

  if (!field || !question || options.length === 0) {
    return undefined;
  }

  return {
    field,
    question,
    reason,
    options
  };
}

function normalizeOptionalString(value: unknown): string | undefined {
  if (typeof value !== 'string') {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed || undefined;
}

function normalizeOptionalNumber(value: unknown): number | undefined {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return undefined;
  }
  return value;
}

function normalizeStringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) {
    return undefined;
  }
  const result = value
    .map((item) => normalizeOptionalString(item))
    .filter((item): item is string => Boolean(item));
  return result.length > 0 ? result : undefined;
}

function normalizeDetailOptions(value: unknown): DetailOption[] {
  if (!Array.isArray(value)) {
    return [];
  }

  return value
    .map((item) => normalizeDetailOption(item))
    .filter((item): item is DetailOption => Boolean(item));
}

function normalizeDetailOption(value: unknown): DetailOption | undefined {
  if (!value || typeof value !== 'object') {
    return undefined;
  }
  const record = value as Record<string, any>;
  const label = normalizeOptionalString(record.label);
  const optionValue = normalizeOptionalString(record.value);
  const description = normalizeOptionalString(record.description);
  if (!label || !optionValue) {
    return undefined;
  }
  return {
    label,
    value: optionValue,
    description
  };
}
