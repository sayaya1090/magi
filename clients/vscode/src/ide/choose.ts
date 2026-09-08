import * as vscode from 'vscode';
import { Companion } from './workspace';
import { Chat } from './chat';

/**
 * The settings whose values only the daemon knows.
 *
 * These cannot be `contributes.configuration` entries: a static schema cannot list the models a
 * particular backend offers, or the conversations a workspace holds. VS Code's rule against a
 * custom settings page still stands — so they are commands, which is the surface this editor gives
 * for "choose one of these, now".
 */
export function chooseCommands(companion: Companion, chat: Chat): vscode.Disposable[] {
  return [
    vscode.commands.registerCommand('magi.chooseModel', async () => {
      const resp = await companion.ask('models');
      const names = resp?.models ?? [];
      if (!resp?.ok || !names.length) {
        // The door carries `why` for exactly this: an empty list with ok is not a failure, and the
        // reason it is empty (no backend reachable, a gateway that would not answer) lives there.
        // Composing our own sentence instead threw away the only one that says what to do.
        void vscode.window.showWarningMessage(
          `magi: ${resp?.error ?? resp?.why ?? 'the companion did not say which models it has'}`);
        return;
      }
      const pick = await vscode.window.showQuickPick(names, { title: 'magi — model' });
      if (!pick) return;
      const set = await companion.ask('set-model', { name: pick });
      if (!set?.ok) void vscode.window.showWarningMessage(`magi: ${set?.error ?? 'the model did not change'}`);
    }),

    vscode.commands.registerCommand('magi.choosePermission', async () => {
      // The four the core spells, with the sentence each one means. The token that goes on the
      // wire is the core's; the words beside it are for reading.
      const modes = [
        { label: 'ask', description: 'ask before anything dangerous' },
        { label: 'auto', description: "let magi edit its own files; ask for commands and network" },
        { label: 'allow', description: 'everything through' },
        { label: 'deny', description: 'refuse everything' },
      ];
      const pick = await vscode.window.showQuickPick(modes, { title: 'magi — approval' });
      if (!pick) return;
      const set = await companion.ask('set-permission', { name: pick.label });
      if (!set?.ok) void vscode.window.showWarningMessage(`magi: ${set?.error ?? 'the mode did not change'}`);
    }),

    vscode.commands.registerCommand('magi.openConversation', async () => {
      const resp = await companion.ask('sessions');
      const list = (resp?.sessions ?? []) as { id?: string; title?: string; lastActivity?: string }[];
      if (!resp?.ok || !list.length) {
        void vscode.window.showWarningMessage('magi: this workspace has no conversations yet.');
        return;
      }
      const pick = await vscode.window.showQuickPick(
        list.filter((s) => s.id).map((s) => ({
          label: (s.title ?? '(no messages)').split('\n')[0],
          description: s.id!.slice(-6),
          id: s.id!,
        })),
        { title: 'magi — conversation' },
      );
      if (pick) chat.showSession(pick.id);
    }),
  ];
}
