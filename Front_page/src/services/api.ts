import { InterruptData, AIOpsStep, InterruptContext, BashApprovalRequest } from '../types';

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
  data: { approved?: boolean; resolved?: boolean; comment?: string },
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
  data: { approved?: boolean; resolved?: boolean; comment?: string },
  options: StreamOptions
) {
  return streamRequest(`${BASE_URL}/ai_ops_resume_stream`, {
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
      throw new Error(`HTTP error! status: ${response.status}`);
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
        let dataContent = '';
        
        for (const line of lines) {
          if (line.startsWith('data: ')) {
            dataContent += line.slice(6) + '\n';
          }
        }

        const trimmedData = dataContent.trim();
        if (!trimmedData) continue;

        // Handle [DONE]
        if (trimmedData === '[DONE]') {
          onDone?.();
          return;
        }

        // Handle [ERROR]
        if (trimmedData.startsWith('[ERROR]')) {
          onError?.(trimmedData.slice(7).trim());
          return;
        }

        // Try parsing as JSON
        try {
          const json = JSON.parse(trimmedData);
          
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
            onContent(json.content);
            continue;
          }

          // If it's JSON but not a recognized type, maybe it's just content?
          // Or just ignore it if it doesn't match our protocol
        } catch (e) {
          // Not JSON, treat as raw text
          onContent(trimmedData);
        }
      }
    }
  } catch (error) {
    onError?.(error instanceof Error ? error.message : String(error));
  }
}

function mapInterruptData(raw: any): InterruptData {
  const checkpoint_id = typeof raw?.checkpoint_id === 'string' ? raw.checkpoint_id : '';
  const message = typeof raw?.message === 'string' ? raw.message : '';
  const interrupt_contexts = normalizeInterruptContexts(raw?.interrupt_contexts);
  const bash_request = extractBashApprovalRequest(raw, message, interrupt_contexts);

  return {
    checkpoint_id,
    message,
    interrupt_contexts,
    bash_request
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

function extractBashApprovalRequest(
  raw: any,
  message: string,
  contexts: InterruptContext[]
): BashApprovalRequest | undefined {
  const structuredCandidates = [
    raw?.bash_request,
    raw?.interrupt_data,
    raw?.data
  ];
  for (const candidate of structuredCandidates) {
    const parsed = parseBashRequestFromUnknown(candidate);
    if (parsed) {
      return parsed;
    }
  }

  const textCandidates = [
    message,
    ...contexts.map((ctx) => ctx.info),
  ].filter(Boolean);

  for (const text of textCandidates) {
    const parsed = parseBashRequestFromText(text);
    if (parsed) {
      return parsed;
    }
  }

  return undefined;
}

function parseBashRequestFromUnknown(input: unknown): BashApprovalRequest | undefined {
  if (!input) {
    return undefined;
  }

  if (typeof input === 'string') {
    return parseBashRequestFromText(input);
  }

  if (typeof input !== 'object') {
    return undefined;
  }

  const value = input as Record<string, any>;
  const timeout = normalizeTimeout(value.timeout);
  const reason = normalizeOptionalString(value.reason);

  const rawCommand = normalizeOptionalString(value.raw_command);
  const commandField = normalizeOptionalString(value.command) || normalizeOptionalString(value.cmd);
  const argsField = normalizeArgs(value.args);

  if (!commandField && rawCommand) {
    const [cmd, ...restArgs] = tokenizeCommandLine(rawCommand);
    if (!cmd) {
      return undefined;
    }
    return {
      command: cmd,
      args: restArgs,
      timeout,
      reason,
      raw_command: rawCommand
    };
  }

  if (!commandField) {
    return undefined;
  }

  const args = argsField ?? [];
  return {
    command: commandField,
    args,
    timeout,
    reason,
    raw_command: [commandField, ...args].join(' ').trim()
  };
}

function parseBashRequestFromText(text: string): BashApprovalRequest | undefined {
  const trimmed = text.trim();
  if (!trimmed) {
    return undefined;
  }

  // 1) JSON 字符串或包含 JSON 片段
  const jsonParsed = parseBashRequestFromJSONText(trimmed);
  if (jsonParsed) {
    return jsonParsed;
  }

  // 2) Go map 格式：map[command:kubectl args:[get pods] timeout:30 reason:...]
  const mapParsed = parseBashRequestFromGoMapText(trimmed);
  if (mapParsed) {
    return mapParsed;
  }

  // 3) Stringer 文本：待执行命令：kubectl get pods；超时：30s；执行原因：...。
  const sentenceRegex = /待执行命令：(.+?)；超时：(\d+)s(?:；执行原因：(.+?))?(?:。|$)/;
  const match = trimmed.match(sentenceRegex);
  if (!match) {
    return undefined;
  }

  const rawCommand = match[1].trim();
  const timeout = normalizeTimeout(match[2]);
  const reason = normalizeOptionalString(match[3]?.replace(/请确认是否继续[。.]?$/, ''));
  const [command, ...args] = tokenizeCommandLine(rawCommand);
  if (!command) {
    return undefined;
  }

  return {
    command,
    args,
    timeout,
    reason,
    raw_command: rawCommand
  };
}

function parseBashRequestFromJSONText(text: string): BashApprovalRequest | undefined {
  try {
    const direct = JSON.parse(text);
    const parsed = parseBashRequestFromUnknown(direct);
    if (parsed) {
      return parsed;
    }
  } catch (_) {
    // ignore
  }

  const start = text.indexOf('{');
  const end = text.lastIndexOf('}');
  if (start < 0 || end <= start) {
    return undefined;
  }

  try {
    const partial = JSON.parse(text.slice(start, end + 1));
    return parseBashRequestFromUnknown(partial);
  } catch (_) {
    return undefined;
  }
}

function parseBashRequestFromGoMapText(text: string): BashApprovalRequest | undefined {
  if (!text.includes('map[')) {
    return undefined;
  }

  const command = matchGroup(text, /command:([^\s\]]+)/);
  if (!command) {
    return undefined;
  }
  const timeout = normalizeTimeout(matchGroup(text, /timeout:(\d+)/));
  const reason = normalizeOptionalString(matchGroup(text, /reason:(.+?)(?:\s+\w+:|]$)/));
  const argsRaw = matchGroup(text, /args:\[([^\]]*)\]/) || '';
  const args = argsRaw
    .split(/\s+/)
    .map((item) => item.trim())
    .filter(Boolean);

  return {
    command,
    args,
    timeout,
    reason,
    raw_command: [command, ...args].join(' ').trim()
  };
}

function matchGroup(text: string, regex: RegExp): string | undefined {
  const result = text.match(regex);
  if (!result || !result[1]) {
    return undefined;
  }
  return result[1].trim();
}

function normalizeTimeout(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return Math.max(1, Math.round(value));
  }
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number.parseInt(value, 10);
    if (Number.isFinite(parsed) && parsed > 0) {
      return parsed;
    }
  }
  return 30;
}

function normalizeOptionalString(value: unknown): string | undefined {
  if (typeof value !== 'string') {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed || undefined;
}

function normalizeArgs(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) {
    return undefined;
  }
  return value
    .map((item) => (typeof item === 'string' ? item.trim() : String(item)))
    .filter(Boolean);
}

function tokenizeCommandLine(rawCommand: string): string[] {
  const matches = rawCommand.match(/"[^"]*"|'[^']*'|\S+/g) || [];
  return matches.map((item) => {
    const trimmed = item.trim();
    if ((trimmed.startsWith('"') && trimmed.endsWith('"')) || (trimmed.startsWith("'") && trimmed.endsWith("'"))) {
      return trimmed.slice(1, -1);
    }
    return trimmed;
  });
}
