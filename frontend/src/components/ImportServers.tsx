import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Modal } from 'antd';
import { endpoints } from '@/api/endpoints';
import { ApiError } from '@/api/client';
import { keys } from '@/api/query';
import type { ImportResult } from '@/api/types';
import { cx } from '@/lib/cx';
import styles from './ImportServers.module.css';

/**
 * Pasting another client's configuration.
 *
 * Almost nobody starts here with nothing: they already have a working
 * mcpServers block in Claude Desktop, Cursor or VS Code, and the
 * alternative to pasting it is retyping every server through the form.
 *
 * The document is sent exactly as pasted. Which fields need translating —
 * "type" against "transport", a stdio server recognised by having a
 * command — is the gateway's business, not this dialog's, so that the
 * same file works through `curl` as through here.
 */

const EXAMPLE = `{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    },
    "remote": {
      "type": "http",
      "url": "https://example.com/mcp"
    }
  }
}`;

export function ImportServers({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation();
  const queries = useQueryClient();

  const [text, setText] = useState('');
  const [results, setResults] = useState<ImportResult[] | null>(null);

  const parsed = (() => {
    if (text.trim() === '') return { ok: false, error: null };
    try {
      JSON.parse(text);
      return { ok: true, error: null };
    } catch (error) {
      return { ok: false, error: (error as Error).message };
    }
  })();

  const submit = useMutation({
    mutationFn: () => endpoints.importServers(text),
    onSettled: () => {
      // The list changes whether or not every entry succeeded, so it is
      // refreshed either way.
      void queries.invalidateQueries({ queryKey: keys.servers.all });
    },
    onSuccess: (answer) => setResults(answer.results),
    onError: (error) => {
      // A document from which nothing could be imported is a failed
      // request, and the envelope still carries one field error per
      // entry. Dropping those would leave only the summary sentence on
      // screen, and the reasons are the useful part.
      if (error instanceof ApiError && error.fields.length > 0) {
        setResults(error.fields.map((field) => ({ name: field.field, error: field.message })));
      }
    },
  });

  const close = () => {
    setText('');
    setResults(null);
    submit.reset();
    onClose();
  };

  const imported = results?.filter((result) => !result.error) ?? [];
  const failed = results?.filter((result) => result.error) ?? [];

  const failure = submit.error instanceof ApiError ? submit.error : null;
  const unexplained = failure !== null && failed.length === 0;

  return (
    <Modal
      open={open}
      onCancel={close}
      width={640}
      title={t('import.title')}
      destroyOnHidden
      footer={
        <div className={styles.footer}>
          <Button onClick={close}>{t('common.close')}</Button>
          <Button
            type="primary"
            loading={submit.isPending}
            disabled={!parsed.ok}
            onClick={() => submit.mutate()}
          >
            {t('import.run')}
          </Button>
        </div>
      }
    >
      <div className={styles.body}>
        <p className={styles.hint}>{t('import.hint')}</p>

        <textarea
          className={styles.editor}
          value={text}
          spellCheck={false}
          placeholder={EXAMPLE}
          onChange={(e) => {
            setText(e.target.value);
            setResults(null);
          }}
          rows={12}
        />
        {parsed.error ? (
          <span className={styles.invalid}>
            {t('call.invalidJson')} — {parsed.error}
          </span>
        ) : null}

        {unexplained ? <Alert type="error" showIcon message={failure.message} /> : null}

        {results !== null ? (
          <div className={styles.results}>
            {/* Both halves are shown. A dialog that reported only the
                failures would leave someone wondering what did land, and
                one that reported only a count would not say which. */}
            {imported.length > 0 ? (
              <Alert
                type="success"
                showIcon
                message={t('import.imported', { count: imported.length })}
                description={
                  <span className={styles.names}>
                    {imported.map((result) => (
                      <span key={result.name} className={styles.name}>
                        {result.name}
                      </span>
                    ))}
                  </span>
                }
              />
            ) : null}

            {failed.length > 0 ? (
              <Alert
                type="warning"
                showIcon
                message={t('import.failed', { count: failed.length })}
                description={
                  <ul className={cx(styles.failures)}>
                    {failed.map((result) => (
                      <li key={result.name}>
                        <span className={styles.name}>{result.name}</span>
                        <span className={styles.reason}>{result.error}</span>
                      </li>
                    ))}
                  </ul>
                }
              />
            ) : null}
          </div>
        ) : null}
      </div>
    </Modal>
  );
}
