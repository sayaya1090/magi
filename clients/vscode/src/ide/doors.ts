import * as vscode from 'vscode';
import { Companion } from './workspace';
import { Chat } from './chat';
import { Row, turnsBack } from '../core/transcript';
import * as activity from '../core/activity';
import { jobs as jobsOf, schedules, originWord, localStamp } from '../core/panel';
import { whyNoCompletion } from '../core/complete';
import { Response } from '../core/protocol';

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
      const list = r.profiles ?? [];
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
        NonNullable<Response['config']>;
      if (!list.length) { void vscode.window.showInformationMessage('magi: this companion exposes no settings.'); return; }
      // ⚠ **A broken config file and an empty one look identical in a list of values.** The door
      // carries `unreadable` for exactly that: the layer would not parse, and the string is why.
      // The core calls a read that cannot say "your global file is broken" the third silence — and
      // it is the one that matters most here, because a person then edits a setting, saves, and
      // watches nothing take effect.
      //
      // Said BEFORE the picker, not inside it: a row in the list would be one line among thirty,
      // and this is a fact about the whole read rather than about any one key.
      const broken = [...new Set(list.map((c) => c.unreadable).filter(Boolean))];
      for (const why of broken) void vscode.window.showWarningMessage(`magi: a settings file could not be read — ${why}`);
      const pick = await vscode.window.showQuickPick(
        list.filter((c) => c.key).map((c) => ({
          label: c.key!,
          // ⚠ **Where the value came from is not where a write would go.** `tier` is the file this
          // screen would edit; `source` is the layer the CURRENT value comes from, and the core says
          // it can be "env". An environment variable beats every file, so editing one that came from
          // `env` writes something and changes nothing a person can see — the same silence the
          // `unreadable` warning above exists to break. This screen showed neither, so the two cases
          // were one line. The JetBrains settings screen has drawn the source since the field landed.
          description: c.value ? `= ${c.value}${c.source ? ` (from ${c.source})` : ''}` : '(unset)',
          detail: [c.doc, c.applies && `applies ${c.applies}`, c.tier].filter(Boolean).join(' · '),
          key: c.key!, value: c.value ?? '', tier: c.tier ?? '',
          source: c.source ?? '', profile: c.profile === true,
        })),
        { title: 'magi — settings', matchOnDetail: true },
      );
      if (!pick) return;
      // An env var wins over whatever this writes. Said before the box rather than after the save:
      // afterwards it is an explanation for something that already looked broken.
      if (pick.source === 'env') {
        void vscode.window.showWarningMessage(
          `magi: ${pick.key} comes from the environment, which beats the file this writes to — `
          + 'the new value takes effect only where that variable is unset.');
      }
      /**
       * ⚠ **A key whose value must NAME a profile is asked with the list, not with a text box.**
       *
       * The core carries `profile` for exactly this and says why it is on the wire rather than left
       * to each client: "Every client that hardcodes which keys are profile-shaped is a copy of a
       * list that lives here." A free-text box takes any word, and a name that is not a profile is
       * accepted and then does nothing — the setting reads as changed and the behaviour does not.
       *
       * The empty entry stays: clearing is how a person goes back to the default, and a picker with
       * no way out would make that unsayable (the same reason `magi.chooseBackend` offers one).
       */
      let value: string | undefined;
      if (pick.profile) {
        const known = await call('profiles');
        const names = (known?.profiles ?? []).map((p) => p.name).filter((n): n is string => !!n);
        const chosen = await vscode.window.showQuickPick(
          [{ label: '(unset)', name: '' }, ...names.map((n) => ({ label: n, name: n }))],
          { title: `magi — ${pick.key}`, placeHolder: pick.value || '(unset)' },
        );
        value = chosen?.name;
      } else {
        value = await vscode.window.showInputBox({
          title: `magi — ${pick.key}`, value: pick.value,
          prompt: 'Empty clears it.', ignoreFocusOut: true,
        });
      }
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
    /**
     * Put this companion on a newer build, and on the build it just fetched.
     *
     * The door does both halves: it swaps the binary and then restarts, and its answer says which
     * of the two happened ("updated A → B — restarting", or "already up to date"). So this shows
     * the daemon's own sentence rather than inventing one — a client that said "updated" would be
     * guessing at the half it cannot see.
     *
     * It asks first, for the reason `magi.compact` does: restarting ends whatever turn is running,
     * and a person who reads "update" as "check for updates" would not expect that.
     *
     * ⚠ Same-machine only, by the core's design: `update`, `restart` and `shutdown` are refused
     * across the network door on purpose, so this is only ever the companion for this workspace.
     */
    reg('magi.updateCore', async () => {
      const ok = await vscode.window.showWarningMessage(
        'Update this companion? A running turn ends when it restarts.',
        { modal: true }, 'Update');
      if (ok !== 'Update') return;
      const r = await call('update');
      // The daemon's own words: it knows whether anything changed and whether it can restart.
      if (r) void vscode.window.showInformationMessage(`magi: ${r.out || 'already up to date'}`);
    }),

    /** Start this companion again on the build it already has. Ends a running turn — so it asks. */
    reg('magi.restartDaemon', async () => {
      const ok = await vscode.window.showWarningMessage(
        'Restart this companion? A running turn ends with it.',
        { modal: true }, 'Restart');
      if (ok !== 'Restart') return;
      if (await call('restart')) void vscode.window.showInformationMessage('magi: restarting.');
    }),

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
        asked.slice().reverse().map((r: Row, i) => ({
          label: r.text.split('\n')[0].slice(0, 80),
          description: i === 0 ? 'the last thing you asked' : `${i + 1} turns back`,
          seq: r.seq,
        })),
        { title: 'magi — go back to just before' },
      );
      if (!pick) return;
      // ⚠ The door counts TURNS (`n`), not sequence numbers, and it does not read `since` at all.
      // This sent `since: seq` and the daemon rewound by its own default — the chosen point had
      // nothing to do with it.
      const n = turnsBack(asked, pick.seq);
      if (!n) { void vscode.window.showWarningMessage('magi: that point is no longer in the conversation.'); return; }
      const ok = await vscode.window.showWarningMessage(
        `Drop the last ${n} turn(s)? Everything after that point goes.`, { modal: true }, 'Rewind');
      if (ok !== 'Rewind') return;
      if (await call('rewind', { session: chat.session, n })) chat.reload();
    }),

    /** Pick up a conversation the daemon is not currently on. */
    reg('magi.resume', async () => {
      if (!await has('sessions', 'resuming')) return;
      const r = await call('sessions');
      if (!r) return;
      const list = r.sessions ?? [];
      const pick = await vscode.window.showQuickPick(
        list.filter((s) => s.id).map((s) => ({
          label: (s.title || '(no messages)').split('\n')[0],
          // The time in the reader's own clock. It was the wire's RFC3339 — UTC, with the T and
          // the Z — and this list is how somebody finds the conversation they had this morning.
          description: [s.model, localStamp(s.lastActivity)].filter(Boolean).join(' · '),
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
      if (!pick) return;
      const r2 = await call('job-kill', { name: pick.label });
      // `ok` alone cannot tell the two endings apart, and the daemon knows which one it was:
      // `removed` is true when this call stopped something and absent when there was nothing left
      // to stop (`answerJobKill` — "pressed twice must read 'already gone', not 'failure'").
      // Saying "asked it to stop" either way is a lie exactly when the list was stale — which is
      // the case this button is most often pressed in, because the row outlives the job by one poll.
      if (r2) {
        void vscode.window.showInformationMessage(r2.removed
          ? `magi: asked ${pick.label} to stop.`
          : `magi: ${pick.label} had already finished — nothing to stop.`);
      }
    }),

    /** The conversations this one spawned. Read-only: a child is somebody else's turn. */
    reg('magi.children', async () => {
      if (!await has('children', 'listing child conversations')) return;
      const r = await call('children', { session: chat.session });
      if (!r) return;
      // ⚠ `children`, not `sessions`. This read `sessions` and so answered "no children" on every
      // build — the same defect class as the panel reading `out`: an absent field is an empty list.
      const list = r.children ?? [];
      if (!list.length) { void vscode.window.showInformationMessage('magi: this conversation has no children.'); return; }
      const pick = await vscode.window.showQuickPick(
        list.filter((s) => s.id).map((s) => ({
          label: (s.title || '(no messages)').split('\n')[0],
          // WHO opened it, before the id. A meeting room and a subagent are different things —
          // one is work this conversation delegated, the other a conversation somebody else
          // convened — and this list drew them the same, so the only way to tell was to open one.
          // `origin` is the discriminator and `agent` is not; the core's own comment says why.
          description: [originWord(s.origin), s.id!.slice(-6)].filter(Boolean).join(' · '),
          id: s.id!,
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
