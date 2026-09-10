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
