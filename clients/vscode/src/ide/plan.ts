import { panelText } from '../core/i18n';
import { idePaletteCss } from '../core/palette';
import * as vscode from 'vscode';
import * as activity from '../core/activity';
import { Companion } from './workspace';
import { context as contextOf, fleetRows, gauge as gaugeOf, jobs as jobsOf, outgoing, schedules } from '../core/panel';
import { planLines } from '../core/transcript';

/**
 * The plan and the dials, in the sidebar.
 *
 * The JetBrains client folded these into its settings screen, by a decision its manual records.
 * That option is closed here: VS Code says ❌ "Create your own settings page/webview", so the
 * facts go back to a panel of their own — which is where they were before that fold.
 *
 * ⚠ This panel is often collapsed. Anything a person must not miss (a permission waiting on them)
 * is NOT only here; the status bar says it too.
 */
export class Plan implements vscode.WebviewViewProvider, vscode.Disposable {
  static readonly viewId = 'magi.plan';

  private view: vscode.WebviewView | null = null;
  private timer: NodeJS.Timeout | null = null;
  private compacting = false;
  private changingCouncil = false;
  private changing = false;
  private choices: { models: string[]; profiles: string[]; sessions: { id: string; title: string }[] } = { models: [], profiles: [], sessions: [] };
  private choicesLoaded = false;
  private choicesSession = '';
  private choicesLoading: Promise<void> | null = null;
  private readonly subs: vscode.Disposable[] = [];

  constructor(private readonly companion: Companion, private readonly selectSession?: (sid: string) => void) {
    this.subs.push(companion.onChanged(() => void this.refresh()));
  }

  resolveWebviewView(view: vscode.WebviewView): void {
    this.view = view;
    view.webview.options = { enableScripts: true };
    view.webview.html = this.html(view.webview);
    this.subs.push(view.webview.onDidReceiveMessage((m: { kind?: unknown; session?: unknown; command?: unknown; field?: unknown; value?: unknown }) => {
      if (!m) return;
      if (m.kind === 'ready') { this.choicesLoaded = false; void this.refresh(); }
      if (m.kind === 'select' && typeof m.session === 'string' && m.session === this.companion.session
          && typeof m.field === 'string' && typeof m.value === 'string' && !this.changing) {
        this.changing = true;
        void this.refresh();
        void this.applySelection(m.session, m.field, m.value).catch(error => {
          void vscode.window.showWarningMessage(String(error));
        }).finally(() => { this.changing = false; void this.refresh(); });
      }
      if (m.kind === 'council' && typeof m.session === 'string' && m.session
          && m.session === this.companion.session && !this.changingCouncil) {
        this.changingCouncil = true;
        void this.changeCouncil(m.session).catch(error => {
          void vscode.window.showWarningMessage(panelText(vscode.env?.language).changeFailed + ' ' + String(error));
        }).finally(() => { this.changingCouncil = false; void this.refresh(); });
      }
      const controls = new Set(['magi.chooseModel', 'magi.chooseBackend', 'magi.choosePermission',
        'magi.openConversation', 'magi.newConversation', 'magi.changeSetting', 'magi.setup', 'magi.approvals']);
      if (m.kind === 'control' && typeof m.command === 'string' && controls.has(m.command)
          && typeof m.session === 'string' && m.session === this.companion.session) {
        void Promise.resolve(vscode.commands.executeCommand(m.command)).catch(error => {
          void vscode.window.showWarningMessage('magi: ' + String(error));
        }).finally(() => void this.refresh());
      }

      if (m.kind === 'compact' && typeof m.session === 'string' && m.session.length > 0 && m.session === this.companion.session && !this.compacting) {
        this.compacting = true;
        void this.refresh();
        void Promise.resolve(vscode.commands.executeCommand('magi.compact', m.session)).catch(error => {
          void vscode.window.showWarningMessage('magi: ' + String(error));
        }).finally(() => {
          this.compacting = false;
          void this.refresh();
        });
      }
    }));
    view.onDidDispose(() => { this.view = null; if (this.timer) clearTimeout(this.timer); });
    const tick = () => { void this.refresh(); this.timer = setTimeout(tick, 5000); };
    tick();
  }

  private async loadChoices(): Promise<void> {
    if (this.choicesLoading) return this.choicesLoading;
    const session = this.companion.session;
    this.choicesLoading = (async () => {
      const [models, profiles, sessions] = await Promise.all([
        this.companion.ask('models', session ? { session } : {}), this.companion.ask('profiles'), this.companion.ask('sessions'),
      ]);
      this.choices = {
        models: models?.ok ? models.models || [] : [],
        profiles: profiles?.ok ? (profiles.profiles || []).flatMap(p => p.name ? [p.name] : []) : [],
        sessions: sessions?.ok ? (sessions.sessions || []).flatMap(s => s.id ? [{ id: s.id, title: s.title || s.id }] : []) : [],
      };
      this.choicesSession = session;
      this.choicesLoaded = !!models?.ok && !!profiles?.ok && !!sessions?.ok;
    })().finally(() => { this.choicesLoading = null; });
    return this.choicesLoading;
  }

  private async applySelection(session: string, field: string, value: string): Promise<void> {
    let method: string;
    if (field === 'session') {
      if (this.choices.sessions.some(s => s.id === value)) this.selectSession?.(value);
      return;
    }
    if (!session || session !== this.companion.session) return;
    if (field === 'model' && this.choices.models.includes(value)) method = 'set-model';
    else if (field === 'provider' && (!value || this.choices.profiles.includes(value))) method = value ? 'use-profile' : 'use-backend';
    else if (field === 'permission' && ['ask', 'auto', 'allow', 'deny'].includes(value)) method = 'set-permission';
    else return;
    const result = await this.companion.ask(method, { session, name: value });
    if (!result?.ok) throw new Error(result?.error || panelText(vscode.env?.language).changeFailed);
    if (field === 'provider') { this.choicesLoaded = false; await this.loadChoices(); }
    await this.companion.refresh();
  }

  private async changeCouncil(session: string): Promise<void> {
    const t = panelText(vscode.env?.language);
    const status = await this.companion.ask('status', { session });
    if (!status?.ok || session !== this.companion.session) return;
    const enabled = status.council !== true;
    const answer = await vscode.window.showWarningMessage(
      enabled ? t.councilEnableConfirm : t.councilDisableConfirm,
      { modal: true }, t.confirm);
    if (answer !== t.confirm || session !== this.companion.session) return;
    const result = await this.companion.ask('set-council', { session, name: String(enabled) });
    if (!result?.ok) throw new Error(result?.error || '데몬이 변경을 확인하지 못했습니다.');
    await this.companion.refresh();
  }

  private async refresh(): Promise<void> {
    if (!this.view) return;
    const session = this.companion.session;
    // Each of these is a door the daemon may or may not have. `about` says which, and a door that
    // is not advertised is not called — reading a refusal to find out is how a client learns to
    // draw an empty panel for an old build.
    // Nothing to talk to draws nothing — the same panel an old build with no doors draws.
    const caps = (await this.companion.caps()) ?? new Set<string>();
    if (caps.has('settings') && (!this.choicesLoaded || this.choicesSession !== session) && !this.choicesLoading) {
      void this.loadChoices().then(() => { if (this.choicesLoaded) void this.refresh(); }).catch(() => { this.choicesLoaded = false; });
    }
    const [jobs, ctx, fleet, cron, status] = await Promise.all([
      caps.has('job-kill') ? this.companion.ask('jobs') : null,
      /**
       * ⚠ **This door needs the conversation named, and asking without one is refused outright.**
       *
       * `answerContext` reads `req.Session` and returns "no session named" when it is empty — no
       * fallback to whatever is current, and the dispatcher never fills the field (the core's own
       * client passes `Session: sid` at every call site that needs it). This asked bare, so the
       * context section drew nothing on every poll, on every build, with `panel.context` returning
       * '' on the refusal and the panel saying nothing about it.
       *
       * Measured 2026-09-10 against a freshly built daemon in an isolated config: bare `context`
       * answers `ok:false, "no session named"`; the same call carrying the session answers with
       * `model`, `window`, `used`, `parts`. The JetBrains client has sent it all along.
       *
       * No session yet means no window to report, so it is not asked — a conversation that has not
       * started has nothing to be full of.
       */
      caps.has('context') && session
        ? this.companion.ask('context', { session: session })
        : null,
      caps.has('roster') ? this.companion.ask('roster') : null,
      caps.has('cron') ? this.companion.ask('cron') : null,
      caps.has('settings') && session ? this.companion.ask('status', { session }) : null,
    ]);
    if (!this.view || session !== this.companion.session) return;
    this.view.webview.postMessage({
      kind: 'plan',
      approvals: jobs?.jobs?.approvals ?? [],
      state: activity.label(this.companion.state),
      connection: this.companion.state.state,
      // "not asked" and "asked and empty" are different facts, and the panel says which.
      // ⚠ From the STRUCTURED fields. These four never fill `out` — measured against a live daemon —
      // and reading `out` drew three empty sections on every build, without failing: an absent field
      // is an empty string, and an empty string is what "nothing to report" looks like.
      jobs: jobs === null ? null : jobLines(jobs),
      // The door first, because it says more (what is filling the window, not just how much). The
      // stream is the fallback, and it is the only source on a daemon without that capability —
      // measured on one that has nine and not this.
      context: ctx === null ? (this.usage || null) : (contextOf(ctx) || this.usage),
      // The same door, as a gauge (see `panel.gauge`); the text above stays the fallback when there is none.
      gauge: ctx === null ? null : gaugeOf(ctx),
      session,
      compacting: this.compacting,
      settings: status?.ok ? { model: status.model, profile: status.profile || '', backend: status.backend, permission: status.permission, council: status.council === true } : null,
      changingCouncil: this.changingCouncil,
      changing: this.changing,
      choices: this.choices,
      fleet: fleet === null ? null : fleetRows(fleet),
      cron: cron === null ? null : schedules(cron).map((r) => r.line).join('\n'),
      handed: this.handed,
      outgoing: outgoing(jobs),
      hand: this.hand,
      plan: this.plan,
    });
  }

  private handed = '';
  private hand = '';
  private plan = '';
  private usage = '';

  /**
   * What became of this editor's own tools. Three outcomes, one sentence — see `EditorHand.handWhy`.
   *
   * In the panel rather than a popup: nothing is broken when a second window is refused, and a
   * notification would fire on every one of them. But it has to be SOMEWHERE, and the panel is
   * where a person already goes to ask what this companion is doing.
   */
  showHand(line: string): void {
    if (line === this.hand) return;
    this.hand = line;
    void this.refresh();
  }

  /** How full the window is, from the stream. Transient, so a reattached window has none until a turn runs. */
  showUsage(line: string): void {
    if (line === this.usage) return;
    this.usage = line;
    void this.refresh();
  }

  /**
   * The agent's plan, from the conversation stream.
   *
   * Held rather than fetched: `todos.changed` is an event, and the window that already streams the
   * transcript is the one that has it. Asking for it separately would make two readers of one fact.
   */
  showPlan(list: { content: string; status: string }[]): void {
    const next = planLines(list);
    if (next === this.plan) return;
    this.plan = next;
    void this.refresh();
  }

  /**
   * What has been handed to other companions.
   *
   * Held here rather than asked for: the receipts belong to this window (the far side knows them by
   * number, not by who asked), so if this panel had to fetch them there would be nowhere to fetch
   * them from.
   */
  showHanded(work: { who: string; asked: string; line?: string }[]): void {
    this.handed = work
      .map((w) => `${w.who} · ${w.asked.split('\n')[0].slice(0, 50)} → ${w.line ?? 'asked'}`)
      .join('\n');
    void this.refresh();
  }

  dispose(): void {
    if (this.timer) clearTimeout(this.timer);
    for (const s of this.subs) s.dispose();
  }

  private html(w: vscode.Webview): string {
    const nonce = Math.random().toString(36).slice(2) + Date.now().toString(36);
    const uiJson = JSON.stringify(panelText(vscode.env?.language)).replace(/</g, '\\u003c');
    const csp = `default-src 'none'; style-src ${w.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';`;
    return `<!DOCTYPE html><html><head>
<meta http-equiv="Content-Security-Policy" content="${csp}">
<style>
${idePaletteCss}
  body { margin:0; padding:8px 10px; font-family:var(--vscode-font-family);
         font-size:var(--vscode-font-size); color:var(--vscode-foreground); }
  h3 { font-size:.85em; text-transform:uppercase; letter-spacing:.04em; opacity:.7;
       margin:14px 0 4px; font-weight:600; }
  h3:first-child { margin-top:0; }
  .context-heading { display:flex; align-items:center; justify-content:space-between; gap:8px; margin-top:14px; }
  .context-heading h3 { margin:0; }
  .context-compact { color:var(--vscode-button-foreground); background:var(--vscode-button-background); border:1px solid var(--vscode-contrastBorder, transparent); border-radius:3px; padding:4px 8px; cursor:pointer; }
  .context-compact:hover:not(:disabled) { background:var(--vscode-button-hoverBackground); }
  .context-compact:disabled { opacity:.5; cursor:default; }
  .connection { display:flex; align-items:center; gap:7px; margin-bottom:10px; }
  .connection::before { content:''; width:9px; height:9px; border-radius:50%; flex:none; background:var(--vscode-descriptionForeground); }
  .connection.attached::before, .connection.working::before { background:var(--magi-success); }
  .connection.waiting::before { background:var(--magi-warn); }
  .connection.not-running::before { background:var(--magi-error); }
  .controls { display:flex; flex-wrap:wrap; gap:6px; margin:8px 0 16px; }
  .settings-form { display:grid; grid-template-columns:minmax(0,1fr); gap:10px; margin:10px 0 16px; }
  .field { display:grid; gap:4px; min-width:0; }
  .field-name { color:var(--vscode-descriptionForeground); font-size:.9em; }
  .field select { width:100%; min-width:0; box-sizing:border-box; height:28px; color:var(--vscode-dropdown-foreground); background:var(--vscode-dropdown-background); border:1px solid var(--vscode-dropdown-border, var(--vscode-panel-border)); border-radius:3px; padding:2px 6px; }
  .field select:focus-visible { outline:1px solid var(--vscode-focusBorder); }
  .session-control { display:flex; gap:6px; min-width:0; }
  .session-control select { flex:1; }
  .session-control button { flex:none; }
  .switch-row { display:flex; justify-content:space-between; align-items:center; gap:12px; }
  .switch-row input { appearance:none; width:30px; height:18px; border-radius:9px; border:1px solid var(--vscode-descriptionForeground); background:var(--vscode-input-background); cursor:pointer; position:relative; }
  .switch-row input::before { content:''; position:absolute; width:12px; height:12px; left:2px; top:2px; border-radius:50%; background:var(--vscode-descriptionForeground); }
  .switch-row input:checked { background:var(--vscode-button-background); border-color:var(--vscode-button-background); }
  .switch-row input:checked::before { left:14px; background:var(--vscode-button-foreground); }
  input:focus-visible { outline:1px solid var(--vscode-focusBorder); outline-offset:2px; }
  button:disabled, select:disabled, input:disabled { opacity:.5; cursor:default; }
  .section-heading { display:flex; align-items:center; gap:8px; }
  .section-heading::after { content:''; height:1px; flex:1; background:var(--vscode-panel-border, #80808044); }

  .controls button { color:var(--vscode-button-secondaryForeground); background:var(--vscode-button-secondaryBackground); border:1px solid var(--vscode-contrastBorder, transparent); border-radius:3px; padding:4px 8px; cursor:pointer; }
  .controls button:hover { background:var(--vscode-button-secondaryHoverBackground); }
  button:focus-visible { outline:1px solid var(--vscode-focusBorder); outline-offset:2px; }
  pre { margin:0; white-space:pre-wrap; word-break:break-word;
        font-family:var(--vscode-editor-font-family); font-size:.9em; }
  .handoff-row { display:flex; align-items:center; gap:6px; padding:3px 0; }
  .handoff-spinner { width:10px; height:10px; flex:none; border:2px solid currentColor; border-right-color:transparent; border-radius:50%; animation:handoff-spin 1s linear infinite; }
  @keyframes handoff-spin { to { transform:rotate(360deg); } }
  .none { opacity:.6; font-style:italic; }
  /* Context gauge — the Office task pane's shape: one thin bar whose scale is the whole window. */
  .gauge { display:flex; align-items:center; gap:8px; margin:2px 0; }
  .bar { flex:1 1 auto; height:6px; display:flex; overflow:hidden; border-radius:3px;
         background:var(--vscode-editorWidget-border, rgba(128,128,128,.25)); }
  .bar i { display:block; height:100%; }
  .bar i + i { box-shadow: inset 1px 0 0 var(--vscode-sideBar-background, transparent); }
  .gtext { flex:none; white-space:nowrap; font-size:.85em; opacity:.8; font-variant-numeric:tabular-nums; }
  .keys { display:flex; flex-wrap:wrap; gap:2px 12px; font-size:.85em; opacity:.8; }
  .keys span::before { content:""; display:inline-block; width:8px; height:8px; border-radius:50%;
                       margin-right:4px; vertical-align:-1px; background:currentColor; }
  .keys .est::before { display:none; }
  .p-system { color:var(--magi-partSystem); } .p-tools { color:var(--magi-partTools); }
  .p-talk { color:var(--magi-accent); } .p-calls { color:var(--magi-partCalls); }
  .p-results { color:var(--magi-partResults); }
  .bar i.p-system { background:var(--magi-partSystem); } .bar i.p-tools { background:var(--magi-partTools); }
  .bar i.p-talk { background:var(--magi-accent); } .bar i.p-calls { background:var(--magi-partCalls); }
  .bar i.p-results { background:var(--magi-partResults); }
  .note { font-size:.85em; opacity:.7; margin-top:2px; }
  /* Fleet rows: a short line and a state dot; the rest is the row's tooltip. */
  .row { white-space:nowrap; overflow:hidden; text-overflow:ellipsis; padding:1px 0; }
  .row::before { content:""; display:inline-block; width:8px; height:8px; border-radius:50%; margin-right:6px;
                 vertical-align:-1px; background:var(--vscode-descriptionForeground); }
  .row.working::before { background:var(--vscode-textLink-foreground); }
  .row.waiting::before { background:var(--magi-partCalls); }
  .row.gone::before { background:var(--vscode-errorForeground); }
</style></head><body><div id="body"></div>
<script nonce="${nonce}">
const vs = acquireVsCodeApi();
const t = ${uiJson};
const body = document.getElementById('body');
function section(title, text) {
  const h = document.createElement('h3'); h.className = 'section-heading'; h.textContent = title;
  const p = document.createElement('pre');
  if (text === null) { p.className = 'none'; p.textContent = t.unsupported; }
  else if (!String(text).trim()) { p.className = 'none'; p.textContent = t.empty; }
  else p.textContent = text;
  body.append(h, p);
}
function head(title) { const h = document.createElement('h3'); h.className = 'section-heading'; h.textContent = title; body.append(h); }
function contextSection(g, text, session, compacting) {
  const heading = document.createElement('div'); heading.className = 'context-heading';
  const title = document.createElement('h3'); title.textContent = t.context;
  const compact = document.createElement('button'); compact.type = 'button'; compact.className = 'context-compact';
  compact.textContent = compacting ? t.compacting : t.compact;
  compact.disabled = !session || compacting;
  compact.setAttribute('aria-busy', String(Boolean(compacting)));
  compact.addEventListener('click', () => vs.postMessage({ kind: 'compact', session }));
  heading.append(title, compact); body.append(heading);
  if (!g) { const fallback = document.createElement('pre'); fallback.textContent = text || t.noUsage; body.append(fallback); return; }
  const row = document.createElement('div'); row.className = 'gauge';
  const bar = document.createElement('div'); bar.className = 'bar';
  for (const s of g.segments) {
    const i = document.createElement('i'); i.className = 'p-' + s.kind;
    i.style.width = (s.fraction * 100) + '%'; i.title = s.label + ' ' + s.share + '%'; bar.appendChild(i);
  }
  const gaugeText = document.createElement('span'); gaugeText.className = 'gtext'; gaugeText.textContent = g.text;
  row.append(bar, gaugeText); body.append(row);
  if (g.segments.length) {
    const keys = document.createElement('div'); keys.className = 'keys';
    for (const s of g.segments) {
      const k = document.createElement('span'); k.className = 'p-' + s.kind; k.textContent = s.label + ' ' + s.share + '%'; keys.appendChild(k);
    }
    // The parts are a chars/4 estimate and speak only as shares of their own sum — say so.
    const est = document.createElement('span'); est.className = 'est'; est.textContent = t.estimated; keys.appendChild(est);
    body.append(keys);
  }
  if (g.note) { const n = document.createElement('div'); n.className = 'note'; n.textContent = g.note; body.append(n); }
}
function fleetSection(rows) {
  if (rows === null || !Array.isArray(rows) || rows.length === 0) { section(t.fleet, rows === null ? null : ''); return; }
  head(t.fleet);
  for (const r of rows) {
    const d = document.createElement('div'); d.className = 'row ' + r.dot; d.textContent = r.line; d.title = r.tip; body.append(d);
  }
}
function field(form, label, name, values, current, m) {
  const wrapper = document.createElement('div'); wrapper.className = 'field';
  const caption = document.createElement('label'); caption.className = 'field-name'; caption.textContent = label; caption.htmlFor = 'select-' + name;
  const select = document.createElement('select'); select.id = 'select-' + name; select.setAttribute('aria-label', label);
  const options = values.slice();
  if (current && !options.some(o => o.value === current)) options.unshift({ value: current, label: current });
  if (!options.length) options.push({ value: '', label: t.unavailable });
  else if (!current && !options.some(o => o.value === '')) options.unshift({ value: '', label: t.choose });
  for (const item of options) { const option = document.createElement('option'); option.value = item.value; option.textContent = item.label; select.append(option); }
  select.value = current || ''; select.disabled = m.changing || (name !== 'session' && !m.session) || !values.length;
  select.addEventListener('change', () => { vs.postMessage({ kind: 'select', field: name, value: select.value, session: m.session }); select.blur(); });
  wrapper.append(caption, select); form.append(wrapper); return wrapper;
}
window.addEventListener('message', (e) => {
  const m = e.data;
  if (m.kind !== 'plan') return;
  // A polling update must not remove a native dropdown while a person is choosing.
  if (document.activeElement && ['SELECT', 'INPUT', 'BUTTON'].includes(document.activeElement.tagName)) return;
  body.textContent = '';
  const connection = document.createElement('div'); connection.className = 'connection';
  const states = { attached: t.connected, working: t.working, waiting: t.waiting, 'not-running': t.disconnected, unknown: t.unknown };
  if (Object.hasOwn(states, m.connection)) connection.classList.add(m.connection);
  connection.textContent = states[m.connection] || states.unknown;
  connection.title = m.state || ''; connection.setAttribute('role', 'status');
  body.append(connection);
  const heading = document.createElement('h3'); heading.className = 'section-heading'; heading.textContent = t.conversation; body.append(heading);
  const form = document.createElement('div'); form.className = 'settings-form';
  const choices = m.choices || { sessions: [], models: [], profiles: [] };
  const session = field(form, t.session, 'session', choices.sessions.map(s => ({ value: s.id, label: s.title })), m.session, m);
  const sessionControl = document.createElement('div'); sessionControl.className = 'session-control';
  sessionControl.append(session.querySelector('select'));
  const create = document.createElement('button'); create.type = 'button'; create.textContent = '+'; create.title = t.newSession; create.setAttribute('aria-label', t.newSession);
  create.addEventListener('click', () => { vs.postMessage({ kind: 'control', command: 'magi.newConversation', session: m.session }); create.blur(); });
  sessionControl.append(create); session.append(sessionControl);
  const settings = m.settings || {};
  field(form, t.provider, 'provider', [{ value: '', label: t.defaultProvider }, ...choices.profiles.map(value => ({ value, label: value }))], settings.profile || '', m);
  field(form, t.model, 'model', choices.models.map(value => ({ value, label: value })), settings.model || '', m);
  field(form, t.permission, 'permission', ['ask', 'auto', 'allow', 'deny'].map(value => ({ value, label: t.permissions[value] })), settings.permission || '', m);
  if (typeof settings.council === 'boolean') {
    const label = document.createElement('label'); label.className = 'switch-row';
    const caption = document.createElement('span'); caption.textContent = t.council;
    const toggle = document.createElement('input'); toggle.type = 'checkbox'; toggle.setAttribute('role', 'switch'); toggle.setAttribute('aria-label', t.council);
    toggle.checked = settings.council; toggle.disabled = m.changingCouncil || !m.session;
    toggle.addEventListener('change', () => { toggle.disabled = true; toggle.checked = settings.council; vs.postMessage({ kind: 'council', session: m.session }); toggle.blur(); });
    label.append(caption, toggle); form.append(label);
  }
  body.append(form);
  const progress = document.createElement('div'); progress.className = 'note'; progress.setAttribute('role', 'status'); progress.textContent = m.changing || m.changingCouncil ? t.applying : ''; body.append(progress);
  const controls = document.createElement('div'); controls.className = 'controls';
  for (const [label, command] of [[t.agentSettings, 'magi.changeSetting'], [t.connectionSettings, 'magi.setup']]) {
    const button = document.createElement('button'); button.type = 'button'; button.textContent = label;
    button.addEventListener('click', () => { vs.postMessage({ kind: 'control', command, session: m.session }); button.blur(); }); controls.append(button);
  }
  body.append(controls);
  if ((m.approvals || []).length) {
    head(t.approvals);
    for (const pending of m.approvals) {
      const button = document.createElement('button');
      button.textContent = pending.requester + ' · ' + pending.prompt.what;
      button.addEventListener('click', () => vs.postMessage({ kind: 'control', command: 'magi.approvals', session: m.session }));
      body.append(button);
    }
  }
  section(t.plan, m.plan);
  section(t.jobs, m.jobs);
  head(t.handed);
  for (const row of m.outgoing || []) {
    const line = document.createElement('div'); line.className = 'handoff-row'; line.setAttribute('aria-busy', 'true');
    line.title = row.to + '\\n' + row.request + '\\n' + row.receipt;
    const spinner = document.createElement('span'); spinner.className = 'handoff-spinner'; spinner.setAttribute('aria-hidden', 'true');
    const text = document.createElement('span'); text.textContent = row.to + ' · ' + t.handoffWaiting + ' · ' + row.request.split('\\n')[0].slice(0, 90);
    line.append(spinner, text); body.append(line);
  }
  const handed = document.createElement('pre'); handed.textContent = m.handed || (!(m.outgoing || []).length ? t.empty : ''); body.append(handed);
  contextSection(m.gauge, m.context, m.session, m.compacting);
  fleetSection(m.fleet);
  section(t.scheduled, m.cron);
  section(t.editorTools, m.hand);
});
vs.postMessage({ kind: 'ready' });
</script></body></html>`;
  }
}

/**
 * The jobs section: what is running beside the turn, and what runs next.
 *
 * Queued work is drawn with it rather than in a section of its own — a person asking "what else is
 * it doing" means both, and two sections that are usually empty read as a broken panel.
 */
function jobLines(resp: Parameters<typeof jobsOf>[0]): string {
  const { queued, jobs: running } = jobsOf(resp);
  return [
    ...running.map((j) => `${j.id} · ${j.what}`),
    ...queued.map((q) => `next — ${q}`),
  ].join('\n');
}
