import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./src/pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./src/components/**/*.{js,ts,jsx,tsx,mdx}",
    "./src/app/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        back: {
          DEFAULT: "#72BBEF",
          light: "#A6D8F7",
          dark: "#4A9FDB",
        },
        lay: {
          DEFAULT: "#FAA9BA",
          light: "#FCC8D3",
          dark: "#E8879B",
        },
        // Teal brand color (replacing red lotus)
        lotus: {
          DEFAULT: "#0D9488",
          light: "#14B8A6",
          dark: "#0F766E",
        },
        profit: "#22C55E",
        loss: "#EF4444",
        surface: {
          DEFAULT: "var(--bg-surface)",
          light: "var(--bg-surface-light)",
          lighter: "#2D3143",
        },
      },
      fontFamily: {
        sans: ["var(--font-inter)", "Inter", "system-ui", "sans-serif"],
        mono: ["var(--font-mono)", "JetBrains Mono", "monospace"],
      },
    },
  },
  plugins: [],
};

export default config;
