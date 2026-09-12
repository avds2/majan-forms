import { afterEach, describe, expect, it, vi } from 'vitest';
import { api, APIError } from './api';

function streamResponse(chunks: string[]): Response {
  const encoder = new TextEncoder();
  return new Response(
    new ReadableStream({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
        controller.close();
      }
    }),
    { status: 200, headers: { 'Content-Type': 'text/event-stream' } }
  );
}

afterEach(() => vi.unstubAllGlobals());

describe('AI SSE client', () => {
  it('reassembles split CRLF frames and delivers deltas', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
      streamResponse([
        'event: meta\r\ndata: {"conversation_id":"conversation-1"}\r\n\r\nevent: delta\r\ndata: {"text":"پاسخ ',
        'زنده"}\r\n\r\nevent: done\r\ndata: {"message":{"id":"message-1","conversation_id":"conversation-1",',
        '"role":"assistant","content":"پاسخ زنده","created_at":"2026-09-07T00:00:00Z"}}\r\n\r\n'
      ])
    );
    vi.stubGlobal('fetch', fetchMock);
    const deltas: string[] = [];
    const message = await api.streamAIMessage('project-1', 'conversation-1', 'پرسش', (delta) => deltas.push(delta));
    expect(deltas).toEqual(['پاسخ زنده']);
    expect(message.content).toBe('پاسخ زنده');
    const request = fetchMock.mock.calls[0][1] as RequestInit;
    expect(request.credentials).toBe('same-origin');
    expect(JSON.parse(String(request.body))).toEqual({ content: 'پرسش' });
  });

  it('turns a streamed safe error into APIError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        streamResponse(['event: error\ndata: {"code":"ai_not_ready","message":"قابلیت خاموش است"}\n\n'])
      )
    );
    await expect(api.streamAIMessage('project-1', 'conversation-1', 'پرسش', () => {})).rejects.toMatchObject({
      name: 'APIError',
      code: 'ai_not_ready',
      message: 'قابلیت خاموش است'
    } satisfies Partial<APIError>);
  });
});
