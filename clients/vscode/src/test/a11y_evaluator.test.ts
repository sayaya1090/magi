import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import {
  evaluateAxeAudit,
  type AxeAuditRawResult,
  type A11yAuditContext,
} from './support/a11y_evaluator';

test('evaluateAxeAudit: clean result passes with zero violations and incompletes', () => {
  const raw: AxeAuditRawResult = {
    violations: [],
    incomplete: [],
  };
  const ctx: A11yAuditContext = {
    stateId: 'state_1_conversation',
    themeName: 'dark',
    viewport: { width: 420, height: 700 },
  };

  const res = evaluateAxeAudit(raw, ctx);
  assert.equal(res.passed, true);
  assert.equal(res.violations.length, 0);
  assert.equal(res.incompletes.length, 0);
  assert.equal(res.errorMessages.length, 0);
  assert.match(res.summary, /violations=0, incomplete=0/);
});

test('evaluateAxeAudit: unexpected violation causes failure', () => {
  const raw: AxeAuditRawResult = {
    violations: [
      {
        id: 'color-contrast',
        impact: 'serious',
        description: 'Ensures the contrast between foreground and background colors meets WCAG 2 AA contrast ratio thresholds',
        nodes: [
          {
            target: ['#say'],
            html: '<textarea id="say"></textarea>',
            failureSummary: 'Fix element contrast',
          },
        ],
      },
    ],
    incomplete: [],
  };
  const ctx: A11yAuditContext = {
    stateId: 'state_1_conversation',
    themeName: 'light',
    viewport: { width: 420, height: 700 },
  };

  const res = evaluateAxeAudit(raw, ctx);
  assert.equal(res.passed, false);
  assert.equal(res.violations.length, 1);
  assert.match(res.errorMessages[0], /color-contrast/);
  assert.match(res.errorMessages[0], /#say/);
  assert.match(res.errorMessages[0], /failureSummary: Fix element contrast/);
});

test('evaluateAxeAudit: unexpected incomplete check causes failure', () => {
  const raw: AxeAuditRawResult = {
    violations: [],
    incomplete: [
      {
        id: 'color-contrast',
        impact: 'moderate',
        description: 'Element has background image that needs manual review',
        nodes: [
          {
            target: ['.hero-banner'],
            html: '<div class="hero-banner"></div>',
            failureSummary: 'Check background manually',
          },
        ],
      },
    ],
  };
  const ctx: A11yAuditContext = {
    stateId: 'state_2_multiple_choice',
    themeName: 'dark',
    viewport: { width: 320, height: 600 },
  };

  const res = evaluateAxeAudit(raw, ctx);
  assert.equal(res.passed, false);
  assert.equal(res.incompletes.length, 1);
  assert.match(res.errorMessages[0], /incomplete/i);
  assert.match(res.errorMessages[0], /\.hero-banner/);
  assert.match(res.errorMessages[0], /failureSummary: Check background manually/);
});
