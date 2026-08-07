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

## 1. The three questions this page answers

A supervisor asks three things a day, several times:

1. **Who is doing what** — and which of them is waiting on *me*
2. **What did I have to say** — anything I said twice is a rule waiting to be written
3. **What have they learned** — and how far does each lesson reach (project / global)
4. **What can they reach** — which MCP server is attached to which companion

The four tabs are exactly those — the fourth being **what can they reach** (MCP servers). A
fifth tab needs a fifth question first.

## 2. The console's screens

### 2.1 The fleet (`/`)

A resource table. As in a Kubernetes console, **state comes first**, then the name, then what it is
doing. The order is the order the eye should travel: **waiting on a person → working → idle → gone.**

- The **summary tiles** are counts and filters at once. A tile at zero is not clickable — a control
  that can be pressed and does nothing is worse than one that cannot.
- A row: state badge · **how far through its own plan** (`3/7`) · name · **role** · team (and
  whether it speaks for it) · workspace path · what it is doing · host and IP · idle time · actions
  (interrupt, and the answer buttons when it asked). Not a progress bar: a todo list is not a
  schedule, and a bar would promise a completion time nobody can honour.
- **A blocked agent shows the question**, not the work — the question is why nothing is happening.
  The buttons sit under the question rather than in the actions column, because they belong to the
  prompt and not to the row.
- The composer at the bottom has an **address field**: a name, words from a role, or a team
  (MANUAL §13.5).

### 2.2 One companion (`/?d=<socket>`)

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

Below that: the live transcript (SSE) and the composer.

### 2.3 What I had to say (`/?v=interventions`)

Every moment a person stepped into a running turn. Derived, not recorded — a user prompt arriving
while a turn is open *is* a steer — so **a screen written today answers for last month's logs**.

- **Grouped by the words.** The same correction to three companions is a rule waiting to be written.
  Grouped on the text itself, normalised only for case and spacing: anything cleverer merges two
  different corrections and puts words in somebody's mouth.
- It says **how far into the turn** the person stepped in (`stepped in 8s–20m into the turn`). Eight
  seconds in corrects the *instruction*, and a rule can prevent the next one; twenty minutes in
  corrects the *work*, and no rule would have helped.
- Each one can be **promoted into the project or the global tier**. Never automatically (§4).

### 2.4 What they have learned (`/?v=skills`)

Both tiers of the store — rules and remembered facts, local and from every federated console.

- Each row leads with **the reach, in words**: `every companion` / `only api on laptop`. Words rather
  than a colour, because the decision made here is exactly which of those two a rule should be.
- A fact is set in italic, quoted. **A stale fact is merely wrong; a stale rule is an instruction
  still being followed.**
- `seen 3× · 2026-07-14 → 2026-08-07` tells a settled lesson from a one-off.
- A wrong one can be forgotten. **Nobody promotes into a store they cannot correct.**

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

## 3. The design language

### 3.1 Material 3, actually — audited

The colours used M3's role NAMES over a palette of our own and nothing else of the system, until
somebody looked at the screen and said so. Counted then: every radius 2px (not on the shape scale),
type at 9.5·10.5·11.5·12.5·13.5·15.5·17px (not on the type scale), no `on-` roles, no surface
containers, no state layers. Counted now, mechanically, by the audit in `.claude/skills/material-3`:

| | |
|---|---|
| shape | 8 radii, **0 off the scale** (4·8·12·16·24·full, plus a stated 0) |
| type | literals are 11·12·14·16px, the rest through tokens; **0 off the scale** |
| colour | 10 `on-` roles, 5 surface-container roles, both themes |
| interaction | 11 component kinds bring their own; one state-layer recipe for the 7 surfaces that are not components |
| motion | `cubic-bezier(0.2, 0, 0, 1)` / 100ms, from material-components-android's Motion.md |
| targets | 48px minimum, Material's, with 4 `:focus-visible` rules of ours and the rest the components' |

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
touch targets are at least 44px.

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

### 3.5a Two widths, one navigation

The breakpoint is **768/769px**, the handbook's, so the two products break in the same place.

| | wide (≥769px) | narrow (≤768px) |
|---|---|---|
| navigating | the rail, beside the page | the tabs, as before |
| the hamburger | widens the rail into a drawer | slides the drawer in over the page |
| scrim | none — the page behind stays usable | yes, and picking a destination closes the drawer |
| tabs | hidden — two navigations for four sections is one too many | the navigation |

It is **one element in two modes**, not two that have to agree. The rail's items are
`md-list-item` with an `href`, which the component renders as a real anchor: a navigation made of
addresses should survive a middle click.

Which section is current is written once, in `render()`, beside the tabs' own index. Two places
saying where you are is how they come to disagree.

### 3.5b The drawer's other half

Preferences and identity, which had nowhere else to live: **theme** (system/light/dark),
**language** (browser/English/한국어), and **which machine this console is** — the host and the
config directory, from `/console`.

That last one is not an account. magi has no users to log in; the console is reachable by whoever
can reach the port. What a supervisor with three of these open actually asks is which machine they
are looking at, and before this the answer was to recognise the companions in the list — which
fails exactly when two machines are running the same work.

### 3.6 On a phone

- Under 640px the composer wraps so the text box keeps a full row. Measured: without it the box was
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
icon is served by the binary too, for the reason in §3.2.

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
5. **No automatic promotion.** A person decides that a correction is a rule, and which tier it goes
   in.
6. **No authentication of its own.** Loopback, behind whatever the organisation already runs.

## 5. How it is verified

The page is a Go string, so no Go test can execute it. `cmd/magi-web/testdata/dom.mjs` is a **fake
DOM** — createElement, textContent, className, append, replaceChildren, about that much — and the
tests run the page's real JavaScript against it under node. Anything the fake cannot express is a
sign the page is doing more than it should, which is why it is not jsdom.

A test also checks that every path the page references is a path this binary serves. That is the
real meaning of "self-contained", and it cannot be checked against a list that exists only as
statements.

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

- **A placement review screen** — [`proposals/companion-performance-2026-08-07.md`](proposals/companion-performance-2026-08-07.md).
- **A reply channel.** Answers are read from the receiver's transcript — §2.2 collects them, but
  nothing notifies the asker.
