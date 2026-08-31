import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { setupServer } from 'msw/node';
import { http, HttpResponse } from 'msw';
import { renderWithProviders } from '@/test/harness';
import { ImportConfig } from './ImportConfig';

const api = setupServer();
beforeAll(() => api.listen({ onUnhandledRequest: 'error' }));
afterEach(() => api.resetHandlers());
afterAll(() => api.close());

/*
 * The pair to the export.
 *
 * A file that can be downloaded and not uploaded is a backup nobody can
 * restore. What is worth pinning here is the order of operations: the
 * document is checked without saving first, and only a document the
 * gateway accepts is written. Getting that backwards would mean a bad
 * paste replacing a working configuration and the reasons arriving too
 * late to matter.
 */

const DOCUMENT = `version: 1
listen:
  host: 127.0.0.1
  port: 7788
`;

/** Records what each endpoint was asked to do, which is the thing under
 *  test: this dialog's job is which requests it makes, in what order. */
function serving({ valid = true, fields = [] as { field: string; message: string }[] } = {}) {
  const calls: { checked: unknown[]; saved: unknown[] } = { checked: [], saved: [] };

  api.use(
    http.post('/api/config/validate', async ({ request }) => {
      calls.checked.push(await request.json());
      return HttpResponse.json({ valid, fields });
    }),
    http.put('/api/config', async ({ request }) => {
      calls.saved.push(await request.json());
      return HttpResponse.json({ config: {}, changes: [{ field: 'listen.port', old: '1', new: '2' }] });
    }),
  );

  renderWithProviders(<ImportConfig />);
  return calls;
}

async function openDialog() {
  await userEvent.click(screen.getByRole('button', { name: /导入/ }));
  return within(await screen.findByRole('dialog'));
}

async function paste(dialog: ReturnType<typeof within>, text: string) {
  const editor = dialog.getByRole('textbox', { name: '配置文档' });
  // Typed character by character, userEvent's paste is what a person
  // actually does with a document this size — and typing YAML would have
  // the editor auto-indenting halfway through.
  editor.focus();
  await userEvent.paste(text);
}

describe('importing a whole configuration', () => {
  it('says up front that this replaces everything', async () => {
    serving();
    const dialog = await openDialog();

    expect(dialog.getByText(/替换整份配置/)).toBeInTheDocument();
    // And the button says what it does rather than "OK".
    expect(dialog.getByRole('button', { name: '替换配置' })).toBeInTheDocument();
  });

  it('will not act on an empty or unparseable document', async () => {
    serving();
    const dialog = await openDialog();

    expect(dialog.getByRole('button', { name: '替换配置' })).toBeDisabled();

    await paste(dialog, 'listen: [1, 2\n');
    expect(dialog.getByRole('button', { name: '替换配置' })).toBeDisabled();
  });

  // A YAML document that parses to a string is not a configuration, and
  // sending it would earn an error from the gateway about something else.
  it('refuses something that is not a document', async () => {
    serving();
    const dialog = await openDialog();

    await paste(dialog, 'just a sentence');

    expect(dialog.getByText(/不是一份配置文档/)).toBeInTheDocument();
    expect(dialog.getByRole('button', { name: '替换配置' })).toBeDisabled();
  });

  it('checks before it writes', async () => {
    const calls = serving();
    const dialog = await openDialog();

    await paste(dialog, DOCUMENT);
    await userEvent.click(dialog.getByRole('button', { name: '替换配置' }));

    await waitFor(() => expect(calls.saved).toHaveLength(1));
    expect(calls.checked).toHaveLength(1);
    // The document as parsed, under the key the endpoint takes.
    expect(calls.saved[0]).toMatchObject({ config: { listen: { port: 7788 } } });
  });

  // The whole point of checking first: a document the gateway would
  // reject must not reach the file, and the reasons have to be per field.
  it('does not write a document the gateway rejects', async () => {
    const calls = serving({
      valid: false,
      fields: [{ field: 'listen.port', message: 'is 99999, want at most 65535' }],
    });
    const dialog = await openDialog();

    await paste(dialog, DOCUMENT);
    await userEvent.click(dialog.getByRole('button', { name: '替换配置' }));

    expect(await screen.findByText('listen.port')).toBeInTheDocument();
    expect(screen.getByText(/want at most 65535/)).toBeInTheDocument();
    expect(calls.saved).toHaveLength(0);
  });

  it('reads a chosen file', async () => {
    const calls = serving();
    const dialog = await openDialog();

    const file = new File([DOCUMENT], 'config.yaml', { type: 'text/yaml' });
    await userEvent.upload(dialog.getByLabelText('选择配置文件'), file);

    // The file's contents land in the editor, so they can be read and
    // edited before anything is replaced.
    await waitFor(() =>
      expect(dialog.getByRole('textbox', { name: '配置文档' })).toHaveValue(DOCUMENT),
    );

    await userEvent.click(dialog.getByRole('button', { name: '替换配置' }));
    await waitFor(() => expect(calls.saved).toHaveLength(1));
  });

  // The export hands out placeholders in place of credentials, and what
  // happens to them on the way back in is not guessable.
  it('explains what happens to the redacted secrets', async () => {
    serving();
    const dialog = await openDialog();

    expect(dialog.getByText(/占位符/)).toBeInTheDocument();
  });
});
