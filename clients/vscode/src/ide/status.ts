import * as vscode from 'vscode';
import * as activity from '../core/activity';

/**
 * One status bar item, on the left.
 *
 * Left because the companion belongs to the workspace, not to whichever file is open — that is the
 * rule VS Code states, and it is the true one here. ONE item, because the guidelines say ❌ "Add
 * more than one item (unless necessary)" and every other extension shares this bar.
 *
 * ⚠ **No colour.** A warning background is allowed only "as a last resort ... given their
 * prominence", and a companion waiting for an answer is not an error. It says so in words instead.
 *
 * It stays visible when the panel is closed, which is the whole reason it exists: the guidelines
 * say plainly that "users often minimize the Panel", and the conversation lives there.
 */
export class Status implements vscode.Disposable {
  private readonly item: vscode.StatusBarItem;

  constructor() {
    this.item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    // Opens "what is running" rather than the conversation. The conversation now opens by itself,
    // and this item is the only part of magi visible when the panel is closed — so it is the right
    // door to the thing a person could not find.
    this.item.command = 'magi.setup';
    this.draw(activity.cannotSay());
    this.item.show();
  }

  private setup: activity.Setup = {};

  draw(a: activity.Activity): void {
    this.last = a;
    const spin = a.state === activity.State.Working ? '$(loading~spin) ' : '';
    // The model on the item itself, when it is known. "What is answering me" was invisible
    // everywhere until now, and it is the first thing a person checks after switching backends.
    const model = this.setup.model ? ` · ${this.setup.model}` : '';
    this.item.text = `${spin}magi: ${activity.label(a)}${model}`;
    this.item.tooltip = a.state === activity.State.NotRunning
      ? 'No companion is listening on this workspace. Open the conversation to start one.'
      : [`magi — ${activity.label(a)}`,
         this.setup.model && `model: ${this.setup.model}`,
         this.setup.council && `council: ${this.setup.council}`,
         this.setup.backend && `backend: ${this.setup.backend}`,
         this.setup.permission && `approval: ${this.setup.permission}`,
         'click to change'].filter(Boolean).join('\n');
  }

  /** What it is running on. Drawn separately because it changes when somebody changes it. */
  show(s: activity.Setup): void {
    if (activity.sameSetup(s, this.setup)) return;
    this.setup = s;
    this.draw(this.last);
  }

  private last: activity.Activity = activity.cannotSay();

  dispose(): void { this.item.dispose(); }
}
