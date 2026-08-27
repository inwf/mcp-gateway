import type { ReactNode } from 'react';
import { cx } from '@/lib/cx';
import styles from './Panel.module.css';

/** A titled surface. The one container used across pages, so that
 *  panels agree about their border, their heading and their padding. */
export function Panel({
  title,
  count,
  actions,
  children,
  flush = false,
  className,
}: {
  title: ReactNode;
  /** Shown beside the title. A count belongs next to what it counts. */
  count?: number | string | undefined;
  actions?: ReactNode | undefined;
  children: ReactNode;
  /** For a body that is a list of full-width rows. */
  flush?: boolean | undefined;
  className?: string | undefined;
}) {
  return (
    <section className={cx(styles.panel, className)}>
      <header className={styles.head}>
        <div className={styles.title}>
          <span className="label">{title}</span>
          {count !== undefined ? <span className={styles.count}>{count}</span> : null}
        </div>
        {actions ? <div className={styles.actions}>{actions}</div> : null}
      </header>

      <div className={cx(styles.body, flush && styles.flush)}>{children}</div>
    </section>
  );
}
