/**
 * Joins class names, dropping anything falsy.
 *
 * CSS module lookups are typed as possibly-undefined under
 * `noUncheckedIndexedAccess`, which is right for data but noise for a
 * stylesheet that certainly has the class. Rather than weaken the
 * setting, everything goes through here: a missing class contributes
 * nothing instead of putting the word "undefined" in the attribute.
 */
export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(' ');
}
