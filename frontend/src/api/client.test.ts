import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { ApiError, api } from './client';

const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

// Relative patterns, matched against whatever origin the test
// environment happens to use. Spelling out an origin here would tie the
// tests to jsdom's default, which is not the client's concern.
const AT = '';

describe('a request that succeeds', () => {
  it('returns the decoded body', async () => {
    server.use(http.get(`${AT}/api/health`, () => HttpResponse.json({ status: 'ok' })));

    await expect(api.get<{ status: string }>('/api/health')).resolves.toEqual({ status: 'ok' });
  });

  it('sends the body as JSON', async () => {
    let received: unknown;
    server.use(
      http.put(`${AT}/api/config`, async ({ request }) => {
        received = await request.json();
        return HttpResponse.json({ changes: [] });
      }),
    );

    await api.put('/api/config', { config: { version: 1 } });
    expect(received).toEqual({ config: { version: 1 } });
  });

  it('treats an empty response as no value rather than a parse failure', async () => {
    server.use(
      http.delete(`${AT}/api/servers/files`, () => new HttpResponse(null, { status: 204 })),
    );

    await expect(api.delete('/api/servers/files')).resolves.toBeUndefined();
  });

  it('drops query parameters that have no value', async () => {
    let url = '';
    server.use(
      http.get(`${AT}/api/logs`, ({ request }) => {
        url = new URL(request.url).search;
        return HttpResponse.json({ entries: [] });
      }),
    );

    await api.get('/api/logs', { query: { level: 'warn', server: undefined, module: '' } });
    expect(url).toBe('?level=warn');
  });
});

// The gateway understood the request and refused it. Everything the UI
// needs to explain that has to survive: which code, which fields, and
// the identifier that ties it to the server's own log.
describe('a request the gateway refuses', () => {
  it('carries the code, the message and the field errors', async () => {
    server.use(
      http.put(`${AT}/api/config`, () =>
        HttpResponse.json(
          {
            error: {
              code: 'validation_failed',
              message: 'the configuration is not valid',
              requestId: 'abc123',
              fields: [{ field: 'listen.port', message: 'must be between 0 and 65535' }],
            },
          },
          { status: 422 },
        ),
      ),
    );

    const error = await api.put('/api/config', {}).catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    const failure = error as ApiError;
    expect(failure.code).toBe('validation_failed');
    expect(failure.status).toBe(422);
    expect(failure.requestId).toBe('abc123');
    expect(failure.fields).toEqual([
      { field: 'listen.port', message: 'must be between 0 and 65535' },
    ]);
    expect(failure.isValidation).toBe(true);
    expect(failure.isOffline).toBe(false);
  });

  it('does not mistake a refusal for being offline', async () => {
    server.use(
      http.get(`${AT}/api/servers/nowhere`, () =>
        HttpResponse.json(
          { error: { code: 'not_found', message: 'no such server' } },
          { status: 404 },
        ),
      ),
    );

    const error = (await api.get('/api/servers/nowhere').catch((e: unknown) => e)) as ApiError;
    expect(error.isOffline).toBe(false);
    expect(error.code).toBe('not_found');
  });

  // Something other than the gateway answered — a proxy, or a dev
  // server serving index.html for an unmatched path. Reporting the JSON
  // parse failure would send someone looking at the wrong layer.
  it('reports a non-envelope body as an unreadable response', async () => {
    server.use(
      http.get(
        `${AT}/api/servers`,
        () =>
          new HttpResponse('<!doctype html><title>Gateway Timeout</title>', {
            status: 504,
            headers: { 'Content-Type': 'text/html' },
          }),
      ),
    );

    const error = (await api.get('/api/servers').catch((e: unknown) => e)) as ApiError;
    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(504);
    expect(error.message).toContain('504');
    expect(error.message).not.toContain('JSON.parse');
  });

  it('falls back to the status when the envelope has no message', async () => {
    server.use(
      http.get(`${AT}/api/servers`, () =>
        HttpResponse.json({ nonsense: true }, { status: 409 }),
      ),
    );

    const error = (await api.get('/api/servers').catch((e: unknown) => e)) as ApiError;
    expect(error.code).toBe('conflict');
  });
});

// The gateway was not reachable at all. Nothing the user typed is at
// fault and retrying is reasonable, so this has to be distinguishable.
describe('a request that never arrives', () => {
  it('is reported as being unable to reach the gateway', async () => {
    server.use(http.get(`${AT}/api/health`, () => HttpResponse.error()));

    const error = (await api.get('/api/health').catch((e: unknown) => e)) as ApiError;

    expect(error).toBeInstanceOf(ApiError);
    expect(error.isOffline).toBe(true);
    expect(error.status).toBe(0);
    expect(error.code).toBe('unavailable');
  });

  it('reports a timeout as such rather than as a refusal', async () => {
    server.use(
      http.get(`${AT}/api/health`, async () => {
        await new Promise((resolve) => setTimeout(resolve, 60));
        return HttpResponse.json({ status: 'ok' });
      }),
    );

    const error = (await api
      .get('/api/health', { timeoutMs: 10 })
      .catch((e: unknown) => e)) as ApiError;

    expect(error).toBeInstanceOf(ApiError);
    expect(error.isOffline).toBe(true);
    expect(error.message).toContain('超时');
  });

  // A component that unmounted mid-request cancelled it deliberately.
  // Turning that into an error would put a failure notice on screen for
  // something the user did by navigating away.
  it('lets a deliberate cancellation through unchanged', async () => {
    server.use(
      http.get(`${AT}/api/health`, async () => {
        await new Promise((resolve) => setTimeout(resolve, 60));
        return HttpResponse.json({ status: 'ok' });
      }),
    );

    const controller = new AbortController();
    const pending = api.get('/api/health', { signal: controller.signal });
    controller.abort();

    const error = await pending.catch((e: unknown) => e);
    expect(error).not.toBeInstanceOf(ApiError);
  });
});
