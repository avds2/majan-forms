import type {
  AIConversation,
  AIMessage,
  AIProjectSettings,
  AIProviderCompatibility,
  AIProviderSettings,
  APIErrorBody,
  Draft,
  FormSchema,
  FormVersion,
  Project,
  ProjectPackageImportResult,
  ProjectPackagePreview,
  ProjectReport,
  PublishValidation,
  ReportQuery,
  SystemHealth,
  SystemBackupJob,
  SystemBackupList,
  SystemRestoreRequest,
  ResponseDetail,
  ResponseList,
  ResponseQuery,
  Role,
  RuntimeConfig,
  User
} from './types';

function queryString<T extends object>(values: T): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(values) as Array<[string, string | number | undefined]>) {
    if (value !== undefined && value !== '') query.set(key, String(value));
  }
  const encoded = query.toString();
  return encoded ? `?${encoded}` : '';
}

export class APIError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
    public readonly body: APIErrorBody
  ) {
    super(message);
    this.name = 'APIError';
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  headers.set('Accept', 'application/json');
  const response = await fetch(path, {
    ...init,
    headers,
    credentials: 'same-origin'
  });
  if (response.status === 204) return undefined as T;
  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    throw new APIError(response.status, 'invalid_response', 'پاسخ سرور قابل‌خواندن نبود', {});
  }
  if (!response.ok) {
    const body = payload as APIErrorBody;
    throw new APIError(
      response.status,
      body.error?.code ?? 'request_failed',
      body.error?.message ?? 'درخواست انجام نشد',
      body
    );
  }
  return (payload as { data: T }).data;
}

async function streamAIMessage(
  projectID: string,
  conversationID: string,
  content: string,
  onDelta: (text: string) => void
): Promise<AIMessage> {
  const response = await fetch(
    `/api/projects/${encodeURIComponent(projectID)}/ai/conversations/${encodeURIComponent(conversationID)}/messages/stream`,
    {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
      body: JSON.stringify({ content })
    }
  );
  if (!response.ok) {
    let body: APIErrorBody = {};
    try {
      body = (await response.json()) as APIErrorBody;
    } catch {
      // The common request helper reports the same safe fallback for malformed responses.
    }
    throw new APIError(
      response.status,
      body.error?.code ?? 'request_failed',
      body.error?.message ?? 'درخواست انجام نشد',
      body
    );
  }
  if (!response.body) throw new APIError(502, 'invalid_response', 'جریان پاسخ provider قابل‌خواندن نبود', {});
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let completed: AIMessage | null = null;
  const consume = (frame: string) => {
    let event = 'message';
    const data: string[] = [];
    for (const line of frame.replaceAll('\r', '').split('\n')) {
      if (line.startsWith('event:')) event = line.slice(6).trim();
      if (line.startsWith('data:')) data.push(line.slice(5).trimStart());
    }
    if (data.length === 0) return;
    const value = JSON.parse(data.join('\n')) as Record<string, unknown>;
    if (event === 'delta' && typeof value.text === 'string') onDelta(value.text);
    if (event === 'done') completed = (value.message ?? null) as AIMessage | null;
    if (event === 'error') {
      throw new APIError(
        502,
        String(value.code ?? 'ai_stream_error'),
        String(value.message ?? 'پاسخ هوش مصنوعی کامل نشد'),
        {
          error: {
            code: String(value.code ?? 'ai_stream_error'),
            message: String(value.message ?? 'پاسخ هوش مصنوعی کامل نشد')
          }
        }
      );
    }
  };
  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value, { stream: !done });
    const frames = buffer.replaceAll('\r\n', '\n').split('\n\n');
    buffer = frames.pop() ?? '';
    for (const frame of frames) consume(frame);
    if (done) break;
  }
  if (buffer.trim()) consume(buffer);
  if (!completed) throw new APIError(502, 'incomplete_stream', 'پاسخ هوش مصنوعی ناتمام ماند', {});
  return completed;
}

export const api = {
  runtimeConfig: () => request<RuntimeConfig>('/api/runtime-config'),
  me: () => request<User>('/api/auth/me'),
  login: (identifier: string, password: string) =>
    request<User>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ identifier, password })
    }),
  logout: () => request<void>('/api/auth/logout', { method: 'POST' }),
  projects: (trash = false) => request<Project[]>(`/api/projects${trash ? '?trash=true' : ''}`),
  project: (id: string) => request<Project>(`/api/projects/${encodeURIComponent(id)}`),
  createProject: (title: string, slug: string) =>
    request<Project>('/api/projects', {
      method: 'POST',
      body: JSON.stringify({ title, slug })
    }),
  updateProject: (id: string, title: string, slug: string) =>
    request<Project>(`/api/projects/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify({ title, slug })
    }),
  trashProject: (id: string) =>
    request<Project>(`/api/projects/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    }),
  restoreProject: (id: string) =>
    request<Project>(`/api/projects/${encodeURIComponent(id)}/restore`, {
      method: 'POST'
    }),
  closeProject: (id: string) => request<Project>(`/api/projects/${encodeURIComponent(id)}/close`, { method: 'POST' }),
  reopenProject: (id: string) => request<Project>(`/api/projects/${encodeURIComponent(id)}/reopen`, { method: 'POST' }),
  draft: (projectID: string) => request<Draft>(`/api/projects/${encodeURIComponent(projectID)}/draft`),
  saveDraft: (projectID: string, expectedRevision: number, schema: FormSchema) =>
    request<Draft>(`/api/projects/${encodeURIComponent(projectID)}/draft`, {
      method: 'PUT',
      body: JSON.stringify({ expected_revision: expectedRevision, schema })
    }),
  validatePublish: (projectID: string) =>
    request<PublishValidation>(`/api/projects/${encodeURIComponent(projectID)}/publish/validate`, {
      method: 'POST'
    }),
  publish: (projectID: string, expectedRevision: number, idempotencyKey: string) =>
    request<FormVersion>(`/api/projects/${encodeURIComponent(projectID)}/publish`, {
      method: 'POST',
      headers: { 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify({ expected_revision: expectedRevision })
    }),
  versions: (projectID: string) => request<FormVersion[]>(`/api/projects/${encodeURIComponent(projectID)}/versions`),
  responses: (projectID: string, query: ResponseQuery = {}) =>
    request<ResponseList>(`/api/projects/${encodeURIComponent(projectID)}/responses${queryString(query)}`),
  response: (projectID: string, responseID: string) =>
    request<ResponseDetail>(
      `/api/projects/${encodeURIComponent(projectID)}/responses/${encodeURIComponent(responseID)}`
    ),
  deleteResponses: (projectID: string, ids: string[]) =>
    request<{ updated: number }>(`/api/projects/${encodeURIComponent(projectID)}/responses/delete`, {
      method: 'POST',
      body: JSON.stringify({ ids })
    }),
  restoreResponses: (projectID: string, ids: string[]) =>
    request<{ updated: number }>(`/api/projects/${encodeURIComponent(projectID)}/responses/restore`, {
      method: 'POST',
      body: JSON.stringify({ ids })
    }),
  report: (projectID: string, query: ReportQuery = {}) =>
    request<ProjectReport>(`/api/projects/${encodeURIComponent(projectID)}/reports/summary${queryString(query)}`),
  exportURL: (projectID: string, format: 'csv' | 'xlsx' | 'json', query: ResponseQuery = {}) =>
    `/api/projects/${encodeURIComponent(projectID)}/exports/responses${queryString({ ...query, format })}`,
  printReportURL: (projectID: string, query: ReportQuery = {}) =>
    `/api/projects/${encodeURIComponent(projectID)}/reports/print${queryString(query)}`,
  projectPackageURL: (projectID: string, versions?: number[]) =>
    `/api/projects/${encodeURIComponent(projectID)}/package${queryString({
      versions: versions?.length ? versions.join(',') : 'all'
    })}`,
  previewProjectPackage: (file: File) =>
    request<ProjectPackagePreview>('/api/project-packages/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/zip' },
      body: file
    }),
  importProjectPackage: (file: File, title: string, slug: string) =>
    request<ProjectPackageImportResult>(`/api/project-packages/import${queryString({ title, slug })}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/zip' },
      body: file
    }),
  aiProvider: () => request<AIProviderSettings>('/api/ai/provider'),
  saveAIProvider: (input: {
    base_url: string;
    api_mode: 'responses' | 'chat_completions';
    model: string;
    timeout_seconds: number;
    max_output_tokens: number;
    allow_private_endpoint: boolean;
    api_key?: string;
  }) => request<AIProviderSettings>('/api/ai/provider', { method: 'PUT', body: JSON.stringify(input) }),
  testAIProvider: () => request<AIProviderCompatibility>('/api/ai/provider/test', { method: 'POST' }),
  aiProjectSettings: (projectID: string) =>
    request<AIProjectSettings>(`/api/projects/${encodeURIComponent(projectID)}/ai/settings`),
  saveAIProjectSettings: (projectID: string, enabled: boolean, textSamplingEnabled: boolean) =>
    request<AIProjectSettings>(`/api/projects/${encodeURIComponent(projectID)}/ai/settings`, {
      method: 'PUT',
      body: JSON.stringify({ enabled, text_sampling_enabled: textSamplingEnabled })
    }),
  aiConversations: (projectID: string) =>
    request<AIConversation[]>(`/api/projects/${encodeURIComponent(projectID)}/ai/conversations`),
  createAIConversation: (projectID: string, title = '') =>
    request<AIConversation>(`/api/projects/${encodeURIComponent(projectID)}/ai/conversations`, {
      method: 'POST',
      body: JSON.stringify({ title })
    }),
  aiMessages: (projectID: string, conversationID: string) =>
    request<AIMessage[]>(
      `/api/projects/${encodeURIComponent(projectID)}/ai/conversations/${encodeURIComponent(conversationID)}/messages`
    ),
  deleteAIConversation: (projectID: string, conversationID: string) =>
    request<void>(
      `/api/projects/${encodeURIComponent(projectID)}/ai/conversations/${encodeURIComponent(conversationID)}`,
      { method: 'DELETE' }
    ),
  streamAIMessage,
  users: () => request<User[]>('/api/users'),
  systemHealth: () => request<SystemHealth>('/api/system/health'),
  systemBackups: () => request<SystemBackupList>('/api/system/backups'),
  createSystemBackup: () => request<SystemBackupJob>('/api/system/backups', { method: 'POST' }),
  systemBackupURL: (name: string) => `/api/system/backups/${encodeURIComponent(name)}`,
  prepareSystemRestore: (name: string, confirmation: string) =>
    request<SystemRestoreRequest>(`/api/system/backups/${encodeURIComponent(name)}/restore`, {
      method: 'POST',
      body: JSON.stringify({ confirmation })
    }),
  createUser: (input: { email: string; username: string; display_name: string; password: string; role: Role }) =>
    request<User>('/api/users', {
      method: 'POST',
      body: JSON.stringify(input)
    }),
  updateUser: (id: string, input: { display_name: string; role: Role; active: boolean }) =>
    request<User>(`/api/users/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(input)
    })
};
