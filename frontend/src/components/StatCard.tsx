import type { ReactNode } from 'react';
import { cx } from '@/lib/cx';
import { stagger } from '@/lib/motion';
import motion from '@/styles/motion.module.css';
import styles from './StatCard.module.css';

export type Note = 'plain' | 'ok' | 'warn' | 'danger';

/** A metric in a shared definition list; values appear without a count-up. */
export function StatCard({
  label,
  value,
  index = 0,
  note,
  noteTone = 'plain',
}: {
  label: string;
  value: number | string;
  index?: number | undefined;
  note?: ReactNode;
  noteTone?: Note;
}) {
  return (
    <div className={cx(styles.metric, motion.enter)} style={stagger(index)}>
      <dt className={styles.label}>{label}</dt>
      <dd className={styles.value}>{value}</dd>
      <dd className={cx(styles.note, styles[noteTone])}>{note}</dd>
    </div>
  );
}
