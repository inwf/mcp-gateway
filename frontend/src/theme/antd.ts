import { theme, type ThemeConfig } from 'antd';

/*
 * antd, dressed to match.
 *
 * The values here are the ones in styles/tokens.css. They are repeated
 * as literals rather than read out of the stylesheet at runtime because
 * antd computes derived colours from them — hovers, borders, disabled
 * states, the ten-step palette behind every component — and it does
 * that once, at theme construction. Feeding it values read from the
 * document would mean doing that work on every mount to obtain the same
 * answer.
 *
 * Both files change together. tokens.css is the one to read; this is
 * the one that tells antd about it.
 *
 * There are two palettes and one configuration built from them. Writing
 * the configuration out twice would be the same mistake as two
 * stylesheets: the failure mode is a component that was themed in dark
 * and forgotten in light, and it is only visible to someone who switches.
 */

/** The colours a theme is made of. Every field is required, so a palette
 *  cannot be added with a gap in it. */
interface Palette {
  void: string;
  surface: string;
  raised: string;
  overlay: string;
  sunken: string;

  accent: string;
  info: string;
  ok: string;
  warn: string;
  danger: string;

  ink: string;
  inkMuted: string;
  inkFaint: string;
  inkGhost: string;

  line: string;
  lineStrong: string;

  /** Panels are translucent so the moving field shows through them. */
  container: string;
  /** Inputs sit below their panel rather than on it. */
  input: string;

  /** The accent at the four alphas the components need. */
  accentGhost: string;
  accentDim: string;
  accentSoft: string;
  /** A neutral hover, used where an accent tint would read as selection. */
  hover: string;
  /** The ground of a tag or segmented track. */
  chip: string;
}

const DARK: Palette = {
  void: '#06080d',
  surface: '#0b0f16',
  raised: '#111823',
  overlay: '#18202c',
  sunken: '#05070b',

  accent: '#2dd4bf',
  info: '#a78bfa',
  ok: '#4ade80',
  warn: '#fbbf24',
  danger: '#fb7185',

  ink: '#e8edf5',
  inkMuted: '#93a1b5',
  inkFaint: '#5d6b80',
  inkGhost: '#43506380',

  line: 'rgba(255,255,255,0.07)',
  lineStrong: 'rgba(255,255,255,0.13)',

  container: 'rgba(17,24,35,0.72)',
  input: 'rgba(6,8,13,0.6)',

  accentGhost: 'rgba(45,212,191,0.045)',
  accentDim: 'rgba(45,212,191,0.09)',
  accentSoft: 'rgba(45,212,191,0.14)',
  hover: 'rgba(255,255,255,0.04)',
  chip: 'rgba(255,255,255,0.05)',
};

/*
 * The light palette is not the dark one lightened. Its surfaces run the
 * other way — the floor is the darkest and panels rise out of it — and
 * both the accent and the state colours are deepened, because every
 * colour in the dark palette is chosen to be legible against near-black
 * and none of them are legible against white.
 */
const LIGHT: Palette = {
  void: '#eef1f5',
  surface: '#f7f9fb',
  raised: '#ffffff',
  overlay: '#ffffff',
  sunken: '#e7ebf1',

  accent: '#0d9488',
  info: '#6d28d9',
  ok: '#15803d',
  warn: '#b45309',
  danger: '#be123c',

  ink: '#16202e',
  inkMuted: '#55647a',
  inkFaint: '#78879e',
  inkGhost: '#78879e80',

  line: 'rgba(15,30,55,0.11)',
  lineStrong: 'rgba(15,30,55,0.20)',

  container: 'rgba(255,255,255,0.88)',
  input: 'rgba(255,255,255,0.94)',

  accentGhost: 'rgba(13,148,136,0.05)',
  accentDim: 'rgba(13,148,136,0.10)',
  accentSoft: 'rgba(13,148,136,0.14)',
  hover: 'rgba(15,30,55,0.05)',
  chip: 'rgba(15,30,55,0.06)',
};

const FONT_UI =
  "system-ui, -apple-system, 'Segoe UI Variable Text', 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif";
const FONT_MONO =
  "ui-monospace, 'SF Mono', 'JetBrains Mono', 'Cascadia Code', 'Roboto Mono', Menlo, Consolas, monospace";

function configFor(p: Palette, dark: boolean): ThemeConfig {
  return {
    algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,

    token: {
      colorPrimary: p.accent,
      colorInfo: p.info,
      colorSuccess: p.ok,
      colorWarning: p.warn,
      colorError: p.danger,

      colorBgBase: p.void,
      colorTextBase: p.ink,

      // Panels are translucent so the moving field shows through them.
      // Opaque panels would hide it everywhere it matters and leave it
      // visible only in the gaps, which looks like a rendering fault
      // rather than a background.
      colorBgLayout: 'transparent',
      colorBgContainer: p.container,
      colorBgElevated: p.overlay,
      colorBgSpotlight: p.overlay,

      colorBorder: p.lineStrong,
      colorBorderSecondary: p.line,
      colorSplit: p.line,

      colorTextSecondary: p.inkMuted,
      colorTextTertiary: p.inkFaint,
      colorTextQuaternary: p.inkGhost,

      fontFamily: FONT_UI,
      fontFamilyCode: FONT_MONO,
      fontSize: 14,

      borderRadius: 8,
      borderRadiusLG: 14,
      borderRadiusSM: 4,

      controlHeight: 34,
      lineWidth: 1,
      wireframe: false,

      motionDurationMid: '0.14s',
      motionDurationSlow: '0.32s',
      motionEaseInOut: 'cubic-bezier(0.22, 1, 0.36, 1)',
    },

    components: {
      /*
       * The table is the single strongest tell that a page was built with
       * a component library: a filled header band, a full grid of rules,
       * and a hover that flashes a pale block. All three go. What is left
       * is a hairline under the header, rows separated by nothing but
       * space, and a hover that lifts the row's ground very slightly.
       */
      Table: {
        headerBg: 'transparent',
        headerColor: p.inkFaint,
        headerSplitColor: 'transparent',
        headerBorderRadius: 0,
        borderColor: p.line,
        rowHoverBg: p.accentGhost,
        rowSelectedBg: p.accentDim,
        rowSelectedHoverBg: p.accentSoft,
        cellPaddingBlock: 13,
        footerBg: 'transparent',
      },

      /* The form's other tell: a bold label above every field. Quiet,
         spaced and small reads as an instrument panel rather than a
         registration page. */
      Form: {
        labelColor: p.inkMuted,
        labelFontSize: 12,
        verticalLabelPadding: '0 0 6px',
        itemMarginBottom: 20,
      },

      Layout: {
        bodyBg: 'transparent',
        headerBg: 'transparent',
        siderBg: 'transparent',
        headerHeight: 52,
        headerPadding: '0 20px',
      },

      Menu: {
        itemBg: 'transparent',
        subMenuItemBg: 'transparent',
        itemSelectedBg: p.accentSoft,
        itemSelectedColor: p.accent,
        itemHoverBg: p.hover,
        itemBorderRadius: 8,
        activeBarWidth: 0,
      },

      Card: {
        colorBgContainer: p.container,
        headerBg: 'transparent',
        headerFontSize: 13,
        paddingLG: 20,
      },

      Modal: { contentBg: p.overlay, headerBg: 'transparent', titleFontSize: 16 },
      Drawer: { colorBgElevated: p.surface, paddingLG: 20 },

      Input: {
        colorBgContainer: p.input,
        activeShadow: `0 0 0 3px ${p.accentSoft}`,
      },
      InputNumber: { colorBgContainer: p.input },
      Select: { colorBgContainer: p.input, optionSelectedBg: p.accentSoft },

      Button: {
        primaryShadow: 'none',
        defaultShadow: 'none',
        dangerShadow: 'none',
        fontWeight: 500,
      },

      Tabs: {
        itemColor: p.inkMuted,
        itemSelectedColor: p.accent,
        inkBarColor: p.accent,
        horizontalMargin: '0 0 16px 0',
      },

      Tag: { defaultBg: p.chip, defaultColor: p.inkMuted, borderRadiusSM: 4 },

      Tooltip: { colorBgSpotlight: p.overlay, colorTextLightSolid: p.ink },

      Segmented: {
        itemColor: p.inkMuted,
        itemSelectedBg: p.accentSoft,
        itemSelectedColor: p.accent,
        trackBg: p.chip,
      },

      Empty: { colorTextDescription: p.inkFaint },
      Divider: { colorSplit: p.line },
      Descriptions: { labelBg: 'transparent', titleColor: p.inkMuted },
      Statistic: { contentFontSize: 28, titleFontSize: 12 },
      Popover: { colorBgElevated: p.overlay },
      Dropdown: { colorBgElevated: p.overlay },
      Message: { colorBgElevated: p.overlay },
      Notification: { colorBgElevated: p.overlay },
    },
  };
}

/*
 * Both themes are built once, at module load. antd derives a great deal
 * from each token — the ten-step palette behind every component, every
 * hover and disabled state — and doing that on each render of the
 * provider would repeat the work to reach the same two answers.
 */
const THEMES = {
  dark: configFor(DARK, true),
  light: configFor(LIGHT, false),
} as const;

/** The antd configuration for a resolved theme. */
export function antdThemeFor(mode: keyof typeof THEMES): ThemeConfig {
  return THEMES[mode];
}
