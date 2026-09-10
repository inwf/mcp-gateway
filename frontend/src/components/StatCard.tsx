import type { ReactNode } from 'react';
import { cx } from '@/lib/cx';
import styles from './StatCard.module.css';

export type Note = 'plain' | 'ok' | 'warn' | 'danger';

/** A metric in a shared definition list; values appear without a count-up. */
export function StatCard({
  label,
  value,
  note,
  noteTone = 'plain',
}: {
  label: string;
  value: number | string;
  note?: ReactNode;
  noteTone?: Note;
}) {
  return (
    <div className={styles.metric}>
      <dt className={styles.label}>{label}</dt>
      <dd className={styles.value}>{value}</dd>
      <dd className={cx(styles.note, styles[noteTone])}>{note}</dd>
    </div>
  );
}
