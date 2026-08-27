import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Input } from 'antd';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import { IconButton } from '@/components/IconButton';
import { cx } from '@/lib/cx';
import styles from './KeyValueEditor.module.css';

/*
 * Editors for the two list-shaped kinds of field on a server: plain
 * lists of strings (arguments, ready patterns, exposed tools) and maps
 * of string to string (environment, headers, tags).
 *
 * Both own their rows rather than deriving them from the value on every
 * render, because the value has no place to put a half-typed row. A map
 * rebuilt from the value would lose a row the moment its key was
 * cleared to be retyped, and the field under the cursor would vanish.
 * Each row therefore carries an id of its own that survives editing.
 *
 * The consequence is a contract worth stating: the value is read once,
 * at mount, and changes to it afterwards are ignored. To point one of
 * these at different data, remount it — which is what a `key` is for,
 * and what the drawer holding them already does.
 */

let nextId = 0;
const newId = () => (nextId += 1);

interface Row {
  id: number;
  key: string;
  value: string;
}

function rowsFromMap(map: Record<string, string> | undefined): Row[] {
  return Object.entries(map ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => ({ id: newId(), key, value }));
}

function mapFromRows(rows: Row[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    // A row with no key yet is one being typed, not an entry.
    if (key) out[key] = row.value;
  }
  return out;
}

export function KeyValueEditor({
  value,
  onChange,
  keyPlaceholder,
  valuePlaceholder,
  /** Values worth hiding on screen — an environment holding a token,
   *  a header holding a key. */
  secret = false,
}: {
  // Optional because antd's Form.Item supplies both by cloning this
  // element. Stating them as required would mean writing a pair of
  // placeholders at every use site that antd then discards.
  value?: Record<string, string> | undefined;
  onChange?: ((next: Record<string, string>) => void) | undefined;
  keyPlaceholder?: string | undefined;
  valuePlaceholder?: string | undefined;
  secret?: boolean | undefined;
}) {
  const { t } = useTranslation();
  const [rows, setRows] = useState<Row[]>(() => rowsFromMap(value));

  const apply = (next: Row[]) => {
    setRows(next);
    onChange?.(mapFromRows(next));
  };

  return (
    <div className={styles.editor}>
      {rows.map((row) => (
        <div key={row.id} className={styles.row}>
          <Input
            value={row.key}
            placeholder={keyPlaceholder ?? t('form.key')}
            onChange={(e) =>
              apply(rows.map((r) => (r.id === row.id ? { ...r, key: e.target.value } : r)))
            }
            className={cx(styles.key, 'mono')}
          />
          {secret ? (
            <Input.Password
              value={row.value}
              placeholder={valuePlaceholder ?? t('form.value')}
              visibilityToggle
              onChange={(e) =>
                apply(rows.map((r) => (r.id === row.id ? { ...r, value: e.target.value } : r)))
              }
              className="mono"
            />
          ) : (
            <Input
              value={row.value}
              placeholder={valuePlaceholder ?? t('form.value')}
              onChange={(e) =>
                apply(rows.map((r) => (r.id === row.id ? { ...r, value: e.target.value } : r)))
              }
              className="mono"
            />
          )}
          <IconButton
            label={t('form.remove')}
            icon={<DeleteOutlined />}
            onClick={() => apply(rows.filter((r) => r.id !== row.id))}
          />
        </div>
      ))}

      <Button
        type="dashed"
        icon={<PlusOutlined aria-hidden />}
        onClick={() => apply([...rows, { id: newId(), key: '', value: '' }])}
        className={cx(styles.add)}
      >
        {t('form.addItem')}
      </Button>
    </div>
  );
}

interface ListRow {
  id: number;
  value: string;
}

export function StringListEditor({
  value,
  onChange,
  placeholder,
}: {
  value?: string[] | undefined;
  onChange?: ((next: string[]) => void) | undefined;
  placeholder?: string | undefined;
}) {
  const { t } = useTranslation();
  const [rows, setRows] = useState<ListRow[]>(() =>
    (value ?? []).map((v) => ({ id: newId(), value: v })),
  );

  const apply = (next: ListRow[]) => {
    setRows(next);
    // An empty row is one being typed. Sending it would put an empty
    // argument on a command line, which is not the same as no argument.
    onChange?.(next.map((r) => r.value).filter((v) => v !== ''));
  };

  return (
    <div className={styles.editor}>
      {rows.map((row, index) => (
        <div key={row.id} className={styles.listRow}>
          <span className={styles.index}>{index}</span>
          <Input
            value={row.value}
            placeholder={placeholder ?? ''}
            onChange={(e) =>
              apply(rows.map((r) => (r.id === row.id ? { ...r, value: e.target.value } : r)))
            }
            className="mono"
          />
          <IconButton
            label={t('form.remove')}
            icon={<DeleteOutlined />}
            onClick={() => apply(rows.filter((r) => r.id !== row.id))}
          />
        </div>
      ))}

      <Button
        type="dashed"
        icon={<PlusOutlined aria-hidden />}
        onClick={() => apply([...rows, { id: newId(), value: '' }])}
        className={cx(styles.add)}
      >
        {t('form.addItem')}
      </Button>
    </div>
  );
}
