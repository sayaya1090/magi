/** Semantic colors shared with JetBrains Palette and the TUI palette. */
export const ideColors = {
  primary: { light: '#B45309', dark: '#FF7A1A' },
  accent: { light: '#0E7490', dark: '#5CD8E6' },
  muted: { light: '#4A453C', dark: '#C9C2B8' },
  error: { light: '#B3261E', dark: '#F2B8B5' },
  warn: { light: '#92600A', dark: '#FFD479' },
  success: { light: '#15803D', dark: '#86EFAC' },
  melchior: { light: '#B45309', dark: '#FFB454' },
  balthasar: { light: '#0E7490', dark: '#5CD8E6' },
  casper: { light: '#6D28D9', dark: '#D8B4FE' },
  partSystem: { light: '#8A8F98', dark: '#A3A8B0' },
  partTools: { light: '#6B5BD2', dark: '#AFA3F5' },
  partCalls: { light: '#C77700', dark: '#F2B45C' },
  partResults: { light: '#0284C7', dark: '#7CC8F2' },
} as const;

const declarations = (theme: 'light' | 'dark') => Object.entries(ideColors)
  .map(([role, colors]) => `--magi-${role}:${colors[theme]};`).join('');

export const idePaletteCss = `
body { ${declarations('dark')} }
body.vscode-light, body.vscode-high-contrast-light { ${declarations('light')} }
`;
