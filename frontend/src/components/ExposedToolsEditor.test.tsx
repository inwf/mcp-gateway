import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from '@/test/harness';
import { ExposedToolsEditor } from './ExposedToolsEditor';

const api = setupServer();
beforeAll(() => api.listen({ onUnhandledRequest: 'error' }));
afterEach(() => api.resetHandlers());
afterAll(() => api.close());

// Typing tool names by hand is the wrong way round: the server has
// already told the gateway what it offers, so the work is picking from a
// list. A typo in a typed name silently exposes nothing, and nothing on
// screen says so.

const TOOLS = [
  { name: 'read', description: 'read a file' },
  { name: 'write', description: 'write a file' },
  { name: 'list', description: 'list a directory' },
];

function serving(tools = TOOLS) {
  api.use(http.get('/api/servers/files/tools', () => HttpResponse.json({ tools })));
}

function offline() {
  api.use(
    http.get('/api/servers/files/tools', () =>
      HttpResponse.json(
        { error: { code: 'unavailable', message: '"files" is not connected' } },
        { status: 503 },
      ),
    ),
  );
}

describe('picking from what the server offers', () => {
  it("shows a box for each of the server's tools", async () => {
    serving();
    renderWithProviders(<ExposedToolsEditor server="files" value={[]} />);

    expect(await screen.findByText('read')).toBeInTheDocument();
    expect(screen.getByText('write')).toBeInTheDocument();
    expect(screen.getByText('list')).toBeInTheDocument();
  });

  it('reports what is ticked', async () => {
    serving();
    const onChange = vi.fn();
    renderWithProviders(<ExposedToolsEditor server="files" value={[]} onChange={onChange} />);

    await userEvent.click(await screen.findByText('read'));
    expect(onChange).toHaveBeenCalledWith(['read']);
  });

  it('ticks and clears them all at once', async () => {
    serving();
    const onChange = vi.fn();
    renderWithProviders(<ExposedToolsEditor server="files" value={[]} onChange={onChange} />);

    await userEvent.click(await screen.findByText('全选'));
    expect(onChange).toHaveBeenCalledWith(['read', 'write', 'list']);
  });

  // An empty list means every tool, which is not what an empty set of
  // boxes looks like it means.
  it('says that ticking nothing exposes everything', async () => {
    serving();
    renderWithProviders(<ExposedToolsEditor server="files" value={[]} />);

    expect(await screen.findByText('未勾选 = 全部暴露')).toBeInTheDocument();
  });

  it('counts what is ticked once something is', async () => {
    serving();
    renderWithProviders(<ExposedToolsEditor server="files" value={['read']} />);

    expect(await screen.findByText('已选 1 / 3')).toBeInTheDocument();
  });
});

// The list is only there once the server is connected, and a server
// being added is not connected yet. Typing is the only thing that works
// then, so it stays rather than being removed.
describe('when there is nothing to pick from', () => {
  it('falls back to typing while the server is being added', async () => {
    renderWithProviders(<ExposedToolsEditor value={[]} />);

    expect(await screen.findByText(/服务器还没创建/)).toBeInTheDocument();
  });

  it('falls back to typing when the server is not connected', async () => {
    offline();
    renderWithProviders(<ExposedToolsEditor server="files" value={['read']} />);

    expect(await screen.findByText(/服务器未连接/)).toBeInTheDocument();
    // What is already configured has to stay editable, not vanish.
    expect(screen.getByDisplayValue('read')).toBeInTheDocument();
  });

  it('says so when the server offers nothing', async () => {
    serving([]);
    renderWithProviders(<ExposedToolsEditor server="files" value={[]} />);

    expect(await screen.findByText('这台服务器没有提供任何工具')).toBeInTheDocument();
  });
});

// A configured name the server no longer offers has no box to tick.
// Dropping it from the list would remove it from the configuration on the
// next save, which is not something the user asked for.
describe('a tool that is no longer there', () => {
  it('keeps a configured name the server does not offer', async () => {
    serving();
    renderWithProviders(<ExposedToolsEditor server="files" value={['read', 'vanished']} />);

    expect(await screen.findByText(/配置里有，但这台服务器当前并不提供/)).toBeInTheDocument();
    expect(screen.getByText('vanished')).toBeInTheDocument();
  });

  it('lets it be dropped deliberately', async () => {
    serving();
    const onChange = vi.fn();
    renderWithProviders(
      <ExposedToolsEditor server="files" value={['read', 'vanished']} onChange={onChange} />,
    );

    await screen.findByText('vanished');
    await userEvent.click(screen.getByRole('button', { name: '×' }));

    await waitFor(() => expect(onChange).toHaveBeenCalledWith(['read']));
  });
});
