/**
 * Structured file navigation extracted from tool contracts.
 *
 * Invariant: only builtins and verified IDE companion tools with confirmed file contracts
 * are navigated. Directories (`list`), glob patterns (`glob`), commands (`bash`), and
 * queries (`grep`) are NEVER treated as files.
 *
 * Line navigation is only included when the tool contract specifies a verified start line
 * (`offset` for `read`, `at` for `edit`, `line` for `show`/`mcp__*__show`).
 *
 * Arbitrary MCP servers without verified declarations must NEVER be guessed or linked.
 */

export interface FileNav {
  path: string;
  line?: number;
}

/**
 * Verified file tools and their line parameter names.
 *
 * Only builtins and verified IDE companion tools (JetBrains / VS Code hand servers)
 * with confirmed file schemas are allowed.
 */
const VERIFIED_FILE_TOOLS: Record<string, string | null> = {
  read: 'offset',
  edit: 'at',
  show: 'line',
  write: null,
  multiedit: null,
  apply_edit: null,
  mcp__vscode__show: 'line',
  mcp__vscode__apply_edit: null,
  mcp__jetbrains__show: 'line',
  mcp__jetbrains__apply_edit: null,
};

/** Parse a positive 1-based integer line number from number or string. */
export function parsePositiveInteger(val: unknown): number | undefined {
  if (typeof val === 'number') {
    return Number.isInteger(val) && val > 0 ? val : undefined;
  }
  if (typeof val === 'string') {
    const s = val.trim();
    if (/^\d+$/.test(s)) {
      const n = Number.parseInt(s, 10);
      return n > 0 ? n : undefined;
    }
  }
  return undefined;
}

/** Parse arguments payload (Record or JSON string) safely. */
export function parseToolArgs(args: unknown): Record<string, unknown> | undefined {
  if (!args) return undefined;
  if (typeof args === 'object' && !Array.isArray(args)) {
    return args as Record<string, unknown>;
  }
  if (typeof args === 'string') {
    const s = args.trim();
    if (!s || (!s.startsWith('{') && !s.startsWith('['))) return undefined;
    try {
      const parsed = JSON.parse(s);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>;
      }
    } catch {
      return undefined;
    }
  }
  return undefined;
}

/**
 * Extract structured file and line navigation from a tool call.
 *
 * Returns undefined if:
 * - Tool is not a verified file tool (e.g. list, glob, bash, grep, or unknown MCP servers)
 * - Path is missing, empty, or not a string
 */
export function extractFileNav(toolName: string, args: unknown): FileNav | undefined {
  const norm = toolName.trim().toLowerCase();
  if (!Object.prototype.hasOwnProperty.call(VERIFIED_FILE_TOOLS, norm)) {
    return undefined;
  }

  const parsed = parseToolArgs(args);
  if (!parsed) return undefined;

  const rawPath = parsed.path;
  if (typeof rawPath !== 'string') return undefined;
  const path = rawPath.trim();
  if (!path) return undefined;

  const lineProp = VERIFIED_FILE_TOOLS[norm];
  const line = lineProp ? parsePositiveInteger(parsed[lineProp]) : undefined;

  return line !== undefined ? { path, line } : { path };
}

/**
 * Extract target file path for an approval request (permission ask).
 * Returns undefined if what is not a verified file tool or has no valid path.
 */
export function extractAskFilePath(ask: { what: string; args?: unknown }): string | undefined {
  const nav = extractFileNav(ask.what, ask.args);
  return nav?.path;
}
