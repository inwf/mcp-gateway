import { useEffect, useRef, useState } from 'react';
import { useInView } from 'motion/react';

/**
 * A number that counts up to its value.
 *
 * Worth the movement for a figure that means something — how many
 * servers are connected, how many tools are published — because the
 * animation is what draws the eye to a figure that has changed. It is
 * not worth it for a figure in a table cell, which is why this is a
 * component rather than a default.
 *
 * Subsequent changes animate from the previous value rather than from
 * zero: a count going 4 → 5 that swept up from nothing would read as
 * the whole panel reloading.
 */
export function CountUp({
  value,
  durationMs = 900,
  className,
}: {
  value: number;
  durationMs?: number | undefined;
  className?: string | undefined;
}) {
  const ref = useRef<HTMLSpanElement>(null);
  const inView = useInView(ref, { once: true });

  const [shown, setShown] = useState(0);
  const from = useRef(0);
  const frame = useRef<number>(0);

  useEffect(() => {
    if (!inView) return;

    const start = performance.now();
    const origin = from.current;
    const distance = value - origin;

    // Nothing to animate. Assigning directly also covers the case of a
    // value arriving before the element is on screen.
    if (distance === 0) {
      setShown(value);
      return;
    }

    const step = (at: number) => {
      const progress = Math.min(1, (at - start) / durationMs);
      // Decelerating: fast at first, settling onto the final value.
      // A linear count reads as a progress bar rather than as a figure
      // arriving.
      const eased = 1 - (1 - progress) ** 3;
      setShown(Math.round(origin + distance * eased));

      if (progress < 1) {
        frame.current = requestAnimationFrame(step);
      } else {
        from.current = value;
      }
    };

    frame.current = requestAnimationFrame(step);
    return () => cancelAnimationFrame(frame.current);
  }, [value, durationMs, inView]);

  return (
    <span ref={ref} className={className}>
      {shown}
    </span>
  );
}
