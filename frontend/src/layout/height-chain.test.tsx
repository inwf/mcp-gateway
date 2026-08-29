import { describe, expect, it } from 'vitest';
import { render } from '@testing-library/react';
import { App as AntApp, ConfigProvider } from 'antd';
import { antdThemeFor } from '@/theme/antd';

/*
 * The height chain.
 *
 * The shell is sized to the window with height: 100%, which only works if
 * every element between the document and it has a resolved height. antd's
 * App renders a div in that gap, and a div is auto-height — so the chain
 * broke there, the shell collapsed to its content, and the window was left
 * part empty below it.
 *
 * The visible symptom was cosmetic and only in the light theme, where the
 * shell's chrome is a different colour from the ground behind it. The real
 * one was not: a shell that is content-height never overflows, so the pane
 * inside it that carries overflow: auto never scrolls, and anything past
 * the bottom of the window is clipped by the body with no way to reach it.
 *
 * This is the part of that which a test can hold. The rest — that the
 * shell actually fills the window, that a tall page actually scrolls —
 * needs a layout engine, and jsdom has none. Nothing here would have
 * caught the original fault; only looking at it would have, which is what
 * eventually did.
 */

describe("the wrapper antd's App renders", () => {
  it('carries a height, so the chain to the shell is not broken', () => {
    const { container } = render(
      <ConfigProvider theme={antdThemeFor('dark')}>
        <AntApp style={{ height: '100%' }}>
          <div data-testid="child" />
        </AntApp>
      </ConfigProvider>,
    );

    // Whatever antd calls it, there is an element between the mount point
    // and the child, and it is the one that needs the height.
    const child = container.querySelector('[data-testid="child"]');
    expect(child).not.toBeNull();

    const wrapper = child?.parentElement;
    expect(wrapper).not.toBe(container);
    expect(wrapper?.style.height).toBe('100%');
  });
});
