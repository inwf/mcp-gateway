/*
 * Turning the gateway's values into something readable.
 *
 * The API speaks in the configuration file's terms — durations as
 * strings like `30s`, timestamps as RFC 3339 — so these are mostly
 * about presentation, not conversion.
 */

/** Formats a timestamp as a wall clock, which is what a log line or an
 *  event wants. Seconds included: events arrive in bursts and the
 *  minute alone would show several as simultaneous. */
export function clockTime(iso: string): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return '—';
  return at.toLocaleTimeString('zh-CN', { hour12: false });
}

/** Formats a timestamp in full, for a tooltip or a detail row. */
export function fullTime(iso: string | undefined): string {
  if (!iso) return '—';
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return '—';
  return at.toLocaleString('zh-CN', { hour12: false });
}

/**
 * How long ago, in words.
 *
 * `now` is a parameter rather than read from the clock so that a list
 * of these all agree with each other, and so a test can state the
 * moment it is describing.
 */
export function relative(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return '—';
  const at = new Date(iso).getTime();
  if (Number.isNaN(at)) return '—';

  const seconds = Math.max(0, Math.round((now - at) / 1000));
  if (seconds < 10) return '刚刚';
  if (seconds < 60) return `${seconds} 秒前`;

  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} 分钟前`;

  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;

  return `${Math.round(hours / 24)} 天前`;
}

/**
 * A span of seconds as a coarse duration: `3天 4小时`, `12分 05秒`.
 *
 * Two units at most. A process that has been up for three days does not
 * need its seconds, and one that has been up for twelve seconds does
 * not need its days.
 */
export function humanDuration(totalSeconds: number): string {
  if (!Number.isFinite(totalSeconds) || totalSeconds < 0) return '—';

  const seconds = Math.floor(totalSeconds);
  const days = Math.floor(seconds / 86_400);
  const hours = Math.floor((seconds % 86_400) / 3_600);
  const minutes = Math.floor((seconds % 3_600) / 60);
  const rest = seconds % 60;

  if (days > 0) return `${days} 天 ${hours} 小时`;
  if (hours > 0) return `${hours} 小时 ${minutes} 分`;
  if (minutes > 0) return `${minutes} 分 ${String(rest).padStart(2, '0')} 秒`;
  return `${rest} 秒`;
}

/** How long the gateway has been up, from the health response. */
export function uptime(seconds: number): string {
  return humanDuration(seconds);
}

/** Tags are a map on the wire; a list of `key=value` is what reads
 *  well in a cell or a chip row. */
export function tagPairs(tags: Record<string, string> | undefined): Array<[string, string]> {
  if (!tags) return [];
  return Object.entries(tags).sort(([a], [b]) => a.localeCompare(b));
}

/** Shortens a value that would otherwise stretch a column: a long
 *  command line, a session id. The middle goes, because both ends
 *  carry more information than the middle does. */
export function ellipsize(value: string, max = 48): string {
  if (value.length <= max) return value;
  const keep = Math.floor((max - 1) / 2);
  return `${value.slice(0, keep)}…${value.slice(-keep)}`;
}

/** What a server is configured to reach: the command for the transports
 *  that spawn a process, the URL for the ones that speak HTTP. */
export function endpointOf(server: {
  command?: string | undefined;
  args?: string[] | undefined;
  url?: string | undefined;
}): string {
  if (server.url) return server.url;
  if (!server.command) return '—';
  return [server.command, ...(server.args ?? [])].join(' ');
}
