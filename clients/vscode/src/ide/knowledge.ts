import * as vscode from 'vscode';
import { Companion } from './workspace';
import { KnowledgeItem } from '../core/protocol';

/** Native, project-scoped management. Every action carries the displayed revision. */
export async function manageKnowledge(companion: Companion): Promise<void> {
  const t = vscode.l10n.t;
  const caps = await companion.caps();
  if (!caps?.has('knowledge')) { await vscode.window.showWarningMessage(t('Knowledge management is unavailable. Update or reconnect the companion.')); return; }
  for (;;) {
    const response = await companion.ask('knowledge');
    if (!response?.ok) { await vscode.window.showWarningMessage(response?.error || t('Could not load project knowledge.')); return; }
    const items = response.knowledge || [];
    if (!items.length) { await vscode.window.showInformationMessage(t('No saved project knowledge.')); return; }
    const selected = await vscode.window.showQuickPick(items.map(item => ({
      label: item.description || item.name,
      description: [item.kind === 'memory' ? t('Memory') : t('Skill'),
        item.archived ? t('Archived') : item.archive_candidate ? t('Archive candidate') : item.cold ? t('Cold') : t('Active'),
        ...(item.pinned ? [t('Pinned')] : []), ...(item.protected ? [t('In use')] : [])].join(' · '),
      detail: item.name, item,
    })), { title: t('Project memory and skills'), matchOnDescription: true, matchOnDetail: true, ignoreFocusOut: true });
    if (!selected) return;
    const item: KnowledgeItem = selected.item;
    const actions = [{ label: t('Back to list'), action: '' }];
    if (item.importable) actions.unshift({ label: t('Register a managed copy'), action: 'import' });
    if (item.managed) {
      actions.unshift({ label: item.pinned ? t('Unpin') : t('Pin'), action: item.pinned ? 'unpin' : 'pin' },
        { label: item.archived ? t('Restore') : t('Archive locally'), action: item.archived ? 'restore' : 'archive' });
    }
    const pick = await vscode.window.showQuickPick(actions, {
      title: item.description || item.name,
      placeHolder: item.managed ? t('Verified runs: {0} · Last verified: {1}', item.application_count, item.last_applied?.startsWith('0001-') ? t('Never') : item.last_applied || t('Never')) : t('Legacy item: retention controls are not available.'),
      ignoreFocusOut: true,
    });
    if (!pick) return;
    if (!pick.action) continue;
    if (pick.action === 'import' && await vscode.window.showWarningMessage(t('Register a pinned managed copy? The original stays unchanged. This does not mark the skill as verified.'), { modal: true }, t('Register a managed copy')) !== t('Register a managed copy')) continue;
    if (pick.action === 'archive' && await vscode.window.showWarningMessage(t('Archive locally? The original file is preserved and this item leaves automatic search.'), { modal: true }, t('Archive locally')) !== t('Archive locally')) continue;
    const result = await vscode.window.withProgress({ location: vscode.ProgressLocation.Notification, title: t('Updating project knowledge…') }, () => companion.ask('knowledge', { args: { name: item.name, revision: item.revision, action: pick.action } }));
    if (!result?.ok) await vscode.window.showWarningMessage(t('Knowledge was not changed. The list will reload: {0}', result?.error || t('Connection lost')));
  }
}
