import { motion, useReducedMotion } from 'motion/react';
import type { ReactNode } from 'react';

/**
 * Entrance movement: a short rise and fade as an element appears.
 *
 * `index` staggers a list so that its rows arrive in order rather than
 * all at once, which is what makes a panel read as being filled in
 * rather than as having popped into place. The stagger is capped, or a
 * list of two hundred log lines would take twenty seconds to finish
 * appearing.
 */
export function Reveal({
  children,
  index = 0,
  className,
}: {
  children: ReactNode;
  index?: number | undefined;
  className?: string | undefined;
}) {
  const still = useReducedMotion();

  if (still) return <div className={className}>{children}</div>;

  return (
    <motion.div
      className={className}
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{
        duration: 0.32,
        delay: Math.min(index * 0.045, 0.4),
        ease: [0.22, 1, 0.36, 1],
      }}
    >
      {children}
    </motion.div>
  );
}

/**
 * Movement for something arriving in a live list.
 *
 * Different from [Reveal] in two ways that matter for a feed: it slides
 * in from the side, which reads as "new at the top" rather than as
 * "the list rendered", and it has no stagger, because an event that
 * arrives on its own should not wait its turn.
 */
export function SlideIn({ children, className }: { children: ReactNode; className?: string }) {
  const still = useReducedMotion();

  if (still) return <div className={className}>{children}</div>;

  return (
    <motion.div
      className={className}
      initial={{ opacity: 0, x: -10 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.26, ease: [0.22, 1, 0.36, 1] }}
    >
      {children}
    </motion.div>
  );
}
