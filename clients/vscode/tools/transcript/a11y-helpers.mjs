import assert from 'node:assert/strict';
import { createRequire } from 'node:module';

const require = createRequire(new URL('../../../web/e2e/package.json', import.meta.url));
const { AxeBuilder } = require('@axe-core/playwright');
const localRequire = createRequire(import.meta.url);
const { evaluateAxeAudit } = localRequire('../../out/test/support/a11y_evaluator.js');

export const A11Y_THEMES = {
  dark: {
    '--vscode-font-family': 'system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
    '--vscode-font-size': '13px',
    '--vscode-foreground': '#cccccc',
    '--vscode-panel-background': '#1e1e1e',
    '--vscode-panel-border': '#333333',
    '--vscode-editor-background': '#1e1e1e',
    '--vscode-editor-foreground': '#cccccc',
    '--vscode-focusBorder': '#007fd4',
    '--vscode-textLink-foreground': '#3794ff',
    '--vscode-textLink-activeForeground': '#3794ff',
    '--vscode-descriptionForeground': '#8b949e',
    '--vscode-button-background': '#0e639c',
    '--vscode-button-foreground': '#ffffff',
    '--vscode-button-hoverBackground': '#1177bb',
    '--vscode-button-secondaryBackground': '#3a3d41',
    '--vscode-button-secondaryForeground': '#ffffff',
    '--vscode-button-secondaryHoverBackground': '#45494e',
    '--vscode-input-background': '#3c3c3c',
    '--vscode-input-foreground': '#cccccc',
    '--vscode-input-border': '#3c3c3c',
    '--vscode-editorWarning-foreground': '#cca700',
    '--vscode-editorWarning-background': 'rgba(204,167,0,0.1)',
    '--vscode-errorForeground': '#f14c4c',
    '--vscode-editorError-foreground': '#f14c4c',
    '--vscode-badge-background': '#4d4d4d',
    '--vscode-badge-foreground': '#ffffff',
    '--vscode-editorWidget-background': '#252526',
    '--vscode-sideBarSectionHeader-background': 'rgba(128,128,128,0.15)',
    '--vscode-sideBarSectionHeader-foreground': '#cccccc',
    '--vscode-editor-lineHighlightBackground': 'rgba(128,128,128,0.08)',
  },
  light: {
    '--vscode-font-family': 'system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
    '--vscode-font-size': '13px',
    '--vscode-foreground': '#616161',
    '--vscode-panel-background': '#ffffff',
    '--vscode-panel-border': '#e5e5e5',
    '--vscode-editor-background': '#ffffff',
    '--vscode-editor-foreground': '#616161',
    '--vscode-focusBorder': '#0090f1',
    '--vscode-textLink-foreground': '#006ab1',
    '--vscode-textLink-activeForeground': '#006ab1',
    '--vscode-descriptionForeground': '#717171',
    '--vscode-button-background': '#007acc',
    '--vscode-button-foreground': '#ffffff',
    '--vscode-button-hoverBackground': '#0062a3',
    '--vscode-button-secondaryBackground': '#5f6a79',
    '--vscode-button-secondaryForeground': '#ffffff',
    '--vscode-button-secondaryHoverBackground': '#4d5664',
    '--vscode-input-background': '#ffffff',
    '--vscode-input-foreground': '#616161',
    '--vscode-input-border': '#cecece',
    '--vscode-editorWarning-foreground': '#b8860b',
    '--vscode-editorWarning-background': 'rgba(184,134,11,0.1)',
    '--vscode-errorForeground': '#e51400',
    '--vscode-editorError-foreground': '#e51400',
    '--vscode-badge-background': '#c4c4c4',
    '--vscode-badge-foreground': '#333333',
    '--vscode-editorWidget-background': '#f3f3f3',
    '--vscode-sideBarSectionHeader-background': 'rgba(0,0,0,0.05)',
    '--vscode-sideBarSectionHeader-foreground': '#333333',
    '--vscode-editor-lineHighlightBackground': 'rgba(0,0,0,0.04)',
  },
  highContrast: {
    '--vscode-font-family': 'system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
    '--vscode-font-size': '13px',
    '--vscode-foreground': '#ffffff',
    '--vscode-panel-background': '#000000',
    '--vscode-panel-border': '#6fc3df',
    '--vscode-editor-background': '#000000',
    '--vscode-editor-foreground': '#ffffff',
    '--vscode-focusBorder': '#f38518',
    '--vscode-contrastBorder': '#6fc3df',
    '--vscode-textLink-foreground': '#3794ff',
    '--vscode-textLink-activeForeground': '#3794ff',
    '--vscode-descriptionForeground': '#ffffff',
    '--vscode-button-background': '#000000',
    '--vscode-button-foreground': '#ffffff',
    '--vscode-button-border': '#6fc3df',
    '--vscode-button-hoverBackground': '#000000',
    '--vscode-button-secondaryBackground': '#000000',
    '--vscode-button-secondaryForeground': '#ffffff',
    '--vscode-button-secondaryHoverBackground': '#000000',
    '--vscode-input-background': '#000000',
    '--vscode-input-foreground': '#ffffff',
    '--vscode-input-border': '#6fc3df',
    '--vscode-editorWarning-foreground': '#00ffff',
    '--vscode-editorWarning-background': 'rgba(0,255,255,0.1)',
    '--vscode-errorForeground': '#ff0000',
    '--vscode-editorError-foreground': '#ff0000',
    '--vscode-badge-background': '#000000',
    '--vscode-badge-foreground': '#ffffff',
    '--vscode-editorWidget-background': '#000000',
    '--vscode-sideBarSectionHeader-background': '#000000',
    '--vscode-sideBarSectionHeader-foreground': '#ffffff',
    '--vscode-editor-lineHighlightBackground': 'rgba(255,255,255,0.1)',
  }
};

export const ALL_A11Y_THEME_KEYS = Array.from(
  new Set(Object.values(A11Y_THEMES).flatMap(theme => Object.keys(theme)))
);

export async function injectA11yTheme(page, themeVars) {
  await page.evaluate(({ allKeys, vars }) => {
    for (const k of allKeys) {
      document.documentElement.style.removeProperty(k);
    }
    for (const [k, v] of Object.entries(vars)) {
      document.documentElement.style.setProperty(k, v);
    }
  }, { allKeys: ALL_A11Y_THEME_KEYS, vars: themeVars });
}

export async function runA11yStateAudit(page, { stateId, reverseThemes = false } = {}) {
  const viewports = [[320, 600], [420, 700]];
  const themeEntries = reverseThemes
    ? Object.entries(A11Y_THEMES).slice().reverse()
    : Object.entries(A11Y_THEMES);

  for (const [themeName, themeVars] of themeEntries) {
    await injectA11yTheme(page, themeVars);

    // Verify no highContrast residuals leaked into dark or light (§5.8 Item A)
    if (themeName !== 'highContrast') {
      const lingeringBorders = await page.evaluate(() => {
        const style = document.documentElement.style;
        return {
          contrastBorder: style.getPropertyValue('--vscode-contrastBorder'),
          buttonBorder: style.getPropertyValue('--vscode-button-border'),
        };
      });
      assert.equal(
        lingeringBorders.contrastBorder,
        '',
        `[${stateId}] Leaked --vscode-contrastBorder lingering in ${themeName} theme`
      );
      assert.equal(
        lingeringBorders.buttonBorder,
        '',
        `[${stateId}] Leaked --vscode-button-border lingering in ${themeName} theme`
      );
    }

    for (const [w, h] of viewports) {
      await page.setViewportSize({ width: w, height: h });
      const rawResults = await new AxeBuilder({ page }).analyze();

      const evaluation = evaluateAxeAudit(
        rawResults,
        { stateId, themeName, viewport: { width: w, height: h } }
      );

      console.log(`    [a11y] ${evaluation.summary}`);

      assert.equal(
        evaluation.passed,
        true,
        `[${stateId}] Accessibility audit failed in ${themeName} ${w}x${h}:\n${evaluation.errorMessages.join('\n')}`
      );
    }
  }
}
