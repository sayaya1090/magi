/**
 * Accessibility audit result evaluator for axe-core.
 * Evaluates violations and incomplete checks with a zero-tolerance policy.
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
}

export interface A11yAuditEvaluation {
  passed: boolean;
  allRecords: A11yAuditRecord[];
  violations: A11yAuditRecord[];
  incompletes: A11yAuditRecord[];
  summary: string;
  errorMessages: string[];
}

/**
 * Evaluates raw axe-core audit results against context with zero-tolerance policy.
 * Any violation or incomplete check results in a failed evaluation.
 */
export function evaluateAxeAudit(
  rawResult: AxeAuditRawResult,
  context: A11yAuditContext
): A11yAuditEvaluation {
  const violations: A11yAuditRecord[] = [];
  const incompletes: A11yAuditRecord[] = [];
  const errorMessages: string[] = [];

  const rawViolations = rawResult.violations || [];
  for (const v of rawViolations) {
    const nodes = v.nodes || [];
    for (const node of nodes) {
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
      };
      violations.push(record);
      errorMessages.push(
        `[${context.stateId}] Violation in ${context.themeName} (${context.viewport.width}x${context.viewport.height}) - ` +
        `Rule: ${v.id} (${v.impact || 'unknown'}): ${v.description} at target [${(node.target || []).join(' ')}]` +
        (node.failureSummary ? ` - failureSummary: ${node.failureSummary}` : '')
      );
    }
  }

  const rawIncompletes = rawResult.incomplete || [];
  for (const inc of rawIncompletes) {
    const nodes = inc.nodes || [];
    for (const node of nodes) {
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
      };
      incompletes.push(record);
      errorMessages.push(
        `[${context.stateId}] Incomplete check in ${context.themeName} (${context.viewport.width}x${context.viewport.height}) - ` +
        `Rule: ${inc.id} (${inc.impact || 'unknown'}): ${inc.description} at target [${(node.target || []).join(' ')}]` +
        (node.failureSummary ? ` - failureSummary: ${node.failureSummary}` : '')
      );
    }
  }

  const passed = violations.length === 0 && incompletes.length === 0;
  const summary = `[${context.stateId}] ${context.themeName} ${context.viewport.width}x${context.viewport.height}: ` +
    `violations=${violations.length}, incomplete=${incompletes.length}`;

  return {
    passed,
    allRecords: [...violations, ...incompletes],
    violations,
    incompletes,
    summary,
    errorMessages,
  };
}
