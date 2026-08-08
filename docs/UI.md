# magi's screens — the web console and the TUI

Both surfaces: what is on them, the design rules, and why. §1–5 are the **web console**
(`cmd/magi-web`); §6 is the **terminal UI** (`internal/adapter/tui`). Korean: [`UI.ko.md`](UI.ko.md).
How to run them: [`MANUAL.md`](MANUAL.md) (§4 the TUI, §12 the console). Internals:
[`ARCHITECTURE.md`](ARCHITECTURE.md) §11.

> **Look at it:** <https://sayaya1090.github.io/magi/> — the real page, answered by a mock in the
> browser. Every action there reports what it would have sent rather than pretending it happened,
> and every reading is a fixture: it shows the screens, not a working server. Published by
> `.github/workflows/pages.yml` on any change under `cmd/magi-web/`, and only after that package's
> tests pass. A push publishes only from main; a hand-run publishes from whatever
> branch it was asked for, if the github-pages environment allows that branch (Settings →
> Environments → github-pages → Deployment branches).

> **As-built.** The console's front end is one file — `cmd/magi-web/page.go`, a Go string holding
> the HTML, CSS and JS — and the rules below are pinned by tests that run its real JavaScript under
> node (`render_test.go`). The TUI lives in `internal/adapter/tui`, with its own tests for
> rendering, mouse handling and width measurement.

---

## 1. The four questions this page answers

A supervisor asks four things a day, several times:

1. **Who is doing what** — and which of them is waiting on *me*
2. **What have they learned** — and how far does each lesson reach (project / team / global)
3. **What has been done, and when** — a board of work with a day you can move
4. **What can they reach** — which MCP server is attached to which companion

The four destinations are exactly those. A fifth needs a fifth question first.

⚠ There used to be a fifth, **what I had to say** — every moment somebody stepped into a running
turn, grouped by the words, each promotable into the project or global tier. It is gone. Grouping
corrections was a rule factory with no evidence that any rule it produced was wanted, and the
promotion pipeline asked a person to decide something they had no grounds for. What survived is the
useful half: the interventions for ONE companion, on that companion's own page, as a reading about
it rather than as a queue of candidate rules.

## 2. The console's screens

### 2.1 The fleet (`/`)

A resource table. As in a Kubernetes console, **state comes first**, then the name, then what it is
doing. The order is the order the eye should travel: **waiting on a person → working → idle → gone.**

- The **summary tiles** are counts and filters at once. A tile at zero is not clickable — a control
  that can be pressed and does nothing is worse than one that cannot.
- **Grouped by team**, trouble first within each. A heading names the team, says which companion
  speaks for it, and badges how many of its members are waiting.
- A row: state badge · **how far through its own plan** (`3/7`) · name · **role** · workspace path ·
  what it is doing · host and IP · idle time · one icon button to interrupt. Not a progress bar: a
  todo list is not a schedule, and a bar would promise a completion time nobody can honour. There is
  no "open" button — the row is a link, and a button repeating what the row already does is a second
  way to do one thing.
- **A blocked agent shows the question**, not the work — the question is why nothing is happening,
  and under it **the grounds it wants decided on**: what the agent tried, what each way costs, and
  which it would pick (§2.6). The answer controls sit under the question rather than in the actions
  column, because they belong to the prompt and not to the row.
- The composer at the bottom has **no address field**. Naming a companion by typing, from a list
  where it is already on screen and one click away, is a second way to do what the list does — and
  the harder one, since it asks somebody to spell a name they can see. The `/dispatch` endpoint that
  resolves an address by role is still there; nothing on this page calls it.

### 2.2 One companion (`/?d=<socket>`)

**Two things are wanted here and they are not the same thing.** Somebody opens a companion to see
its state, or to read what it is saying and steer it. Everything stacked in one column served the
first and buried the second: measured at 430px the transcript began 1073px down a 900px screen, and
at 900px it was the same — this was never a phone problem.

| | ≥1100px | below |
|---|---|---|
| layout | two columns: the conversation, and the facts beside it | two panels, **대화 / 상태** |
| what stays put | the side column is sticky — a plan that scrolls away is one you re-find | — |

Which panel you were on is **not in the URL**. The destination is the companion; which half of it
you were reading is a scroll position, and putting it in the address bar would make a link somebody
sends land on a screen they did not mean to share. Leaving a companion resets it, because the next
one is opened for its conversation.

The facts card **folds**, at every width, remembered across companions. It answers "what am I
looking at" once and is then 547px of masthead between a reader and the conversation; folded it is
58px with the state and the workspace still on its summary line.

Header fields: status · workspace · role · team · host, address and pid · steps · last activity ·
session id · **model** · **context** · **what was summarised away**.

The context line is the point of this screen:

- `82,000 / 100,000 tokens · measured · 41 messages` — **measured and estimated are never confused.**
  One is the provider's own count, the other is arithmetic over the transcript, and a decision made
  on them is worth different amounts.
- No bar when the window is unknown. An empty track reads as "nearly empty", which is the opposite
  of "we do not know".
- `2 folds · 31,000 tokens shed · last 40,000→9,000 at 04:31Z`. A compaction is the one moment a
  companion silently stops knowing something, and how many there have been decides whether its
  earlier reasoning can be assumed still there.
- The folded **topics are named**. That is where "the detail is not lost" stops being a claim.
- `82% of it cached`, **only when the backend says so**. A backend reporting `cached: 0` means the
  cache missed; one reporting nothing means it does not tell us, and drawing both as 0% would call
  a working cache broken. Measured on the default local backend (Ollama /v1, 2026-08-07): it sends
  no cache field at all, so the panel says "this backend does not report it" and shows no figure.

Then **the plan it is working through** — the agent's own todo list as it last recorded it, shown
as it is: an item it dropped is gone, because the record is the whole plan each time and merging
would resurrect what it decided against.

Then **what it handed out**: work this companion gave to others, and the answers. A receiver
answers in its own transcript — there is no reply channel — so this is the five-page walk done
once. **An answer appears only when the receiver is idle**: a line taken mid-turn is what it
happened to be saying, not a conclusion.

Then **what I had to say to this one** — every moment somebody stepped into one of its turns, with
how far in (`8s into the turn` corrects the instruction; `20m in` corrects the work, and no rule
would have helped). Derived, not recorded: a user prompt arriving while a turn is open *is* a steer,
so a screen written today answers for last month's logs.

Then **what it has done before now** — every session it has ever run, newest first, by the request
that opened it. The title is the request as it was made; a summary would be this process deciding
what the work was about, which is a judgement it has no grounds for.

Below that: the live transcript (SSE) and the composer.

**When it is blocked on a question, the composer becomes the answer field.** Both drawn, the page
had two text fields stacked — the upper one answering the question, the lower one addressing work to
an agent that is not listening — with nothing saying which was which. One field in two roles: the
same component, a different label and note, and what is typed goes to `/answer`. A permission prompt
keeps its own three buttons and leaves the composer alone, because they are buttons and nothing
collides, and "do something else instead" is a legitimate reply to being asked whether to drop a
table.

### 2.3 The board (`/?v=board`)

Work as cards, a column per companion, and a day you can move.

A kanban's columns are usually a **state**, and a state is a fact about NOW: there is no such thing
as the state a companion was in last Tuesday, because the fleet derives state from what is open in a
log and nothing is open in a log from last week. Columns of state would be a board that could only
ever show today — the one day the fleet page already covers. So the column is **who did it** and the
card is **the work**, which reads the same on any day.

- Arrows step a day at a time, in UTC and in whole days: local-midnight arithmetic lands on 23:00
  the previous day twice a year, and a board that skips a day is worse than no arrows. The date
  field stays for jumping a month back, where stepping would be thirty clicks. Forward is disabled
  on today, not hidden — a control that vanishes moves the ones beside it.
- A session counts for a day if it was **running at any point in it**. A task started at 23:40 and
  finished at 01:10 belongs to both days somebody might ask about, and belonging to neither is how a
  long night disappears from a board.
- ⚠ **There is no unassigned column, and there cannot be one.** Every `/submit` resolves a socket
  and a session before it is accepted, so a request with no owner has no way to exist in this
  system. A column for it would be permanently empty, which is a lie about the shape of the thing.

### 2.4 Experience (`/?v=skills`)

**Three** tiers of the store — rules and remembered facts, local and from every federated console.

- Each row leads with **the reach, in words**: `every companion` / `the frontend team` / `only api
  on laptop`. Words rather than a colour, because the decision made here is exactly which of those a
  rule should be. Sorted widest first, so the rows read as three rings rather than an alphabet, and
  each tier gets its own colour — painted with the project's, a team rule was indistinguishable from
  a project one, which is the single thing that word is on the row to say.
- The **team tier** sits between a workspace and a machine, because that is where most of what a
  team knows actually lives: a convention four companions share is neither one project's nor every
  project's. Listed from the directory rather than from a roster — a team that has been disbanded
  still has knowledge, and reading membership would make it vanish the moment nobody was left on it.
  ⚠ It is **one machine's** directory (`<config>/teams/<name>/experience`); nothing syncs it between
  machines yet (§7).
- **The body can be read**, and it says **what the agent was doing when it learned it**. Every entry
  used to record its source as the word "agent", which answers who wrote it — a question nobody is
  asking. The question in front of a rule you did not write is what it came out of.
- A fact is set in italic, quoted. **A stale fact is merely wrong; a stale rule is an instruction
  still being followed.**
- `seen 3× · 2026-07-14 → 2026-08-07` tells a settled lesson from a one-off.
- A wrong one can be forgotten. **Nobody writes into a store they cannot correct.**

### 2.5 What they can reach (`/?v=mcp`)

An MCP server is where a companion's reach leaves this machine's file system: a tracker, a design
tool, an internal API. Which ones each has meant opening config files one at a time, and a
supervisor holding five companions could not answer "which of these can see production".

- Each row carries the **transport line complete and unprettified** (`npx -y figma-mcp`, or the
  URL), because that line is the answer to "what actually runs".
- Environment variables are **named, never valued**. A token on a page is a token in a browser
  history, a screenshot and a support ticket.
- Adding, changing and removing write to that companion's own `.magi/config.toml`. A person editing
  their own companion's config from the console is the same act as opening the file; a COMPANION
  accepting a server definition from another companion is still refused, because that is a command
  arriving from the network.
- **It says when the change takes effect.** Servers attach when a daemon starts, so this changed a
  file and not a running process.

Refused: a definition that is neither a url nor a command; one that is both (the url branch wins at
startup, so the command would sit there never running); and a name that cannot be a TOML table
header — refused rather than sanitised, since a name quietly rewritten is a server nobody can find
again in their own file.

### 2.6 The grounds a decision rests on

Until recently a decision reached the console as a question and two to four option labels — "which
branch should this land on?" with `[main] [engine-ui-split]` — and nothing about what had been
tried, what either choice costs, or what the agent would pick. A person was asked to decide with no
grounds, by a machine that had spent an hour accumulating exactly those grounds and then dropped
them.

The shape is defined by a **skill**, not a config field: what belongs in a report is a matter of
taste, and taste differs per person and per kind of work. Skills already carry that — a companion
whose workspace holds its own `decision-report` gets its own report, everyone else gets the global
one, and with neither the default applies: **tried · stakes · lean**.

magi checks the sections rather than trusting the prose. A skill that says "write what you tried" is
advice, and this tree has repeatedly watched advice evaporate under pressure. The model supplies
named fields; the tool refuses a report with one missing or blank, naming what is absent.

⚠ The section names are the skill's own identifiers and are **not translated** — a custom skill's
keys could not be, and translating only magi's defaults would make the two inconsistent.

### 2.7 Being told (`/push`, `/sw.js`)

The console had exactly one way to say that a companion is blocked: the tab title. That reaches
somebody with the tab in front of them, and the fleet page exists for the person who is not.

- **Web Push**, the browser's own mechanism, implemented against RFC 8291 and 8292 in
  `internal/core/webpush` with no dependency — every primitive is in the standard library, and the
  arithmetic is pinned to the RFC's own worked example byte for byte. That is the only test that
  says anything about interoperating: this shape round-trips against itself with the info string
  wrong or the salt and key material swapped, and a decrypt-what-I-encrypted test passes all of it.
- The push service relays and **must not be able to read**. The payload is encrypted to a key the
  browser generated and never sent anywhere but here; the request is signed with a key this console
  generated and never sends at all.
- Only the **edge** into waiting is announced. A blocked companion stays blocked until somebody
  answers, so state alone would buzz every three seconds; the other direction is silent, because "it
  is no longer waiting" is not worth waking a phone for and notifications that say nothing teach
  people to swipe them away unread.
- The switch **says why when it cannot be used**, and the reasons are different: no push support, or
  not a secure context, or a permission already refused — only the last of which the reader can
  undo, and only in their own settings.
- ⚠ A service worker needs a secure context. magi-web binds loopback and nothing else, so the
  ordinary way of reaching it from a phone is a tunnel to `localhost` — which **is** a secure
  context. No certificate is needed.
- The console still has no authentication, so anyone who can open it can point a notification at
  their own browser. That is the access they already have to read every transcript on it.

## 3. The design language

### 3.1 Material 3, actually — audited

The colours used M3's role NAMES over a palette of our own and nothing else of the system, until
somebody looked at the screen and said so. Counted then: every radius 2px (not on the shape scale),
type at 9.5·10.5·11.5·12.5·13.5·15.5·17px (not on the type scale), no `on-` roles, no surface
containers, no state layers. Counted now, mechanically, by the audit in `.claude/skills/material-3`:

| | |
|---|---|
| shape | every radius is a `--shape-*` token or a stated 0 — **checked**, after three dots drawn `border-radius:50%` were found |
| type | literals are 11·12·14·16px, the rest through tokens — **checked**, after 10px and 13px had crept back in |
| colour | 10 `on-` roles, 5 surface-container roles, both themes |
| interaction | 11 component kinds bring their own; one state-layer recipe for the 7 surfaces that are not components |
| motion | `cubic-bezier(0.2, 0, 0, 1)` / 100ms, from material-components-android's Motion.md |
| targets | 48px, measured on the components' own touch target rather than on their visual box — every control on the page clears it |

### 3.1b The components, and what is left to us

Every control on the page is Material Web's: `md-filled-button`,
`md-filled-tonal-button`, `md-text-button`, `md-outlined-text-field`, `md-outlined-select` /
`md-select-option`, `md-filter-chip` in an `md-chip-set`, `md-outlined-card`, and `md-tabs` /
`md-primary-tab`. They are vendored and served from the binary — see `cmd/magi-web/vendor/README.md`
for the build, which runs once and is committed.

The rule that came out of doing it: **a rule on a component's host cannot reach its label**, which
lives in a shadow root. Fonts, colours and sizes are said in the component's own tokens
(`--md-text-button-label-text-*`); only inherited properties — `letter-spacing`, `text-transform` —
cross the boundary. A page that keeps its old CSS beside a component gets two descriptions of one
control, and the one it can no longer see wins nothing: this page carried inert `font:` declarations
on four controls and a `border-bottom` drawing a second underline around a button's own box.

What is still ours: the layout, the roles and typescale set once at `:root` (a component reads
`--md-sys-color-*` and `--md-sys-typescale-*-font`, and draws the library's purple and its fallback
face for any role we leave unsaid), and the surfaces that are not controls — rows, panels, the
transcript.

Deliberate deviations, each for a reason written at the rule: the interactive rows in the fleet are
links rather than cards (`md-outlined-card` has no role and no focus ring, so a card there would
trade the keyboard for a box), and where a control of ours is disabled it is drawn by the cursor and
a missing hover rather than by fading — the contrast guard caught 3.49:1 on the first attempt. The
components fade their own disabled labels, which the guard cannot see into and does not need to:
WCAG exempts inactive controls.

### 3.1a Roles and editorial layout

The colours use **M3 role names** with values taken **verbatim** from the TUI's palette
(nervDark / nervLight in `internal/adapter/tui/styles.go`). Two surfaces drawing the same agent in
different colours is one thing to learn twice.

**That is checked now, not asserted.** It was a sentence in this document, and it happened to be
true — but nothing would have said so when it stopped being, which is the shape of half the defects
this tree has found. `TestTheWebTakesItsColoursFromHere` reads both files and fails on any shared
role whose values disagree, and on any colour the stylesheet declares that the palette does not
name — the web inventing a value the TUI cannot follow.

Making it checkable moved 28 roles into the palette: the `on-` half of every container pair, the
five tonal surface containers per theme, the page's own background and foreground, and the scrim.
**The terminal draws none of them** — it has no stacked surfaces to tone and lets its own
background show through — so they sit in `styles.go` as the origin rather than as something
lipgloss reads. The alternative was letting the web own a palette of its own, which is how the two
surfaces drift.

The editorial part is the layout: a `74ch` measure for prose, `108ch` for the transcript (its lines
are code, where wrapping costs more than width), generous rules, a gutter of small-caps labels, and
a user's turn set like a pull quote — display face, rule down the left.

### 3.2 Language

Labels come from a pack per locale — `cmd/magi-web/i18n/language.{en,ko}.json`, flat dot-keyed —
chosen by `localStorage['lang']`, then the browser, falling back to English. The convention is
borrowed wholesale from the handbook project rather than invented.

The handler inlines the English pack ahead of the page, so the FIRST paint already has words; a
pack that lands later repaints the labels written in the markup and **does not re-render the
view** — a pack can arrive mid-interaction, and re-rendering there wipes what somebody is reading
(found exactly that way: a detail panel lost its context block while the language was in flight).

A test requires every key the page asks for to exist in BOTH packs, so a label can never render as
its own dotted key because a string was added to one file.

### 3.3 Type

- **Newsreader** (OFL), an editorial serif drawn for screens, **embedded in the binary** and served
  from it (`/font/`). A page that fetched its typeface would make its own appearance depend on
  somebody else's machine, tell that machine when you look at your agents, and fall back to
  something else on a laptop with no route out.
- Subset, ~60KB. What the subset cannot carry, the system stack behind it does — that stack is there
  so a Korean or Japanese transcript does not render as tofu.
- Monospace for labels, states, paths and the transcript. Every line there is something the machine
  said or did, and a serif would be dressing up evidence.

### 3.4 Contrast and focus

Contrast is **computed, not eyeballed**: the quiet parts still have to be readable, so `--muted`
clears WCAG AA against its background. Everything interactive has a `:focus-visible` outline, and
every touch target measures **48px or more** — read off the components' own touch element, not off
their visual box, which is 40px and would have read as a failure it is not.

### 3.5 Dark and light, and the reader's say in it

Both palettes are defined, and the reader can override the machine **in both directions** — which
is the part a `prefers-color-scheme` query alone cannot do. It took stating the light palette
twice: once under the query, for the machine's answer, and once on `:root[color-theme="light"]`,
for the reader's. CSS has no way to give one ruleset both selectors across a media boundary, so
`TestBothLightThemesSayTheSameThing` fails if the two copies drift.

`system` is a choice too, and it is stored as that word rather than as the value it resolves to —
somebody who picks it on a light morning is still following the machine that evening. It is the
absence of the attribute, so the query underneath answers.

The choice is applied by four lines in the head, **before the stylesheet**. After first paint it is
a flash of the other theme, and on a dark-preferring machine set to light that is a white flash in
a dark room.

(This section used to say "there is no toggle — the OS already knows, and asking again is asking
twice." The OS knows what the machine prefers, which is not always what the reader wants on it.)

### 3.5a Three widths, one navigation

The breakpoints are **600px** — M3's own boundary between compact and medium, so the rail appears
where the guide says it should — and **1100px**, where a companion's two columns fit.

| | ≥1100px | 600–1099px | <600px |
|---|---|---|---|
| navigating | the rail | the rail | the tabs |
| a companion | two columns | two panels, 대화 / 상태 | two panels |
| the rail, opened | floats over the page | floats over the page | slides in over the page |

It is **one element in three modes**, not several that have to agree. The rail's items are
`md-list-item` with an `href`, which the component renders as a real anchor: a navigation made of
addresses should survive a middle click.

**Opening the drawer moves nothing.** The rail's width and the gutter the page reserves used to be
one number, so widening the rail took 184px out of the content box: `main` and the header stayed
put, measured at dx 0, while everything inside them shifted right and narrowed. Two names now —
`--rail-w` is the gutter and never changes, `--rail-now` is how wide the rail is drawing itself. A
test fails if anything but the rail's own width reads the second one.

The **scrim is an element**, at every width. Drawn as a box-shadow on the rail it darkened the page
without covering it, so a page that looked disabled took a click and navigated away underneath; and
its spread animated with the rail's width, so the dimming swept across rather than arriving. It
does not animate at all now — what is behind the drawer is behind it the moment it opens — and a
click on it closes the drawer, which is the dismissal every modal surface has.

Which destination is current is written once, in `render()`, beside the tabs' own index. Two places
saying where you are is how they come to disagree. ⚠ The selected state is painted on the **host**,
not through `--md-list-item-container-color`, which does nothing in the shipped build — measured
with an opaque colour, the container stays transparent. The slotted icon takes the colour the same
way the label does, because the label is `display:none` on a collapsed rail, which is how it stands
most of the time. A companion's page keeps the companions destination lit: it is inside it.

### 3.5b Preferences

One dialog, reached from one icon at every width: **language** (browser/English/한국어),
**notifications** (§2.7), and **which machine this console is** — the host and the config
directory, from `/console`.

**Theme is not in it.** It has a toggle in the masthead — one tap for the setting that gets changed
most — and a select saying the same thing three feet away was one preference with two controls and
two ways to be wrong about it.

That last one is not an account. magi has no users to log in; the console is reachable by whoever
can reach the port. What a supervisor with three of these open actually asks is which machine they
are looking at, and before this the answer was to recognise the companions in the list — which
fails exactly when two machines are running the same work.

### 3.5c Motion

M3's **fade-through** where the page swaps one body of content for another — a destination, a
companion's two panels — from 96% rather than 92%, because a table of monospaced text at 92% is
visibly the wrong size for a tenth of a second and the point is to say "this is new", not to zoom.
The prompt bar rises ten pixels instead, since it arrives from the edge it is fixed to.

Fired on **navigation only**. The fleet redraws itself every three seconds and the prompt is rebuilt
on each of those polls, so animating on "this is visible" would restart the entrance three times a
minute under somebody trying to read the question.

`prefers-reduced-motion` turns all of it off — not shortens, off. The override is universal so it
reaches the components' own animations, including md-tabs' indicator inside a shadow root this
stylesheet cannot otherwise name. 0.01ms rather than 0, so an `animationend` that something waits on
still arrives.

### 3.6 On a phone

- The composer wraps so the text box keeps a full row. Measured: without it the box was
  squeezed to a third of the row and the placeholder was cut mid-sentence.
- The tab row wraps too. Three sentence-shaped labels are wider than 390px, and a nav that overflows
  takes the whole page sideways with it — the one scroll direction a phone should never have.
- The transcript's label gutter folds above its text: five and a half characters of gutter is most
  of a narrow screen.
- The dock (prompt bar + composer) is **measured** and its height reserved as padding. A constant
  either wastes a screen or hides the last thing the agent said — on a phone, the second.
- Enter sends on a keyboard and inserts a newline on touch: a soft keyboard's return is the only way
  to break a line there.

### 3.7 PWA

`manifest.webmanifest` and `icon.svg`, so it opens from a home screen without an address bar. The
icon is served by the binary too, for the reason in §3.2. A page added to a home screen is also
what lets iOS deliver a push (§2.7).

The service worker at `/sw.js` **caches nothing**, deliberately. A cached fleet is worse than no
fleet: the whole page is an answer to "what is happening right now". It exists only to receive
notifications, and everything it knows arrives in the message — so changing what a notification says
needs no new worker and no reinstall, which is the part that would otherwise go stale on somebody's
phone for months.

## 4. The rules this page keeps

1. **Derive, never record.** State, interventions and context all come out of events already on
   disk. No status file exists, so nothing can be stale, and a session from last week answers the
   same way.
2. **The console reads; the daemons act.** Its App has **no LLM and no tools** — it cannot run a
   turn even by mistake.
3. **Draw not-knowing as not-knowing.** No bar for an unknown window; `~` and "estimated" for an
   estimate; and when the console itself cannot be reached, the last good table stays on screen
   under `cannot reach magi-web` — **an empty table reads as "no agents".**
4. **Refuse rather than pick.** An address matching several companions is refused with both names.
   Sending work to the wrong workspace runs a turn there, and noticing later does not undo it.
   (The page no longer addresses by role — §2.1 — but `/dispatch` keeps the rule.)
5. **Nothing is written on somebody's behalf.** An agent proposes what it learned and a person can
   forget it; no screen turns a repeated correction into a rule by itself. The pipeline that offered
   to is gone, because deciding a correction is a rule was a judgement nobody had grounds for.
6. **No authentication of its own.** Loopback, behind whatever the organisation already runs.
7. **A demo is not the product.** `-emit-demo` writes a static copy answered by a mock in the
   browser. It hid a dead console for weeks (§5), so a change to this page is confirmed against a
   running `magi-web` before it is believed.

## 5. How it is verified

The page is a Go string, so no Go test can execute it. `cmd/magi-web/testdata/dom.mjs` is a **fake
DOM** — createElement, textContent, className, classList, append, replaceChildren, a listener
registry, about that much — and the tests run the page's real JavaScript against it under node.
Anything the fake cannot express is a sign the page is doing more than it should, which is why it is
not jsdom.

⚠ **The fake has been wrong three times, always in the direction that hides a bug**, and each
correction is worth more than the test it unblocked: `textContent` did not clear children, so a
readout rebuilt from a string plus a button kept the old button; `matchMedia` answered `min-width`
queries with the narrow flag itself, saying yes to exactly the question a narrow screen answers no
to; and `addEventListener` was a no-op, so a page listening for md-tabs' `change` — the only way
that component reports a switch — would have looked correct while doing nothing.

The standing guards, each pinned by removing the thing it guards and watching it fail:

| | |
|---|---|
| self-contained | every `href`, `src`, `url()` **and ES `import`** is a path this binary serves, and the language packs have a route |
| cascade | the layout media queries are **last in the sheet**, checked by position rather than effect |
| themes | the two light palettes say the same thing; the web's colours all come from the TUI's `styles.go` |
| contrast | every `opacity` clears AA against its role in both themes (keyframes excepted — a keyframe is not a resting state) |
| the migration | no rule names `button`, `textarea`, `select` or `input`, which this page has not had since Material Web; the check fails first if the markup grows one back |
| language | every phrase in the pack is asked for, counting keys built from a prefix; no English label sits beside its own translation key |
| the drawer | nothing but the rail's own width reads the live rail width |
| motion | there are keyframes AND a `prefers-reduced-motion` escape that overrides them universally |

**The demo hid a dead console.** `import '/vendor/material.js'` answered 404 on a real magi-web —
`asset` was written and never routed — and a module whose import fails does not execute at all, so a
live console had no components, no script, and no language beyond the seed inlined into the HTML.
Measured on a running binary: `customElements.get('md-primary-tab')` false, the page's own `BASE`
undefined, every button showing the raw English in the markup. It went unnoticed because
`-emit-demo` writes those files to disk beside the page, and weeks of reviewing this console were
reviews of the demo — the one copy where those routes are not needed. The check that should have
caught it read `href`, `src` and `url()`; an ES import is none of those.

---

## 6. The terminal UI

The other surface on the same engine. The console is for **supervising several companions**; the
TUI is for **working with one**. They share a palette: the values live in
`internal/adapter/tui/styles.go` and the web takes them verbatim (§3.1).

### 6.1 The layout

```
┌ header ──────────────── model · permission · scroll chip ⇅42% ─┐
│ transcript (full width)                          ┌ post-it ┐   │
│   ▸ a user's turn = pull quote                   │ todos   │   │
│   ▸ tool calls/results = folded blocks           │ jobs    │   │
│   ▸ edit/write = coloured diffs                  │ context │   │
│                                                  └─────────┘   │
├ background job panes (only when alive, one colour each) ───────┤
├ ▣ turn: 14 steps · 3 file(s) · council r2 · 3m49s ─────────────┤
└ input (alive while a turn is running) ─────────────────────────┘
```

- The **post-it** appears only when there are todos, background jobs or context to report. The
  transcript keeps the full width and the box is drawn over it, bottom-aligned so it usually floats
  above empty space; drag its left edge to resize. Its border is assembled from the terminal's
  **real cell widths**, so a todo line carrying 🚀 keeps its right `│` aligned whether that terminal
  draws the emoji one cell wide or two.
- **A turn receipt** closes every turn (`▣ turn: …`), so its cost is visible without scrolling back.
- **Job panes** tile below while a background command or a plugin's child agent is alive, each in
  its own colour. A child's pane shows **its own transcript**, rendered as the main one is.

### 6.2 Typing and scrolling are separate

Typing keys go only to the input. Scrolling happens only through its own keys (PgUp/PgDn, Ctrl+U/D,
Shift+↑↓) — so writing body text, spaces included, never moves the view. While you are scrolled up
reading, streaming **does not yank the view down**; auto-follow only when at the bottom. There is no
drawn scrollbar: a header **chip** (`⇅ 42% (120/300)`, plus `↓ new` when output arrives below) took
its place, which also removed a whole class of ambiguous-width misalignment on Windows.

### 6.3 A mouse with no modes

Wheel, drag-select and click-to-focus all work **without a mode switch**, because the app does
selection and copying itself. Releasing a drag copies (OS clipboard and OSC52, both attempted).
Selection edges **snap to grapheme boundaries**, so a wide character — Hangul, an emoji — is never
half-selected. Every block carries a **⧉ chip** that copies its **source** text rather than the
styled render.

Ctrl+C is deliberately left alone: it belongs to the terminal's own copy.

### 6.4 You can talk while it works

The input stays alive during a turn, and Enter **injects the message into the running turn** — the
agent sees it at its next step rather than after the turn ends. When a steer lands, a durable
`Steer applied …` line goes into the transcript: it exists so an agent cannot verbally agree and
carry on unchanged.

A question that can only be answered later is queued but **stays where you typed it**; when the
answer is ready the query is pulled down next to it so the two read as a pair.

### 6.5 How things are drawn

- `edit`/`write` render as **coloured diffs** (language by extension, line-number gutter).
- `bash`, `read`, `grep`, `glob`, `list`, `webfetch`, `websearch` results are **folded blocks** with
  a `… +N more` footer; clicking expands.
- The council shows a one-line verdict with its reason wrapped beneath; clicking a member opens the
  full text.
- While a round is open the footer names **what it is waiting on**, so a pause does not read as a
  stall.

### 6.6 The permission modal

`y` once · `a` this session · `p` persist to the project · `n` deny. `p` writes an allow rule into
`.magi/config.toml` **as narrowly as the tool allows** — `bash` persists only the program you
approved (`curl https://x` records `bash(curl:*)`, never `bash(**)`). A command that opens with a
pipe or redirect has no stable program to pin to and stays session-only. The destructive and egress
scanners still re-prompt even for an approved program.

### 6.7 Theme and width

Dark or light is detected from the terminal's background colour at startup and **followed within
seconds if you change the OS theme while it runs**. Force it with `--theme` / `MAGI_THEME`.

Ambiguous-width characters are **measured once** at startup (a Console-API cursor delta on Windows,
a cursor-position query elsewhere). Force it with `MAGI_AMBIGUOUS_WIDTH=wide|narrow`, disable the
probe with `MAGI_WIDTH_PROBE=0`.

### 6.8 How the two relate

| | TUI | Web console |
|---|---|---|
| Subject | **one** companion | **all** of them, on this machine and its peers |
| Relationship | works with it | supervises them |
| Execution | is the engine (`magi`), or attaches to one (`--attach`) | **never runs a turn** — no LLM, no tools |
| Palette | the original (`styles.go`) | takes it verbatim |
| State | its own session | derived from logs, plus a daemon probe |

A TUI and a browser can watch the same daemon at once. Whichever one you steer from, the same event
lands in the same log — **there is no way for the two screens to tell different stories**, which is
the point of the arrangement.

## 7. Not there yet

Listed with what each would actually take, so none of them reads as a wish.

- **The team tier is one machine's.** `<config>/teams/<name>/experience` is a directory on the box
  the console runs on. Two machines in a team keep two stores that never meet. The shape that would
  fix it is the same one [`proposals/distributed-magi-2026-08-06.md`](proposals/distributed-magi-2026-08-06.md)
  describes, and probably an MCP server holding the shared store rather than a sync.
- **An agent cannot ask another for what it knows.** A companion can hand out WORK (`/handoffs`) and
  it can read the shared store, but "tell me what you know about X" is neither: it is a question
  about knowledge, answered from a transcript nobody is reading. What it needs is a tool on the
  asking side and a shape for the answer, and neither exists.
- **Notification is only about blocking.** §2.7 wakes a phone when a companion starts waiting.
  Nothing tells the asker that a handoff came back — the answer is in the receiver's transcript and
  §2.2 collects it, but only when somebody looks.
- **A placement review screen** — [`proposals/companion-performance-2026-08-07.md`](proposals/companion-performance-2026-08-07.md).
- **Provisioning.** The console can only talk to companions that are already running; there is no
  way to start one. Deliberately, for now —
  [`proposals/provisioning-2026-08-08.md`](proposals/provisioning-2026-08-08.md) argues the unit is
  a machine rather than a process, which makes it a different tool's job.
- ⚠ **A child's clone is not drawn, and there is nothing to draw.** `internal/app/workspace.go`
  does give every child its own checkout and merge the work back — but the only path that makes a
  child is a plugin declaring the `spawn` capability, and the one plugin compiled into the binary
  (`engram`) does not declare it. The two that do are examples and are not shipped. So on a default
  install no child clone exists, and a screen for them would be permanently empty — the same reason
  the board has no unassigned column (§2.3).
- **The report's section names are untranslated** (§2.6), and cannot be while a custom skill can
  name its own.
