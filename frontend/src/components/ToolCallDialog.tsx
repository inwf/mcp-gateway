import { useMemo, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Modal } from 'antd';
import { endpoints } from '@/api/endpoints';
import { ApiError } from '@/api/client';
import type { ContentBlock, Tool, ToolCallResult } from '@/api/types';
import { cx } from '@/lib/cx';
import styles from './ToolCallDialog.module.css';

/**
 * Calling a tool by hand.
 *
 * The arguments are edited as JSON rather than as a generated form. A
 * form built from an arbitrary JSON Schema is a large amount of code
 * that still cannot express what upstream schemas actually contain —
 * unions, conditionals, free-form objects — and the person reaching for
 * this dialog is debugging a tool, which means they want to send exactly
 * what they typed.
 *
 * What the schema is used for is a starting point: a skeleton with the
 * required properties present, so the common case is filling in values
 * rather than remembering names.
 */

interface Schema {
  type?: string;
  properties?: Record<string, Schema>;
  required?: string[];
  default?: unknown;
  enum?: unknown[];
  items?: Schema;
}

/** A plausible empty value for a property, by type. */
function placeholder(schema: Schema): unknown {
  if (schema.default !== undefined) return schema.default;
  if (schema.enum?.length) return schema.enum[0];

  switch (schema.type) {
    case 'string':
      return '';
    case 'number':
    case 'integer':
      return 0;
    case 'boolean':
      return false;
    case 'array':
      return [];
    case 'object':
      return {};
    default:
      return null;
  }
}

/** Builds a skeleton holding the required properties. Only the required
 *  ones: a skeleton with every optional field in it would have to be
 *  pruned by hand before sending, which is more work than typing the
 *  two that are wanted. */
function skeletonFor(inputSchema: unknown): string {
  const schema = inputSchema as Schema | undefined;
  if (!schema?.properties) return '{}';

  const required = new Set(schema.required ?? []);
  const out: Record<string, unknown> = {};
  for (const [name, property] of Object.entries(schema.properties)) {
    if (required.has(name)) out[name] = placeholder(property);
  }
  return JSON.stringify(out, null, 2);
}

function Block({ block }: { block: ContentBlock }) {
  if (block.type === 'text' && typeof block.text === 'string') {
    return <pre className={styles.text}>{block.text}</pre>;
  }
  if (block.type === 'image' && typeof block.data === 'string') {
    return (
      <img
        className={styles.image}
        src={`data:${block.mimeType ?? 'image/png'};base64,${block.data}`}
        alt=""
      />
    );
  }
  // Anything else is shown as it arrived. A tool is free to return a
  // content type this UI has never heard of, and hiding it would be
  // worse than printing it.
  return <pre className={styles.text}>{JSON.stringify(block, null, 2)}</pre>;
}

export function ToolCallDialog({
  open,
  onClose,
  server,
  tool,
}: {
  open: boolean;
  onClose: () => void;
  server: string;
  tool: Tool | null;
}) {
  const { t } = useTranslation();

  const skeleton = useMemo(() => (tool ? skeletonFor(tool.inputSchema) : '{}'), [tool]);
  const [text, setText] = useState(skeleton);
  const [seed, setSeed] = useState(skeleton);
  const [result, setResult] = useState<ToolCallResult | null>(null);

  // Re-seed when the dialog is pointed at a different tool. Adjusted
  // during render rather than in an effect: an effect runs after the
  // first paint, so the textarea would show the previous tool's
  // arguments for a frame.
  if (seed !== skeleton) {
    setSeed(skeleton);
    setText(skeleton);
    setResult(null);
  }

  const parsed = useMemo(() => {
    try {
      return { value: JSON.parse(text) as unknown, error: null };
    } catch (error) {
      return { value: null, error: (error as Error).message };
    }
  }, [text]);

  const call = useMutation({
    mutationFn: () => endpoints.callTool(server, tool?.name ?? '', parsed.value),
    onSuccess: setResult,
  });

  const failure = call.error instanceof ApiError ? call.error : null;

  return (
    <Modal
      open={open}
      onCancel={onClose}
      width={720}
      title={<span className="mono">{t('call.title', { tool: tool?.name ?? '' })}</span>}
      destroyOnHidden
      footer={
        <div className={styles.footer}>
          <Button onClick={onClose}>{t('common.close')}</Button>
          <Button
            type="primary"
            loading={call.isPending}
            disabled={parsed.error !== null}
            onClick={() => call.mutate()}
          >
            {call.isPending ? t('call.running') : t('call.run')}
          </Button>
        </div>
      }
    >
      <div className={styles.body}>
        {tool?.description ? <p className={styles.description}>{tool.description}</p> : null}

        <div>
          <span className="label">{t('call.arguments')}</span>
          <textarea
            className={styles.editor}
            value={text}
            spellCheck={false}
            onChange={(e) => setText(e.target.value)}
            rows={8}
          />
          {parsed.error ? (
            <span className={styles.invalid}>
              {t('call.invalidJson')} — {parsed.error}
            </span>
          ) : null}
        </div>

        {failure ? <Alert type="error" showIcon message={failure.message} /> : null}

        {result ? (
          <div>
            <span className="label">{t('call.result')}</span>
            {/* A tool that ran and reported a problem is a successful
                call with a failed result. Both have to be visible, and
                differently: the first is the gateway working. */}
            {result.isError ? (
              <Alert
                type="warning"
                showIcon
                message={t('call.failed')}
                className={cx(styles.verdict)}
              />
            ) : null}
            <div className={styles.result}>
              {result.content.length === 0 && result.structuredContent === undefined ? (
                <p className={styles.description}>{t('call.empty')}</p>
              ) : (
                <>
                  {result.content.map((block, index) => (
                    <Block key={index} block={block} />
                  ))}
                  {result.structuredContent !== undefined ? (
                    <pre className={styles.text}>
                      {JSON.stringify(result.structuredContent, null, 2)}
                    </pre>
                  ) : null}
                </>
              )}
            </div>
          </div>
        ) : null}
      </div>
    </Modal>
  );
}
