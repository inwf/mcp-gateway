import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { App, Alert, Button, Drawer, Form, Input, Segmented, Switch } from 'antd';
import { TRANSPORTS, type FieldError, type MCPServer, type Transport } from '@/api/types';
import { ApiError } from '@/api/client';
import { useServerActions } from '@/hooks/use-server-actions';
import { KeyValueEditor, StringListEditor } from '@/components/KeyValueEditor';

/*
 * Adding and editing a server.
 *
 * The transports differ in what they need, and showing every field for
 * every transport would present a stdio server with a URL box it must
 * leave empty — which the gateway would then reject. So the form shows
 * what applies:
 *
 *   stdio                  a command, its arguments and its environment
 *   streamable-http        a URL, its headers and an optional proxy
 *   streamable-http-local  both, plus the patterns that say the child
 *                          process has finished starting
 *
 * Switching transport keeps whatever was typed. Someone converting a
 * local server to a remote one should not have to retype its name,
 * description, tags and timeout; and the fields that no longer apply
 * are dropped on submission rather than on switch, so switching back
 * restores them.
 */

const NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;

export interface ServerFormValues {
  name: string;
  transport: Transport;
  enabled: boolean;
  description: string;
  timeout: string;
  command: string;
  args: string[];
  env: Record<string, string>;
  url: string;
  headers: Record<string, string>;
  proxy: string;
  readyPatterns: string[];
  exposedTools: string[];
  tags: Record<string, string>;
}

function emptyValues(): ServerFormValues {
  return {
    name: '',
    transport: 'stdio',
    enabled: true,
    description: '',
    timeout: '1m0s',
    command: '',
    args: [],
    env: {},
    url: '',
    headers: {},
    proxy: '',
    readyPatterns: [],
    exposedTools: [],
    tags: {},
  };
}

function valuesFrom(name: string, server: MCPServer): ServerFormValues {
  return {
    name,
    transport: server.transport,
    enabled: server.enabled,
    description: server.description ?? '',
    timeout: server.timeout,
    command: server.command ?? '',
    args: server.args ?? [],
    env: server.env ?? {},
    url: server.url ?? '',
    headers: server.headers ?? {},
    proxy: server.proxy ?? '',
    readyPatterns: server.readyPatterns ?? [],
    exposedTools: server.exposedTools ?? [],
    tags: server.tags ?? {},
  };
}

/** Builds what the API takes, dropping everything the chosen transport
 *  does not use and everything left empty. */
function serverFrom(values: ServerFormValues): MCPServer {
  const spawns = values.transport !== 'streamable-http';
  const overHTTP = values.transport !== 'stdio';

  const server: MCPServer = {
    transport: values.transport,
    enabled: values.enabled,
    timeout: values.timeout.trim(),
  };

  if (values.description.trim()) server.description = values.description.trim();
  if (Object.keys(values.tags).length) server.tags = values.tags;
  if (values.exposedTools.length) server.exposedTools = values.exposedTools;

  if (spawns) {
    if (values.command.trim()) server.command = values.command.trim();
    if (values.args.length) server.args = values.args;
    if (Object.keys(values.env).length) server.env = values.env;
  }
  if (overHTTP) {
    if (values.url.trim()) server.url = values.url.trim();
    if (Object.keys(values.headers).length) server.headers = values.headers;
    if (values.proxy.trim()) server.proxy = values.proxy.trim();
  }
  if (values.transport === 'streamable-http-local' && values.readyPatterns.length) {
    server.readyPatterns = values.readyPatterns;
  }

  return server;
}

/** The form's own field names, for recognising which of the gateway's
 *  field paths correspond to an input on screen. */
const FIELD_NAMES = [
  'name',
  'transport',
  'enabled',
  'description',
  'timeout',
  'command',
  'args',
  'env',
  'url',
  'headers',
  'proxy',
  'readyPatterns',
  'exposedTools',
  'tags',
] as const satisfies ReadonlyArray<keyof ServerFormValues>;

type FieldName = (typeof FIELD_NAMES)[number];

function isFieldName(value: string): value is FieldName {
  return (FIELD_NAMES as ReadonlyArray<string>).includes(value);
}

/**
 * Turns the gateway's field paths into the form's field names, so that a
 * rejected save marks the input that caused it rather than only printing
 * a sentence at the top.
 *
 * A path that does not correspond to an input is dropped rather than
 * guessed at: the gateway validates the whole configuration, so it can
 * legitimately report a problem that has no field on this form. Those
 * still reach the user through the message at the top.
 */
function markFields(
  fields: FieldError[],
  name: string,
): Array<{ name: FieldName; errors: string[] }> {
  const prefix = `mcpServers.${name}.`;
  const marked: Array<{ name: FieldName; errors: string[] }> = [];

  for (const field of fields) {
    const path = field.field.startsWith(prefix)
      ? field.field.slice(prefix.length)
      : field.field;
    if (isFieldName(path)) marked.push({ name: path, errors: [field.message] });
  }
  return marked;
}

/**
 * Adding and editing a server.
 *
 * The drawer is the outer shell; everything with state lives in the body
 * below and is keyed on what is being edited. That is what resets the
 * form between uses — rather than an effect that pushes the new values
 * into a form that still holds the old ones, which has to run after the
 * first render and so shows the previous server for a frame.
 */
export function ServerForm({
  open,
  onClose,
  /** Absent when adding. */
  editing,
}: {
  open: boolean;
  onClose: () => void;
  editing?: { name: string; server: MCPServer } | undefined;
}) {
  const { t } = useTranslation();

  return (
    <Drawer
      open={open}
      onClose={onClose}
      size={620}
      title={editing ? `${t('servers.edit')} · ${editing.name}` : t('servers.add')}
      destroyOnHidden
    >
      <FormBody key={editing?.name ?? '#new'} onClose={onClose} editing={editing} />
    </Drawer>
  );
}

function FormBody({
  onClose,
  editing,
}: {
  onClose: () => void;
  editing?: { name: string; server: MCPServer } | undefined;
}) {
  const { t } = useTranslation();
  const { message } = App.useApp();
  const { create, update } = useServerActions();

  const initial = editing ? valuesFrom(editing.name, editing.server) : emptyValues();

  const [form] = Form.useForm<ServerFormValues>();
  const [transport, setTransport] = useState<Transport>(initial.transport);
  const [failure, setFailure] = useState<string | null>(null);

  const spawns = transport !== 'streamable-http';
  const overHTTP = transport !== 'stdio';
  const isLocal = transport === 'streamable-http-local';

  const submit = async () => {
    // A form that does not validate is an expected outcome, not a
    // failure: antd has already marked the offending inputs, and
    // letting the rejection escape would put an unhandled rejection in
    // the console for someone leaving a required box empty.
    let values: ServerFormValues;
    try {
      values = await form.validateFields();
    } catch {
      return;
    }

    const server = serverFrom(values);
    setFailure(null);

    try {
      if (editing) {
        await update.mutateAsync({ name: editing.name, server });
      } else {
        await create.mutateAsync({ name: values.name.trim(), server });
      }
      void message.success(t('settings.saved'));
      onClose();
    } catch (error) {
      const failed = error instanceof ApiError ? error : null;
      if (failed?.fields.length) {
        form.setFields(markFields(failed.fields, editing?.name ?? values.name.trim()));
      }
      setFailure(failed?.message ?? t('error.unknown'));
    }
  };

  const saving = create.isPending || update.isPending;

  return (
    <>
      {failure ? (
        <Alert
          type="error"
          showIcon
          message={failure}
          style={{ marginBottom: 'var(--space-4)' }}
        />
      ) : null}

      <Form form={form} layout="vertical" initialValues={initial} requiredMark={false}>
        <Form.Item
          name="name"
          label={t('form.name')}
          extra={t('form.nameHint')}
          rules={[
            { required: true, message: t('form.required') },
            {
              pattern: NAME_PATTERN,
              // The gateway enforces this too; checking here is what
              // stops a round trip to be told about a typo.
              message: t('form.nameHint'),
            },
          ]}
        >
          {/* A rename would be a different server as far as the gateway
              is concerned, so editing addresses the existing one. */}
          <Input className="mono" disabled={Boolean(editing)} autoFocus={!editing} />
        </Form.Item>

        <Form.Item name="transport" label={t('form.transport')}>
          <Segmented
            options={TRANSPORTS.map((value) => ({ label: value, value }))}
            onChange={(value) => setTransport(value as Transport)}
            block
          />
        </Form.Item>

        {spawns ? (
          <>
            <Form.Item
              name="command"
              label={t('form.command')}
              rules={[{ required: true, message: t('form.required') }]}
            >
              <Input className="mono" placeholder="npx" />
            </Form.Item>

            <Form.Item name="args" label={t('form.args')}>
              <StringListEditor placeholder="-y" />
            </Form.Item>

            <Form.Item name="env" label={t('form.env')}>
              <KeyValueEditor secret keyPlaceholder="API_TOKEN" />
            </Form.Item>
          </>
        ) : null}

        {overHTTP ? (
          <>
            <Form.Item
              name="url"
              label={t('form.url')}
              rules={[{ required: true, message: t('form.required') }]}
            >
              <Input className="mono" placeholder="https://example.com/mcp" />
            </Form.Item>

            <Form.Item name="headers" label={t('form.headers')}>
              <KeyValueEditor secret keyPlaceholder="Authorization" />
            </Form.Item>

            <Form.Item name="proxy" label={t('form.proxy')}>
              <Input className="mono" placeholder="http://127.0.0.1:7890" />
            </Form.Item>
          </>
        ) : null}

        {isLocal ? (
          <Form.Item
            name="readyPatterns"
            label={t('form.readyPatterns')}
            extra={t('form.readyPatternsHint')}
          >
            <StringListEditor placeholder="listening on" />
          </Form.Item>
        ) : null}

        <Form.Item
          name="timeout"
          label={t('form.timeout')}
          extra={t('form.timeoutHint')}
          rules={[{ required: true, message: t('form.required') }]}
        >
          <Input className="mono" placeholder="1m0s" style={{ maxWidth: 180 }} />
        </Form.Item>

        <Form.Item
          name="description"
          label={t('form.description')}
          extra={t('form.descriptionHint')}
        >
          <Input.TextArea rows={2} />
        </Form.Item>

        <Form.Item name="tags" label={t('form.tags')}>
          <KeyValueEditor keyPlaceholder="env" valuePlaceholder="prod" />
        </Form.Item>

        <Form.Item
          name="exposedTools"
          label={t('form.exposedTools')}
          extra={t('form.exposedToolsHint')}
        >
          <StringListEditor />
        </Form.Item>

        <Form.Item name="enabled" label={t('form.enabled')} valuePropName="checked">
          <Switch />
        </Form.Item>
      </Form>

      <div
        style={{
          display: 'flex',
          gap: 'var(--space-2)',
          justifyContent: 'flex-end',
          paddingTop: 'var(--space-4)',
          borderTop: '1px solid var(--line)',
        }}
      >
        <Button onClick={onClose}>{t('form.cancel')}</Button>
        <Button type="primary" loading={saving} onClick={() => void submit()}>
          {editing ? t('form.save') : t('form.create')}
        </Button>
      </div>
    </>
  );
}
