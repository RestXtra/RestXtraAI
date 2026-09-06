import localFont from "next/font/local";

// Public mock builds must be reproducible without reaching Google Fonts. The
// Geist package is already locked in package.json, so these files are available
// immediately after npm ci and are emitted as normal self-hosted Next fonts.
const geist = localFont({
  src: "../../../node_modules/geist/dist/fonts/geist-sans/Geist-Variable.woff2",
  variable: "--font-geist",
  weight: "100 900",
});

const geistMono = localFont({
  src: "../../../node_modules/geist/dist/fonts/geist-mono/GeistMono-Variable.woff2",
  variable: "--font-geist-mono",
  weight: "100 900",
});

const geistPixelSquare = localFont({
  src: "../../../node_modules/geist/dist/fonts/geist-pixel/GeistPixel-Square.woff2",
  variable: "--font-geist-pixel-square",
  weight: "400",
});

export const fontRegistry = {
  geist: { label: "Geist", font: geist },
  geistMono: { label: "Geist Mono", font: geistMono },
  geistPixelSquare: { label: "Geist Pixel Square", font: geistPixelSquare },
} as const;

export type FontKey = keyof typeof fontRegistry;

export const fontVars = Object.values(fontRegistry)
  .map((entry) => entry.font.variable)
  .join(" ");

export const fontOptions = Object.entries(fontRegistry).map(([key, entry]) => ({
  key: key as FontKey,
  label: entry.label,
  variable: entry.font.variable,
}));
