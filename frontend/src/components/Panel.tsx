import { useId, type ReactNode } from 'react';
import { RightOutlined } from '@ant-design/icons';
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
  collapsible = false,
  open = true,
  onToggle,
}: {
  title: ReactNode;
  /** Shown beside the title. A count belongs next to what it counts. */
  count?: number | string | undefined;
  actions?: ReactNode | undefined;
  children: ReactNode;
  /** For a body that is a list of full-width rows. */
  flush?: boolean | undefined;
  className?: string | undefined;
  /** Whether the heading can fold the body away. Off by default, so that
   *  a panel is a plain surface unless someone asks for the handle. */
  collapsible?: boolean | undefined;
  /** Controlled: whoever passes `collapsible` owns the state, because it
   *  is the kind of choice that has to outlive this component — a panel
   *  that forgot on every re-render would be worse than none. */
  open?: boolean | undefined;
  onToggle?: (() => void) | undefined;
}) {
  const bodyId = useId();
  const folded = collapsible && !open;

  // The whole heading row is the target, not just the arrow: it is what a
  // person aims at, and the count and the badge beside the title are part
  // of the same thing being folded. The actions sit outside it, since a
  // state badge with a tooltip is not a fold handle.
  const heading = (
    <>
      {collapsible ? (
        <RightOutlined aria-hidden className={cx(styles.arrow, !folded && styles.arrowOpen)} />
      ) : null}
      {/* A heading element rather than a styled span: these are the
          only headings on most pages, and a reader that cannot see
          the panel's border has nothing else to navigate by. */}
      <h2 className={cx('label', styles.heading)}>{title}</h2>
      {count !== undefined ? <span className={styles.count}>{count}</span> : null}
    </>
  );

  return (
    <section className={cx(styles.panel, className)}>
      <header className={cx(styles.head, folded && styles.headFolded)}>
        {collapsible ? (
          <button
            type="button"
            className={cx(styles.title, styles.handle)}
            onClick={onToggle}
            aria-expanded={open}
            // Only while there is a body to point at: the body is
            // unmounted when folded, and a reference to an element that
            // is not there is worse than no reference.
            {...(folded ? {} : { 'aria-controls': bodyId })}
          >
            {heading}
          </button>
        ) : (
          <div className={styles.title}>{heading}</div>
        )}
        {actions ? <div className={styles.actions}>{actions}</div> : null}
      </header>

      {/* Unmounted rather than hidden. A collapsed group of forty tools
          keeps forty switches and forty tooltips alive if it is only
          styled away, and a screen reader still walks through them. */}
      {folded ? null : (
        <div id={bodyId} className={cx(styles.body, flush && styles.flush)}>
          {children}
        </div>
      )}
    </section>
  );
}
