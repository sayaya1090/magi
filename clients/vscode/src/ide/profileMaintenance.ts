import * as vscode from 'vscode';
import { Companion } from './workspace';

/** Internal sessions are visible on demand, never as user chats. */
export async function profileMaintenance(companion: Companion, session?: string): Promise<void> {
  const t = vscode.l10n.t;
  if (!(await companion.caps())?.has('profile-maintenance')) { await vscode.window.showWarningMessage(t('Description maintenance is unavailable. Update or reconnect the companion.')); return; }
  for (;;) {
    const response = await companion.ask('profile-maintenance');
    if (!response?.ok || !response.out) { await vscode.window.showWarningMessage(response?.error || t('Could not read maintenance status.')); return; }
    let state: { id: string; running: boolean; enabled: boolean; pending: boolean; last_attempt: string; last_success: string; last_error?: string; history?: { at: string; status: string; added?: string[]; updated?: string[]; error?: string }[] };
    try { state = JSON.parse(response.out); } catch { await vscode.window.showWarningMessage(t('Could not read maintenance status.')); return; }
    const date = (s: string) => !s || s.startsWith('0001-') ? t('Never') : s;
    const rows = [
      { label: t('Request description refresh'), description: t('Uses new project-read evidence; waits for idle and respects the cooldown.'), refresh: true },
      { label: !state.enabled ? t('Disabled') : state.running ? t('Running') : state.pending ? t('Waiting for idle or cooldown') : t('Idle'), description: state.id, refresh: false },
      { label: t('Last attempt'), description: date(state.last_attempt), refresh: false },
      { label: t('Last success'), description: date(state.last_success), refresh: false },
      ...(state.last_error ? [{ label: t('Last error'), detail: state.last_error, refresh: false }] : []),
      ...(state.history || []).slice().reverse().map(e => ({ label: `${e.at} · ${e.status === 'published' ? t('Published') : e.status === 'failed' ? t('Failed') : t('Unchanged')}`, description: [...(e.added || []), ...(e.updated || [])].join(', '), detail: e.error, refresh: false })),
    ];
    const selected = await vscode.window.showQuickPick(rows, { title: t('Internal description session'), matchOnDescription: true, matchOnDetail: true, ignoreFocusOut: true });
    if (!selected) return;
    if (selected.refresh) {
      const result = await companion.ask('profile-maintenance', { name: 'refresh', session });
      if (!result?.ok) await vscode.window.showWarningMessage(result?.error || t('Could not request maintenance.'));
      else await vscode.window.showInformationMessage(t('Refresh requested. It uses new evidence and runs when idle; it may wait for the cooldown.'));
      return;
    }
  }
}
