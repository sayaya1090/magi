package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The console as a static page, answered by a mock magi in the browser.
//
// # Why this exists
//
// Nobody can look at this screen without running a daemon, a console and at least one companion.
// That is a lot of setup to answer "what does it look like", and it means a change to the front end
// is reviewed as a diff of a Go string. A published demo turns the review into looking at it.
//
// # Why the mock is in the browser
//
// GitHub Pages serves files, not processes, so there is no /fleet to fetch. The demo replaces the
// page's `fetch` with one that answers from a fixture — the same trick the render tests use under
// node, for the same reason: the page's REAL javascript runs, against data shaped like the real
// handlers' output.
//
// The page itself is not copied or edited. This writes indexHTML verbatim and appends one script,
// so a demo can never drift from what the binary serves: there is only one page.
//
// # What it proves, and what it does not
//
// It proves the markup, the styling and the page's own logic — the states, the filters, the tabs,
// the forms, how it behaves on a phone. It does not prove a single handler: every answer is a
// fixture. The Go tests are what check the handlers, and the workflow runs those first, so a demo
// that deploys is a demo whose server side passed its own tests.
const demoScript = `
<script>
// A mock magi, in the page. Every answer below is shaped like the handler it stands in for; the
// page's own code is untouched and does not know the difference.
(() => {
  // The language packs are NOT inlined here. They were, as a Go string literal holding a copy of
  // language.en.json, and a copy is a thing that drifts — this demo would have kept showing the
  // old wording of a label the day somebody changed the real pack. The emitted directory carries
  // the real files and the fetch below hands their requests to the network untouched, so the demo
  // switches language exactly the way the console does, Korean included.
  // RFC3339 without the fractional seconds, because that is what the handlers emit
  // (time.RFC3339) — a fixture that carries milliseconds shows a timestamp shape the real console
  // never produces, and the demo is supposed to be evidence about the real one.
  const now = new Date().toISOString().replace(/\.\d+Z$/, 'Z');
  const fleet = [
    {socket: '/demo/design.sock', name: 'design', role: 'the design system: component specs and visual review',
     team: 'frontend', hub: true, workdir: '/Users/you/work/design-system', session: 'd1',
     state: 'working', live: true, task: 'spec the empty state for the fleet table, and name the exact tokens',
     doing: 'check 6, 4m12s elapsed, not met yet (exit 1)',
     steps: 7, planDone: 2, planTotal: 5, idle: 12, host: 'studio', addr: '10.0.0.4', pid: 4127},
    {socket: '/demo/api.sock', name: 'api', role: 'the billing API and its contracts',
     team: 'backend', workdir: '/Users/you/work/billing', session: 'a1',
     state: 'waiting', live: true, asking: 'run: psql -c "drop table staging_invoices"',
     askId: 'call_42', askKind: 'permission', task: 'add the idempotency key', steps: 3,
     planDone: 1, planTotal: 4, idle: 4, host: 'studio', addr: '10.0.0.4', pid: 4128},
    // A question rather than a permission, so the demo shows the other prompt and the report a
    // person is meant to decide on. Its sections are the default contract's — a console whose
    // operator has written their own decision-report skill would show theirs.
    {socket: '/demo/design.sock2', name: 'palette', role: 'colour and type', team: 'frontend',
     workdir: '/Users/you/work/design-system', session: 'p1', state: 'waiting', live: true,
     asking: 'which surface should the empty state sit on?',
     askId: 'call_51', askKind: 'question',
     report: [
       {key: 'tried', text: 'drew it on surface and on surface-container-low, both themes; measured 4.7:1 and 6.1:1 against the muted label'},
       {key: 'stakes', text: 'surface matches the table around it but the empty state stops reading as a panel; the container reads as a panel and is one more layer to keep in step with the cards'},
       {key: 'lean', text: 'surface-container-low — the contrast is the one with headroom, and light is already the tighter theme'},
     ],
     task: 'spec the empty state', steps: 5, idle: 22, host: 'studio', addr: '10.0.0.4', pid: 4131},
    {socket: '/demo/buttons.sock', name: 'buttons', role: 'components', team: 'frontend',
     workdir: '/Users/you/work/ui-kit', session: 'b1', state: 'idle', live: true,
     task: 'the toggle now reads its state from the store rather than a prop', idle: 640,
     host: 'studio', addr: '10.0.0.4', pid: 4129},
    {socket: '/demo/ops.sock', name: 'ops', role: 'deploys and alerting', workdir: '/Users/you/work/infra',
     session: 'o1', state: 'stopped', live: false, task: 'rotated the staging certificates', idle: 90000,
     host: 'mini', addr: '10.0.0.9'},
  ];
  // Sessions, dated against the clock so the board's day picker has yesterday and last week in it.
  const day = n => new Date(Date.now() - n * 86400000);
  const iso = d => d.toISOString().replace(/\.\d+Z$/, 'Z');
  const ran = (id, title, daysAgo, startH, hours, current, labels) => {
    const st = day(daysAgo); st.setHours(startH, 0, 0, 0);
    const en = new Date(st.getTime() + hours * 3600000);
    return {id, title, started: iso(st), ended: iso(current ? new Date() : en),
            // Two models across the fixtures, because one everywhere would not show that the label
            // is per SESSION rather than per companion.
            model: daysAgo > 1 ? 'qwen3-coder:30b' : 'qwen3-coder-next',
            labels: labels,
            ago: current ? 0 : Math.round((Date.now() - en) / 1000), current: !!current};
  };
  const HISTORY = {
    '/demo/design.sock': [
      ran('d1', 'spec the empty state for the fleet table, and name the exact tokens', 0, 9, 2, true, ['empty-state', 'tokens']),
      ran('d0', 'audit the button emphasis against the M3 scale and fix the inversions', 0, 6, 2),
      ran('c9', 'the filter chips are not reachable with a keyboard on the corrections page', 1, 14, 3),
      ran('b7', 'move the palette into styles.go so the two surfaces cannot drift', 4, 10, 5),
    ],
    '/demo/api.sock': [
      ran('a1', 'add the idempotency key to the billing endpoint', 0, 8, 3, true, ['billing']),
      ran('a0', 'why does the invoice job double-charge on retry', 1, 9, 6, false, ['billing']),
    ],
    '/demo/design.sock2': [
      ran('p1', 'which surface should the empty state sit on', 0, 11, 1, true),
    ],
    '/demo/buttons.sock': [
      ran('b1', 'the toggle should read its state from the store rather than a prop', 0, 7, 2, false, ['components']),
      ran('b0', 'ripple is missing on the tonal buttons in dark mode', 2, 13, 2),
    ],
    '/demo/ops.sock': [
      ran('o1', 'rotate the staging certificates before they expire', 1, 22, 2),
    ],
  };

  const answers = {
    '/fleet': fleet,
    // A search over what was said. Two hits, one of them a scheduled run, because unattended work
    // reads very differently from something a person asked for and a demo should show both.
    '/search': [
      {id: 's_7f2a', title: 'spec the empty state for the fleet table', when: '2026-08-03T10:12:00Z',
       turns: 3, snippets: [{ref: 's_7f2a#12', prompt: 'the empty state should sit on surface-container-low'},
                            {ref: 's_7f2a#31', prompt: 'name the exact tokens for it'}]},
      {id: 's_31bd', title: 'nightly audit', when: '2026-08-02T03:00:00Z', scheduled: 'nightly-audit',
       turns: 1, snippets: [{ref: 's_31bd#4', prompt: 'the empty state changed and nothing said so'}]},
    ],
    // The scheduled work. Two jobs, and one of them broken on purpose: a schedule that can never
    // run is the state the list exists to mark, and a demo where everything is fine never shows it.
    '/cron': [
      {name: 'nightly-audit', schedule: '0 3 * * *', enabled: true,
       next: new Date(Date.now() + 7 * 3600e3).toISOString(),
       prompt: 'walk yesterday\'s commits and report anything that looks like a regression',
       file: '/Users/you/work/design-system/.magi/config.toml'},
      {name: 'weekly-report', schedule: '0 9 * * 1', enabled: false,
       prompt: 'summarise what changed in the design system this week',
       file: '/Users/you/.config/magi/config.toml', global: true},
      {name: 'leap-day', schedule: '0 0 30 2 *', enabled: true,
       problem: 'this schedule never comes round',
       prompt: 'the one nobody noticed had stopped',
       file: '/Users/you/work/design-system/.magi/config.toml'},
    ],
    // Which machine this console is. A demo that left it blank would be showing the drawer with a
    // hole in it, and the hole is the part that answers "am I looking at the right one".
    '/console': {host: 'studio', configDir: '/Users/you/.config/magi',
                 peers: ['mini', 'laptop']},
    // A key so the notifications switch draws its live state rather than "this console has no push
    // key". Nothing can be subscribed here — there is no server to post to and the mock refuses
    // writes — but the reason a reader sees is then the browser's own, which is the true one.
    '/push': {key: 'BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8', count: 0},
    '/interventions': [
      {companion: 'design', socket: '/demo/design.sock', kind: 'steer', afterSec: 8,
       text: 'use the tokens from the scale, not hand-written spacing', at: now},
      {companion: 'buttons', socket: '/demo/buttons.sock', kind: 'steer', afterSec: 1140,
       text: 'use the tokens from the scale, not hand-written spacing', at: now},
      {companion: 'api', socket: '/demo/api.sock', kind: 'denied', afterSec: 95,
       text: 'call_31', at: now},
    ],
    '/skills': [
      {name: 'skill-tests-before-done', kind: 'skill', tier: 'global', observed: 6,
       firstSeen: '2026-06-30', lastSeen: '2026-08-07',
       description: 'run the tests before saying it is done',
       body: 'Run the project\'s own test command and read the output before reporting a task '
           + 'finished. A build that compiles is not a test that passed, and "it should work" is '
           + 'the sentence that precedes every regression in this repository.\n\n'
           + 'If the tests cannot be run, say so and say why, rather than landing the work quietly.'
           + '\n\n(source: agent)'},
      {name: 'skill-tokens', kind: 'skill', tier: 'project', companion: 'design',
       socket: '/demo/design.sock', observed: 3, firstSeen: '2026-07-14', lastSeen: '2026-08-06',
       description: 'spacing comes from the scale, never hand-written',
       body: 'Every margin and padding is a token from the spacing scale. A hand-written value is '
           + 'one more thing to keep in step with the rest, and it will not be.\n\n(source: agent)'},
      {name: 'skill-empty-states', kind: 'skill', tier: 'team', team: 'frontend', observed: 4,
       firstSeen: '2026-07-20', lastSeen: '2026-08-08',
       description: 'an empty state names the thing that is absent and how it stops being absent',
       body: 'Two lines. The first says what is not there; the second says the one action that '
           + 'would put something there. No illustrations, no apologies.\n\n(source: agent · '
           + 'spec the empty state for the fleet table)'},
      {name: 'mem-staging', kind: 'memory', tier: 'project', companion: 'api',
       socket: '/demo/api.sock', observed: 1, lastSeen: '2026-08-05', tags: ['ops'],
       description: 'the staging database is restored from prod every Monday'},
    ],
    '/mcp': [
      {name: 'docs', tier: 'global', url: 'http://localhost:3000/mcp', file: '~/.config/magi/config.toml'},
      {name: 'figma', tier: 'project', companion: 'design', socket: '/demo/design.sock',
       command: 'npx', args: ['-y', 'figma-mcp'], envNames: ['FIGMA_TOKEN'],
       file: '/Users/you/work/design-system/.magi/config.toml'},
    ],
    '/context': {model: 'qwen3-coder-next', window: 128000, used: 104300, estimated: false,
      messages: 61, compactions: 2, shed: 39000, lastBefore: 48000, lastAfter: 9000,
      lastAt: now, topics: ['internal/adapter/fleet/fleet.go', 'cmd/magi-web/page.go', 'discussion']},
    '/plan': [
      {content: 'read what the empty states do now', status: 'completed'},
      {content: 'write the spec', status: 'completed'},
      {content: 'name the tokens it uses', status: 'in_progress'},
      {content: 'get it reviewed by buttons', status: 'pending'},
      {content: 'fold it into the component docs', status: 'pending'},
    ],

    '/handoffs': [
      {from: 'design', to: 'buttons', socket: '/demo/buttons.sock', state: 'idle',
       request: 'make the toggle read its state from the store',
       answer: 'the toggle now reads its state from the store rather than a prop'},
      {from: 'design', to: 'api', socket: '/demo/api.sock', state: 'waiting',
       request: 'confirm the invoice endpoint is idempotent'},
    ],
  };
  // A demo cannot notify anybody: there is no console behind it to watch a fleet, and the service
  // worker the switch would register is not among the files exported here. Said out loud rather
  // than left to fail as "ServiceWorker registration failed", which reads as a broken page instead
  // of a copy of one. The page reads this flag; nothing else does.
  globalThis.MAGI_DEMO = true;
  const banner = document.createElement('div');
  banner.className = 'demo-banner';
  // The rail is fixed to the top of the window and knows nothing about this notice, so on a wide
  // screen the notice covered its first control — the button that widens it. The offset is set
  // from here because the banner is the demo's, and the page should not carry a rule about
  // furniture that only exists in a copy of itself.
  const pushBelowBanner = () => {
    const h = Math.ceil(banner.getBoundingClientRect().height);
    if (!h) return;   // not laid out yet; the caller tries again
    document.documentElement.style.setProperty('--demo-banner', h + 'px');
    const rail = document.getElementById('rail');
    if (rail) rail.style.paddingTop = 'calc(' + h + 'px + .7rem)';
  };
  banner.textContent = 'demo — the real page, answered by a mock. Nothing here is a running agent, ' +
                       'and every action reports what it would have sent.';
  addEventListener('DOMContentLoaded', () => {
    document.body.prepend(banner);
    // AFTER it is in the document. Measured before, it is zero high, and the rail stayed under it —
    // which is exactly what this exists to prevent.
    requestAnimationFrame(pushBelowBanner);
    addEventListener('resize', pushBelowBanner);
  });

  // Kept before the mock takes over: the language packs are real files sitting beside this page,
  // and they are the one thing here that must NOT be answered by a fixture.
  // ── the mock runs ────────────────────────────────────────────────────────
  // A still fixture cannot show what this page spends most of its code on: the fleet poll, the
  // board rebuilding itself, the transcript arriving a frame at a time, the live region announcing
  // a change. Those were unverifiable here — the demo answered once and never again — so the
  // things built for them could only be reasoned about, never watched. This ticks.
  //
  // It moves the fixture, not the page. Every mutation below is the shape a real handler would
  // return a second later; nothing reaches into the console's own code.
  // The state a companion is in is the column a person reads this page for, and until now only one
  // of them ever changed. A fleet that opens with one of each and then holds still shows the five
  // words but none of the four transitions between them — and the transitions are what the row,
  // the badge, the plan bar and the live region are all built to report.
  //
  // So: a cycle that visits every state change the console draws, and returns to where it began so
  // it can be watched twice. Each entry is [beat, what changes], and the opening state is kept so
  // the loop can restore it rather than drift.
  const opening = JSON.parse(JSON.stringify(fleet));
  const of = name => fleet.find(a => a.name === name);
  const script = [
    // idle → working. A companion that was sitting there picks something up.
    [2, () => Object.assign(of('buttons'), {state: 'working', live: true, steps: 0, idle: 0,
      task: 'give the switch a disabled state and the tokens for it', planDone: 0, planTotal: 3})],
    // working → waiting. The one thing a person watches this page for.
    [4, () => Object.assign(of('design'), {state: 'waiting',
      asking: 'the empty state needs a word for "nothing yet" — pick one', askKind: 'question', askId: 'call_77'})],
    // waiting → working: an answer arrives and the question clears. The asking line must go with it
    // or the row keeps showing a question nobody is being asked.
    [6, () => { const a = of('api'); a.state = 'working'; a.planDone = Math.max(a.planDone, 2);
      delete a.asking; delete a.askId; delete a.askKind; }],
    // A run lands on the board, which is the view that had no way to change at all.
    [7, () => { const runs = HISTORY['/demo/api.sock'] || (HISTORY['/demo/api.sock'] = []);
      runs.unshift({title: 'retry the failed invoice sync', started: new Date().toISOString().replace(/\.\d+Z$/, 'Z'),
                    current: true, model: 'qwen3-coder-next', labels: ['billing', 'retry']}); }],
    // stopped → live. A machine that was down answers again, which changes the fleet's own count.
    [8, () => Object.assign(of('ops'), {state: 'idle', live: true, idle: 0,
      task: 'rotated the staging certificates'})],
    [10, () => { const d = of('design'); d.state = 'working'; d.planDone = Math.max(d.planDone, 4);
      delete d.asking; delete d.askId; delete d.askKind; }],
    [12, () => { const p = of('palette'); p.state = 'working'; p.planTotal = 3; p.planDone = Math.max(p.planDone || 0, 1);
      delete p.asking; delete p.askId; delete p.askKind; delete p.report; }],
    [14, () => { const a = of('api'); a.planDone = Math.max(a.planDone, 3); }],
    // working → idle. The plan is spent and the companion is waiting for the next thing.
    [16, () => Object.assign(of('buttons'), {state: 'idle', planDone: 3, idle: 1})],
    [18, () => Object.assign(of('design'), {state: 'idle', planDone: 5, idle: 1})],
    // live → stopped, so the row that started stopped is reached from the other direction too.
    [20, () => Object.assign(of('ops'), {state: 'stopped', live: false, idle: 90000})],
    // A companion JOINS. The fleet's length changes, which is the case a list that only ever
    // mutates its rows never exercises — the count in the masthead, the team's own count, the
    // empty-to-populated path of the team block, and whatever keying the render does.
    [9, () => fleet.push({socket: '/demo/docs.sock', name: 'docs', role: 'the handbook and its examples',
      team: 'frontend', workdir: '/Users/you/work/docs', session: 'x1', state: 'working', live: true,
      task: 'write the empty-state page from the spec', steps: 0, planDone: 0, planTotal: 2,
      idle: 0, host: 'mini', addr: '10.0.0.9', pid: 4140})],
    // And LEAVES, which is the other half and the one that breaks a stale reference.
    [22, () => { const i = fleet.findIndex(a => a.name === 'docs'); if (i >= 0) fleet.splice(i, 1); }],
  ];

  // The context reading is a chart — a bar that fills and turns warn past 80% — and it was a
  // constant. It moves the way a real one does: up as turns are added, and down all at once when
  // a fold lands, which is the only event that ever gives a window room back.
  const ctx = answers['/context'];
  const ctxOpening = {...ctx};
  const planOpening = JSON.parse(JSON.stringify(answers['/plan']));
  let beat = 0;
  setInterval(() => {
    beat++;
    for (const a of fleet) {
      if (a.state === 'working') {
        a.steps++; a.idle = 0;
        // The plan bar moves on its own too, one step every third beat, and stops at the total —
        // a bar that runs past its plan is the bug this is here to make visible.
        if (a.planTotal && beat % 3 === 0 && a.planDone < a.planTotal) a.planDone++;
      } else if (a.live) a.idle++;
    }
    for (const [at, change] of script) if (beat === at) change();

    // The gauge climbs, and a fold drops it — 104300 of 128000 is 81%, so it opens just past the
    // threshold where the bar turns, comes down under it, and climbs back. Both sides of that
    // line get drawn.
    ctx.used = Math.min(ctx.window, ctx.used + 900);
    ctx.messages++;
    if (beat === 11) {
      ctx.lastBefore = ctx.used; ctx.used = Math.round(ctx.used * 0.42); ctx.lastAfter = ctx.used;
      ctx.compactions++; ctx.shed += ctx.lastBefore - ctx.used;
      ctx.lastAt = new Date().toISOString().replace(/\.\d+Z$/, 'Z');
    }

    // The plan changes too, and not only by ticking along: an item completes, and then one is
    // ADDED mid-run, which is what a planner doing its job looks like and is the case a bar built
    // from a fixed total gets wrong.
    const plan = answers['/plan'];
    if (beat === 5) { const at = plan.findIndex(t => t.status === 'in_progress');
      if (at >= 0) { plan[at].status = 'completed';
        if (plan[at + 1]) plan[at + 1].status = 'in_progress'; } }
    if (beat === 13) plan.push({content: 'check the Korean wording with the docs companion', status: 'pending'});
    if (beat === 19) { const at = plan.findIndex(t => t.status === 'in_progress');
      if (at >= 0) { plan[at].status = 'completed';
        if (plan[at + 1]) plan[at + 1].status = 'in_progress'; } }

    // The connection itself. A console that never loses one cannot show what it does when it has:
    // the state dot going red, the live region saying so, and the recovery that follows.
    if (beat === 15) dropStream();
    if (beat === 17) restoreStream();

    if (beat === 24) {                       // back to the opening, so the cycle can be watched twice
      fleet.splice(0, fleet.length, ...JSON.parse(JSON.stringify(opening)));
      Object.assign(ctx, ctxOpening);
      answers['/plan'] = JSON.parse(JSON.stringify(planOpening));
      beat = 0;
    }
  }, 3000);

  const realFetch = globalThis.fetch.bind(globalThis);
  globalThis.fetch = async (path, init) => {
    const url = String(path).split('?')[0];
    if (/i18n\/language\.[a-z]{2}\.json$/.test(url)) return realFetch(path);
    // A shell run answers with output, because the whole point of the row is what came back. The
    // other actions say what they would have done; this one cannot, since a summary reading "would
    // have run" beside an empty body teaches that the feature does nothing.
    if (url === '/shell' && init && init.method === 'POST') {
      const cmd = new URLSearchParams(String(init.body || '')).get('cmd') || '';
      const out = 'demo — this would have run in the daemon\'s workspace, as its user:\n  ' + cmd;
      banner.textContent = 'demo — would have run: ' + cmd;
      return {ok: true, status: 200, json: async () => ({out: out, exit: 0}),
              text: async () => JSON.stringify({out: out, exit: 0})};
    }
    if (init && init.method === 'POST') {
      // Actions say what they would have done rather than pretending they did it: a demo that
      // silently accepts a delete teaches the wrong thing about the real console.
      const body = init.body ? ' ' + init.body.toString() : '';
      banner.textContent = 'demo — would have sent: POST ' + url + body;
      return {ok: true, status: 204, text: async () => ''};
    }
      // History is per companion, so it is answered from the FULL path rather than the stripped one:
    // a board with the same four cards in every lane would be showing the mock, not the shape.
    if (url === '/history') {
      const who = new URLSearchParams(String(path).split('?')[1] || '').get('d') || '';
      const runs = HISTORY[who] || [];
      return {ok: true, status: 200, json: async () => runs, text: async () => JSON.stringify(runs)};
    }
    const body = answers[url];
    if (body === undefined) return {ok: false, status: 404, json: async () => [], text: async () => ''};
    return {ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body)};
  };
  // The transcript arrives over SSE, which a static host cannot serve either.
  //
  // Every instance is kept, because losing the connection is a thing this console has to be
  // watchable doing: the state dot going red, the live region saying it is reconnecting, and the
  // recovery afterwards. The page's own handler is what runs — the mock only fires onerror the way
  // a dropped socket would, and then hands back a fresh stream when it is asked for one.
  const streams = new Set();
  const dropStream = () => { for (const s of streams) { clearTimeout(s.timer); if (s.onerror) s.onerror(new Event('error')); } };
  const restoreStream = () => { for (const s of streams) if (s.onopen) { s.replay(); } };
  globalThis.EventSource = class {
    constructor() {
      // Delivered a turn at a time, the way the real stream does — the page re-renders the whole
      // transcript per frame, and a demo that hands it the finished conversation never shows that.
      // The reply carries a table, a fenced block and an inline mention of a tag, because those are
      // what the transcript renderer does and a demo that never shows them is a demo of the old
      // page. The tag is deliberate too: raw HTML in a transcript must appear as characters, and
      // this is where somebody can see for themselves that it does.
      //
      // The fence is built from char codes because this whole file is a Go raw string, and three
      // backticks written literally would end it. It has ended it twice.
      const FENCE = String.fromCharCode(96, 96, 96);
      const turns = [
        {who: 'user', text: 'spec the empty state for the fleet table, and name the exact tokens'},
        {who: 'thinking', text: 'Three empty states, and the tokens differ between them. Reading each before writing anything down.'},
        {who: 'assistant', text: 'Reading what the empty states do today.'},
        // The call and its result as ONE row, which is what the server sends: ok says how it
        // ended and the glyph on the summary flips with it. A second row here would be showing a
        // shape the console does not produce.
        {who: 'tool', tool: 'grep', args: 'pattern: empty, path: cmd/magi-web/page.go',
         ok: true, text: 'page.go:612  e.innerHTML = \'Nothing learned yet.<br>\'\npage.go:988  empty state for the board\npage.go:1136 .empty { max-width:52ch }'},
        // And one that failed, because the glyph and the colour only mean something against the
        // other case — and a failed call opens by itself, which is the behaviour to be able to see.
        {who: 'tool', tool: 'bash', args: 'go test ./cmd/magi-web/', ok: false,
         out: '--- FAIL: TestTheEmptyStateNamesItsTokens\n    page_test.go:88: no token named for the board'},
        // Left open, with no ok: the call running right now. The bar under it is the only place
        // this page says WHICH call it is waiting on.
        {who: 'tool', tool: 'write', args: 'path: docs/UI.md', pending: true},
        {who: 'assistant', text: 'Three of them, and none says what would be there.\n\n' +
          '| where | today | should be |\n|---|---|---|\n' +
          '| fleet | *Nothing learned yet.* | surface-container-low |\n' +
          '| board | (blank) | surface |\n| shared | (blank) | surface |\n\n' +
          'The rule, as one line:\n\n' +
          FENCE + 'css\n.empty { background: var(--magi-ref-surfaceContainerLow); max-width: 52ch; }\n' + FENCE + '\n\n' +
          'Note the current markup writes a literal <br> into innerHTML — that is the third defect, not a fourth.'},
      ];
      let n = 0;
      const step = () => {
        n++;
        if (this.onopen && n === 1) this.onopen();
        if (this.onmessage) this.onmessage({data: JSON.stringify(turns.slice(0, n))});
        if (n < turns.length) this.timer = setTimeout(step, 1400);
      };
      this.timer = setTimeout(step, 200);
      // Sending what it already sent is right, not lazy: a reconnecting stream replays the
      // transcript from the top, and the page has to end up with one copy rather than two.
      this.replay = () => { if (this.onopen) this.onopen();
        if (this.onmessage) this.onmessage({data: JSON.stringify(turns.slice(0, n))}); };
      streams.add(this);
    }
    close() { clearTimeout(this.timer); streams.delete(this); }
  };
})();
</script>
<style>
  .demo-banner {
    position:sticky; top:0; z-index:50; padding:.55rem 1.2rem;
    background:var(--magi-ref-primaryContainer); color:var(--magi-ref-fg);
    font:600 var(--md-sys-typescale-label-small-size)/1.5 var(--magi-ref-mono); letter-spacing:.06em; border-bottom:1px solid var(--magi-ref-outlineVariant);
  }
</style>
`

// emitDemo writes the static demo into dir.
//
// The page verbatim plus the mock, and a .nojekyll so a host that would otherwise hide files
// starting with an underscore does not eat part of the site. Nothing here is templated: the moment
// this function edits the page, the demo stops being evidence about the page.
func emitDemo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Root-relative becomes relative, for the demo only.
	//
	// The binary serves the page AT the root, so `/vendor/rxjs.js` is right there. GitHub Pages
	// serves a project site under /<repo>/, where a leading slash escapes to the domain root and
	// 404s — the module import among them, which means the script never runs and the page is
	// blank. Shipped exactly that way, and a local check that served the directory at the root
	// said everything was fine, which is how it got past.
	//
	// Only the prefixes the page loads are rewritten. Navigation hrefs keep their absolute form:
	// they are pushState targets read by the page's own router, not fetches.
	page := indexHTML
	for _, prefix := range []string{"/vendor/", "/font/"} {
		page = strings.ReplaceAll(page, "'"+prefix, "'."+prefix)
		page = strings.ReplaceAll(page, `"`+prefix, `".`+prefix)
		page = strings.ReplaceAll(page, "url("+prefix, "url(."+prefix)
	}
	for _, file := range []string{"/icon.svg", "/manifest.webmanifest"} {
		page = strings.ReplaceAll(page, `"`+file+`"`, `".`+file+`"`)
	}
	page += demoScript
	if strings.Contains(indexHTML, demoScript) {
		return fmt.Errorf("the page already carries the demo script — it must only ever be appended here")
	}
	for name, body := range map[string]string{
		"index.html": page,
		".nojekyll":  "",
		// The two the page links for a home-screen install. Same bytes the server answers with;
		// without them the demo is a page with a broken manifest, which is what it looked like
		// until a check started walking every path the page references.
		"manifest.webmanifest": manifestJSON,
		"icon.svg":             iconSVG,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	// Everything the page IMPORTS, at the path it imports it from. A missing font degrades the
	// look; a missing module means the script never runs and the page is blank — which is exactly
	// what shipped the first time this became a module, because emitDemo copied the fonts and
	// nobody had written down that the list had grown.
	// Everything in vendor/, not a list somebody keeps in step: the page imports what it imports,
	// and a hand-written list is one more thing to forget when a second bundle arrives — which is
	// exactly what happened with the Material Web one.
	vendored, verr := assetFS.ReadDir("vendor")
	if verr != nil {
		return verr
	}
	var carry []string
	for _, f := range vendored {
		if strings.HasSuffix(f.Name(), ".js") {
			carry = append(carry, "vendor/"+f.Name())
		}
	}
	// Every language pack, for the same reason and read the same way. The demo used to answer these
	// from a copy inlined in its own script, which meant the deployed page was English whatever the
	// reader's browser asked for — the one part of the console a visitor can check against their own
	// machine, and it was the part that did not work.
	packs, perr := assetFS.ReadDir("i18n")
	if perr != nil {
		return perr
	}
	for _, f := range packs {
		if strings.HasSuffix(f.Name(), ".json") {
			carry = append(carry, "i18n/"+f.Name())
		}
	}
	for _, name := range carry {
		b, rerr := assetFS.ReadFile(name)
		if rerr != nil {
			return rerr
		}
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	// The language seed the handler inlines for a real page. Written into the file here for the
	// same reason: without it the first paint is a screen of dotted keys.
	if pack, perr := assetFS.ReadFile("i18n/language.en.json"); perr == nil {
		page = "<script>window.__LANG=" + string(pack) + "</script>\n" + page
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
			return err
		}
	}

	// The font the page asks for, at the path it asks for it. Without this the demo falls back to
	// the system serif and stops being a fair look at the thing.
	fonts, err := fontFS.ReadDir("fonts")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "font"), 0o755); err != nil {
		return err
	}
	for _, f := range fonts {
		if !strings.HasSuffix(f.Name(), ".woff2") {
			continue
		}
		b, rerr := fontFS.ReadFile("fonts/" + f.Name())
		if rerr != nil {
			return rerr
		}
		if werr := os.WriteFile(filepath.Join(dir, "font", f.Name()), b, 0o644); werr != nil {
			return werr
		}
	}
	return nil
}
