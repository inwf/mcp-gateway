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
 */

const VOID = '#06080d';
const SURFACE = '#0b0f16';
const RAISED = '#111823';
const OVERLAY = '#18202c';

const ACCENT = '#2dd4bf';
const INK = '#e8edf5';
const INK_MUTED = '#93a1b5';

const LINE = 'rgba(255,255,255,0.07)';
const LINE_STRONG = 'rgba(255,255,255,0.13)';

const FONT_UI =
  "system-ui, -apple-system, 'Segoe UI Variable Text', 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif";
const FONT_MONO =
  "ui-monospace, 'SF Mono', 'JetBrains Mono', 'Cascadia Code', 'Roboto Mono', Menlo, Consolas, monospace";

export const antdTheme: ThemeConfig = {
  algorithm: theme.darkAlgorithm,

  token: {
    colorPrimary: ACCENT,
    colorInfo: '#a78bfa',
    colorSuccess: '#4ade80',
    colorWarning: '#fbbf24',
    colorError: '#fb7185',

    colorBgBase: VOID,
    colorTextBase: INK,

    // Panels are translucent so the moving field shows through them.
    // Opaque panels would hide it everywhere it matters and leave it
    // visible only in the gaps, which looks like a rendering fault
    // rather than a background.
    colorBgLayout: 'transparent',
    colorBgContainer: 'rgba(17,24,35,0.72)',
    colorBgElevated: OVERLAY,
    colorBgSpotlight: OVERLAY,

    colorBorder: LINE_STRONG,
    colorBorderSecondary: LINE,
    colorSplit: LINE,

    colorTextSecondary: INK_MUTED,
    colorTextTertiary: '#5d6b80',
    colorTextQuaternary: '#43506380',

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
      headerColor: '#5d6b80',
      headerSplitColor: 'transparent',
      headerBorderRadius: 0,
      borderColor: LINE,
      rowHoverBg: 'rgba(45,212,191,0.045)',
      rowSelectedBg: 'rgba(45,212,191,0.09)',
      rowSelectedHoverBg: 'rgba(45,212,191,0.12)',
      cellPaddingBlock: 13,
      footerBg: 'transparent',
    },

    /* The form's other tell: a bold label above every field. Quiet,
       spaced and small reads as an instrument panel rather than a
       registration page. */
    Form: {
      labelColor: INK_MUTED,
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
      itemSelectedBg: 'rgba(45,212,191,0.12)',
      itemSelectedColor: ACCENT,
      itemHoverBg: 'rgba(255,255,255,0.04)',
      itemBorderRadius: 8,
      activeBarWidth: 0,
    },

    Card: {
      colorBgContainer: 'rgba(17,24,35,0.6)',
      headerBg: 'transparent',
      headerFontSize: 13,
      paddingLG: 20,
    },

    Modal: { contentBg: OVERLAY, headerBg: 'transparent', titleFontSize: 16 },
    Drawer: { colorBgElevated: SURFACE, paddingLG: 20 },

    Input: {
      colorBgContainer: 'rgba(6,8,13,0.6)',
      activeShadow: '0 0 0 3px rgba(45,212,191,0.14)',
    },
    InputNumber: { colorBgContainer: 'rgba(6,8,13,0.6)' },
    Select: { colorBgContainer: 'rgba(6,8,13,0.6)', optionSelectedBg: 'rgba(45,212,191,0.12)' },

    Button: {
      primaryShadow: 'none',
      defaultShadow: 'none',
      dangerShadow: 'none',
      fontWeight: 500,
    },

    Tabs: {
      itemColor: INK_MUTED,
      itemSelectedColor: ACCENT,
      inkBarColor: ACCENT,
      horizontalMargin: '0 0 16px 0',
    },

    Tag: { defaultBg: 'rgba(255,255,255,0.05)', defaultColor: INK_MUTED, borderRadiusSM: 4 },

    Tooltip: { colorBgSpotlight: OVERLAY, colorTextLightSolid: INK },

    Segmented: {
      itemColor: INK_MUTED,
      itemSelectedBg: 'rgba(45,212,191,0.14)',
      itemSelectedColor: ACCENT,
      trackBg: 'rgba(6,8,13,0.55)',
    },

    Empty: { colorTextDescription: '#5d6b80' },
    Divider: { colorSplit: LINE },
    Descriptions: { labelBg: 'transparent', titleColor: INK_MUTED },
    Statistic: { contentFontSize: 28, titleFontSize: 12 },
    Popover: { colorBgElevated: OVERLAY },
    Dropdown: { colorBgElevated: OVERLAY },
    Message: { colorBgElevated: OVERLAY },
    Notification: { colorBgElevated: OVERLAY },
  },
};

export const SURFACES = { VOID, SURFACE, RAISED, OVERLAY } as const;
