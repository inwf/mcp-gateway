import '@testing-library/jest-dom/vitest';

/*
 * What jsdom does not implement but the components use.
 *
 * These are shims, not stand-ins for behaviour: each returns the
 * inertest truthful answer, so a component mounts and does nothing
 * observable rather than throwing. A test that needs the behaviour
 * should drive it explicitly instead of relying on these.
 */

// antd's responsive components and the theme's reduced-motion query.
window.matchMedia ??= ((query: string) => ({
  matches: false,
  media: query,
  onchange: null,
  addListener: () => {},
  removeListener: () => {},
  addEventListener: () => {},
  removeEventListener: () => {},
  dispatchEvent: () => false,
})) as unknown as typeof window.matchMedia;

// antd's Table and Drawer measure themselves.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

// motion's useInView, which the count-up uses to start only once the
// figure is on screen. Reported as visible: a figure that never counts
// is invisible to a test that asserts on it.
globalThis.IntersectionObserver ??= class {
  readonly root = null;
  readonly rootMargin = '';
  readonly thresholds: ReadonlyArray<number> = [];
  private readonly callback: IntersectionObserverCallback;

  constructor(callback: IntersectionObserverCallback) {
    this.callback = callback;
  }

  observe(target: Element) {
    this.callback(
      [{ isIntersecting: true, target } as unknown as IntersectionObserverEntry],
      this as unknown as IntersectionObserver,
    );
  }
  unobserve() {}
  disconnect() {}
  takeRecords(): IntersectionObserverEntry[] {
    return [];
  }
} as unknown as typeof IntersectionObserver;
