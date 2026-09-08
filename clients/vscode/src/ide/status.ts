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
    this.item.command = 'magi.focusChat';
    this.draw(activity.cannotSay());
    this.item.show();
  }

  draw(a: activity.Activity): void {
    const spin = a.state === activity.State.Working ? '$(loading~spin) ' : '';
    this.item.text = `${spin}magi: ${activity.label(a)}`;
    this.item.tooltip = a.state === activity.State.NotRunning
      ? 'No companion is listening on this workspace. Open the conversation to start one.'
      : `magi — ${activity.label(a)}`;
  }

  dispose(): void { this.item.dispose(); }
}
