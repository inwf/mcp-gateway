import { useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, App, Button, Modal } from 'antd';
import { UploadOutlined } from '@ant-design/icons';
import { parse } from 'yaml';
import { endpoints } from '@/api/endpoints';
import { ApiError } from '@/api/client';
import type { Config, FieldError } from '@/api/types';
import styles from './ImportConfig.module.css';

/**
 * Loading a whole configuration back in.
 *
 * The pair to the export. A file that can be downloaded and not uploaded
 * is a backup nobody can restore, and the only way back in was to open the
 * file, select all of it, and paste it into the YAML tab.
 *
 * Two things make this different from importing servers. It replaces
 * everything rather than adding to it — every server not in the document
 * is gone — so it says so, and the button that does it is labelled with
 * what it does. And it is checked before it is written: the gateway
 * validates without saving, so a document it would reject never reaches
 * the file and the reasons arrive per field rather than as one sentence.
 */

/** YAML and JSON both, because the export writes YAML and every other
 *  client's configuration is JSON. The YAML parser reads both — JSON is a
 *  subset — so the distinction never has to be made. */
const ACCEPT = '.yaml,.yml,.json,application/json,text/yaml';

export function ImportConfig() {
  const { t } = useTranslation();
  const { message } = App.useApp();
  const queries = useQueryClient();

  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const [fields, setFields] = useState<FieldError[]>([]);
  const [failure, setFailure] = useState<string | null>(null);
  const file = useRef<HTMLInputElement>(null);

  const parsed = (() => {
    if (text.trim() === '') return { value: null, error: null };
    try {
      const value = parse(text) as unknown;
      if (value === null || typeof value !== 'object' || Array.isArray(value)) {
        return { value: null, error: t('importConfig.notADocument') };
      }
      return { value: value as Config, error: null };
    } catch (error) {
      return { value: null, error: (error as Error).message };
    }
  })();

  function reset() {
    setText('');
    setFields([]);
    setFailure(null);
  }

  const save = useMutation({
    mutationFn: (candidate: Config) => endpoints.putConfig(candidate),
    onSuccess: (result) => {
      void message.success(t('importConfig.done', { count: result.changes.length }));
      // A configuration change can touch anything on any page.
      void queries.invalidateQueries();
      reset();
      setOpen(false);
    },
    onError: (error) => report(error),
  });

  // Checked before it is written, so a document the gateway would refuse
  // never reaches the file. The field errors are the useful part: "the
  // configuration is not valid" does not say which line to fix.
  const check = useMutation({
    mutationFn: (candidate: Config) => endpoints.validateConfig(candidate),
    onSuccess: (result, candidate) => {
      setFields(result.fields);
      setFailure(result.valid ? null : t('importConfig.rejected'));
      if (result.valid) save.mutate(candidate);
    },
    onError: (error) => report(error),
  });

  function report(error: unknown) {
    const failed = error instanceof ApiError ? error : null;
    setFields(failed?.fields ?? []);
    setFailure(failed?.message ?? t('error.unknown'));
  }

  async function load(chosen: File | undefined) {
    if (!chosen) return;
    setFields([]);
    setFailure(null);
    setText(await chosen.text());
  }

  const busy = check.isPending || save.isPending;

  return (
    <>
      <Button icon={<UploadOutlined aria-hidden />} onClick={() => setOpen(true)}>
        {t('importConfig.open')}
      </Button>

      <Modal
        open={open}
        onCancel={() => {
          reset();
          setOpen(false);
        }}
        width={640}
        title={t('importConfig.title')}
        destroyOnHidden
        footer={
          <div className={styles.footer}>
            <Button
              onClick={() => {
                reset();
                setOpen(false);
              }}
            >
              {t('common.close')}
            </Button>
            <Button
              type="primary"
              danger
              loading={busy}
              disabled={parsed.value === null}
              onClick={() => parsed.value && check.mutate(parsed.value)}
            >
              {t('importConfig.replace')}
            </Button>
          </div>
        }
      >
        <div className={styles.body}>
          {/* Said before the file is chosen rather than in a confirmation
              afterwards: the point of this dialog is a decision, and the
              decision needs the fact. */}
          <Alert type="warning" showIcon message={t('importConfig.warning')} />

          <div className={styles.pick}>
            <input
              ref={file}
              type="file"
              accept={ACCEPT}
              className={styles.file}
              aria-label={t('importConfig.choose')}
              onChange={(e) => void load(e.target.files?.[0])}
            />
          </div>

          <p className={styles.hint}>{t('importConfig.secretsHint')}</p>

          <textarea
            className={styles.editor}
            value={text}
            spellCheck={false}
            placeholder={t('importConfig.placeholder')}
            aria-label={t('importConfig.document')}
            rows={12}
            onChange={(e) => {
              setText(e.target.value);
              setFields([]);
              setFailure(null);
            }}
          />

          {parsed.error ? <span className={styles.invalid}>{parsed.error}</span> : null}

          {failure ? <Alert type="error" showIcon message={failure} /> : null}

          {fields.length > 0 ? (
            <ul className={styles.fields}>
              {fields.map((field) => (
                <li key={field.field}>
                  <span className={styles.field}>{field.field}</span>
                  <span className={styles.reason}>{field.message}</span>
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      </Modal>
    </>
  );
}
