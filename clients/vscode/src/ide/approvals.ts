import * as vscode from 'vscode';
import { Companion } from './workspace';
import { Waiting } from '../core/protocol';

type Pending = { session: string; requester: string; prompt: Waiting };

/** Workspace-wide approvals, including delegated chats that are not selected. */
export function approvalSupport(companion: Companion): vscode.Disposable {
  const t = vscode.l10n.t;
  let disposed = false;
  let reading = false;
  let pending: Pending[] = [];
  let seen = new Set<string>();
  const key = (p: Pending) => JSON.stringify([p.session, p.prompt.id]);
  const badge = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 90);
  badge.command = 'magi.approvals';
  badge.tooltip = t('Review pending delegated approvals and questions');
  const read = async () => {
    if (disposed || reading) return;
    reading = true;
    try {
      const r = await companion.ask('jobs');
      if (disposed || !r?.ok) return;
      pending = r.jobs?.approvals ?? [];
      const current = new Set(pending.map(key));
      seen = new Set([...seen].filter(k => current.has(k)));
      badge.text = `$(bell) Magi: ${pending.length}`;
      if (pending.length) badge.show(); else badge.hide();
      for (const item of pending) {
        if (seen.has(key(item))) continue;
        seen.add(key(item));
        void vscode.window.showWarningMessage(t('Magi: {0} is waiting for your response ({1}).', item.requester, item.prompt.what ?? ''), t('Review')).then(action => {
          if (!disposed && action === t('Review')) void review(item);
        });
      }
    } finally { reading = false; }
  };
  const review = async (item: Pending) => {
    const p = item.prompt;
    if (!p.id) return;
    let decision: string | undefined;
    let answer: string | undefined;
    const detail = [item.requester, item.session, p.what, p.reason,
      p.args ? JSON.stringify(p.args, null, 2) : '', p.diff,
      p.report ? JSON.stringify(p.report, null, 2) : ''].filter(Boolean).join('\n\n');
    if (p.kind === 'permission') {
      const action = await vscode.window.showWarningMessage(t('Review delegated tool request'), { modal: true, detail }, t('Allow once'), t('Reject'));
      if (!action) return; // Closing the review leaves the request pending.
      decision = action === t('Allow once') ? 'allow' : 'deny';
    } else if (p.kind === 'question') {
      const action = await vscode.window.showInformationMessage(p.what ?? '', { modal: true, detail }, t('Answer'));
      if (!action) return;
      answer = p.options?.length
        ? await vscode.window.showQuickPick(p.options, { title: p.what })
        : await vscode.window.showInputBox({ title: p.what, prompt: p.reason });
      if (answer === undefined) return;
    } else return;
    const fresh = await companion.ask('jobs');
    if (!fresh?.ok) {
      void vscode.window.showErrorMessage(t('Response was not sent: {0}', fresh?.error ?? t('Companion unavailable')));
      return;
    }
    if (!fresh.jobs?.approvals?.some(x => key(x) === key(item))) {
      void vscode.window.showWarningMessage(t('This request is no longer pending.'));
      return;
    }
    const result = await companion.ask(p.kind === 'permission' ? 'permission' : 'answer', {
      session: item.session, callId: p.id, ...(decision ? { decision } : { answer }),
    });
    if (!result?.ok) void vscode.window.showErrorMessage(t('Response was not sent: {0}', result?.error ?? t('Companion unavailable')));
    await read();
  };
  const command = vscode.commands.registerCommand('magi.approvals', async () => {
    await read();
    const pick = await vscode.window.showQuickPick(pending.map(item => ({ label: item.prompt.what ?? '', description: item.requester, detail: item.session, item })), { title: t('Pending approvals and questions') });
    if (pick) await review(pick.item);
  });
  const timer = setInterval(() => { void read(); }, 3000);
  void read();
  return { dispose() { disposed = true; clearInterval(timer); command.dispose(); badge.dispose(); } };
}
