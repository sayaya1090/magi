import * as fs from 'fs';
import * as nodePath from 'path';
import { inside, resolvePath } from './hand';
import { Event } from './protocol';
import { rows } from './transcript';
import { pendingAsk } from './touched';
import { AskStore } from './diff';
import { extractAskFilePath } from './nav_tool';

export * from './nav_tool';

export interface FileOpener {
  openDocument(absPath: string, line?: number): Promise<{ opened: boolean; line?: number }>;
}

export interface DiffOpener {
  openDiff(workdir: string, sessionId: string, ask: any): Promise<boolean | void>;
}

export interface ActionTargetRequest {
  session?: string;
  callId?: string;
  seq?: number;
}

export interface ActionOriginOptions {
  m: ActionTargetRequest;
  session: string;
  companionWorkdir: string;
  companionState?: string;
  asks: AskStore;
  events: Event[];
  postNote: (text: string) => void;
}

export type ActionOrigin =
  | { kind: 'tool'; row: ReturnType<typeof rows>[number]; companionWorkdir: string }
  | { kind: 'ask'; ask: any; companionWorkdir: string; sessionId: string };

/**
 * Shared origin and session validation for file navigation and diff inspection.
 * Rejects remote companions, missing/mismatched sessions, missing call IDs, and expired asks.
 */
export function resolveActionOrigin(opts: ActionOriginOptions): ActionOrigin | undefined {
  const {
    m,
    session,
    companionWorkdir,
    companionState,
    asks,
    events,
    postNote,
  } = opts;

  if (companionState === 'remote') {
    postNote('원격 컴패니언의 파일은 로컬 매핑이 없어 열 수 없습니다.');
    return undefined;
  }

  if (!m.session) {
    postNote('세션 식별자가 누락된 이동 요청은 열 수 없습니다.');
    return undefined;
  }
  if (m.session !== session) {
    postNote('이전 세션의 요청은 현재 세션에서 열 수 없습니다.');
    return undefined;
  }

  if (m.seq !== undefined) {
    if (!m.callId) {
      postNote('도구 호출 식별자가 누락된 이동 요청은 열 수 없습니다.');
      return undefined;
    }
    const row = rows(events).find((r) => r.seq === m.seq);
    if (!row) {
      postNote('현재 세션에서 해당 행을 찾을 수 없습니다.');
      return undefined;
    }
    if (row.callId !== m.callId) {
      postNote('이전 세션의 도구 요청은 현재 세션에서 열 수 없습니다.');
      return undefined;
    }
    return { kind: 'tool', row, companionWorkdir };
  }

  if (!m.callId) {
    postNote('도구 호출 식별자가 누락된 이동 요청은 열 수 없습니다.');
    return undefined;
  }

  const stored = asks.get(m.callId);
  if (stored) {
    if (stored.sessionId !== session || m.session !== stored.sessionId) {
      postNote('이전 세션의 승인 요청은 현재 세션에서 열 수 없습니다.');
      return undefined;
    }
    return { kind: 'ask', ask: stored.ask, companionWorkdir: stored.companionId, sessionId: stored.sessionId };
  }

  const raw = pendingAsk(events);
  if (raw && raw.callId === m.callId) {
    return { kind: 'ask', ask: raw, companionWorkdir, sessionId: session };
  }

  postNote('만료되었거나 유효하지 않은 승인 요청입니다.');
  return undefined;
}

export type OpenTargetRequest = ActionTargetRequest;

export interface ResolveAndOpenOptions extends ActionOriginOptions {
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
 *
 * Invariant: Requests from old/replaced sessions or mismatched call IDs are rejected.
 * Missing files are NEVER created. Directory paths are rejected.
 */
export async function resolveAndOpenFile(opts: ResolveAndOpenOptions): Promise<boolean> {
  const origin = resolveActionOrigin(opts);
  if (!origin) return false;

  let targetPath: string | undefined;
  let line: number | undefined;
  let targetWorkdir = origin.companionWorkdir;

  if (origin.kind === 'tool') {
    if (!origin.row.fileNav) {
      opts.postNote('해당 도구 행에는 파일 이동 정보가 없습니다.');
      return false;
    }
    targetPath = origin.row.fileNav.path;
    line = origin.row.fileNav.line;
  } else {
    targetPath = extractAskFilePath(origin.ask);
    if (!targetPath) {
      opts.postNote('승인 요청에서 유효한 파일 경로를 찾을 수 없습니다.');
      return false;
    }
  }

  const {
    postNote,
    opener,
    pathLib = nodePath,
    fsLib = fs,
  } = opts;

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

export interface ResolveAndOpenDiffOptions extends ActionOriginOptions {
  opener: DiffOpener;
}

export async function resolveAndOpenDiff(opts: ResolveAndOpenDiffOptions): Promise<boolean> {
  const origin = resolveActionOrigin(opts);
  if (!origin) return false;

  if (origin.kind !== 'ask') {
    opts.postNote('승인 요청이 아닌 대상은 비교 화면을 열 수 없습니다.');
    return false;
  }

  try {
    await opts.opener.openDiff(origin.companionWorkdir, origin.sessionId, origin.ask);
    return true;
  } catch (err) {
    opts.postNote(`비교 화면 열기 실패: ${(err as Error).message}`);
    return false;
  }
}
