/**
 * Accessibility audit result evaluator for axe-core.
 * Classifies violations, incompletes, and strict allowed exceptions.
 */

export interface AxeNodeResult {
  target: string[];
  html?: string;
  failureSummary?: string;
  [key: string]: unknown;
}

export interface AxeRuleResult {
  id: string;
  impact?: string | null;
  description: string;
  help?: string;
  helpUrl?: string;
  nodes: AxeNodeResult[];
  [key: string]: unknown;
}

export interface AxeAuditRawResult {
  violations?: AxeRuleResult[];
  incomplete?: AxeRuleResult[];
  passes?: AxeRuleResult[];
  inapplicable?: AxeRuleResult[];
}

export interface A11yAuditContext {
  stateId: string;
  themeName: string;
  viewport: { width: number; height: number };
}

export interface AllowedA11yException {
  ruleId: string;
  targetSelector: string;
  stateId?: string;
  themeName?: string;
  reason: string;
}

export interface A11yAuditRecord {
  stateId: string;
  themeName: string;
  viewport: { width: number; height: number };
  type: 'violation' | 'incomplete';
  ruleId: string;
  impact: string | null;
  description: string;
  target: string[];
  html?: string;
  failureSummary?: string;
  isAllowedException: boolean;
  exceptionReason?: string;
}

export interface A11yAuditEvaluation {
  passed: boolean;
  allRecords: A11yAuditRecord[];
  violations: A11yAuditRecord[];
  incompletes: A11yAuditRecord[];
  allowedExceptions: A11yAuditRecord[];
  unexpectedViolations: A11yAuditRecord[];
  unexpectedIncompletes: A11yAuditRecord[];
  summary: string;
  errorMessages: string[];
}

function escapeRegex(str: string): string {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * Matches target selector tokens strictly to prevent loose substring false positives.
 * E.g., selector '.ask-status' must match '.ask-status' or '#parent > .ask-status',
 * but MUST NOT match '.ask-status-other'.
 */
export function matchesTargetSelector(target: string | string[], selector: string): boolean {
  const targetStr = Array.isArray(target) ? target.join(' ') : target;
  const trimmed = selector.trim();
  if (!trimmed || !targetStr) return false;

  // Single class selector, e.g. '.ask-status'
  if (trimmed.startsWith('.') && !trimmed.includes(' ') && !trimmed.includes('>') && !trimmed.includes('#')) {
    const className = trimmed.slice(1);
    const regex = new RegExp(`\\.${escapeRegex(className)}(?![a-zA-Z0-9_-])`);
    return regex.test(targetStr);
  }

  // Single id selector, e.g. '#reply-target'
  if (trimmed.startsWith('#') && !trimmed.includes(' ') && !trimmed.includes('>') && !trimmed.includes('.')) {
    const idName = trimmed.slice(1);
    const regex = new RegExp(`#${escapeRegex(idName)}(?![a-zA-Z0-9_-])`);
    return regex.test(targetStr);
  }

  // Multi-part selector, e.g. '#reply-mode .reply-tag'
  const parts = trimmed.split(/\s+/).filter(Boolean);
  if (parts.length > 1) {
    return parts.every(part => matchesTargetSelector(targetStr, part));
  }

  // Exact fallback
  return targetStr === trimmed;
}

/**
 * Evaluates raw axe-core audit results against context and documented allowed exceptions.
 */
export function evaluateAxeAudit(
  rawResult: AxeAuditRawResult,
  context: A11yAuditContext,
  allowedExceptions: AllowedA11yException[] = []
): A11yAuditEvaluation {
  const violations: A11yAuditRecord[] = [];
  const incompletes: A11yAuditRecord[] = [];
  const matchedExceptions: A11yAuditRecord[] = [];
  const unexpectedViolations: A11yAuditRecord[] = [];
  const unexpectedIncompletes: A11yAuditRecord[] = [];
  const errorMessages: string[] = [];

  const rawViolations = rawResult.violations || [];
  for (const v of rawViolations) {
    const nodes = v.nodes || [];
    for (const node of nodes) {
      const matchedEx = allowedExceptions.find(ex => {
        if (ex.ruleId !== v.id) return false;
        if (ex.stateId && ex.stateId !== context.stateId) return false;
        if (ex.themeName && ex.themeName !== context.themeName) return false;
        return matchesTargetSelector(node.target || [], ex.targetSelector);
      });

      const record: A11yAuditRecord = {
        stateId: context.stateId,
        themeName: context.themeName,
        viewport: context.viewport,
        type: 'violation',
        ruleId: v.id,
        impact: v.impact ?? null,
        description: v.description,
        target: node.target || [],
        html: node.html,
        failureSummary: node.failureSummary,
        isAllowedException: !!matchedEx,
        exceptionReason: matchedEx?.reason,
      };

      violations.push(record);
      if (matchedEx) {
        matchedExceptions.push(record);
      } else {
        unexpectedViolations.push(record);
        errorMessages.push(
          `[${context.stateId}] Unexpected violation in ${context.themeName} (${context.viewport.width}x${context.viewport.height}) - ` +
          `Rule: ${v.id} (${v.impact || 'unknown'}): ${v.description} at target [${(node.target || []).join(' ')}]`
        );
      }
    }
  }

  const rawIncompletes = rawResult.incomplete || [];
  for (const inc of rawIncompletes) {
    const nodes = inc.nodes || [];
    for (const node of nodes) {
      const matchedEx = allowedExceptions.find(ex => {
        if (ex.ruleId !== inc.id) return false;
        if (ex.stateId && ex.stateId !== context.stateId) return false;
        if (ex.themeName && ex.themeName !== context.themeName) return false;
        return matchesTargetSelector(node.target || [], ex.targetSelector);
      });

      const record: A11yAuditRecord = {
        stateId: context.stateId,
        themeName: context.themeName,
        viewport: context.viewport,
        type: 'incomplete',
        ruleId: inc.id,
        impact: inc.impact ?? null,
        description: inc.description,
        target: node.target || [],
        html: node.html,
        failureSummary: node.failureSummary,
        isAllowedException: !!matchedEx,
        exceptionReason: matchedEx?.reason,
      };

      incompletes.push(record);
      if (matchedEx) {
        matchedExceptions.push(record);
      } else {
        unexpectedIncompletes.push(record);
        errorMessages.push(
          `[${context.stateId}] Unexpected incomplete check in ${context.themeName} (${context.viewport.width}x${context.viewport.height}) - ` +
          `Rule: ${inc.id} (${inc.impact || 'unknown'}): ${inc.description} at target [${(node.target || []).join(' ')}]`
        );
      }
    }
  }

  const passed = unexpectedViolations.length === 0 && unexpectedIncompletes.length === 0;
  const summary = `[${context.stateId}] ${context.themeName} ${context.viewport.width}x${context.viewport.height}: ` +
    `violations=${violations.length} (allowed=${matchedExceptions.filter(e => e.type === 'violation').length}, unexpected=${unexpectedViolations.length}), ` +
    `incomplete=${incompletes.length} (allowed=${matchedExceptions.filter(e => e.type === 'incomplete').length}, unexpected=${unexpectedIncompletes.length})`;

  return {
    passed,
    allRecords: [...violations, ...incompletes],
    violations,
    incompletes,
    allowedExceptions: matchedExceptions,
    unexpectedViolations,
    unexpectedIncompletes,
    summary,
    errorMessages,
  };
}
