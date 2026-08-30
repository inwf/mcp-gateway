import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from '@/test/harness';
import Tools from './Tools';

const api = setupServer();
beforeAll(() => api.listen({ onUnhandledRequest: 'error' }));
afterEach(() => api.resetHandlers());
afterAll(() => api.close());

// Two things this page got wrong, both of them omissions.
//
// The gateway serves its own tools to every client alongside the forwarded
// ones, and this page showed only the forwarded ones — so the tools a
// model uses to find its way around an installation were invisible here
// while being served perfectly well over MCP.
//
// And the forwarded ones were one flat list. Which server a tool came
// from was a word on its card, so "what does this server offer" meant
// reading every card, and "why is this server contributing nothing" had
// no answer on the page at all.

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

/** A server as the API reports it: what was configured, and what came of
 *  connecting to it. */
function view(name: string, overrides: Record<string, unknown> = {}) {
  const { enabled = true, exposedCount = 0, ...status } = overrides;
  return {
    name,
    config: { transport: 'stdio', enabled, timeout: '30s', command: 'x' },
    exposedCount,
    status: {
      name,
      state: 'connected',
      toolCount: 1,
      resourceCount: 0,
      hasTools: true,
      hasResources: false,
      ...status,
    },
  };
}

const VIEWS = [view('files'), view('bing')];

function serving(forwarded = FORWARDED, own = OWN, views: unknown[] = VIEWS) {
  api.use(
    http.get('/api/tools', () => HttpResponse.json({ tools: forwarded, total: forwarded.length })),
    http.get('/api/gateway/tools', () => HttpResponse.json(own)),
    http.get('/api/servers', () => HttpResponse.json({ servers: views })),
  );
}

/** The panel whose heading is this name. Server names appear in the
 *  server filter's options as well, so a group has to be found by its
 *  heading rather than by the text alone. */
function group(name: string): HTMLElement {
  const heading = screen.getByRole('heading', { name });
  const panel = heading.closest('section');
  if (!panel) throw new Error(`the heading for ${name} is not inside a panel`);
  return panel;
}

describe("the gateway's own tools", () => {
  it('are shown', async () => {
    serving();
    renderWithProviders(<Tools />);

    expect(await screen.findByText('list_servers')).toBeInTheDocument();
    expect(screen.getByText('search_tools')).toBeInTheDocument();
  });

  it('are shown in a group of their own, not under a server', async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByText('list_servers');
    expect(within(group('系统工具')).getByText('list_servers')).toBeInTheDocument();
    expect(within(group('files')).queryByText('list_servers')).not.toBeInTheDocument();
  });

  // The gateway publishes its own tools and the forwarded ones through
  // the same endpoint. Taking all of them would list every forwarded tool
  // a second time, under the group that says they belong to no server.
  it('do not include the forwarded ones', async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByText('list_servers');
    expect(screen.getAllByText('files_read')).toHaveLength(1);
  });

  it('are called through the gateway rather than through a server', async () => {
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
    const own = within(group('系统工具'));
    await userEvent.click(own.getAllByRole('button', { name: /调用/ })[0]!);
    await userEvent.click(screen.getByRole('button', { name: '执行' }));

    await waitFor(() => expect(path).toBe('/api/gateway/tools/list_servers/call'));
  });
});

describe('one group per server', () => {
  it('puts each tool under the server it came from', async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByText('files_read');
    expect(within(group('files')).getByText('files_read')).toBeInTheDocument();
    expect(within(group('bing')).getByText('bing_search')).toBeInTheDocument();
    expect(within(group('files')).queryByText('bing_search')).not.toBeInTheDocument();
  });

  it('follows the order of the server list', async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByText('files_read');
    const headings = screen.getAllByRole('heading').map((node) => node.textContent);
    expect(headings.indexOf('files')).toBeLessThan(headings.indexOf('bing'));
  });

  // The pair, not the count. The gateway offers nothing it has not been
  // asked to, so a bare number reads as everything the server has.
  it('reports how much of each server is exposed', async () => {
    serving(FORWARDED, OWN, [
      view('files', { exposedCount: 1, toolCount: 9 }),
      view('bing', { exposedCount: 0, toolCount: 4 }),
    ]);
    renderWithProviders(<Tools />);

    await screen.findByText('files_read');
    expect(within(group('files')).getByText('已暴露 1 / 9')).toBeInTheDocument();
    expect(within(group('bing')).getByText('已暴露 0 / 4')).toBeInTheDocument();
  });

  it("reports each server's state on its group", async () => {
    serving(FORWARDED, OWN, [view('files'), view('bing', { state: 'failed', error: 'boom' })]);
    renderWithProviders(<Tools />);

    await screen.findByText('files_read');
    expect(within(group('bing')).getByText('连接失败')).toBeInTheDocument();
  });
});

// A server contributing nothing is the case a flat list could not
// express: it simply was not there, which looks the same as never having
// been configured.
describe('a server with no tools on offer', () => {
  it('still has a group', async () => {
    serving([], OWN, [view('files', { toolCount: 0 })]);
    renderWithProviders(<Tools />);

    await screen.findByRole('heading', { name: 'files' });
    expect(within(group('files')).getByText('没有暴露任何工具')).toBeInTheDocument();
  });

  it('says the server is disabled when it is', async () => {
    serving([], OWN, [view('files', { enabled: false, state: 'disconnected', toolCount: 0 })]);
    renderWithProviders(<Tools />);

    await screen.findByRole('heading', { name: 'files' });
    expect(within(group('files')).getByText(/启用之后它的工具/)).toBeInTheDocument();
  });

  it('says the connection failed when it did', async () => {
    serving([], OWN, [view('files', { state: 'failed', error: 'boom', toolCount: 0 })]);
    renderWithProviders(<Tools />);

    await screen.findByRole('heading', { name: 'files' });
    expect(within(group('files')).getByText(/拿不到工具列表/)).toBeInTheDocument();
  });

  it('says the server declares no tools when it does not', async () => {
    serving([], OWN, [view('files', { hasTools: false, toolCount: 0 })]);
    renderWithProviders(<Tools />);

    await screen.findByRole('heading', { name: 'files' });
    expect(within(group('files')).getByText(/本来就不提供工具/)).toBeInTheDocument();
  });

  // The ordinary case, not a fault: the gateway exposes nothing it has not
  // been asked to, so a server that was just added contributes none of its
  // tools. The group has to say that, and say they are still callable —
  // otherwise it reads as a server that arrived broken.
  it('says the tools are unexposed rather than missing, and still callable', async () => {
    serving([], OWN, [view('files', { toolCount: 4 })]);
    renderWithProviders(<Tools />);

    await screen.findByRole('heading', { name: 'files' });
    const hint = within(group('files')).getByText(/4 个工具/);
    expect(hint.textContent).toContain('没有勾选暴露');
    expect(hint.textContent).toContain('call_tool');
  });
});

describe('narrowing the list', () => {
  // Picking a server asks about that server. The gateway's own tools are
  // on no server, so they are not an answer to it.
  it("leaves out the other servers and the gateway's own tools", async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByText('list_servers');
    await userEvent.click(screen.getByRole('combobox'));
    await userEvent.click(await screen.findByTitle('files'));

    await waitFor(() => expect(screen.queryByText('list_servers')).not.toBeInTheDocument());
    expect(screen.getByText('files_read')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'bing' })).not.toBeInTheDocument();
  });

  // Every configured server is on offer in the filter, including one with
  // no tools — asking about it is how its group's explanation is reached.
  it('offers a server with no tools in the filter', async () => {
    serving([], OWN, [view('quiet', { toolCount: 0 })]);
    renderWithProviders(<Tools />);

    await screen.findByRole('heading', { name: 'quiet' });
    await userEvent.click(screen.getByRole('combobox'));
    expect(await screen.findByTitle('quiet')).toBeInTheDocument();
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

  // Under a search a group with nothing in it is not part of the answer,
  // where without one it is reporting the state of its server.
  it('drops the groups a search did not reach', async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByRole('heading', { name: 'bing' });
    api.use(
      http.get('/api/tools', () =>
        HttpResponse.json({ tools: [FORWARDED[0]], total: 1 }),
      ),
    );
    await userEvent.type(screen.getByRole('searchbox'), 'disk');

    await waitFor(() =>
      expect(screen.queryByRole('heading', { name: 'bing' })).not.toBeInTheDocument(),
    );
    expect(screen.getByRole('heading', { name: 'files' })).toBeInTheDocument();
  });

  it('says so when nothing matched at all', async () => {
    serving();
    renderWithProviders(<Tools />);

    await screen.findByRole('heading', { name: 'files' });
    api.use(
      http.get('/api/tools', () => HttpResponse.json({ tools: [], total: 0 })),
      http.get('/api/gateway/tools', () =>
        HttpResponse.json({ tools: [], total: 0, systemTools: [] }),
      ),
    );
    await userEvent.type(screen.getByRole('searchbox'), 'nothing like this');

    expect(await screen.findByText('没有匹配的工具')).toBeInTheDocument();
  });
});

describe('no servers configured', () => {
  it('says so rather than reporting that nothing matched', async () => {
    serving([], OWN, []);
    renderWithProviders(<Tools />);

    expect(await screen.findByText('没有可用的工具')).toBeInTheDocument();
    // The gateway's own tools are still on offer, and still shown.
    expect(screen.getByText('list_servers')).toBeInTheDocument();
  });
});

// The endpoint reports the number of tools it returned, not the number it
// had, so a list cut off at the limit is indistinguishable from a complete
// one — and every group count on the page is then a lower bound.
describe('a list that hit the limit', () => {
  const many = Array.from({ length: 200 }, (_, i) => ({
    server: 'files',
    tool: `t${i}`,
    exposed: `files_t${i}`,
    description: `tool number ${i}`,
  }));

  it('says the list was cut off', async () => {
    serving(many);
    renderWithProviders(<Tools />);

    expect(await screen.findByText(/只列出了前 200 个工具/)).toBeInTheDocument();
  });

  // Alphabetical order by exposed name means the servers late in the
  // alphabet are the ones cut, so "this server exposes nothing" is
  // exactly the wrong conclusion to state confidently.
  it('does not claim an empty group has nothing to offer', async () => {
    serving(many, OWN, [view('files', { toolCount: 200 }), view('zzz', { toolCount: 7 })]);
    renderWithProviders(<Tools />);

    await screen.findByRole('heading', { name: 'zzz' });
    const empty = within(group('zzz'));
    expect(empty.getByText(/可能只是没被列出来/)).toBeInTheDocument();
    expect(empty.queryByText(/都没有勾选对外开放/)).not.toBeInTheDocument();
  });
});
