import * as fs from 'fs';
import * as nodePath from 'path';
import { inside, resolvePath } from './hand';
import { Event } from './protocol';
import { rows } from './transcript';
import { pendingAsk } from './touched';
import { AskStore } from './diff';

/**
 * Structured file navigation extracted from tool contracts.
 *
 * Invariant: only tools with verified file paths in their schemas/contracts are navigated.
 * Directories (such as `list`), glob patterns (`glob`), commands (`bash`), and queries (`grep`)
 * are NEVER treated as files.
 *
 * Line navigation is only included when the tool contract specifies a verified start line
 * (`offset` for `read`, `at` for `edit`, `line` for `show`).
 */

export interface FileNav {
  path: string;
  line?: number;
}

/** Supported file tools and their line parameter names. */
const FILE_TOOLS: Record<string, string | null> = {
  read: 'offset',
  edit: 'at',
  show: 'line',
  write: null,
  multiedit: null,
  apply_edit: null,
};

/** Normalize tool name by stripping MCP prefix and trimming. */
export function normalizeToolName(name: string): string {
  const trimmed = name.trim().toLowerCase();
  return trimmed.replace(/^mcp__.*?__/, '');
}

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
 * - Tool is not a recognized file tool (e.g. list, glob, bash, grep, problems)
 * - Path is missing, empty, or not a string
 */
export function extractFileNav(toolName: string, args: unknown): FileNav | undefined {
  const norm = normalizeToolName(toolName);
  if (!Object.prototype.hasOwnProperty.call(FILE_TOOLS, norm)) {
    return undefined;
  }

  const parsed = parseToolArgs(args);
  if (!parsed) return undefined;

  const rawPath = parsed.path;
  if (typeof rawPath !== 'string') return undefined;
  const path = rawPath.trim();
  if (!path) return undefined;

  const lineProp = FILE_TOOLS[norm];
  const line = lineProp ? parsePositiveInteger(parsed[lineProp]) : undefined;

  return line !== undefined ? { path, line } : { path };
}

/**
 * Extract target file path for an approval request (permission ask).
 * Returns undefined if what is not a file tool or has no valid path.
 */
export function extractAskFilePath(ask: { what: string; args?: unknown }): string | undefined {
  const nav = extractFileNav(ask.what, ask.args);
  return nav?.path;
}

export interface FileOpener {
  openDocument(absPath: string, line?: number): Promise<{ opened: boolean; line?: number }>;
}

export interface OpenTargetRequest {
  callId?: string;
  seq?: number;
}

export interface ResolveAndOpenOptions {
  m: OpenTargetRequest;
  session: string;
  companionWorkdir: string;
  companionState?: string;
  asks: AskStore;
  events: Event[];
  postNote: (text: string) => void;
  opener: FileOpener;
  pathLib?: typeof nodePath;
  fsLib?: {
    existsSync(p: string): boolean;
    statSync(p: string): { isDirectory(): boolean };
  };
}

/**
 * Validate navigation request, resolve path within workspace boundary, verify existence,
 * and navigate via opener.
 */
export async function resolveAndOpenFile(opts: ResolveAndOpenOptions): Promise<boolean> {
  const {
    m,
    session,
    companionWorkdir,
    companionState,
    asks,
    events,
    postNote,
    opener,
    pathLib = nodePath,
    fsLib = fs,
  } = opts;

  if (companionState === 'remote') {
    postNote('원격 컴패니언의 파일은 로컬 매핑이 없어 열 수 없습니다.');
    return false;
  }

  let targetPath: string | undefined;
  let line: number | undefined;
  let targetWorkdir = companionWorkdir;

  if (m.callId) {
    const stored = asks.get(m.callId);
    if (stored) {
      if (stored.sessionId !== session) {
        postNote('이전 세션의 승인 요청은 현재 세션에서 열 수 없습니다.');
        return false;
      }
      targetWorkdir = stored.companionId;
      targetPath = extractAskFilePath(stored.ask);
    } else {
      const raw = pendingAsk(events);
      if (raw && raw.callId === m.callId) {
        targetWorkdir = companionWorkdir;
        targetPath = extractAskFilePath(raw);
      }
    }
    if (!targetPath) {
      postNote('승인 요청에서 유효한 파일 경로를 찾을 수 없습니다.');
      return false;
    }
  } else if (m.seq !== undefined) {
    const row = rows(events).find((r) => r.seq === m.seq);
    if (!row) {
      postNote('현재 세션에서 해당 행을 찾을 수 없습니다.');
      return false;
    }
    if (!row.fileNav) {
      postNote('해당 도구 행에는 파일 이동 정보가 없습니다.');
      return false;
    }
    targetPath = row.fileNav.path;
    line = row.fileNav.line;
  } else {
    return false;
  }

  if (!targetWorkdir) {
    postNote('워크스페이스 경로를 확인할 수 없습니다.');
    return false;
  }

  if (!inside(targetWorkdir, targetPath, pathLib)) {
    postNote(`워크스페이스 외부 경로는 열 수 없습니다: ${targetPath}`);
    return false;
  }

  let absPath: string;
  try {
    absPath = resolvePath(targetWorkdir, targetPath, pathLib);
  } catch (err) {
    postNote(`경로 해석 실패: ${(err as Error).message}`);
    return false;
  }

  if (!fsLib.existsSync(absPath)) {
    postNote(`파일이 존재하지 않습니다: ${targetPath}`);
    return false;
  }

  try {
    const stat = fsLib.statSync(absPath);
    if (stat.isDirectory()) {
      postNote(`디렉터리는 파일로 열 수 없습니다: ${targetPath}`);
      return false;
    }
  } catch (err) {
    postNote(`파일 상태 확인 실패: ${(err as Error).message}`);
    return false;
  }

  try {
    const res = await opener.openDocument(absPath, line);
    if (line !== undefined && res.line !== undefined && res.line !== line) {
      postNote(`${targetPath}의 마지막 줄(${res.line}행)로 이동했습니다 (요청: ${line}행).`);
    }
    return res.opened;
  } catch (err) {
    postNote(`파일 열기 실패: ${(err as Error).message}`);
    return false;
  }
}
