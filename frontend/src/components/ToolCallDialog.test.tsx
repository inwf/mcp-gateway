import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from '@/test/harness';
import { ToolCallDialog } from './ToolCallDialog';

const api = setupServer();
beforeAll(() => api.listen({ onUnhandledRequest: 'error' }));
afterEach(() => api.resetHandlers());
afterAll(() => api.close());

const ECHO = {
  name: 'echo',
  description: 'returns its argument',
  inputSchema: {
    type: 'object',
    properties: { message: { type: 'string' } },
    required: ['message'],
  },
};

function answer() {
  return HttpResponse.json({ isError: false, content: [{ type: 'text', text: 'done' }] });
}

async function run() {
  await userEvent.click(screen.getByRole('button', { name: '执行' }));
}

// The gateway takes the arguments under a key and rejects a body with
// anything else in it, so posting them bare fails the request before the
// tool is reached — with a message about an unknown field, which reads
// like the tool refused the call rather than like the request never
// arrived.
describe('the shape of the request', () => {
  it('sends the arguments under a key rather than as the body', async () => {
    let received: unknown;
    api.use(
      http.post('/api/servers/files/tools/echo/call', async ({ request }) => {
        received = await request.json();
        return answer();
      }),
    );

    renderWithProviders(
      <ToolCallDialog open onClose={() => {}} server="files" tool={ECHO} />,
    );
    await run();

    await waitFor(() => expect(received).toEqual({ arguments: { message: '' } }));
  });
});

// The gateway's own tools belong to no server, so the per-server route
// has no name to put in it and they have one of their own.
describe('where a call is sent', () => {
  it('sends a server tool to that server', async () => {
    let path = '';
    api.use(
      http.post('/api/servers/files/tools/echo/call', ({ request }) => {
        path = new URL(request.url).pathname;
        return answer();
      }),
    );

    renderWithProviders(
      <ToolCallDialog open onClose={() => {}} server="files" tool={ECHO} />,
    );
    await run();

    await waitFor(() => expect(path).toBe('/api/servers/files/tools/echo/call'));
  });

  it("sends one of the gateway's own tools to the gateway", async () => {
    let path = '';
    api.use(
      http.post('/api/gateway/tools/list_servers/call', ({ request }) => {
        path = new URL(request.url).pathname;
        return answer();
      }),
    );

    renderWithProviders(
      <ToolCallDialog
        open
        onClose={() => {}}
        server=""
        tool={{ name: 'list_servers', description: 'lists the servers' }}
      />,
    );
    await run();

    await waitFor(() => expect(path).toBe('/api/gateway/tools/list_servers/call'));
  });
});

describe('what comes back', () => {
  it('shows what the tool said', async () => {
    api.use(http.post('/api/servers/files/tools/echo/call', () => answer()));

    renderWithProviders(
      <ToolCallDialog open onClose={() => {}} server="files" tool={ECHO} />,
    );
    await run();

    expect(await screen.findByText('done')).toBeInTheDocument();
  });

  // A tool that ran and reported a problem is a successful call with a
  // failed result. Both have to be visible, and differently: the first is
  // the gateway working.
  it("marks a tool's own error as a failed result rather than a failed call", async () => {
    api.use(
      http.post('/api/servers/files/tools/echo/call', () =>
        HttpResponse.json({
          isError: true,
          content: [{ type: 'text', text: 'the file is not there' }],
        }),
      ),
    );

    renderWithProviders(
      <ToolCallDialog open onClose={() => {}} server="files" tool={ECHO} />,
    );
    await run();

    expect(await screen.findByText('the file is not there')).toBeInTheDocument();
    expect(screen.getByText('工具报告了一个错误')).toBeInTheDocument();
  });
});
