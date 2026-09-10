import * as vscode from 'vscode';
import { around, noteCompletion, usable } from '../core/complete';
import { Companion } from './workspace';

/**
 * Grey text at the cursor — the console's `complete` door, drawn where this editor draws one.
 *
 * There is no second switch here. magi already has `[autocomplete]` in its own config, and a
 * plugin-side toggle would make "why is it off" a question with two answers. The setting below is
 * only about asking at all.
 */
export function inlineCompletion(companion: Companion): vscode.Disposable {
  const provider: vscode.InlineCompletionItemProvider = {
    async provideInlineCompletionItems(doc, pos, _ctx, token) {
      if (!vscode.workspace.getConfiguration('magi').get<boolean>('complete', false)) return null;
      // Only the window is read. `doc.getText()` with no range is the whole document — copied on
      // every pause in typing, with all but the window thrown away straight after. `positionAt`
      // clamps an offset past either end, so no bounds arithmetic is needed here.
      const args = around(
        (from, to) => doc.getText(new vscode.Range(doc.positionAt(from), doc.positionAt(to))),
        doc.offsetAt(pos),
      );
      if (!args.prefix.trim()) return null;
      const resp = await companion.ask('complete', { name: doc.uri.fsPath, args });
      // Cancelled means the person kept typing. Their next keystroke has already asked again, and
      // drawing this one would put a suggestion under a cursor that has moved.
      if (token.isCancellationRequested) return null;
      // A refusal is recorded before it is dropped. Returning here without noting it is what made
      // "why is completion silent" unanswerable in the one case the daemon answered it.
      if (!resp?.ok) { noteCompletion('', undefined, resp?.error ?? 'the companion did not answer'); return null; }
      const out = usable(resp.out ?? '', args.prefix);
      // Remember why nothing came back. Not shown here — a message per keystroke is noise — but
      // `magi.setup` reads it, which is where a person looks when completion is silent.
      noteCompletion(out, resp.reason);
      if (!out) return null;
      return [new vscode.InlineCompletionItem(out, new vscode.Range(pos, pos))];
    },
  };
  return vscode.languages.registerInlineCompletionItemProvider({ pattern: '**' }, provider);
}
