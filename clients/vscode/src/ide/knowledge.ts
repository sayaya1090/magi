import * as vscode from 'vscode';
import { Companion } from './workspace';
import { KnowledgeItem } from '../core/protocol';

export async function runSlotAction(companion: Companion, plugin: string, action: string, title?: string, slot?: string): Promise<void> {
  const t = vscode.l10n.t;
  const result = await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title: title || t('Executing action…') },
    () => companion.ask('knowledge', { args: { name: plugin, action, ...(slot ? { slot } : {}) } })
  );
  if (!result?.ok) {
    await vscode.window.showWarningMessage(result?.error || t('Operation failed.'));
  } else {
    await vscode.window.showInformationMessage(result?.doing || t('Operation completed.'));
  }
}

/** Native, project-scoped management. Every action carries the displayed revision. */
export async function manageKnowledge(companion: Companion): Promise<void> {
  const t = vscode.l10n.t;
  const caps = await companion.caps();
  if (!caps?.has('knowledge')) { await vscode.window.showWarningMessage(t('Knowledge management is unavailable. Update or reconnect the companion.')); return; }
  for (;;) {
    const response = await companion.ask('knowledge');
    if (!response?.ok) { await vscode.window.showWarningMessage(response?.error || t('Could not load project knowledge.')); return; }
    const items = response.knowledge || [];
    const slots = response.slots || [];

    const quickPickItems: Array<{
      label: string;
      description?: string;
      detail?: string;
      item?: KnowledgeItem;
      slotAction?: { plugin: string; slot: string; action: string; title: string };
      kind?: vscode.QuickPickItemKind;
    }> = items.map(item => ({
      label: item.description || item.name,
      description: [item.kind === 'memory' ? t('Memory') : t('Skill'),
        item.archived ? t('Archived') : item.archive_candidate ? t('Archive candidate') : item.cold ? t('Cold') : t('Active'),
        ...(item.pinned ? [t('Pinned')] : []), ...(item.protected ? [t('In use')] : [])].join(' · '),
      detail: item.name, item,
    }));

    const sepKind = (vscode as any).QuickPickItemKind?.Separator;

    // Dynamically render plugin-contributed UI slots
    for (const slot of slots) {
      const statusBadge = slot.status ? ` [${slot.status.label}${slot.status.badge !== undefined ? ` (${slot.status.badge})` : ''}]` : '';
      quickPickItems.push({ label: `${slot.title}${statusBadge}`, kind: sepKind });

      if (slot.items) {
        for (const item of slot.items) {
          const icon = item.warning ? '$(warning)' : '$(info)';
          quickPickItems.push({
            label: `${icon} ${item.label}: ${item.value}`,
            description: item.description,
            detail: slot.description,
          });
        }
      }

      if (slot.actions) {
        for (const act of slot.actions) {
          const icon = act.variant === 'primary' ? '$(sparkle)' : '$(play)';
          quickPickItems.push({
            label: `${icon} ${act.label}`,
            description: act.description,
            slotAction: { plugin: slot.plugin, slot: slot.id, action: act.id, title: act.label },
          });
        }
      }
    }

    // A quick pick's rows cannot be replaced once it is open, so a slot change closes it and the
    // loop opens it again with fresh values.
    let stale = false;
    const closing = new vscode.CancellationTokenSource();
    const watching = await companion.watchSlots(() => { stale = true; closing.cancel(); });
    let selected: (typeof quickPickItems)[number] | undefined;
    try {
      selected = await vscode.window.showQuickPick(quickPickItems, {
        title: t('Project memory and skills'),
        matchOnDescription: true, matchOnDetail: true, ignoreFocusOut: true
      }, closing.token);
    } finally {
      watching.dispose();
      closing.dispose();
    }
    if (!selected && stale) continue;
    if (!selected) return;

    if (selected.slotAction) {
      await runSlotAction(companion, selected.slotAction.plugin, selected.slotAction.action, selected.slotAction.title, selected.slotAction.slot);
      continue;
    }

    if (!selected.item) continue;
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
