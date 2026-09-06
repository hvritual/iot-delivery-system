/**
 * IOT-delivery Design Tokens
 * Generated as a TypeScript mirror of iot-delivery-tokens.css.
 * CSS remains the canonical source of truth.
 */

export const iotDeliveryTokens = {
  font: {
    sans:
      'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif',
    mono:
      '"Geist Mono", "SFMono-Regular", Menlo, Consolas, monospace',
    size: {
      micro: 11,
      xs: 12,
      sm: 14,
      md: 16,
      lg: 20,
      xl: 28,
    },
    weight: {
      regular: 400,
      medium: 500,
      semibold: 600,
      bold: 700,
    },
    lineHeight: {
      micro: 14,
      xs: 16,
      sm: 20,
      md: 24,
      lg: 28,
      xl: 36,
    },
  },

  space: {
    0: 0,
    1: 4,
    2: 8,
    3: 12,
    4: 16,
    5: 20,
    6: 24,
    7: 32,
    8: 40,
    9: 48,
  },

  radius: {
    xs: 4,
    sm: 6,
    md: 8,
    lg: 10,
    xl: 12,
    pill: 999,
  },

  color: {
    background: {
      app: "#FAFAFB",
      sidebar: "#F8F8F9",
      panel: "#FFFFFF",
      subtle: "#F6F6F7",
      hover: "#F2F2F4",
      active: "#ECECEF",
      disabled: "#F3F3F5",
    },
    text: {
      primary: "#222226",
      secondary: "#666A73",
      tertiary: "#9297A1",
      disabled: "#B6BAC2",
      onDark: "#FFFFFF",
    },
    border: {
      subtle: "#ECECEF",
      default: "#E1E3E7",
      strong: "#D4D7DD",
    },
    primary: {
      default: "#242428",
      hover: "#18181B",
      active: "#111113",
      foreground: "#FFFFFF",
    },

    // Proposed extensions: not directly extracted from the settings screenshot.
    semantic: {
      success: "#2F7D56",
      warning: "#A66A16",
      danger: "#C44536",
      info: "#4E6FAE",
      focus: "#5C79B8",
    },

    chart: [
      "#4E6FAE",
      "#6F8CC1",
      "#91A9D0",
      "#B3C5DF",
      "#D7E0EC",
    ] as const,
  },

  layout: {
    sidebarPrimaryWidth: 236,
    sidebarSecondaryWidth: 220,
    headerHeight: 48,
    pagePaddingX: 24,
    pagePaddingY: 24,
    contentMaxWidth: 1040,
    contentWideMaxWidth: 1152,
  },

  control: {
    height: {
      sm: 28,
      md: 32,
      lg: 36,
    },
    navItemHeight: 36,
    secondaryNavItemHeight: 32,
    switch: {
      width: 32,
      height: 20,
      thumbSize: 14,
    },
  },

  motion: {
    fast: 120,
    normal: 180,
    slow: 240,
    easing: "cubic-bezier(0.2, 0, 0, 1)",
  },

  breakpoint: {
    sm: 640,
    md: 768,
    lg: 1024,
    xl: 1280,
    "2xl": 1536,
  },
} as const;

export type IotDeliveryTokens = typeof iotDeliveryTokens;

export const semanticAliases = {
  appBackground: iotDeliveryTokens.color.background.app,
  panelBackground: iotDeliveryTokens.color.background.panel,
  sidebarBackground: iotDeliveryTokens.color.background.sidebar,
  navActiveBackground: iotDeliveryTokens.color.background.active,
  textPrimary: iotDeliveryTokens.color.text.primary,
  textSecondary: iotDeliveryTokens.color.text.secondary,
  divider: iotDeliveryTokens.color.border.subtle,
  primaryAction: iotDeliveryTokens.color.primary.default,
} as const;
