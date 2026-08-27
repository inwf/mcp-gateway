import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Button, Input, Popconfirm, Skeleton, Table, Tooltip } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  DeleteOutlined,
  EditOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  StopOutlined,
} from '@ant-design/icons';
import { endpoints } from '@/api/endpoints';
import { keys } from '@/api/query';
import type { ServerView } from '@/api/types';
import { useServerActions } from '@/hooks/use-server-actions';
import { IconButton } from '@/components/IconButton';
import { Panel } from '@/components/Panel';
import { StateBadge } from '@/components/StateBadge';
import { ErrorNotice } from '@/components/ErrorNotice';
import { Nothing } from '@/components/Nothing';
import { ServerForm } from '@/components/ServerForm';
import { ellipsize, endpointOf, tagPairs } from '@/lib/format';
import { cx } from '@/lib/cx';
import styles from './Servers.module.css';

/** Matches a server against what was typed. Name, description and
 *  endpoint are all searched, because all three are things someone
 *  might remember a server by. */
function matches(server: ServerView, needle: string): boolean {
  if (!needle) return true;
  const term = needle.toLowerCase();
  return (
    server.name.toLowerCase().includes(term) ||
    (server.config.description ?? '').toLowerCase().includes(term) ||
    endpointOf(server.config).toLowerCase().includes(term)
  );
}

export default function Servers() {
  const { t } = useTranslation();
  const { connect, disconnect, remove } = useServerActions();

  const [search, setSearch] = useState('');
  const [activeTags, setActiveTags] = useState<string[]>([]);
  const [editing, setEditing] = useState<ServerView | null>(null);
  const [adding, setAdding] = useState(false);

  const servers = useQuery({ queryKey: keys.servers.list(), queryFn: endpoints.listServers });

  const toggleTag = (pair: string) =>
    setActiveTags((current) =>
      current.includes(pair) ? current.filter((p) => p !== pair) : [...current, pair],
    );

  const shown = useMemo(() => {
    const all = servers.data ?? [];
    return all.filter(
      (server) =>
        matches(server, search) &&
        // Every selected tag has to be present: narrowing is what a
        // filter is for, and matching any would widen as tags are added.
        activeTags.every((pair) =>
          tagPairs(server.config.tags).some(([key, value]) => `${key}=${value}` === pair),
        ),
    );
  }, [servers.data, search, activeTags]);

  const columns: ColumnsType<ServerView> = [
    {
      title: t('servers.name'),
      key: 'name',
      render: (_, server) => (
        <span className={styles.name}>
          <Link
            to={`/servers/${encodeURIComponent(server.name)}/overview`}
            className={styles.nameLink}
          >
            {server.name}
          </Link>
          {server.config.description ? (
            <span className={styles.description}>{server.config.description}</span>
          ) : null}
        </span>
      ),
    },
    {
      title: t('servers.status'),
      key: 'status',
      width: 130,
      render: (_, server) => (
        <StateBadge
          state={server.status.state}
          error={server.status.error}
          enabled={server.config.enabled}
        />
      ),
    },
    {
      title: t('servers.transport'),
      key: 'transport',
      width: 190,
      render: (_, server) => (
        <span className={styles.name}>
          <span className={styles.transport}>{server.config.transport}</span>
          <Tooltip title={endpointOf(server.config)}>
            <span className={styles.endpoint}>{ellipsize(endpointOf(server.config), 34)}</span>
          </Tooltip>
        </span>
      ),
    },
    {
      title: `${t('servers.tools')} / ${t('servers.resources')}`,
      key: 'counts',
      width: 110,
      render: (_, server) => (
        <span className={styles.counts}>
          <span className={cx(server.status.toolCount === 0 && styles.countZero)}>
            {server.status.toolCount}
          </span>
          <span className={cx(server.status.resourceCount === 0 && styles.countZero)}>
            {server.status.resourceCount}
          </span>
        </span>
      ),
    },
    {
      title: t('servers.tags'),
      key: 'tags',
      render: (_, server) => {
        const pairs = tagPairs(server.config.tags);
        if (pairs.length === 0) return <span className={styles.endpoint}>—</span>;
        return (
          <span className={styles.tags}>
            {pairs.map(([key, value]) => {
              const pair = `${key}=${value}`;
              return (
                <button
                  key={pair}
                  type="button"
                  className={cx(styles.tag, activeTags.includes(pair) && styles.tagOn)}
                  onClick={() => toggleTag(pair)}
                >
                  {key}={value}
                </button>
              );
            })}
          </span>
        );
      },
    },
    {
      title: t('servers.actions'),
      key: 'actions',
      width: 150,
      align: 'right',
      render: (_, server) => {
        const live = server.status.state === 'connected';
        const busy =
          (connect.isPending && connect.variables === server.name) ||
          (disconnect.isPending && disconnect.variables === server.name);

        return (
          <span className={styles.actions}>
            {live ? (
              <>
                <IconButton
                  label={t('servers.reconnect')}
                  icon={<ReloadOutlined />}
                  loading={busy}
                  onClick={() => connect.mutate(server.name)}
                />
                <IconButton
                  label={t('servers.disconnect')}
                  icon={<StopOutlined />}
                  loading={busy}
                  onClick={() => disconnect.mutate(server.name)}
                />
              </>
            ) : (
              <IconButton
                label={t('servers.connect')}
                icon={<PlayCircleOutlined />}
                loading={busy}
                onClick={() => connect.mutate(server.name)}
              />
            )}

            <IconButton
              label={t('servers.edit')}
              icon={<EditOutlined />}
              onClick={() => setEditing(server)}
            />

            {/* Deleting stops a running process and drops the
                configuration, neither of which can be undone from
                here, so it asks. */}
            <Popconfirm
              title={t('servers.deleteConfirm', { name: server.name })}
              description={t('servers.deleteHint')}
              okText={t('common.confirm')}
              cancelText={t('common.cancel')}
              okButtonProps={{ danger: true }}
              onConfirm={() => remove.mutate(server.name)}
            >
              <IconButton label={t('servers.delete')} icon={<DeleteOutlined />} danger />
            </Popconfirm>
          </span>
        );
      },
    },
  ];

  const addButton = (
    <Button type="primary" icon={<PlusOutlined aria-hidden />} onClick={() => setAdding(true)}>
      {t('servers.add')}
    </Button>
  );

  return (
    <div className={styles.page}>
      <div className={styles.bar}>
        <Input.Search
          allowClear
          placeholder={t('servers.search')}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className={cx(styles.search)}
        />
        {activeTags.length > 0 ? (
          <Button type="text" onClick={() => setActiveTags([])}>
            {t('common.close')} ({activeTags.length})
          </Button>
        ) : null}
        <span className={styles.spacer} />
        {addButton}
      </div>

      <Panel title={t('nav.servers')} count={shown.length} flush>
        {servers.isPending ? (
          <div style={{ padding: 'var(--space-4)' }}>
            <Skeleton active paragraph={{ rows: 5 }} title={false} />
          </div>
        ) : servers.isError ? (
          <ErrorNotice error={servers.error} onRetry={() => void servers.refetch()} />
        ) : (servers.data ?? []).length === 0 ? (
          <Nothing
            title={t('servers.empty')}
            hint={t('servers.emptyHint')}
            action={addButton}
          />
        ) : shown.length === 0 ? (
          <Nothing title={t('tools.noMatch')} />
        ) : (
          <Table
            dataSource={shown}
            columns={columns}
            rowKey="name"
            pagination={false}
            size="middle"
          />
        )}
      </Panel>

      <ServerForm open={adding} onClose={() => setAdding(false)} />
      <ServerForm
        open={editing !== null}
        onClose={() => setEditing(null)}
        {...(editing ? { editing: { name: editing.name, server: editing.config } } : {})}
      />
    </div>
  );
}
