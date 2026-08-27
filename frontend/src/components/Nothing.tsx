import type { ReactNode } from 'react';

/**
 * Nothing here, and why.
 *
 * Two lines rather than one: "no tools" on its own reads as a fault,
 * where "no tools — connect a server and its tools appear here" reads
 * as a state with something to do about it. Almost every empty list in
 * this application is the first-run case.
 */
export function Nothing({
  title,
  hint,
  action,
}: {
  title: string;
  hint?: string | undefined;
  action?: ReactNode | undefined;
}) {
  return (
    <div
      style={{
        display: 'grid',
        gap: 'var(--space-3)',
        justifyItems: 'center',
        padding: 'var(--space-7) var(--space-4)',
        textAlign: 'center',
      }}
    >
      <p style={{ margin: 0, color: 'var(--ink-muted)' }}>{title}</p>
      {hint ? (
        <p
          style={{
            margin: 0,
            maxWidth: '42ch',
            color: 'var(--ink-faint)',
            fontSize: 'var(--text-sm)',
          }}
        >
          {hint}
        </p>
      ) : null}
      {action}
    </div>
  );
}
