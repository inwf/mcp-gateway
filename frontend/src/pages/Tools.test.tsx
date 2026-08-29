import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from '@/test/harness';
import Tools from './Tools';

const api = setupServer();
beforeAll(() => api.listen({ onUnhandledRequest: 'error' }));
afterEach(() => api.resetHandlers());
afterAll(() => api.close());

// The gateway serves its own tools to every client alongside the
// forwarded ones. This page showed only the forwarded ones, so the seven
// that a model uses to find its way around an installation were invisible
// here while being served perfectly well over MCP.

const FORWARDED = [
  {
    server: 'files',
    tool: 'read',
    exposed: 'files_read',
    description: 'read a file from disk',
  },
  {
    server: 'bing',
    tool: 'search',
    exposed: 'bing_search',
    description: 'search the web',
  },
];

const OWN = {
  tools: [
    { name: 'list_servers', description: 'list the configured servers' },
    { name: 'search_tools', description: 'search for tools across every server' },
    // What the gateway publishes includes the forwarded tools too, and
    // those must not be counted twice: systemTools is what says which are
    // its own.
    { name: 'files_read', description: 'read a file from disk' },
  ],
  total: 3,
  systemTools: ['list_servers', 'search_tools'],
};

function serving(forwarded = FORWARDED, own = OWN) {
  api.use(
    http.get('/api/tools', () => HttpResponse.json({ tools: forwarded, total: forwarded.length })),
    http.get('/api/gateway/tools', () => HttpResponse.json(own)),
  );
}

describe('the two kinds of tool', () => {
  it("shows the gateway's own tools", async () => {
    serving();
    renderWithProviders(<Tools />);

    expect(await screen.findByText('list_servers')).toBeInTheDocument();
    expect(screen.getByText('search_tools')).toBeInTheDocument();
  });

  it('shows them apart from the forwarded ones', async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByText('list_servers');
    expect(screen.getByText('系统工具')).toBeInTheDocument();
    expect(screen.getByText('服务器工具')).toBeInTheDocument();
  });

  // The gateway publishes its own tools and the forwarded ones through
  // the same endpoint. Taking all of them would list every forwarded tool
  // a second time, under the group that says they belong to no server.
  it('counts only the tools the gateway names as its own', async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByText('list_servers');
    expect(screen.getAllByText('files_read')).toHaveLength(1);
  });

  it('sends a forwarded tool to its server and a gateway tool to the gateway', async () => {
    let path = '';
    serving();
    api.use(
      http.post('*/call', ({ request }) => {
        path = new URL(request.url).pathname;
        return HttpResponse.json({ isError: false, content: [] });
      }),
    );
    renderWithProviders(<Tools />);

    await screen.findByText('list_servers');
    const buttons = screen.getAllByRole('button', { name: /调用/ });

    // The gateway's group comes first, so its first button is a gateway
    // tool's.
    await userEvent.click(buttons[0]!);
    await userEvent.click(screen.getByRole('button', { name: '执行' }));
    await waitFor(() => expect(path).toBe('/api/gateway/tools/list_servers/call'));
  });
});

describe('narrowing the list', () => {
  // Picking a server asks about that server. The gateway's own tools are
  // on no server, so they are not an answer to it.
  it("leaves out the gateway's own tools when a server is picked", async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByText('list_servers');
    await userEvent.click(screen.getByRole('combobox'));
    await userEvent.click(await screen.findByTitle('files'));

    await waitFor(() => expect(screen.queryByText('list_servers')).not.toBeInTheDocument());
    expect(screen.getByText('files_read')).toBeInTheDocument();
    expect(screen.queryByText('bing_search')).not.toBeInTheDocument();
  });

  // The gateway ranks the forwarded tools; these are filtered here. A
  // search that reached only one group would answer half the question
  // without saying so.
  it("applies the search to the gateway's own tools too", async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByText('list_servers');
    await userEvent.type(screen.getByRole('searchbox'), 'servers');

    await waitFor(() => expect(screen.queryByText('search_tools')).not.toBeInTheDocument());
    expect(screen.getByText('list_servers')).toBeInTheDocument();
  });
});
