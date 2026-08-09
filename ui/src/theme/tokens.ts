export type Theme = {
  background: string
  surface: string
  surfaceRaised: string
  border: string
  borderStrong: string
  text: string
  muted: string
  subtle: string
  accent: string
  model: string
  warning: string
  danger: string
  success: string
}

const themes: Record<string, Theme> = {
  default: {
    background: "#07111f",
    surface: "#0b1728",
    surfaceRaised: "#101f34",
    border: "#263a55",
    borderStrong: "#3e5a7c",
    text: "#e5edf7",
    muted: "#9bacc3",
    subtle: "#62738b",
    accent: "#38bdf8",
    model: "#f0a8d5",
    warning: "#fbbf24",
    danger: "#fb7185",
    success: "#4ade80",
  },
  contrast: {
    background: "#020617",
    surface: "#071426",
    surfaceRaised: "#0d1e34",
    border: "#64748b",
    borderStrong: "#cbd5e1",
    text: "#ffffff",
    muted: "#e2e8f0",
    subtle: "#94a3b8",
    accent: "#22d3ee",
    model: "#f0abfc",
    warning: "#fde047",
    danger: "#fda4af",
    success: "#86efac",
  },
  mono: {
    background: "#090909",
    surface: "#111111",
    surfaceRaised: "#191919",
    border: "#404040",
    borderStrong: "#737373",
    text: "#f5f5f5",
    muted: "#d4d4d4",
    subtle: "#737373",
    accent: "#ffffff",
    model: "#ffffff",
    warning: "#e5e5e5",
    danger: "#ffffff",
    success: "#ffffff",
  },
}

export function getTheme(name?: string) {
  return themes[name ?? "default"] ?? themes.default
}
