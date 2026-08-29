import { NavLink, Outlet, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Tooltip } from 'antd';
import {
  AlignLeftOutlined,
  ClusterOutlined,
  FileTextOutlined,
  RadarChartOutlined,
  SettingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import type { ComponentType } from 'react';
import { useEventStore } from '@/stores/events';
import { ThemeSwitch } from '@/components/ThemeSwitch';
import { cx } from '@/lib/cx';
import styles from './Shell.module.css';

interface Section {
  to: string;
  labelKey: string;
  Icon: ComponentType;
  /** An exact match is needed for the root, which is a prefix of
   *  everything else. */
  end?: boolean;
}

const SECTIONS: Section[] = [
  { to: '/', labelKey: 'nav.overview', Icon: RadarChartOutlined, end: true },
  { to: '/servers', labelKey: 'nav.servers', Icon: ClusterOutlined },
  { to: '/tools', labelKey: 'nav.tools', Icon: ThunderboltOutlined },
  { to: '/resources', labelKey: 'nav.resources', Icon: FileTextOutlined },
  { to: '/logs', labelKey: 'nav.logs', Icon: AlignLeftOutlined },
];

const SETTINGS: Section = { to: '/settings', labelKey: 'nav.settings', Icon: SettingOutlined };

function RailItem({ section }: { section: Section }) {
  const { t } = useTranslation();
  const label = t(section.labelKey);

  return (
    <Tooltip title={label} placement="right" mouseEnterDelay={0.3}>
      <NavLink
        to={section.to}
        end={section.end ?? false}
        aria-label={label}
        className={({ isActive }) => cx(styles.item, isActive && styles.active)}
      >
        <section.Icon />
      </NavLink>
    </Tooltip>
  );
}

function ConnectionIndicator() {
  const { t } = useTranslation();
  const connection = useEventStore((s) => s.connection);

  const dotClass =
    connection === 'open'
      ? styles.dotOpen
      : connection === 'connecting'
        ? styles.dotConnecting
        : styles.dotClosed;

  return (
    <Tooltip title={t('connection.hint')}>
      <span className={styles.link}>
        <span className={cx(styles.dot, dotClass)} />
        {t(`connection.${connection}`)}
      </span>
    </Tooltip>
  );
}

/** The page title, taken from whichever section owns the current path.
 *  Nested pages set their own heading; this names the section. */
function useSectionTitle(): string {
  const { t } = useTranslation();
  const { pathname } = useLocation();

  const section =
    [...SECTIONS, SETTINGS]
      .filter((s) => (s.end ? pathname === s.to : pathname.startsWith(s.to)))
      .sort((a, b) => b.to.length - a.to.length)[0] ?? SECTIONS[0];

  return section ? t(section.labelKey) : '';
}

export function Shell() {
  const { t } = useTranslation();
  const title = useSectionTitle();

  return (
    <div className={styles.shell}>
      <nav className={styles.rail} aria-label={t('app.tagline')}>
        <NavLink to="/" className={cx(styles.mark)} aria-label={t('app.name')}>
          M
        </NavLink>

        {SECTIONS.map((section) => (
          <RailItem key={section.to} section={section} />
        ))}

        <span className={styles.spacer} />
        <RailItem section={SETTINGS} />
      </nav>

      <div className={styles.main}>
        <header className={styles.topbar}>
          <h1 className={styles.title}>{title}</h1>
          <span className={styles.crumb}>{t('app.tagline')}</span>
          <div className={styles.topbarRight}>
            <ThemeSwitch />
            <ConnectionIndicator />
          </div>
        </header>

        <main className={styles.content}>
          <Outlet />
        </main>
      </div>
    </div>
  );
}
