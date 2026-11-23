/**
 * The tab row over the right-hand place (user decision, 2026-11-23). The first tab is the
 * conversation's title; each RUNNING subagent gets a tab beside it. When a subagent finishes (or
 * drops out of the list) its tab closes — unless it is the tab being looked at, which stays until
 * the person moves to another tab, so a result or a failure reason is not lost mid-read.
 * The same rule as JetBrains' SubagentTabs and the web console.
 */
export interface ChildJobLike { id: string; task?: string; running?: boolean }
export interface SubagentTab { id: string; label: string; running: boolean }

const TAB_MAX = 24;
const SESSION_MAX = 32;

function firstLine(s: string | undefined): string | undefined {
  return (s ?? '').split('\n').map((l) => l.trim()).find((l) => l !== '');
}

function clip(s: string | undefined, max: number): string | undefined {
  if (!s) return undefined;
  return s.length > max ? s.slice(0, max - 1) + '…' : s;
}

export function tabLabel(job: ChildJobLike): string {
  return clip(firstLine(job.task), TAB_MAX) ?? '·' + job.id.slice(-6);
}

export function sessionLabel(title: string | undefined, fallback: string): string {
  return clip(firstLine(title), SESSION_MAX) ?? fallback;
}

export function nextTabs(open: SubagentTab[], children: ChildJobLike[], selected: string | null): SubagentTab[] {
  const running = new Map<string, ChildJobLike>();
  for (const c of children) if (c.running && c.id) running.set(c.id, c);
  const out: SubagentTab[] = [];
  for (const t of open) {
    const now = running.get(t.id);
    if (now) out.push({ id: t.id, label: tabLabel(now), running: true });
    else if (t.id === selected) out.push({ ...t, running: false });
  }
  for (const [id, job] of running) if (!out.some((t) => t.id === id)) out.push({ id, label: tabLabel(job), running: true });
  return out;
}
