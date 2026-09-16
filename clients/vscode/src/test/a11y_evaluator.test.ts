import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import {
  evaluateAxeAudit,
  matchesTargetSelector,
  type AxeAuditRawResult,
  type A11yAuditContext,
  type AllowedA11yException,
} from '../core/a11y_evaluator';

test('matchesTargetSelector: strict token matching without substring false positives', () => {
  // Class selector
  assert.equal(matchesTargetSelector(['.ask-status'], '.ask-status'), true);
  assert.equal(matchesTargetSelector(['#ask-controls > .ask-status'], '.ask-status'), true);
  assert.equal(matchesTargetSelector(['div.ask-status'], '.ask-status'), true);
  assert.equal(matchesTargetSelector(['#controls > div.ask-status.active'], '.ask-status'), true);

  // Must reject prefix/suffix extended classes
  assert.equal(matchesTargetSelector(['.ask-status-other'], '.ask-status'), false);
  assert.equal(matchesTargetSelector(['#ask-controls > .ask-status_extra'], '.ask-status'), false);
  assert.equal(matchesTargetSelector(['.pre-ask-status'], '.ask-status'), false);

  // ID selector
  assert.equal(matchesTargetSelector(['#reply-target'], '#reply-target'), true);
  assert.equal(matchesTargetSelector(['#reply-mode > #reply-target'], '#reply-target'), true);
  assert.equal(matchesTargetSelector(['#reply-target-other'], '#reply-target'), false);
  assert.equal(matchesTargetSelector(['#pre-reply-target'], '#reply-target'), false);

  // Multi-part selector
  assert.equal(matchesTargetSelector(['#reply-mode > span.reply-tag'], '#reply-mode .reply-tag'), true);
  assert.equal(matchesTargetSelector(['#reply-mode-other > span.reply-tag'], '#reply-mode .reply-tag'), false);
  assert.equal(matchesTargetSelector(['#reply-mode > span.reply-tag-other'], '#reply-mode .reply-tag'), false);
});

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

  const res = evaluateAxeAudit(raw, ctx, []);
  assert.equal(res.passed, true);
  assert.equal(res.violations.length, 0);
  assert.equal(res.incompletes.length, 0);
  assert.equal(res.unexpectedViolations.length, 0);
  assert.equal(res.unexpectedIncompletes.length, 0);
  assert.equal(res.errorMessages.length, 0);
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

  const res = evaluateAxeAudit(raw, ctx, []);
  assert.equal(res.passed, false);
  assert.equal(res.violations.length, 1);
  assert.equal(res.unexpectedViolations.length, 1);
  assert.equal(res.allowedExceptions.length, 0);
  assert.match(res.errorMessages[0], /color-contrast/);
  assert.match(res.errorMessages[0], /#say/);
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

  const res = evaluateAxeAudit(raw, ctx, []);
  assert.equal(res.passed, false);
  assert.equal(res.incompletes.length, 1);
  assert.equal(res.unexpectedIncompletes.length, 1);
  assert.match(res.errorMessages[0], /incomplete/);
  assert.match(res.errorMessages[0], /\.hero-banner/);
});

test('evaluateAxeAudit: correctly matches documented allowed exception and passes', () => {
  const raw: AxeAuditRawResult = {
    violations: [
      {
        id: 'color-contrast',
        impact: 'serious',
        description: 'Contrast issue',
        nodes: [
          {
            target: ['#ask-controls > .ask-status'],
            html: '<span class="ask-status">전송 중...</span>',
          },
        ],
      },
    ],
    incomplete: [],
  };
  const ctx: A11yAuditContext = {
    stateId: 'state_6_inflight_question',
    themeName: 'light',
    viewport: { width: 420, height: 700 },
  };
  const allowedExceptions: AllowedA11yException[] = [
    {
      stateId: 'state_6_inflight_question',
      themeName: 'light',
      ruleId: 'color-contrast',
      targetSelector: '.ask-status',
      reason: 'Light theme status text contrast exception',
    },
  ];

  const res = evaluateAxeAudit(raw, ctx, allowedExceptions);
  assert.equal(res.passed, true);
  assert.equal(res.violations.length, 1);
  assert.equal(res.allowedExceptions.length, 1);
  assert.equal(res.unexpectedViolations.length, 0);
  assert.equal(res.allowedExceptions[0].exceptionReason, 'Light theme status text contrast exception');
});

test('evaluateAxeAudit: rejects violation when class is substring match (.ask-status-other vs .ask-status)', () => {
  const raw: AxeAuditRawResult = {
    violations: [
      {
        id: 'color-contrast',
        impact: 'serious',
        description: 'Contrast issue',
        nodes: [
          {
            target: ['#ask-controls > .ask-status-other'],
            html: '<span class="ask-status-other">전송 중...</span>',
          },
        ],
      },
    ],
    incomplete: [],
  };
  const ctx: A11yAuditContext = {
    stateId: 'state_6_inflight_question',
    themeName: 'light',
    viewport: { width: 420, height: 700 },
  };
  const allowedExceptions: AllowedA11yException[] = [
    {
      stateId: 'state_6_inflight_question',
      themeName: 'light',
      ruleId: 'color-contrast',
      targetSelector: '.ask-status',
      reason: 'Only .ask-status is permitted',
    },
  ];

  const res = evaluateAxeAudit(raw, ctx, allowedExceptions);
  assert.equal(res.passed, false);
  assert.equal(res.unexpectedViolations.length, 1);
  assert.equal(res.allowedExceptions.length, 0);
});

test('evaluateAxeAudit: restricts exception by theme and state', () => {
  const raw: AxeAuditRawResult = {
    violations: [
      {
        id: 'color-contrast',
        impact: 'serious',
        description: 'Contrast issue',
        nodes: [
          {
            target: ['.reply-tag'],
            html: '<span class="reply-tag">[답변]</span>',
          },
        ],
      },
    ],
    incomplete: [],
  };
  const allowedExceptions: AllowedA11yException[] = [
    {
      stateId: 'state_3_answer_mode',
      themeName: 'light',
      ruleId: 'color-contrast',
      targetSelector: '.reply-tag',
      reason: 'Light theme reply tag',
    },
  ];

  // Fails when theme is dark
  const darkRes = evaluateAxeAudit(raw, {
    stateId: 'state_3_answer_mode',
    themeName: 'dark',
    viewport: { width: 420, height: 700 },
  }, allowedExceptions);
  assert.equal(darkRes.passed, false);
  assert.equal(darkRes.unexpectedViolations.length, 1);

  // Fails when state is state_1_conversation
  const stateRes = evaluateAxeAudit(raw, {
    stateId: 'state_1_conversation',
    themeName: 'light',
    viewport: { width: 420, height: 700 },
  }, allowedExceptions);
  assert.equal(stateRes.passed, false);
  assert.equal(stateRes.unexpectedViolations.length, 1);
});
