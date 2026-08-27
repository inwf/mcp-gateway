import type { ComponentType, PointerEvent, ReactNode } from 'react';
import { CountUp } from './CountUp';
import { cx } from '@/lib/cx';
import styles from './StatCard.module.css';

export type Note = 'plain' | 'ok' | 'warn' | 'danger';

interface Props {
  label: string;
  /** Counted up to when it is a number; shown as-is when it is not,
   *  which covers a value like a session mode. */
  value: number | string;
  unit?: string | undefined;
  note?: ReactNode | undefined;
  noteTone?: Note | undefined;
  Icon?: ComponentType | undefined;
}

/**
 * One figure, on a card that lights up under the pointer.
 *
 * The spotlight is done with two custom properties rather than by
 * re-rendering on pointer movement: a component that set state on every
 * mousemove would re-render the whole panel dozens of times a second to
 * move a gradient.
 */
export function StatCard({ label, value, unit, note, noteTone = 'plain', Icon }: Props) {
  const track = (event: PointerEvent<HTMLDivElement>) => {
    const box = event.currentTarget.getBoundingClientRect();
    event.currentTarget.style.setProperty('--spot-x', `${event.clientX - box.left}px`);
    event.currentTarget.style.setProperty('--spot-y', `${event.clientY - box.top}px`);
  };

  const noteClass =
    noteTone === 'ok'
      ? styles.noteOk
      : noteTone === 'warn'
        ? styles.noteWarn
        : noteTone === 'danger'
          ? styles.noteDanger
          : undefined;

  return (
    <div className={styles.card} onPointerMove={track}>
      <div className={styles.body}>
        <div className={styles.head}>
          <span className="label">{label}</span>
          {Icon ? (
            <span className={styles.icon}>
              <Icon />
            </span>
          ) : null}
        </div>

        <div className={styles.value}>
          {typeof value === 'number' ? <CountUp value={value} /> : <span>{value}</span>}
          {unit ? <span className={styles.unit}>{unit}</span> : null}
        </div>

        <div className={cx(styles.note, noteClass)}>{note}</div>
      </div>
    </div>
  );
}
