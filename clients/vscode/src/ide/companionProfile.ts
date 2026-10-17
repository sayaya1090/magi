import * as vscode from 'vscode';

type Task = { id: string; description: string; constraints?: string[] };
type Profile = { purpose: string; responsibilities: string[] | null; tasks: Task[] | null };

/** Edits metadata only. The daemon validates and preserves the authority fields. */
export async function editCompanionProfile(raw: string): Promise<string | undefined> {
  let p: Profile;
  try { p = JSON.parse(raw); } catch { void vscode.window.showErrorMessage(vscode.l10n.t('The workspace description could not be read.')); return; }
  p.responsibilities ??= []; p.tasks ??= [];
  for (;;) {
    const rows = [
      { label: vscode.l10n.t('Save'), action: 'save', index: -1 },
      { label: vscode.l10n.t('Purpose'), description: p.purpose, action: 'purpose', index: -1 },
      { label: vscode.l10n.t('Responsibilities'), description: p.responsibilities.join(', '), action: 'responsibilities', index: -1 },
      ...p.tasks.map((t, index) => ({ label: t.description, description: t.constraints?.join(', '), action: 'task', index })),
      { label: vscode.l10n.t('Add offered task'), action: 'add', index: -1 },
    ];
    const pick = await vscode.window.showQuickPick(rows, { title: vscode.l10n.t('Workspace purpose and offered tasks'), matchOnDescription: true });
    if (!pick) return;
    if (pick.action === 'save') return JSON.stringify(p);
    if (pick.action === 'purpose' || pick.action === 'responsibilities') {
      const value = await vscode.window.showInputBox({ title: pick.label, value: pick.action === 'purpose' ? p.purpose : p.responsibilities.join('; '), prompt: pick.action === 'purpose' ? undefined : vscode.l10n.t('Separate entries with semicolons.'), ignoreFocusOut: true });
      if (value !== undefined) { if (pick.action === 'purpose') p.purpose = value; else p.responsibilities = value.split(';').map(s => s.trim()).filter(Boolean); }
      continue;
    }
    const task = pick.action === 'add' ? { id: `task-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`, description: '', constraints: [] } : p.tasks[pick.index];
    const action = pick.action === 'add' ? 'edit' : await vscode.window.showQuickPick([{ label: vscode.l10n.t('Edit'), value: 'edit' }, { label: vscode.l10n.t('Remove'), value: 'remove' }], { title: task.description });
    if (!action) continue;
    if (typeof action !== 'string' && action.value === 'remove') { p.tasks.splice(pick.index, 1); continue; }
    const description = await vscode.window.showInputBox({ title: vscode.l10n.t('Offered task'), value: task.description, prompt: vscode.l10n.t('Describe the work, required input and expected result.'), ignoreFocusOut: true });
    if (description === undefined || !description.trim()) continue;
    const constraints = await vscode.window.showInputBox({ title: vscode.l10n.t('Task limits'), value: task.constraints?.join('; ') ?? '', prompt: vscode.l10n.t('Separate entries with semicolons.'), ignoreFocusOut: true });
    if (constraints === undefined) continue;
    const updated = { ...task, description, constraints: constraints === (task.constraints?.join('; ') ?? '') ? (task.constraints ?? []) : constraints.split(';').map(s => s.trim()).filter(Boolean) };
    if (pick.action === 'add') p.tasks.push(updated); else p.tasks[pick.index] = updated;
  }
}
