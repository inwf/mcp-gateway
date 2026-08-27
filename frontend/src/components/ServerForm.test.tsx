import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from '@/test/harness';
import type { MCPServer } from '@/api/types';
import { ServerForm } from './ServerForm';

const api = setupServer();
beforeAll(() => api.listen({ onUnhandledRequest: 'bypass' }));
afterEach(() => api.resetHandlers());
afterAll(() => api.close());

function open(editing?: { name: string; server: MCPServer }) {
  renderWithProviders(<ServerForm open onClose={() => {}} editing={editing} />);
}

/** The label of every field on screen. Reading labels rather than
 *  querying by test id: the labels are what the user is looking at, and
 *  a field that is present but unlabelled is a bug of its own. */
function fieldsOnScreen(): string[] {
  return screen.getAllByText(/./, { selector: 'label' }).map((el) => el.textContent ?? '');
}

// The transports need different things, and offering every field for
// every transport would present a stdio server with a URL box that the
// gateway then rejects for being set.
describe('the fields a transport needs', () => {
  it('offers a command and an environment for stdio', () => {
    open();

    const labels = fieldsOnScreen().join(' ');
    expect(labels).toContain('命令');
    expect(labels).toContain('环境变量');
    expect(labels).not.toContain('地址');
    expect(labels).not.toContain('请求头');
  });

  it('offers a URL and headers for streamable-http, and no command', async () => {
    open();

    await userEvent.click(screen.getByText('streamable-http', { selector: 'div' }));

    await waitFor(() => {
      const labels = fieldsOnScreen().join(' ');
      expect(labels).toContain('地址');
      expect(labels).toContain('请求头');
      expect(labels).not.toContain('命令');
    });
  });

  it('offers both plus the ready patterns for streamable-http-local', async () => {
    open();

    await userEvent.click(screen.getByText('streamable-http-local', { selector: 'div' }));

    await waitFor(() => {
      const labels = fieldsOnScreen().join(' ');
      expect(labels).toContain('命令');
      expect(labels).toContain('地址');
      expect(labels).toContain('就绪匹配');
    });
  });

  // Someone converting a local server to a remote one should not have
  // to retype its name, description and timeout.
  it('keeps what was typed when the transport changes', async () => {
    open();

    const name = screen.getByLabelText('名称');
    await userEvent.type(name, 'my-server');
    await userEvent.click(screen.getByText('streamable-http', { selector: 'div' }));

    expect(screen.getByLabelText('名称')).toHaveValue('my-server');
  });
});

describe('what gets sent', () => {
  it('sends only the fields the transport uses', async () => {
    let received: unknown;
    api.use(
      http.post('/api/servers', async ({ request }) => {
        received = await request.json();
        return HttpResponse.json({ name: 'remote', config: {}, status: {} }, { status: 201 });
      }),
    );

    open();
    await userEvent.type(screen.getByLabelText('名称'), 'remote');
    await userEvent.click(screen.getByText('streamable-http', { selector: 'div' }));
    await userEvent.type(screen.getByLabelText('地址'), 'https://example.com/mcp');
    await userEvent.click(screen.getByRole('button', { name: '创建' }));

    await waitFor(() => expect(received).toBeDefined());

    const body = received as { name: string; server: MCPServer };
    expect(body.name).toBe('remote');
    expect(body.server.url).toBe('https://example.com/mcp');
    // A command typed before switching transport must not be sent: the
    // gateway rejects a streamable-http server that has one.
    expect(body.server.command).toBeUndefined();
    expect(body.server.transport).toBe('streamable-http');
  });

  it('refuses to submit without the fields the gateway requires', async () => {
    let called = false;
    api.use(
      http.post('/api/servers', () => {
        called = true;
        return HttpResponse.json({}, { status: 201 });
      }),
    );

    open();
    await userEvent.click(screen.getByRole('button', { name: '创建' }));

    // Checking here rather than letting the gateway do it is what saves
    // a round trip to be told about an empty box.
    await waitFor(() => expect(screen.getAllByText('此项必填').length).toBeGreaterThan(0));
    expect(called).toBe(false);
  });

  it('rejects a name the gateway could not use', async () => {
    open();

    await userEvent.type(screen.getByLabelText('名称'), 'has spaces');
    await userEvent.type(screen.getByLabelText('命令'), 'echo');
    await userEvent.click(screen.getByRole('button', { name: '创建' }));

    await waitFor(() =>
      expect(screen.getAllByText(/字母、数字、中划线或下划线/).length).toBeGreaterThan(0),
    );
  });
});

describe('editing an existing server', () => {
  const existing: MCPServer = {
    transport: 'stdio',
    enabled: true,
    timeout: '45s',
    command: 'npx',
    args: ['-y', 'some-package'],
    description: '既有服务器',
  };

  it('starts from what the server is', () => {
    open({ name: 'files', server: existing });

    expect(screen.getByLabelText('名称')).toHaveValue('files');
    expect(screen.getByLabelText('命令')).toHaveValue('npx');
    expect(screen.getByLabelText('超时')).toHaveValue('45s');
  });

  // A rename would be a different server as far as the gateway is
  // concerned, so the name is fixed while editing.
  it('does not allow the name to be changed', () => {
    open({ name: 'files', server: existing });

    expect(screen.getByLabelText('名称')).toBeDisabled();
  });

  // The gateway reports which field it objected to; a form that only
  // printed the sentence at the top would leave the user hunting.
  it('marks the field the gateway objected to', async () => {
    api.use(
      http.put('/api/servers/files', () =>
        HttpResponse.json(
          {
            error: {
              code: 'validation_failed',
              message: '配置无效',
              fields: [{ field: 'mcpServers.files.timeout', message: '必须为正数' }],
            },
          },
          { status: 422 },
        ),
      ),
    );

    open({ name: 'files', server: existing });
    await userEvent.click(screen.getByRole('button', { name: '保存' }));

    await waitFor(() => expect(screen.getByText('必须为正数')).toBeInTheDocument());
  });
});
