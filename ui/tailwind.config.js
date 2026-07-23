/** @type {import('tailwindcss').Config} */

// Two custom daisyUI themes named `light` / `dark` (overriding the stock pair of
// the same name, so the data-theme toggle in the ui store keeps working). Cool
// neutrals with a faint indigo bias, a single indigo primary, and semantic colors
// reserved for state. Radii are tuned down from daisyUI's defaults — notably
// --rounded-badge, which squares the status pills into chips. base-200 is the page
// ground (see assets/main.css body), base-100 the card surface, base-300 the border.
const shared = {
  "--rounded-box": "0.75rem",
  "--rounded-btn": "0.5rem",
  "--rounded-badge": "0.375rem",
  "--tab-radius": "0.5rem",
  "--border-btn": "1px",
  "--animation-btn": "0.2s",
}

export default {
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {},
  },
  plugins: [require("daisyui")],
  daisyui: {
    logs: false,
    darkTheme: "dark",
    themes: [
      {
        light: {
          "color-scheme": "light",
          primary: "#4F46E5",
          "primary-content": "#FFFFFF",
          secondary: "#64748B",
          "secondary-content": "#FFFFFF",
          accent: "#0D9488",
          "accent-content": "#FFFFFF",
          neutral: "#1F2733",
          "neutral-content": "#F4F6FA",
          "base-100": "#FFFFFF",
          "base-200": "#F4F6FA",
          "base-300": "#E4E8EF",
          "base-content": "#1B2330",
          info: "#2563EB",
          "info-content": "#FFFFFF",
          success: "#16A34A",
          "success-content": "#FFFFFF",
          warning: "#D97706",
          "warning-content": "#FFFFFF",
          error: "#DC2626",
          "error-content": "#FFFFFF",
          ...shared,
        },
      },
      {
        dark: {
          "color-scheme": "dark",
          primary: "#6366F1",
          "primary-content": "#FFFFFF",
          secondary: "#94A3B8",
          "secondary-content": "#0D1017",
          accent: "#2DD4BF",
          "accent-content": "#0D1017",
          neutral: "#232A35",
          "neutral-content": "#E7EBF2",
          "base-100": "#12161D",
          "base-200": "#0D1017",
          "base-300": "#232A35",
          "base-content": "#E7EBF2",
          info: "#3B82F6",
          "info-content": "#FFFFFF",
          success: "#22C55E",
          "success-content": "#052E16",
          warning: "#F59E0B",
          "warning-content": "#1C1300",
          error: "#EF4444",
          "error-content": "#FFFFFF",
          ...shared,
        },
      },
    ],
  },
}
