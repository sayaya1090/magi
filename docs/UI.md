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
2. **What do we share** — what they have learned (project / team / global) and what they can reach
   (which MCP server is attached to which companion)

**Two** destinations. It was four. What a companion has LEARNED and what it can REACH are the two
halves of one thing — knowledge and capability, managed by the same person on the same afternoon —
so two tabs for them made the reader carry the connection. And the board is a question ABOUT the
fleet rather than a place to be, so it is reached from the list it is about (§2.3). Every old
address still lands where it pointed.

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

## 6a. Where this page does not follow the guide

Read out of m3.material.io itself (2026-08-09, two pages: `/components/dialogs/guidelines` and
`/foundations/layout/breakpoints`). ⚠ That site is a SPA — a fetch returns the title and nothing
else, so it has to be read in a browser, and `/guidelines` is the page with the rules while
`/overview` is marketing and `/specs` is dimensions. Not knowing that is how somebody concludes the
guide is silent.

**Three of these are unread rather than chosen**, and all three are open:

| here | the guide | |
|---|---|---|
| the two-column companion page switches at **1100px** | there are five breakpoints — compact <600, medium 600–839, **expanded 840–1199**, large 1200–1599, extra-large 1600+ — and two panes are recommended **from expanded, 840dp** | ⚠ 1100 is a number nobody derived |
| ~~compact navigates with **`md-tabs`**~~ **withdrawn** | the rail page says "compact → navigation bar", but the **nav-bar page says "fewer than three destinations → use tabs"** (a bar assumes 3–5) | ✅ there are **two** destinations here — md-tabs is what the guide asks for. Fixing it would have created the violation |
| ~~the prose measure is **74ch**~~ **withdrawn (with a caveat)** | the lists page separates the ideal from the ceiling: "ideal 40–60, but **large-screen … up to 120**" | ✅ 74 and 108 are both under the ceiling, and "near 120 → raise the line height" is already met at 1.65 |
| the **MCP form** is six fields inline under the list | form fields meet the full-screen dialog criterion, but full-screen is **compact only** — basic above | ⚠ and one action is allowed only when it is an acknowledgement, which a create form is not: it needs add + cancel, trailing-aligned, confirming closest to the edge |
| the fleet **navigates away** to a companion | there are three canonical layouts, and this is **list-detail**: at expanded the list stays and the detail opens beside it | ⚠ **the largest of these.** Both reasons somebody opens a companion — to see its state, to read and steer it — get cheaper when the list survives, and "answering one moves to the next" would happen on one screen instead of needing a trip back. Compact is already right: one pane alternating IS list-detail's compact form |
| the companion's side column is a fixed **22rem** | a supporting pane gives the primary area **about two thirds** | ⚠ 27% at 1300px, 22% at 1600 — a fixed width cannot hold a ratio |
| a disabled control of ours is drawn by **the cursor and a missing hover** | every state carries **two visual indicators**, for accessibility | ⚠ neither of ours survives without a mouse: on touch or a keyboard there are zero. Fading was rejected for a measured reason (3.49:1), so this needs a second indicator that clears AA rather than a revert |

**One is chosen and stays**: the expanded rail is modal with a scrim at every width, where the
guide has it standard from expanded up. The overlap was asked for deliberately (§3.5a) — content
must not move when the drawer opens. Recorded here so the next reader can tell a decision from an
omission.

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

**A8 — Spacing sits off the 8dp scale.** 가이드의 여백 체계는 `space100 = 8dp`의 배수다. 이 페이지가 쓰는
`.35/.7/.9/1.1/1.2/1.4/1.6/2.4rem`은 5.6·11.2·14.4·17.6·19.2·22.4·25.6·38.4dp — 하나도 8의 배수가
아니다. 셰이프·타이포에서 겪은 것과 같은 모양이다: 스케일을 따른다고 적고 값은 손으로 골랐다.
⚠ 하나만 고칠 수 없다 — 페이지 전체의 리듬이므로 한 번에, 그리고 `--measure`(A3)와 함께 재야 한다.

**A9 — Margins live on children.** 가이드는 "Material rarely uses margins in components"라 적고,
**부모 컨테이너의 padding+gap**으로 안의 요소를 정렬하라고 한다. 자식마다 margin을 걸면 균일하지
않고 토큰이 더 든다. 이 페이지에는 자식에 직접 `margin`을 거는 자리가 여럿 있다.

## §6a-2 — The full list of departures from the guide (2026-08-09 기준)

> ⚠ **이 표가 정본이다.** 상세 근거는 `.claude/skills/material-3/deviations.md`에 있으나 그 디렉토리는
> **버전관리 밖**이라, 유실에 대비해 항목 전량을 여기 옮겨 둔다. 세 카테고리(foundations·styles·
> components) 중 **21/약 50 페이지**를 읽은 시점의 목록이다. 더 읽으면 늘거나 **줄 수 있다** —
> 실제로 A2·A3는 더 읽어서 철회됐다.

| # | what this page does | what the guide says | source | size |
|---|---|---|---|---|
| A1 | 컴패니언 2단 전환이 **1100px** | 2판 권장은 **expanded = 840dp**부터. 브레이크포인트는 다섯 | foundations §2 | 한 줄 |
| A4 | **플릿 → 컴패니언이 페이지 이동** | expanded 이상은 **list-detail** — 목록을 남긴 채 옆에 상세. ★ 가이드의 예시가 **정확히 이것**이다: "a pane is a single destination… in a messaging app, **the list of messages is one pane, and a specific conversation thread is another**" | foundations §4.1·§11.6, components/lists | **큼** |
| A5 | 컴패니언 보조 판이 **22rem 고정이면서 접을 수도 없다** | ★ **방향 재구성**: 가이드는 판을 **flexible**(드래그 핸들로 **폭 조절**) 또는 **fixed**(드래그 핸들로 **접고 펴기** — 1판↔2판 전환) 둘 중 하나로 본다. **fixed 자체는 정상**이고, 빠진 것은 **어포던스**다. supporting pane 비율(주 영역 **약 2/3**)은 flexible을 고를 때 적용 | foundations §4.2 | ⚠ **먼저 정할 것**: 이것이 캐노니컬 **supporting pane**(비율 규칙 적용)인가 **side sheet 컴포넌트**(고정 폭이 정상, 16dp 인셋, 닫기 버튼)인가. 시트로 규정하면 고정 폭이 오히려 맞다 — containers/side-sheets. ⚠ 더해서 layout-overview가 **"A pane can be **fixed**, flexible, floating, or semi-permanent"** 라고 한다 — **fixed 판은 가이드가 인정하는 종류**다. `layout-overview`의 **Parts of layout 탭**을 읽고 판정할 것 |
| A6 | MCP 추가 폼 6필드가 목록 아래 **인라인**, 액션은 폼 안의 버튼 하나 | 다이얼로그로. **compact는 full-screen, 그 위는 basic**. 액션은 다이얼로그 슬롯에 `추가`+`취소`, 후행 정렬, 확정이 모서리에 가장 가깝게 | components §1 | 중간 |
| A7 | 비활성 상태를 **커서 + 호버 없음**으로만 표시 | 상태마다 **시각 지표 둘**. 커서와 호버는 마우스가 없으면 **둘 다 관측 불가** — 터치·키보드에서는 지표 0 | foundations §5.1, §11.6 | **작음** — ⚠ "대비 가드와 충돌" 우려는 **해소됨**: `/designing/color-contrast`가 **"Disabled states do not need to meet contrast requirements"** 라고 명시한다 |
| A8 | 여백 값이 **8dp 스케일 밖** — .35/.7/.9/1.1/1.2/1.4/1.6/2.4rem = 5.6/11.2/14.4/17.6/19.2/22.4/25.6/38.4dp | 여백은 **8dp 배수**(space100=8dp) | foundations §6.2 | **큼** — 하나 고치면 리듬 전체가 바뀐다. A10과 함께, 셰이프·타이포처럼 **테스트로 고정** |
| A9 | 자식 요소에 `margin`을 직접 거는 자리가 여럿(`.wlabel`, `.toboard`, `.skwrite` 등) | ❌ 자식에 margin 금지. **부모에 padding+gap** | foundations §6.1 | 중간 |
| A10 | 애니메이션 **210ms·240ms**(`.enter`, `.rise`)가 duration 스케일 밖 | short4=**200**, medium1=**250** | styles §9.2 | **작다** — 두 값 반올림. A8과 같은 커밋에 묶을 것 |
| A11 | `--shape-xl:24px` — **셰이프 스케일에 24가 없다**(20 다음 28) | **28dp** | styles §10.4 | **아주 작다** — 사용처 1곳. 가이드·번들 둘 다 28이라 방어 논거 없음 |
| A12 | 선택된 내비 항목에 **채워진 아이콘이 없고 라벨도 bold가 아니다**(인디케이터+색까지만) | ❌ **"Avoid using the same unfilled icon style for both selected and unselected items"** — **아이콘이 내비 상태의 지배적 단서**다. 채운 버전이 없으면 **semibold**. 라벨은 선택 **bold** / 비선택 **medium** | components/nav-rail(accessibility), styles §11.5 | **중간** — ⚠ 두 번 바뀐 항목: "색만 바뀜"(틀림) → "완결성 부족"(약함) → **명시적 Don't**(accessibility 탭). 접근성 탭을 안 읽어서 과소평가했다 |
| A13 | `font-variation-settings` **0건** — 가변 폰트 축을 아예 안 쓴다 | 다크 배경 아이콘 **grade −25**, 밀집 데스크톱 **opsz 20**, 24dp 최소 **wght 200** | styles §11.1 | 작다 — 한 줄로 A12까지 열린다 |
| A14 | 메뉴 아이콘이 **펼쳐져도 안 바뀐다**(고정 햄버거). `aria-expanded`만 바뀜 | 펼치면 아이콘이 **"접을 수 있다"**를 나타내야 | components/nav-rail | 작다 — 아이콘 하나 토글 |
| A15 | 배지가 접힘/펼침에 상관없이 **한 자리** | 접힘=**아이콘 우상단**, 펼침=**라벨 옆** | components/nav-rail | 작다 |
| A16 | 이미 선택된 목적지를 **다시 눌러도 맨 위로 안 간다**(확인 필요) | 재선택 = 스크롤 top | components/nav-bar | 아주 작다 |
| A17 | `#ptabs`(대화/상태)가 **`md-primary-tab`** | 콘텐츠 영역 **안**의 두 번째 층은 **secondary tab**. secondary는 primary 아래에 | components/tabs | ⚠ **번들에 `md-secondary-tab`이 없다**(실측 0건 — 벤더링이 primary만 포함). 재벤더링은 과하다. 가이드가 "기능은 동일, **인디케이터 스타일만 더 단순**"이라 하므로 **`#ptabs`의 인디케이터 토큰만 secondary 모양으로** 바꾸는 것이 맞는 경로. 눈으로 볼 것 |
| A18 | 배지 값이 **무제한**(`String(n)`) | **"+" 포함 4글자**까지 (99+ 식) | components/tabs | 아주 작다 |
| A19 | 접힌 레일의 아이콘 전용 목적지에 **`.title` 툴팁이 없다**(`aria-label`만) | **"아이콘 전용 버튼에는 plain 툴팁으로 이름을 단다"** | components/tooltips | **작다** — 다른 아이콘 버튼 7개가 이미 `.title`을 쓴다. 레일만 빠졌다. B(라벨 숨김)를 지탱하는 조각이기도 하다 |
| A20 | **헤딩 요소가 하나도 없다** — `<h1>`~`<h6>` 마크업 0건, JS 생성 0건. 시각적 섹션 머리글(`.lanehead`·`.teamhead`·`#state`)은 대문자 스타일일 뿐 | **내용 위계대로 H1~H6**, 레벨 건너뛰기 금지, **페이지 제목 H1 하나** | foundations §8.3.2 | **중간** — 보조기술 사용자는 헤딩으로 페이지를 훑는다. 훑을 것이 없다 |
| A21 | `<nav>` **둘**(`#crumbs`·`#rail`)에 **`aria-label`이 없다** | 같은 랜드마크가 여럿이면 **라벨로 구별**. ⚠ 라벨에 역할 이름을 반복하지 말 것 | foundations §8.3.1 | **아주 작다** — 속성 두 개 |
| A22 | `#rail .lbl { white-space:nowrap }` — **라벨이 줄바꿈되지 못한다** | 텍스트 확대 시 항목이 **세로로 자라고 줄바꿈해도 된다**. **2배까지 라벨 전체가 보여야** | components/nav-rail(accessibility) | **작다** — 한 줄. ⚠ 접힘 상태의 `display:none`과는 별개 문제 |
| A25 | 폼 제출 실패가 **필드가 아니라 전역 `#state` 줄**로 가고(page.go:3586, 80자 절단), 그 줄에 **`role="alert"`도 `aria-live`도 없다** | 오류에는 **`alert` 역할**을 부여하고 메시지를 라벨로. 필드 오류는 **그 필드 자리에** | components/text-fields(accessibility), components/dialogs | **중간** — 두 결함이 겹쳐 **스크린리더는 오류를 전혀 못 듣는다**. ⚠ `post()`는 공유 헬퍼라 **폼 제출에 한정**해 고칠 것 |
| A26 | `<md-dialog id="prefsDialog">`에 **`type` 속성이 없다** → 역할이 `dialog`(기본) | 웹의 **basic 다이얼로그는 `alertdialog` 역할**. 번들이 `type="alert"`일 때만 붙인다 | components/dialogs(accessibility) | **아주 작다** — 속성 하나. ⚠ 단, 이 다이얼로그는 **폼**을 담아 ARIA 관행과 갈릴 수 있다. 고칠 때 확인 |
| A27 | 보드 카드가 **directly actionable인데 그 안에 동작하는 칩**이 있다(`e.stopPropagation()`이 증거) | 카드는 **동작하는 표면**이거나 **동작을 담는 컨테이너**, **둘 중 하나**. "An action shouldn't be placed on an actionable surface" | components/cards(accessibility) | **중간** — ⚠ guidelines 탭만 보고 "허용"이라 적었던 것을 **뒤집음** |
| A28 | 보드 카드의 라벨 칩이 **`<div>` + `onclick`** — 탭 스톱도 역할도 없다 | "**모든 동작 요소는 탭 스톱**이어야", "**스크린리더 포커스와 키보드 포커스를 둘 다**" | components/cards(accessibility) | **중간** — **키보드로 라벨 필터를 쓸 방법이 없다** |

**철회된 항목**: A2(compact `md-tabs`) — 목적지가 셋 미만이면 가이드가 탭을 지시한다.
A3(74ch) — 큰 화면 천장은 120자다. 둘 다 **고쳤다면 위반을 만들었을 것**이다.

**유지하기로 한 것(B)**: 모든 폭에서 modal 레일(가이드가 든 modal 용례 "정보 밀도 높고 공간 제한"에
해당) · 접힌 레일의 라벨 숨김(가이드의 "reduced visual impact" 탈출구, `aria-label`은 유지) ·
108ch 트랜스크립트(천장 안, 줄높이 1.65로 조언 충족) · `--ease-emphasized`가 가이드 대신 번들을
따르는 것(가이드는 CSS에 값이 없다 하고, 번들은 상수를 갖고 있으며 컴포넌트가 그 곡선으로 움직인다).

**아직 판정 못 한 것(7)**: ① MCP 폼의 필수 필드 표시(별표 + 별표의 뜻 설명) ② 폼 오류 표시
방식(오류 텍스트가 보조 텍스트를 **교체**해야 하고 **오류 아이콘**이 강력 권장) ③ 중첩 컨테이너의
optical roundness(바깥반경 − 패딩 = 안쪽반경, 중첩에 같은 값 금지) ④ ~~구분선 용법~~ **종결**(`.sk`·`.srv`는 담기지 않은 리스트라 허용 범주 안. 다만 반복 형식이라 여백만으로 충분할 수 있어 **간소화 후보**) ⑤ 한 화면에 filled 버튼이 둘 이상인
곳(이상은 페이지당 하나) ⑥ 불가능한 동작을 **감추는가 비활성하는가**(가이드는 비활성 — A7과 같은
자리) ⑦ 메뉴 항목 안에 중첩 동작이 있는가(있으면 키보드·스크린리더가 깨진다).
