import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from '@/test/harness';
import ServerDetail from './ServerDetail';

const api = setupServer();
beforeAll(() => api.listen({ onUnhandledRequest: 'error' }));
afterEach(() => api.resetHandlers());
afterAll(() => api.close());

/*
 * The tools tab is where exposure is decided.
 *
 * The gateway offers nothing it has not been asked to offer, so this list
 * is the one place someone sees which of a server's tools reach a client
 * and turns them on. Before it existed, the only way to change that was a
 * checkbox list inside the edit drawer, and the tool list here said
 * nothing at all about it.
 *
 * What is worth pinning here is the request: this writes the whole allow
 * list back through the ordinary server-update endpoint, and getting its
 * shape wrong is a mistake this project has already made once — the tool
 * call dialog sent its arguments bare for weeks while the backend
 * required them under a key, and nothing noticed.
 */

const SERVER = {
  name: 'files',
  config: {
    transport: 'stdio',
    enabled: true,
    timeout: '30s',
    command: 'npx',
    exposedTools: ['read'],
  },
  exposedCount: 1,
  status: {
    name: 'files',
    state: 'connected',
    toolCount: 2,
    resourceCount: 0,
    hasTools: true,
    hasResources: false,
    hasPrompts: false,
    hasLogging: false,
  },
};

const TOOLS = [
  { name: 'read', description: 'read a file', exposed: 'files_read' },
  // No exposed name: this one is not in the gateway's tools/list.
  { name: 'write', description: 'write a file' },
];

function serving(server: object = SERVER, tools: unknown[] = TOOLS) {
  api.use(
    http.get('/api/servers/files', () => HttpResponse.json(server)),
    http.get('/api/servers/files/tools', () => HttpResponse.json({ tools })),
    http.get('/api/servers/files/resources', () => HttpResponse.json({ resources: [] })),
    http.get('/api/logs', () => HttpResponse.json({ entries: [], total: 0 })),
  );
}

function show() {
  return renderWithProviders(
    <MemoryRouter initialEntries={['/servers/files/tools']}>
      <Routes>
        <Route path="/servers/:name/:tab" element={<ServerDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

/** The row for one upstream tool.
 *
 *  Scoped to the list, because the selected tool's name also appears as
 *  the heading of the schema panel beside it. */
async function row(tool: string): Promise<HTMLElement> {
  await screen.findAllByText(tool);
  const list = document.querySelector('[class*="toolList"]');
  if (!list) throw new Error('the tool list is not rendered');

  const found = within(list as HTMLElement).getByText(tool).closest('div[class*="toolRow"]');
  if (!found) throw new Error(`no row for ${tool}`);
  return found as HTMLElement;
}

describe('what the tool list says about exposure', () => {
  it('shows the name a client would call for an exposed tool', async () => {
    serving();
    show();

    expect(within(await row('read')).getByText('files_read')).toBeInTheDocument();
  });

  // Not exposed is the ordinary state, so the row must still be there and
  // must not claim a name that was never registered.
  it('shows no name for a tool that is not exposed', async () => {
    serving();
    show();

    const written = await row('write');
    expect(written.textContent).not.toContain('files_write');
  });

  it('counts the exposed tools against the total', async () => {
    serving();
    show();

    expect(await screen.findByText('已暴露 1 / 2')).toBeInTheDocument();
  });

  it('reflects exposure in the switches', async () => {
    serving();
    show();

    await row('read');
    const switches = screen.getAllByRole('switch');
    expect(switches[0]).toBeChecked();
    expect(switches[1]).not.toBeChecked();
  });
});

describe('turning exposure on and off', () => {
  it('sends the whole allow list with the tool added', async () => {
    let body: unknown = null;
    serving();
    api.use(
      http.put('/api/servers/files', async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(SERVER);
      }),
    );
    show();

    await row('write');
    await userEvent.click(screen.getAllByRole('switch')[1]!);

    await waitFor(() => expect(body).not.toBeNull());
    // The endpoint takes the server under a key and the whole list, not a
    // delta. Both halves of that are easy to get wrong and neither shows
    // up as an error — the write simply does nothing useful.
    expect(body).toMatchObject({
      server: { exposedTools: ['read', 'write'] },
    });
  });

  it('sends the list with the tool removed', async () => {
    let body: unknown = null;
    serving();
    api.use(
      http.put('/api/servers/files', async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(SERVER);
      }),
    );
    show();

    await row('read');
    await userEvent.click(screen.getAllByRole('switch')[0]!);

    await waitFor(() => expect(body).not.toBeNull());
    expect(body).toMatchObject({ server: { exposedTools: [] } });
  });

  // Exposing the last tool of a server that had none must send an empty
  // list plus that one — not undefined, which would leave the field out
  // and change nothing.
  it('works from a server that exposes nothing at all', async () => {
    let body: unknown = null;
    // No exposedTools key at all, which is what the configuration file
    // produces for a server nobody has chosen tools for.
    const { exposedTools: _unset, ...bare } = SERVER.config;
    const quiet = { ...SERVER, exposedCount: 0, config: bare };
    serving(quiet, [
      { name: 'read', description: 'read a file' },
      { name: 'write', description: 'write a file' },
    ]);
    api.use(
      http.put('/api/servers/files', async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(quiet);
      }),
    );
    show();

    await row('read');
    await userEvent.click(screen.getAllByRole('switch')[0]!);

    await waitFor(() => expect(body).not.toBeNull());
    expect(body).toMatchObject({ server: { exposedTools: ['read'] } });
  });

  it('says so when the write fails, rather than looking as if it worked', async () => {
    serving();
    api.use(
      http.put('/api/servers/files', () =>
        HttpResponse.json(
          { error: { code: 'invalid', message: 'the server is not valid' } },
          { status: 422 },
        ),
      ),
    );
    show();

    await row('read');
    await userEvent.click(screen.getAllByRole('switch')[0]!);

    expect(await screen.findByText(/the server is not valid/)).toBeInTheDocument();
  });
});
