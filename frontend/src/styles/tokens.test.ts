import { readFileSync } from 'node:fs';
import { globSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

/*
 * A check on the stylesheet as written.
 *
 * The theme is two palettes of the same token names. A token defined in
 * only one leaves whatever uses it inheriting the other theme's value:
 * legible in the theme it was written for, invisible in the other, and
 * noticed by nobody until they switch. Nothing else in the project would
 * catch that — the type checker does not read CSS, and a component test
 * renders with the stylesheet stubbed out.
 *
 * It reads the file rather than importing it because an import would hand
 * back whatever the bundler made of it, and what matters here is what a
 * person will edit. That also puts this file outside the browser-targeted
 * project; see tsconfig.node.json.
 */

const PATH = 'src/styles/tokens.css';

/** The custom properties declared under one selector. */
function tokensUnder(css: string, selector: string): string[] {
  const at = css.indexOf(selector);
  if (at < 0) throw new Error(`${PATH} has no ${selector} block`);

  const block = css.slice(css.indexOf('{', at) + 1, css.indexOf('}', at));
  return [...block.matchAll(/^\s*(--[a-z0-9-]+)\s*:/gm)].map((match) => match[1]!).sort();
}

describe('the two palettes', () => {
  const css = readFileSync(PATH, 'utf8');

  it('declare the same tokens', () => {
    const light = tokensUnder(css, "[data-theme='light']");
    const dark = tokensUnder(css, "[data-theme='dark']");

    expect(light.length).toBeGreaterThan(20);
    expect(light).toEqual(dark);
  });

  // The browser's own furniture — scrollbars, form controls, the canvas
  // behind an overscroll — reads color-scheme rather than the attribute.
  // A palette without it gives a light page dark scrollbars.
  it('declare a colour scheme each', () => {
    expect(css).toContain('color-scheme: light');
    expect(css).toContain('color-scheme: dark');
  });
});

/*
 * No stylesheet outside this file writes a colour.
 *
 * This is the check that was missing. The parity test above passes
 * happily while every panel in the application is painted with a
 * hardcoded dark gradient — which is how the light theme first shipped
 * with dark grey panels on a white page, having satisfied every test and
 * a build-output inspection.
 *
 * A colour written into a component's stylesheet is legible in the theme
 * it was written for and wrong in the other, and nothing else in the
 * project can tell. So: any colour belongs in tokens.css, and anything
 * genuinely independent of the theme has to say so here by name.
 */
describe('colours outside the palette', () => {
  /** The literals that are deliberately not tokens, each with its reason.
   *  A new entry here is a claim that the value means the same thing in
   *  both themes. */
  const ALLOWED = new Map<string, string>([
    // A mask uses only the alpha channel; the colour is irrelevant.
    ['#000', 'mask-image, where only the alpha is read'],
  ]);

  const COLOUR =
    /#[0-9a-fA-F]{3,8}\b|\brgba?\([^)]*\)|\bhsla?\([^)]*\)|\bcolor-mix\([^)]*\)/g;

  it('are all tokens', () => {
    const offenders: string[] = [];

    for (const path of globSync('src/**/*.css').sort()) {
      if (path.endsWith('tokens.css')) continue;

      readFileSync(path, 'utf8')
        .split('\n')
        .forEach((line, index) => {
          // A declaration reading a token is not writing a colour.
          for (const found of line.match(COLOUR) ?? []) {
            if (ALLOWED.has(found)) continue;
            offenders.push(`${path}:${index + 1}  ${found}   in: ${line.trim()}`);
          }
        });
    }

    expect(offenders, `move these into tokens.css:\n  ${offenders.join('\n  ')}`).toEqual([]);
  });
});
