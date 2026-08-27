import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Skeleton, Tooltip } from 'antd';
import {
  ApiOutlined,
  ClusterOutlined,
  LinkOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { endpoints } from '@/api/endpoints';
import { keys } from '@/api/query';
import type { EventKind } from '@/api/stream';
import { useEventStore } from '@/stores/events';
import { Panel } from '@/components/Panel';
import { StatCard, type Note } from '@/components/StatCard';
import { StateBadge } from '@/components/StateBadge';
import { ErrorNotice } from '@/components/ErrorNotice';
import { Nothing } from '@/components/Nothing';
import { Reveal, SlideIn } from '@/components/Reveal';
import { clockTime, fullTime, uptime } from '@/lib/format';
import { cx } from '@/lib/cx';
import styles from './Overview.module.css';

/** Which events are worth colouring, and how. A failure and a routine
 *  status change should not look alike in a list that is scanned. */
const TONE: Partial<Record<EventKind, string | undefined>> = {
  'server.connected': styles.toneOk,
  'server.disconnected': styles.toneWarn,
  'server.failed': styles.toneDanger,
  'toolcall.failed': styles.toneDanger,
  'toolcall.completed': styles.toneOk,
  'config.updated': styles.toneInfo,
  'tools.changed': styles.toneInfo,
  'resources.changed': styles.toneInfo,
};

function Stats() {
  const { t } = useTranslation();

  const status = useQuery({
    queryKey: keys.gateway.status(),
    queryFn: endpoints.gatewayStatus,
  });

  if (status.isPending) {
    return (
      <div className={styles.stats}>
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} active paragraph={{ rows: 2 }} title={false} />
        ))}
      </div>
    );
  }
  if (status.isError) {
    return <ErrorNotice error={status.error} onRetry={() => void status.refetch()} />;
  }

  const { servers, tools, sessions, sessionMode } = status.data;

  // The failure count is the one figure here that should draw the eye
  // when it is not zero, and should say nothing at all when it is.
  const failureNote: { note: string; tone: Note } =
    servers.failed > 0
      ? { note: `${servers.failed} ${t('overview.failed')}`, tone: 'danger' }
      : { note: '', tone: 'plain' };

  return (
    <div className={styles.stats}>
      <Reveal index={0}>
        <StatCard
          label={t('overview.servers')}
          value={servers.configured}
          note={t('overview.configured')}
          Icon={ClusterOutlined}
        />
      </Reveal>
      <Reveal index={1}>
        <StatCard
          label={t('overview.connected')}
          value={servers.connected}
          note={failureNote.note}
          noteTone={failureNote.tone}
          Icon={LinkOutlined}
        />
      </Reveal>
      <Reveal index={2}>
        <StatCard label={t('overview.tools')} value={tools} Icon={ThunderboltOutlined} />
      </Reveal>
      <Reveal index={3}>
        <StatCard
          label={t('overview.sessions')}
          value={sessions}
          note={sessionMode}
          Icon={ApiOutlined}
        />
      </Reveal>
    </div>
  );
}

function Activity() {
  const { t } = useTranslation();
  const events = useEventStore((s) => s.events);

  return (
    <Panel
      title={t('overview.activity')}
      count={events.length || undefined}
      flush
      className={styles.feed}
    >
      {events.length === 0 ? (
        <Nothing title={t('overview.noActivity')} hint={t('overview.noActivityHint')} />
      ) : (
        events.slice(0, 40).map((event) => (
          // Keyed on the event's own id: several events from one
          // upstream change share a timestamp.
          <SlideIn key={event.id}>
            <div className={cx(styles.row, TONE[event.kind])}>
              <Tooltip title={fullTime(event.at)}>
                <span className={styles.at}>{clockTime(event.at)}</span>
              </Tooltip>
              <span className={styles.what}>
                <span className={styles.kind}>{t(`event.${event.kind}`)}</span>
                {event.server ? <span className={styles.server}>{event.server}</span> : null}
              </span>
            </div>
          </SlideIn>
        ))
      )}
    </Panel>
  );
}

function Servers() {
  const { t } = useTranslation();

  const servers = useQuery({ queryKey: keys.servers.list(), queryFn: endpoints.listServers });

  return (
    <Panel title={t('nav.servers')} count={servers.data?.length} flush>
      {servers.isPending ? (
        <div style={{ padding: 'var(--space-4)' }}>
          <Skeleton active paragraph={{ rows: 3 }} title={false} />
        </div>
      ) : servers.isError ? (
        <ErrorNotice error={servers.error} onRetry={() => void servers.refetch()} />
      ) : servers.data.length === 0 ? (
        <Nothing title={t('servers.empty')} hint={t('servers.emptyHint')} />
      ) : (
        servers.data.map((server, index) => (
          <Reveal key={server.name} index={index}>
            <Link
              to={`/servers/${encodeURIComponent(server.name)}/overview`}
              className={styles.serverRow}
            >
              <span className={styles.serverName}>{server.name}</span>
              <span className={styles.serverCount}>
                {server.status.toolCount > 0
                  ? `${server.status.toolCount} ${t('servers.tools')}`
                  : '—'}
              </span>
              <StateBadge
                state={server.status.state}
                error={server.status.error}
                enabled={server.config.enabled}
              />
            </Link>
          </Reveal>
        ))
      )}
    </Panel>
  );
}

function Meta() {
  const { t } = useTranslation();
  const health = useQuery({
    queryKey: keys.health,
    queryFn: endpoints.health,
    // The uptime advances on its own, and this is the one figure on the
    // page that no event will ever announce a change to.
    refetchInterval: 30_000,
  });

  if (!health.data) return null;

  return (
    <div className={styles.meta}>
      <span>
        {t('overview.version')} <strong>{health.data.version}</strong>
      </span>
      <span>
        {t('overview.uptime')} <strong>{uptime(health.data.uptimeSeconds)}</strong>
      </span>
      {health.data.connections !== undefined ? (
        <span>
          {t('connection.open')} <strong>{health.data.connections}</strong>
        </span>
      ) : null}
    </div>
  );
}

export default function Overview() {
  return (
    <div className={styles.page}>
      <Stats />
      <div className={styles.split}>
        <Activity />
        <Servers />
      </div>
      <Meta />
    </div>
  );
}
