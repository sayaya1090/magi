import * as vscode from 'vscode';
import { Companion } from './workspace';
import { Chat } from './chat';
import { Row } from '../core/transcript';
import * as activity from '../core/activity';
import { jobs as jobsOf, schedules } from '../core/panel';
import { whyNoCompletion } from '../core/complete';

/**
 * The doors the JetBrains client opens and this one did not.
 *
 * Measured, not guessed: the two clients' calls were extracted from source and diffed, and eighteen
 * doors were on one side only. Fourteen of them are "choose one of these, now" or "do this once",
 * which is what a command is for in this editor — a custom settings page is forbidden and a webview
 * for each would be four more surfaces to keep.
 *
 * ⚠ **Every door is asked for before it is called.** The daemon advertises what it answers in
 * `about`, and a build that predates a door refuses it with its own sentence. Reading the
 * advertisement means the command says "this companion does not do that" instead of showing a
 * person a refusal they cannot act on.
 */
export function doorCommands(companion: Companion, chat: Chat): vscode.Disposable[] {
  const reg = (id: string, run: () => Promise<void>): vscode.Disposable =>
    vscode.commands.registerCommand(id, () => void run());

  /** Ask, and say plainly when the answer is no. Returns null when nothing usable came back. */
  const call = async (method: string, extra: Record<string, unknown> = {}) => {
    const r = await companion.ask(method, extra);
    if (!r) { void vscode.window.showWarningMessage('magi: no companion is listening on this workspace.'); return null; }
    if (!r.ok) { void vscode.window.showWarningMessage(`magi: ${r.error ?? `${method} did not work`}`); return null; }
    return r;
  };

  /**
   * A door this build of the daemon does not advertise. Said once, with the door's name.
   *
   * ⚠ **Only for names the daemon can actually advertise.** Thirteen of its forty-four doors carry a
   * capability; the other thirty-one do not, and gating on one of those is a command that says "this
   * companion does not do that" for ever, on every build. Measured: `compact`, `rewind` and
   * `reload-cron` were gated that way here and were dead the whole time. For a capless door, CALL it
   * — a refusal comes back in the daemon's own words, which is the sentence worth showing anyway.
   * `manifest.test.ts` holds the list against the Go source.
   */
  const has = async (cap: string, what: string): Promise<boolean> => {
    const caps = await companion.caps();
    if (caps.has(cap)) return true;
    void vscode.window.showWarningMessage(`magi: this companion does not offer ${what}.`);
    return false;
  };

  return [
    /**
     * One place that SHOWS what is running and lets it be changed.
     *
     * This exists because of a measured complaint: the model and the approval mode were reachable
     * only from a view-title overflow menu, so a person looking for "the settings" found nothing
     * and asked where they had gone. Two separate faults were behind that — they were hidden behind
     * a "…", and nothing anywhere DISPLAYED the current values, so even finding the menu only told
     * you what you could change, never what was in force.
     *
     * So the values are read first and shown as the labels. The status bar item opens this, because
     * that item is the one part of magi that is visible when the panel is closed.
     */
    reg('magi.setup', async () => {
      const st = await companion.ask('status', chat.session ? { session: chat.session } : {});
      const now = activity.setupOf(st ?? null);
      const say = (v: string | undefined) => v ?? 'not said';
      const pick = await vscode.window.showQuickPick([
        { label: 'Open the conversation', id: 'magi.focusChat' },
        { label: `Model: ${say(now.model)}`, id: 'magi.chooseModel', description: 'change' },
        { label: `Backend: ${say(now.backend)}`, id: 'magi.chooseBackend', description: 'change' },
        { label: `Approval: ${say(now.permission)}`, id: 'magi.choosePermission', description: 'change' },
        { label: "magi's own settings", id: 'magi.changeSetting', description: 'models, templates, autocomplete' },
        // Only when there is something to say. A row that reads "completion: fine" every time is a
        // row nobody reads, and this one exists precisely for the case where nothing is appearing.
        ...(whyNoCompletion()
          ? [{ label: `Completion said nothing: ${whyNoCompletion()}`, id: 'magi.changeSetting', description: 'the companion\'s own reason' }]
          : []),
      ], { title: 'magi — what is running' });
      if (pick) await vscode.commands.executeCommand(pick.id);
    }),

    // ---- what it runs on -------------------------------------------------------------------
    /**
     * The backend, from the profiles this daemon actually has.
     *
     * The list cannot be a static setting: which backends exist is in somebody's config file, and
     * `[llm.profiles.*]` is theirs to name. The default backend is offered as its own entry because
     * "go back to the plain one" is otherwise unsayable — a profile list with no way out leaves a
     * person stuck on whichever gateway they tried.
     */
    reg('magi.chooseBackend', async () => {
      if (!await has('settings', 'backend switching')) return;
      const r = await call('profiles');
      if (!r) return;
      const list = (r.profiles ?? []) as { name?: string; tier?: string }[];
      const items = [
        { label: 'the default backend', description: 'config.toml base_url + model', name: '' },
        ...list.filter((p) => p.name).map((p) => ({ label: p.name!, description: p.tier ?? '', name: p.name! })),
      ];
      const pick = await vscode.window.showQuickPick(items, { title: 'magi — backend' });
      if (!pick) return;
      if (await call('use-backend', { name: pick.name })) void companion.refresh();
    }),

    /**
     * The settings only the daemon knows, read and written through its own door.
     *
     * The keys are the daemon's whitelist, not ours: arbitrary TOML down a socket would be a hole
     * (the same file holds the permission posture and the hooks). So the list is asked for, and
     * each row carries where it is written and when it takes effect — a person who changes
     * something that applies "next start" and sees nothing happen would otherwise conclude it
     * failed.
     */
    reg('magi.changeSetting', async () => {
      if (!await has('settings', 'its own settings')) return;
      const r = await call('config-get');
      if (!r) return;
      const list = (r.config ?? []) as
        { key?: string; value?: string; tier?: string; file?: string; applies?: string; doc?: string }[];
      if (!list.length) { void vscode.window.showInformationMessage('magi: this companion exposes no settings.'); return; }
      const pick = await vscode.window.showQuickPick(
        list.filter((c) => c.key).map((c) => ({
          label: c.key!,
          description: c.value ? `= ${c.value}` : '(unset)',
          detail: [c.doc, c.applies && `applies ${c.applies}`, c.tier].filter(Boolean).join(' · '),
          key: c.key!, value: c.value ?? '', tier: c.tier ?? '',
        })),
        { title: 'magi — settings', matchOnDetail: true },
      );
      if (!pick) return;
      const value = await vscode.window.showInputBox({
        title: `magi — ${pick.key}`, value: pick.value,
        prompt: 'Empty clears it.', ignoreFocusOut: true,
      });
      if (value === undefined) return;
      const set = await call('config-set', { name: pick.key, text: value, tier: pick.tier });
      if (set) void vscode.window.showInformationMessage(`magi: ${pick.key} is now ${value || '(unset)'}.`);
    }),

    // ---- the life of a conversation --------------------------------------------------------
    /** A conversation of its own. The daemon names it; nothing here invents an id. */
    reg('magi.newConversation', async () => {
      if (!await has('session-new', 'opening a new conversation')) return;
      const r = await call('session-new');
      if (!r) return;
      const sid = r.session ?? '';
      if (!sid) { void vscode.window.showWarningMessage('magi: the companion opened one but did not say which.'); return; }
      chat.showSession(sid);
    }),

    /**
     * Fold the conversation so far.
     *
     * Named as what it costs, not as what it saves: this rewrites the context the next turn sees,
     * and a person who reads "compact" as "tidy up" would not expect the companion to forget the
     * middle of the work. So it asks first.
     */
    reg('magi.compact', async () => {
      // No capability for this door — see `has`. Ask the person, then let the daemon answer.
      const ok = await vscode.window.showWarningMessage(
        'Fold this conversation? The companion keeps a summary and loses the detail.',
        { modal: true }, 'Fold');
      if (ok !== 'Fold') return;
      if (await call('compact', { session: chat.session })) {
        void vscode.window.showInformationMessage('magi: folded.');
      }
    }),

    /**
     * Go back to a point in the conversation.
     *
     * The points offered are the person's own prompts, because those are the only places a person
     * remembers. Offering every event would be a list of the companion's steps, which is not how
     * anybody thinks about "before I asked for that".
     */
    reg('magi.rewind', async () => {
      // No capability for this door either.
      const asked = chat.userRows();
      if (!asked.length) { void vscode.window.showInformationMessage('magi: nothing to go back to yet.'); return; }
      const pick = await vscode.window.showQuickPick(
        asked.slice().reverse().map((r: Row) => ({
          label: r.text.split('\n')[0].slice(0, 80), description: `#${r.seq}`, seq: r.seq,
        })),
        { title: 'magi — go back to just before' },
      );
      if (!pick) return;
      const ok = await vscode.window.showWarningMessage(
        'Everything after that is dropped from the conversation.', { modal: true }, 'Rewind');
      if (ok !== 'Rewind') return;
      if (await call('rewind', { session: chat.session, since: pick.seq })) chat.reload();
    }),

    /** Pick up a conversation the daemon is not currently on. */
    reg('magi.resume', async () => {
      if (!await has('sessions', 'resuming')) return;
      const r = await call('sessions');
      if (!r) return;
      const list = (r.sessions ?? []) as { id?: string; title?: string; lastActivity?: string; model?: string }[];
      const pick = await vscode.window.showQuickPick(
        list.filter((s) => s.id).map((s) => ({
          label: (s.title || '(no messages)').split('\n')[0],
          description: [s.model, s.lastActivity].filter(Boolean).join(' · '),
          id: s.id!,
        })),
        { title: 'magi — resume' },
      );
      if (!pick) return;
      if (await call('resume', { session: pick.id })) chat.showSession(pick.id);
    }),

    // ---- work beside the turn --------------------------------------------------------------
    /** Stop one background job. The list is the daemon's; a job id typed by hand is a guess. */
    reg('magi.stopJob', async () => {
      if (!await has('job-kill', 'stopping a background job')) return;
      const r = await call('jobs');
      if (!r) return;
      // Only what can be stopped: queued work is not running yet, and offering it would send a kill
      // for an id the daemon has never issued.
      const live = jobsOf(r).jobs.filter((j) => j.running);
      if (!live.length) { void vscode.window.showInformationMessage('magi: nothing is running in the background.'); return; }
      const pick = await vscode.window.showQuickPick(
        live.map((j) => ({ label: j.id, description: j.what })),
        { title: 'magi — stop which job' },
      );
      if (pick && await call('job-kill', { name: pick.label })) {
        void vscode.window.showInformationMessage(`magi: asked ${pick.label} to stop.`);
      }
    }),

    /** The conversations this one spawned. Read-only: a child is somebody else's turn. */
    reg('magi.children', async () => {
      if (!await has('children', 'listing child conversations')) return;
      const r = await call('children', { session: chat.session });
      if (!r) return;
      // ⚠ `children`, not `sessions`. This read `sessions` and so answered "no children" on every
      // build — the same defect class as the panel reading `out`: an absent field is an empty list.
      const list = (r.children ?? []) as { id?: string; title?: string }[];
      if (!list.length) { void vscode.window.showInformationMessage('magi: this conversation has no children.'); return; }
      const pick = await vscode.window.showQuickPick(
        list.filter((s) => s.id).map((s) => ({
          label: (s.title || '(no messages)').split('\n')[0], description: s.id!.slice(-6), id: s.id!,
        })),
        { title: 'magi — child conversations' },
      );
      if (pick) chat.showSession(pick.id);
    }),

    // ---- scheduled work --------------------------------------------------------------------
    /**
     * Schedule a prompt, remove one, or re-read the file.
     *
     * Three doors and one command, because a person opening this wants "the schedules" rather than
     * to pick the verb first. The list is read before anything is offered, so removing names what
     * exists instead of asking somebody to remember it.
     */
    reg('magi.schedules', async () => {
      if (!await has('cron', 'scheduled work')) return;
      const now = await call('cron');
      if (!now) return;
      const rows = schedules(now);
      const names = rows.map((r) => r.name);
      const what = await vscode.window.showQuickPick([
        { label: 'Add a schedule', id: 'add' },
        { label: 'Remove a schedule', id: 'remove', description: names.length ? names.join(', ') : 'none yet' },
        { label: 'Re-read the schedule file', id: 'reload', description: 'after editing it by hand' },
      ], { title: 'magi — scheduled work' });
      if (!what) return;
      if (what.id === 'reload') {
        // `reload-cron` carries no capability, unlike `cron-set`/`cron-remove` below.
        if (await call('reload-cron')) void vscode.window.showInformationMessage('magi: schedules re-read.');
        return;
      }
      if (what.id === 'remove') {
        if (!await has('cron-remove', 'removing a schedule')) return;
        if (!names.length) { void vscode.window.showInformationMessage('magi: there are no schedules.'); return; }
        const pick = await vscode.window.showQuickPick(
          rows.map((r) => ({ label: r.name, description: r.line })),
          { title: 'magi — remove which' },
        );
        if (pick && await call('cron-remove', { name: pick.label })) {
          void vscode.window.showInformationMessage(`magi: ${pick.label} removed.`);
        }
        return;
      }
      if (!await has('cron-set', 'adding a schedule')) return;
      const name = await vscode.window.showInputBox({ title: 'magi — schedule name', ignoreFocusOut: true });
      if (!name) return;
      const when = await vscode.window.showInputBox({
        title: 'magi — when', prompt: 'Five cron fields, in this machine\'s time zone.',
        placeHolder: '7 9 * * 1-5', ignoreFocusOut: true,
      });
      if (!when) return;
      const text = await vscode.window.showInputBox({
        title: 'magi — what to ask each time', ignoreFocusOut: true,
      });
      if (!text) return;
      // `schedule` at the top level — the door reads `req.Schedule`, and `args` is for the `tool`
      // door alone. Sent inside `args` it arrived empty, silently.
      if (await call('cron-set', { name, schedule: when, text })) {
        void vscode.window.showInformationMessage(`magi: ${name} scheduled.`);
      }
    }),
  ];
}
