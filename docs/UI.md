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

| | ≥840px | below |
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

Header fields, in the order the questions come — the grid packs in DOM order, so the list of them
IS the layout:

1. **what it is doing now**: status · steps · last activity
2. **who and where it is**: role · team · host, address and pid · workspace
3. **how to move around it**: session
4. **how it runs**: approvals · model · cache · context · what was summarised away

The long strings — the role, the workspace path, the session — span **two** columns rather than a
whole row. A full-row span breaks the packing on both sides of it, and the card grew three
near-empty rows, one of them holding a five-letter state.

Three of these are controls rather than readings:

- **approvals** and **model** are menus. The model list is asked of that companion's own daemon,
  which asks its backend: a console listing from its own config would offer models that companion
  cannot reach. What is drawn is what the daemon SAYS it is on, so a refused change reverts
  visibly, and the model it is on is always in the menu even if the backend stopped listing it.
- **session** is the way to the others. ⚠ Choosing one OPENS it; it does not move the companion
  into it — that would mean addressing a different session on every submit. The menu is shut while
  the companion is working, which is the rule that will guard the move when it lands.

And two buttons — **tools** and **loop** — lead one level in. The tool roster comes over the socket
because the registry is assembled at startup from the config, the plugins that loaded and the MCP
servers that answered; a console listing the built-ins would describe a companion that does not
exist, and one that cannot answer is drawn as "did not say" rather than as an empty list. The loop
map and the comparison against the session a fork came from are readings of the log, which is why a
console arriving later can show the comparison at all — the terminal only knows what it forked this
run.

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

**A lane is a team, not a companion.** Six companions made six columns whose heads carried a name,
a role sentence and a team word, so every lane began at a different height and the cards under them
never lined up — which is the one thing a board is for. Work belongs to a team; which companion did
it is a fact about the card, beside the time. A companion with no team keeps a lane of its own,
because nothing declares a team on a single-workspace machine.

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
- **The form asks in two sets, because a server is one kind or the other.** Reached over HTTP it is
  a url and that is the whole of it — what it can do is the server's own business, advertised over
  the protocol. Started by this machine it has no url and needs the command. Asking for all six
  every time made somebody filling in a url read four boxes that could not apply to them. The name
  is the one both need, and it is filled in from the url or the command.
- **Editing opens the same dialog, filled in, with the name locked.** The write is by name, so
  retyping it into the add form is how a typo makes a second server instead of changing the first.
- The button to add one is at the **head** of the section. Under the list, a console with a dozen
  servers put all twelve between somebody and the way to add another.
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

### 2.5a Who may use this console (`/?v=access`)

Who exists, what each may do, and which companions they are narrowed to. Behind `admin` and only
that — the server refuses regardless; hiding the screen is for the person who would otherwise be
offered a control that answers 403.

- **The rail's foot carries the way in**, away from the two destinations somebody lives on: this is
  opened when a person joins or leaves. Below 37.4375em the rail is not drawn at all, so there the
  preferences dialog carries it — one door per width, never two on screen at once.
- **It says whose list it is**: `user@host`, with the config directory it was read from. A fleet
  spans machines and accounts and this governs exactly one of them. Not an address — an IP says how
  to reach a machine, a host has several, and a tunnel makes every peer look like `127.0.0.1`.
- **Groups above, people below.** On a console wired to a directory the groups ARE the roster —
  membership is maintained where somebody is hired and let go — and the people under it are the
  exceptions. Groups are read-only here: a console that offered to edit them would be offering to
  disagree with the directory.
- **A row is list anatomy**: the name on the headline, what the role buys under it, the scope under
  that. Three type roles, because every line was the same size before and nothing said which was
  which. Capability words are **not translated** — they are what goes into `auth.toml`.
- Nothing can leave the file with no admin: that console would refuse to start, with the fix behind
  the door. `magi --access`, `--grant`, `--revoke` are the way in when a policy has locked its
  author out.

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
| type | **no px literal at all** — every size comes from --md-sys-typescale-*, so a reader who raised their default font gets it |
| colour | 10 `on-` roles, 5 surface-container roles, both themes |
| interaction | 11 component kinds bring their own; one state-layer recipe for the 7 surfaces that are not components |
| motion | `cubic-bezier(0.2, 0, 0, 1)` / 100ms, from material-components-android's Motion.md |
| targets | 48px, measured on the components' own touch target rather than on their visual box — every control on the page clears it |

### 3.1b The icons, and the build that is not in this repository

Every mark on the page — the rail's destinations, a tick beside a finished step, the cross on a
failed call, the paper plane on the send button — is a `<use>` into a sprite of `<symbol>`s
inlined at the top of the document. The art is **Font Awesome Pro (Sharp Light, with Sharp Solid
for the filled and toggled states, and two brands)**, and it is **not in this repository**: the
licence permits using it in something you deploy and not republishing it as files.

So the repository holds the NAMES and the art arrives at build time:

```sh
MAGI_FA_DIR=~/Downloads/kit-…-web go generate ./cmd/magi-web    # from a kit download
MAGI_FA_DIR=node_modules/@fortawesome/fontawesome-pro go generate ./cmd/magi-web   # in CI
```

Both layouts are `svgs/<style>/<name>.svg`, which is why there is one reader. `gen_icons.go` greps
`page.html` and `page.js` for `#i-<style>-<icon>` — **the page is the manifest**, because a list
kept beside it is the second place to edit and the place an icon goes missing — and writes
`icons_gen.go`, which is git-ignored and sets the sprite in an init.

Three things follow, and each is load-bearing:

- **A build with no licence is a working build.** The sprite is empty, and every use site falls
  back to the character or the hand-drawn path it had before (`icon()`, `iconOr()`, `dressIcons()`).
  The gate is run both ways. A contributor without Pro gets a plainer console, not a broken one.
- **A named icon with no file stops the build.** The alternative is a screen with a gap where a
  control was.
- **The injection is an init, not part of `assemblePage()`.** Every package-level variable is
  initialised before any init runs, so a sprite injected during assembly was reliably the empty
  string — six symbols baked, zero in the page, indistinguishable on screen from having no licence.
  `TestTheSpriteReachesThePage` holds the ordering.

Two icons are deliberately NOT from the sprite. The rail's menu button is five line elements that
travel — each half of a bar keeps the end it shares with its twin, walks it to the centre and turns
45° — and the theme toggle is a sun and a moon that rise and set past the icon's own edge. A symbol
can be exchanged for another symbol; what those two want is the same strokes MOVING, which no
single glyph can be.

Pages bakes them too, from `@fortawesome/fontawesome-pro` restored with `FONTAWESOME_NPM_AUTH_TOKEN`
from the repository's secrets, and skips it without complaint when there is no token — a fork's run
then publishes the fallback, which is what a contributor's build looks like and is worth seeing.

### 3.1c The components, and what is left to us

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

| | ≥840px | 600–839px | <600px |
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
6. **No authentication of its own.** Loopback, behind whatever the organisation already runs. The
   bind is a refusal, not a warning, and a test holds it there. When something in front does
   authenticate, `-exposed` says so and the console drops the two routes that make the MACHINE run
   what the caller chose — `/shell`, and writing an MCP server's command line — because those go
   round the permission policy every tool call goes through. `-peer` is refused with it: a peer is
   reached on the operator's own tunnel, so a shared console would let whoever the gateway admits
   act as the operator on another machine.
7. **What is on screen is what a control reaches.** A companion can be pointed at another of its
   own conversations — from the session dropdown, or by pressing a card on the board, since a card
   IS a conversation. The composer is live only when the session on screen is the one the
   companion's record names; on any other it offers to move first, asks before it does, and is
   disabled outright while a turn is running, because a companion cannot leave a conversation it
   is still speaking in. A page addressed by COMPANION follows the move; one addressed by SESSION
   does not — somebody reading last Tuesday's work keeps reading it. The conversation being left
   gets a line saying where the companion went, so a transcript that stops does not read as a
   daemon that died.
8. **Changes are written down.** Every request that changes something appends a line to
   `console-audit.jsonl` beside the session store: method, path, the companion it named, where it
   came from, and what was answered — refusals included, since the cross-site guard turns those
   away before a handler sees them. Never the body: the session log already holds every word. Who
   comes from the gateway, through the header `-user-header` names; unset, the record says where
   and not who rather than leaving a blank to be misread as a name.
9. **A demo is not the product.** `-emit-demo` writes a static copy answered by a mock in the
   browser. It hid a dead console for weeks (§5), so a change to this page is confirmed against a
   running `magi-web` before it is believed.
10. **One stream per window, and none from a window nobody is looking at.** A browser allows six
   connections to one host and a stream never ends, so streams are a budget the page is spending.
   The roster arrives as a named frame on the transcript's connection rather than on one of its
   own; a meeting screen opens its own, because it watches a room rather than a companion; and a
   hidden tab closes its stream, re-reading the screen when it comes back. Measured before this:
   with three windows open the third one's first fetch never returned, and at six the sixth window
   could not load the document at all.
11. **Coming back to a screen reads it once, and the subscription is left alone.** Frames arrive
   when something changes, so a panel nobody was looking at was never redrawn and a tab left in the
   background was as old as the moment it was left. Arriving at a panel and returning to the tab
   both go through the same loaders `render()` uses — a second path to the same fact is how two
   answers about one companion start to differ.
12. **What is kept is said, and there is a way to overrule it.** The workspace tree is one
   directory per request and only the folders somebody opened; a walk that follows a CHANGE reads,
   a walk that is only a redraw may use what was read in the last ten seconds. A mutation this
   console made throws the kept listings away — that is not old, it is wrong — and the ⟳ control in
   the card's head is for the file that appeared because of something the console cannot see.
13. **The palette is a way IN, never a second way to do something.** Ctrl/⌘+K lists what this
   console can name and every entry ends in the call the on-screen control makes. It offers a verb
   only where that verb means something — a list that includes what does nothing teaches people to
   press things that do nothing — and it asks the server for nothing until there is a query.
14. **A control that only a pointer can reach is a control a phone does not have.** Row menus and
   per-file git actions appear on hover, with focus-within for a keyboard; on touch there is
   neither, so where there is no hover they are simply shown. Measured on an iPhone 13: twelve row
   menus and twenty-nine git actions, none of them reachable.

## 5. How it is verified

The page is a Go string, so no Go test can execute it. `cmd/magi-web/testdata/dom.mjs` is a **fake
DOM** — createElement, textContent, className, classList, append, replaceChildren, a listener
registry, about that much — and the tests run the page's real JavaScript against it under node.
Anything the fake cannot express is a sign the page is doing more than it should, which is why it is
not jsdom.

⚠ **The fake has been wrong seven times, always in the direction that hides a bug**, and each
correction is worth more than the test it unblocked: `textContent` did not clear children, so a
readout rebuilt from a string plus a button kept the old button; `matchMedia` answered `min-width`
queries with the narrow flag itself, saying yes to exactly the question a narrow screen answers no
to; `addEventListener` was a no-op, so a page listening for md-tabs' `change` — the only way that
component reports a switch — would have looked correct while doing nothing; `children` was an array,
so `indexOf` on it passed here and threw in a browser; the id list was hand-kept; there was no
`dataset`; and `classList` had add, remove and contains but no `toggle`.

**And it has no layout at all**, which is a whole class it cannot see: overflow, geometry, what a
finger reaches, what a colour computes to. Those are measured in a real browser instead — see below.

### 5.1 Measured in a browser

`scratchpad/` carries seven probes that run the emitted demo in headless Chromium. They exist because
the fake DOM cannot answer any question about layout, and because the mock now MOVES: the fleet's
states cycle, plans change, the context gauge fills and folds, the connection drops and recovers.

| | |
|---|---|
| `measure.mjs` | headings, live regions, focusables, `div`+onclick, children escaping their parent |
| `contrast.mjs` | WCAG 1.4.3 in both themes, **piercing shadow roots** |
| `geom.mjs` | every box on four screens, for diffing one build against another |
| `cssvalid.mjs` | unknown property names, values the browser drops, `var()` that resolves to nothing |
| `spec.mjs` | the fourteen dimensions the guide states outright, on the elements as drawn |
| `verify.mjs` | touch targets, keyboard focus, truncation, reflow, the **accessibility tree**, and that every `md-*` is a component the bundle defines |
| `sweep.mjs` | presses **every control on every screen at two widths** and asks whether anything happened |

⚠ **A probe is not trusted until breaking something on purpose makes it speak.** Six findings in one
session were the probe's own, not the page's:

- a contrast probe built on `document.querySelectorAll` reads none of the `md-*` components' text,
  and reported a clean sheet for a page it never read;
- its background walk has to cross the shadow boundary too — stopping at it finds no background,
  defaults to black, and turns ten light-theme labels into violations;
- comparing CSS source against the CSSOM **repeats the parser's own mistake**, so the two agree
  about a rule neither read; what survives is the NAME, because prose does not name a property;
- a name can be real and the value dead (`-var(--x)`), so values are asked about separately —
  with the `!important` taken off, which `CSS.supports` rejects;
- a box-measuring probe cannot see a hit area: neither the shadow `.touch` span nor an `::after` is
  a box in the tree, so it has to ask the document what owns the point;
- a zero-size control is not on screen, and a disabled one is not a target — Material marks disabled
  by turning its touch span off, so not skipping it reports the library defending itself.

⚠ **A press that throws is the easy case.** Cancel on the MCP dialog did not throw — it did
NOTHING, quietly, and a sweep listening only for errors called that a pass. So each press has to
MOVE something: the url, the DOM, a dialog's open state, the theme attribute, or what is focused.
That found three more controls drawn as pressable and inert — answer and write-it-down with an
empty field, and the board's Today while already on today. The guide's answer is the one they
were not doing: an action that cannot happen is **disabled**, not hidden and not silently inert.

⚠ **And the gate itself lied twice in one session.** A wrapper that grepped its output for
`--- FAIL` called a Go syntax error green: a build failure prints neither that string nor anything
else the filter kept. Judge by the exit code. Both errors were the same mistake — a backtick in a
comment, which ended the Go raw string the whole page used to live in. The page is three embedded
files now (`page.html`, `page.css`, `page.js`), so that particular trap is gone; the lesson about
the gate is not.

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
| motion | there are keyframes AND a `prefers-reduced-motion` escape; the transitions it leaves alive **must not include `transform`**, or the page still moves |
| type | no font size in px anywhere — sizes come from `--md-sys-typescale-*`, so a reader's larger default reaches them |
| spacing | no rem literal in a `padding`, `gap` or `margin`; the scale is eight `--magi-sys-space-*` tokens |
| fields | nothing a person types into is under 16px, which is where iOS Safari zooms and does not zoom back |

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
- The post-it also carries **what is waiting** and **what is scheduled**. Waiting work is what a
  person typed into a turn already running, plus what another companion handed over — the queue
  lives in the memory of the process running the turn, so an attached window asks the daemon for it
  rather than reporting its own empty one. Waiting work opens the panel by itself; scheduled work
  does not, because it is true all day and a panel that opens for a nightly job is always open. A
  job whose schedule can never match is listed whatever its state — nothing else will mention it
  again — and one somebody switched off is not.

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
| ~~the two-column companion page switches at **1100px**~~ **고침 → 840px** | there are five breakpoints — compact <600, medium 600–839, **expanded 840–1199**, large 1200–1599, extra-large 1600+ — and two panes are recommended **from expanded, 840dp** | ⚠ 1100 is a number nobody derived |
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

## §6a-2 — Departures from the guide — where each stands (2026-08-09)

> ⚠ **이 표가 정본이다.** 상세 근거는 `.claude/skills/material-3/`(16파일)에 있으나 그 디렉토리는
> **버전관리 밖**이라 항목 전량을 여기 옮겨 둔다.
>
> **52건 고침 · 1건 남음 · 5건 철회.** **A 목록은 비었다.** 접힌 레일 라벨까지 닫혔다 — 레일을 96dp 표준 폭으로 올리고 항목을 앵커로 직접 만들어, 자르지도 줄이지도 않고 라벨이 들어간다. 읽기는 끝났다 — Styles 21경로 · Foundations 18경로
> · 컴포넌트 **specs 36/36 · `/overview` 36/36 · accessibility 30/36 · guidelines 36/36**.
> ⚠ 더 읽으면 항목이 **줄 수도** 있다: **A2·A3·A7이 철회**됐고 A12는 심각도가 두 번 바뀌었다.

### Still open (21)

| # | 지금 이 페이지 | 가이드 | 출처 | 크기 |
|---|---|---|---|---|
| ~~A4~~ ✅ | **플릿 → 컴패니언이 페이지 이동** | expanded 이상은 **list-detail** — 목록을 남긴 채 옆에 상세. ★ 가이드의 예시가 **정확히 이것**이다: "a pane is a single destination… in a messaging app, **the list of messages is one pane, and a specific conversation thread is another**" | foundations §4.1·§11.6, components/lists | **고침** — 1000px부터 컴패니언이 목록 **옆에** 열린다(list-detail). 840이 아니라 1000인 이유: 840에선 상세 자체가 이미 2판(대화+사실)이라 셋째 판이 대화를 350px로 만든다. 사실 판은 손으로 접히고 기억되므로 840에서 목록을 원하면 그쪽을 접으면 된다. 목록은 그 폭에서 컬럼을 포기하고 **상태+이름**만 남긴다(나머지는 DOM에 남아 스크린리더가 읽는다). ⚠ `loadFleet`이 컴패니언 페이지에서 일찍 반환해 행을 만들 기회가 없었다 — 갈라냈고, 마스트헤드 개수는 목록 화면에만 남는다. 열린 행 표시를 secondary-container로 했다가 상태 단어가 4.01:1로 떨어져 primary 8% 워시로 (9b9d3f04) |
| ~~A5~~ ✅ | 컴패니언 보조 판을 **닫을 수도 접을 수도 없다**(⚠ **폭은 결함이 아니다** — side sheet 범위가 **256~400dp**이고 22rem=**352px**는 그 안이다. 남은 것은 어포던스뿐) | ★★ **분류가 갈려도 결함 확정**: **side sheet면 닫기 아이콘 버튼이 필수**("Material requires… always present"), **fixed pane이면 드래그 핸들로 접고 펴기**. 선결 질문은 **고치는 모양**만 정한다. 원래 정리: 가이드는 판을 **flexible**(드래그 핸들로 **폭 조절**) 또는 **fixed**(드래그 핸들로 **접고 펴기** — 1판↔2판 전환) 둘 중 하나로 본다. **fixed 자체는 정상**이고, 빠진 것은 **어포던스**다. supporting pane 비율(주 영역 **약 2/3**)은 flexible을 고를 때 적용 | foundations §4.2 | **고침** — `#sideToggle`로 접기/펼치기, 선택은 컴패니언 사이를 넘어 기억된다 (4ae642f6) |
| A6 | MCP 추가 폼 6필드가 목록 아래 **인라인**, 액션은 폼 안의 버튼 하나 | 다이얼로그로. **compact는 full-screen, 그 위는 basic**. 액션은 다이얼로그 슬롯에 `추가`+`취소`, 후행 정렬, 확정이 모서리에 가장 가깝게 | components §1 | 중간 |
| ~~A8~~ ✅ | 여백 값이 **8dp 스케일 밖** — .35/.7/.9/1.1/1.2/1.4/1.6/2.4rem = 5.6/11.2/14.4/17.6/19.2/22.4/25.6/38.4dp | 여백은 **8dp 배수**(space100=8dp) | foundations §6.2 | **고침** — 26개 값 → 8개 `--space-*` 토큰(4dp 격자), 최대 이동 3.2dp. 테스트로 고정 (d53bf827) |
| ~~A9~~ ✅ | 자식 요소에 `margin`을 직접 거는 자리가 여럿(`.wlabel`, `.toboard`, `.skwrite` 등) | ❌ 자식에 margin 금지. **부모에 padding+gap** | foundations §6.1 | **고침** — 부모가 이미 `gap`을 주는데 자식이 `margin`을 덧대던 6곳 제거(상세 카드 아래 48px→24px 등). `#side md-outlined-card{margin-bottom:0}`이라는 **없는 마진을 되돌리던 죽은 규칙**도 함께. 나머지 19곳은 `margin-left:auto`(정렬)와 음수 블리드라 이 규칙의 대상이 아니다 (67e67e56) |
| ~~A13~~ ⊘ | `font-variation-settings` **0건** — 가변 폰트 축을 아예 안 쓴다 | 다크 배경 아이콘 **grade −25**, 밀집 데스크톱 **opsz 20**, 24dp 최소 **wght 200** | styles §11.1 | ⚠ **철회** — magi는 가변 폰트 축을 쓰는 Material Symbols를 아예 안 쓴다 |
| ~~A15~~ ✅ | 배지가 접힘/펼침에 상관없이 **한 자리** | 접힘=**아이콘 우상단**, 펼침=**라벨 옆** | components/nav-rail | **고침** — 접힘=아이콘 우상단 / 펼침=라벨 뒤 8px. ⚠ 기전이 **둘이었고 하나는 죽어 있었다**: CSS가 `calc(100% + 9.2rem)`으로 밀고 `paint()`가 재부모화를 했는데 `paint()`는 내비 토글에 안 걸려 한 번도 안 돌았다. 고정 오프셋이라 **영어 라벨 뒤 52px · 한국어 뒤 87px** — 언어 의존의 정체. 이제 흐름이 자리를 정하므로 계산이 없다 (a748aa5c, 621cbcaa) |
| ~~A16~~ ✅ | 이미 선택된 목적지를 **다시 눌러도 맨 위로 안 간다**(확인 필요) | 재선택 = 스크롤 top | components/nav-bar | **고침** — 같은 목적지를 다시 누르면 맨 위로 (3b40835e) |
| ~~A17~~ ✅ | `#ptabs`(대화/상태)가 **`md-primary-tab`** | 콘텐츠 영역 **안**의 두 번째 층은 **secondary tab**. secondary는 primary 아래에 | components/tabs | **고침** — 2단 탭은 `md-secondary-tab` (d830126b) |
| ~~A32~~ ✅ | 탭 패널 전환(`#ptabs`)이 **fadeThrough + scale(.96)** | **Lateral**은 ⚠ **"does not use a fade or parallax effect… slide in unison"**. 이유까지 있다: ⚠ **"Fading content as it slides makes the peer relationship and swipe gesture less obvious"** + forward/backward로 **오해된다** | styles/motion/transitions(패턴·적용 **두 장**) | **고침** — 동급 전환은 옆으로 미끄러진다 (917c14cb) |
| ~~A33~~ ⊘ | Top level 전환에서 나가는 화면이 **즉시 `hidden`** — 페이드 아웃이 아니라 **점프 컷** | "**Fully fade out content before fading new content in**" + "Jump cuts should generally be **avoided as a default**… **offers no clues** to help a user orient themselves" | styles/motion/transitions | ⊘ **유지 결정** — 가이드가 예외를 준다: "If pure efficiency is a top priority, **like opening a menu in a productivity app**, a jump cut may be preferred." 목적지 전환은 하루에 수십 번이라 나가는 화면을 페이드 아웃시키면 매번 그 시간을 낸다. 들어오는 쪽 페이드만 남긴다. ⚠ A32는 다르다 — 거기엔 예외가 없다 |
| ~~A36~~ ✅ | `text-transform:uppercase` **21곳** — ⚠ 머리글만이 아니다: **버튼 라벨 2곳**(전역 `md-text-button`, `.answer md-filled-tonal-button`), **내비 라벨**(`#rail md-list`), **링크**(`#back`), **상태 값** 포함 | ❌ "**Avoid using caps blocks altogether; they're not accessible.**" / "**Use sentence case for all product text.**" ★ **예외 조항이 존재하지 않는다**(h3 13개 전수 확인). 대체 수단도 지정한다: "**use bold weight instead**" | style-guide/grammar-and-punctuation | **고침** — `text-transform:uppercase` 0곳 (7208bac7) |
| ~~A37~~ ✅ | 영어 라벨 **143개 중 102개가 소문자 시작**(`nav.board:"board"`) | sentence case = "only the first letter of the first word **is capitalized**" | content-design/style-guide | **고침** — 문장 대소문자 (69e220e8) |
| ~~A40~~ ✅ | `#state`(page.go:1329-1333)와 `.foldbar .sum`(935)이 **말줄임으로 자르는데 툴팁·링크가 없다** | ❌ "**Don't cut off text without providing a way for users to view it**" / "Truncated text can be replaced with an ellipsis **if the text is available through a tooltip or link**" | writing/text-truncation | **고침** — 잘린 것은 툴팁으로 읽히고, 포커스는 링을 그린다 (bac3f4cd) |
| ~~A41~~ ✅ | **sys 토큰 ~22개가 정적 값을 직접 보유** — 타입스케일 px 20개(148-176) + `shadow`·`scrim` hex(135-136) | "Whenever possible, **system tokens should point to reference tokens rather than static values**" | design-tokens | **고침(색)** — `--md-sys-color-shadow`·`scrim`이 유일하게 팔레트 층을 안 거치고 hex를 직접 들고 있었다. ⚠ 타입스케일은 M3 자신도 sys에 값을 두므로(ref 층 없음) 대상이 아니다 (0d83c3ef) |
| ~~A42~~ ✅ | 자체 토큰에 **시스템명·클래스 접두사 없음**(`--shape-*`·`--ease-*`·`--measure`·`--melchior` 등) | "All token names **start with the system name**" + "an abbreviation for the token class: **ref / sys / comp**" | design-tokens | **고침** — 3층으로: `magi-ref-`(값) · `magi-sys-`(결정) · `magi-comp-`(레일). ⚠ **11개가 `md-` 접두사를 달았지만 Material 토큰이 아니었다**(번들이 선언도 참조도 안 함) — 규칙이 막으려는 최악의 경우. 646곳 치환, 4화면 기하 **0px** (eba653e6) |
| ~~A46~~ ✅ | `--md-sys-color-secondary`가 **시안**(#5CD8E6, hue≈187°)인데 primary는 **주황**(#FF7A1A, hue≈27°) | "**Secondary, neutral variant, and neutral colors match primary in hue but are progressively less chromatic**" | styles/color/advanced/adjust-existing-colors | **고침** — `--secondary` 신설(primary 색조 49°, 채도 1/3): 다크 `#E8B89F` · 라이트 `#82604F`. tertiary는 시안 그대로. 대비 10.57:1 다크 / 5.31:1 라이트 |
| ~~A47~~ ⊘ | `--success`·`--warn`에 **4-롤 조가 없다**(단일 값) | 정적 색을 정의하면 "**the main color, on-main color, container color, and on-container color**" 넷이 나와야 | styles/color/advanced/define-new-colors | ⚠ **철회(실측)** — `--bg` on `--warn` = **13.40:1 다크 / 5.08:1 라이트**로 통과. 소비자 없는 토큰 3개를 만드는 것은 이 저장소가 반복해 고쳐온 결함(생산자만 있고 소비자 없음) |
| ~~A48~~ ⊘ | 다크 `--casper:#FF8A8A`가 `--error:#F2B8B5`와 **별개의 붉은색** | "Material provides the red Error color out of the box… **you do not need to define your own static color for a semantic red**" | styles/color/advanced/define-new-colors | ⚠ **철회(실측)** — 라이트 배경에서 **4.5:1을 지키면서 `--error`와 구별되는 두 번째 빨강이 없다**: coral 색조(OKLCh 21°)를 라이트 밝기로 옮기면 error와의 대비가 최선 **1.46**이고, 구별될 만큼 밝히면 배경 대비가 **4.24**로 무너진다. 다크는 헤드룸이 있어 `#FF8A8A`와 `#F2B8B5`가 갈라지고 라이트는 못 갈라지는 것 — 부주의가 아니라 매체가 강제한 것이다. 가이드도 같은 쪽을 가리킨다("you do not need to define your own static color for a semantic red") |
| ~~A55~~ ⊘ | `nav.connections`이 화면에 **`MCP`** 로 뜬다(정의 없는 맨 약어) | "**Spell things out whenever possible**" + 약어는 괄호로 정의. 시간 약어 예외 해당 없음 | global-writing/word-choice | ⚠ **유지 결정** — `MCP`는 이 화면의 독자에게 통용되는 이름 |
| ~~A56~~ ✅ | `.state`·`.chip` 등 **직접 만든 인터랙티브 요소에 링 모양 포커스 지시자가 없다**(`.state`는 `:focus-visible::after`로 **오버레이만**) | "it appears in its focused state with a **ring-like keyboard focus indicator**" + "Focus states apply to **all** interactive components". ⚠ 오버레이는 State layers의 focus state이고 **링은 별도 요구** | foundations/interaction/states/applying-states | **고침** — `focus-visible` 링 (bac3f4cd) |

### Fixed (37)

| # | 무엇이었나 | 가이드 | 출처 | 결과 |
|---|---|---|---|---|
| ~~A1~~ ✅ | 컴패니언 2단 전환이 **1100px** | 2판 권장은 **expanded = 840dp**부터. 브레이크포인트는 다섯 | foundations §2 | **고침** — `min-width:840px` 3곳 + `matchMedia`. 문서의 브레이크포인트 표도 함께(59189ab2) |
| ~~A10~~ ✅ | 애니메이션 **210ms·240ms**(`.enter`, `.rise`)가 duration 스케일 밖 | short4=**200**, medium1=**250** | styles §9.2 | **고침** — `.enter` 200ms · `.rise` 250ms (62685ee7) |
| ~~A11~~ ✅ | `--shape-xl:24px` — **셰이프 스케일에 24가 없다**(20 다음 28) | **28dp** | styles §10.4 | **고침** — `--shape-xl:28px` + 주석 정정 (62685ee7, 752519c5) |
| ~~A12~~ ✅ | 선택된 내비 항목에 **채워진 아이콘이 없고 라벨도 bold가 아니다**(인디케이터+색까지만) | ❌ **"Avoid using the same unfilled icon style for both selected and unselected items"** — **아이콘이 내비 상태의 지배적 단서**다. 채운 버전이 없으면 **semibold**. 라벨은 선택 **bold** / 비선택 **medium** | components/nav-rail(accessibility), styles §11.5 | **고침** — 선택 = 인디케이터 + 색 + **bold 라벨(700)** + **굵은 아이콘(stroke 2.4)**. 선 아이콘이라 채운 판이 없어 가이드의 대체("thicker or heavier")를 씀 (cbf90ecb) |
| ~~A14~~ ✅ | 메뉴 아이콘이 **펼쳐져도 안 바뀐다**(고정 햄버거). `aria-expanded`만 바뀜 | 펼치면 아이콘이 **"접을 수 있다"**를 나타내야 | components/nav-rail | **고침** — 펼치면 X 아이콘으로 전환 (9abc1202) |
| ~~A18~~ ✅ | 배지 값이 **무제한**(`String(n)`) | **"+" 포함 4글자**까지 (99+ 식) | components/tabs | **고침** — `999+` 상한 (9abc1202) |
| ~~A19~~ ✅ | 접힌 레일의 아이콘 전용 목적지에 **`.title` 툴팁이 없다**(`aria-label`만) | **"아이콘 전용 버튼에는 plain 툴팁으로 이름을 단다"** | components/tooltips | **고침** — 접힌 레일 포함 전 아이콘 컨트롤에 툴팁 (b7ac4a6f) |
| ~~A20~~ ✅ | ~~**헤딩 요소가 하나도 없다**~~ **고침** — `<h1>`~`<h6>` 마크업 0건, JS 생성 0건. 시각적 섹션 머리글(`.lanehead`·`.teamhead`·`#state`)은 대문자 스타일일 뿐 | **내용 위계대로 H1~H6**, 레벨 건너뛰기 금지, **페이지 제목 H1 하나** | foundations §8.3.2 | **고침**(9ed…) — `h1`(제품) → `h2`(구획: 컴패니언·배운 것·연결) → `h3`(레인·팀 머리). ⚠ `h1,h2,h3{font:inherit;margin:0}`으로 **보이는 건 그대로** |
| ~~A21~~ ✅ | `<nav>` **둘**(`#crumbs`·`#rail`)에 **`aria-label`이 없다** | 같은 랜드마크가 여럿이면 **라벨로 구별**. ⚠ 라벨에 역할 이름을 반복하지 말 것 | foundations §8.3.1 | **고침** — `destinations` / `where you are` — 역할명 미포함 (9abc1202) |
| ~~A22~~ ✅ | `#rail .lbl { white-space:nowrap }` — **라벨이 줄바꿈되지 못한다** | 텍스트 확대 시 항목이 **세로로 자라고 줄바꿈해도 된다**. **2배까지 라벨 전체가 보여야** | components/nav-rail(accessibility) | **고침** — `overflow-wrap:anywhere` (62685ee7) |
| ~~A25~~ ✅ | ~~폼 제출 실패가 **필드가 아니라 전역 `#state` 줄**로 가고(page.go:3586, 80자 절단), 그 줄에 **`role="alert"`도 `aria-live`도 없다** | 오류에는 **`alert` 역할**을 부여하고 메시지를 라벨로. 필드 오류는 **그 필드 자리에** | components/text-fields(accessibility), components/dialogs | **고침** — `#state`에 `role="status" aria-live="polite"` |
| ~~A26~~ ✅ | `<md-dialog id="prefsDialog">`에 **`type` 속성이 없다** → 역할이 `dialog`(기본) | 웹의 **basic 다이얼로그는 `alertdialog` 역할**. 번들이 `type="alert"`일 때만 붙인다 | components/dialogs(accessibility) | **고침** — `type="alert"` (9abc1202) |
| ~~A27~~ ✅ | ~~보드 카드가 directly actionable인데 그 안에 동작하는 칩~~ **고침** | 카드는 **동작하는 표면**이거나 **동작을 담는 컨테이너**, **둘 중 하나**. "An action shouldn't be placed on an actionable surface" | components/cards(accessibility) | **고침**(방금) — 카드를 **컨테이너**로 되돌리고, 제목을 `<a href>`로, 라벨을 `<button>`으로. ★ 대조군이 답을 줬다: 플릿 행의 `<a>`+중첩 버튼은 **multi-action 리스트 항목**이라 허용되고, 보드 카드는 **card**라 금지 — **컴포넌트가 달라 규칙이 다르다** |
| ~~A28~~ ✅ | ~~보드 카드와 라벨 칩이 `<div>` + `onclick`~~ **고침** — 파일 전체에 **`tabindex` 0건, `role="` 0건**(실측). ⚠ 대조군: 같은 페이지의 플릿 행은 `<a href>`라 통과 — **두 목록의 처우가 갈린다** | "**모든 동작 요소는 탭 스톱**이어야", "**스크린리더 포커스와 키보드 포커스를 둘 다**" | components/cards(accessibility) | **고침**(방금) — 제목 `<a href>` + 라벨 `<button>`. 둘 다 탭 스톱이고 주소도 생겨 **가운데 클릭·url 복사**가 된다 |
| ~~A29~~ ✅ | 툴팁 7개가 전부 **네이티브 `title`** — **키보드 포커스에서 안 뜨고** Tooltip 역할도 없다 | "hovered **or focused**", "**Tooltip role**" | components/tooltips(accessibility) | **고침** — 자체 툴팁 — **호버 *또는* 포커스**, 1.5초 뒤 소멸, 한 번에 하나, `role="tooltip"`. ⚠ 네이티브 `title`은 **제거**(둘이 겹쳤다) (b7ac4a6f, cbf90ecb) |
| ~~A30~~ ✅ | ~~검색 결과가 바뀌어도 **아무것도 알리지 않는다** — `aria-live`·`role="status"`·`role="log"` **전부 0건** | "When search suggestions and results appear, the screen reader **must** announce the change" | components/search(accessibility) | **고침** — 영구 `#say` sr-only 영역 + 결과 수 고지. ⚠ 첫 시도(렌더마다 영역 생성)는 **틀렸다** — 라이브 영역은 내용이 바뀌기 전에 DOM에 있어야 한다 |
| ~~A31~~ ✅ | 다크 테마 `--outline:#5A5048`가 `--bg:#14110d` 대비 **2.40:1** (라이트는 3.75:1로 통과) | **3:1 이상**. 칩 클러스터("outline 롤을 써서 **최소 3:1 확보**"), 텍스트 필드("container outline **minimum 3:1**"), color/roles("**or another color providing 3:1**") 세 곳이 같은 요구 | components/chips·text-fields(accessibility), styles/color/roles | **고침** — 다크 `--outline:#72675C` = **3.41:1**, TUI도 동기화 (62685ee7) |
| ~~A34~~ ✅ | 푸시 **본문이 `text.Clip(…,160)`**(push.go:302·346) — 권장치의 **2배** | **펼친 본문 <80자**, 접힌 본문 <40자. 160은 가이드가 **SMS**에 준 숫자다 | content-design/notifications | **고침** — 본문 80자 (d881c27d) |
| ~~A35~~ ✅ | 푸시 **제목에 동적 이름**(301은 **둘**: `To + " answered " + From`) → **<29자 초과 가능** | "**Title: <29 chars**" + ⚠ "**Text that gets truncated in the headline will not expand**" + "**Place dynamic text in the notification body**" | content-design/notifications | **고침** — 제목에서 이름 하나를 본문으로 (d881c27d) |
| ~~A38~~ ✅ | `hint.mcp_command`에 **`e.g.`** | ⚠ "Avoid Latin abbreviations in UI text **such as e.g. or etc.** Instead, use full phrases like **"for example,"**" | content-design/style-guide | **고침** — `for example` (d881c27d) |
| ~~A39~~ ✅ | **단문에 마침표 9건**(`board.nothing`·`notify.unsupported`·`empty.*` 등) | "Avoid using periods to end **single** sentences" — 라벨·툴팁·다이얼로그 본문·목록에서. ⚠ `notify.insecure`는 2문장이라 **정상** | content-design/style-guide | **고침** — 단문 9건 — 긴 문장 3건은 규칙대로 유지 (d881c27d) |
| ~~A43~~ ✅ | `showNotification(m.title \|\| 'magi', …)`(push.go:399) 폴백이 **앱 이름** | "An app's name or logo is **already included** in a notification's design. Use the limited space for other information" | content-design/notifications | **고침** — 폴백을 앱 이름 대신 상황 문구로 (d881c27d) |
| ~~A44~~ ✅ | ~~sys 타입스케일 20개 토큰이 **px**~~ **✅ 고침** | 웹의 폰트 단위는 **rem** — "**Web: rem**", "the conversion is **SP_SIZE/16 = rem**" | styles/typography/type-scale-tokens | **절반 고침**(752519c5) — ★ 숫자는 M3 표와 **10/10 일치**했고 틀린 건 단위뿐이었다. ⚠ **컴포넌트는 이제 사용자 글꼴 설정을 따르지만 손으로 쓴 36곳은 안 따른다** — 200% 리사이즈 요구가 절반만 충족된다 |
| ~~A45~~ ✅ | ~~`title-small`·`title-medium` 서체가 **brand**(`--display`)~~ **✅ 고침** | M3 표는 둘 다 **plain**(brand는 title-large부터). "**The plain typeface is used for smaller type styles**" | styles/typography/type-scale-tokens | ✅ `--mono`로 |
| ~~A49~~ ✅ | 접힌 레일 폭 **72px**(`--rail-w:4.5rem`) | **96dp**(표준) / **80dp**(narrow 변형) — 둘 다 못 미친다. vertical 항목 산식(16+56+16=**88dp**)도 72px에 안 들어간다 | components/navigation-rail/specs | **고침** — `--rail-w:5rem`(80px) — narrow 최소치 (14430866) |
| ~~A50~~ ✅ | ~~레일 아이콘 20px~~ **24px로 고침**(page.go:1452·1462) | **24dp** — lists("leading icon size 24dp")와 nav-rail("item icon size 24dp") 양쪽 | components/{lists,navigation-rail}/specs | **작다** — ⚠ 파일 전체 아이콘: 20px×5·21px×3·22px×2, **24px는 0개** |
| ~~A51~~ ✅ | ~~leading-space 14px~~ **16px로 고침**(page.go:472) | **16dp** — lists·nav-rail 둘 다 | components/{lists,navigation-rail}/specs | **아주 작다** |
| ~~A52~~ ✅ | ~~1인칭·2인칭 혼용~~ **고침** — `field.intervened:"what **I** had to say"` vs `label.answer:"**your** answer"`·`nav.where:"where **you** are"` | ❌ "**Avoid mixing 'me' or 'my' with 'you' or 'your.**' It can cause confusion to see both forms of address in the same context." 법적 동의문 예외에 해당 안 됨 | content-design/style-guide/word-choice | **아주 작다** — 문자열 하나 |
| ~~A53~~ ✅ | ~~축약형 미사용~~ **고침** — `cannot reach magi-web`·`does not report it` | "**Use contractions**… easier to understand and scan." 예외는 **주의를 요할 때**뿐이고 이건 해당 없음 | style-guide/grammar-and-punctuation | **아주 작다** |
| ~~A54~~ ✅ | ~~em dash~~ **4곳 고침**(`hint.compact`·`embed.none`·`hint.mcp_env` 등) | "**Em dashes are best avoided in UX writing**" — 쉼표·마침표·새 문장으로 단순화 | style-guide/grammar-and-punctuation | **아주 작다** |
| ~~A57~~ ✅ | ~~`--md-text-button-label-text-weight:600`~~ | Label Large **weight 500**. 번들 기본이 이미 500이었다 | components/buttons/specs | **고침**(c2a081eb) |
| ~~A58~~ ✅ | ~~`#state .jump` 라벨 **11px**, 좁은 폭에서 **10px**~~ | Text button label = **14pt**(Label Large). ⚠ page.go:1193 주석이 "12개 버튼 중 8개에서 label-small을 쓰던 것을 고쳤다"고 적는데 **`.jump`가 살아남은 하나**였고 미디어쿼리가 **더 작게** 만들고 있었다 | components/buttons/specs | **고침** — 오버라이드 삭제(번들 기본이 맞다) |
| ~~A59~~ ✅ | ~~`.answer md-filled-tonal-button` 라벨 **12px**~~ | Filled tonal label = **14pt** | components/buttons/specs | **고침** |
| ~~A60~~ ✅ | ~~`.tile` 필터 칩 높이 **40px**~~ | Filter chip container height = **32dp**. ⚠ 40은 **토큰(32)도 타겟(48)도 아니었다** — 타겟은 번들 `.touch`가 컨테이너 높이와 무관하게 48px로 보장한다 | components/chips/specs | **고침** — 오버라이드 삭제 |
| ~~A61~~ ✅ | ~~아이콘 버튼 상태 레이어 **36px** 2곳~~(`.actions`, 좁은 폭의 `#prefs`·`#themeToggle`) | 크기 스케일은 **32 / 40 / 56 / 96 / 136**. 36은 없다 | components/icon-buttons/specs | **고침** — 40px로. 접근성 문제는 아니었다(`.touch`가 48px 타겟 보장) |
| ~~A62~~ ✅ | ~~좁은 폭에서 탭 높이 **44px**~~ | **48dp**. ★ 후보였는데 **결함으로 확정**됐다 — 에이전트가 번들 `.touch` 9곳을 전수 확인했고 **`md-primary-tab`은 그중 하나가 아니다**(탭 템플릿에 `.touch`도 `min-height`도 없다). **폰 폭에서 실제 타겟이 44px**였다 | components/tabs/specs | **고침** — 오버라이드 삭제(번들 기본 48px) |
| ~~A63~~ ✅ | ~~탭 아래 **구분선이 없다**~~ | 스펙이 컨테이너의 일부로 그린다: **1dp / outline-variant**. 번들도 magi도 안 그렸다 | components/tabs/specs | **고침** — `#tabs`·`#ptabs`에 `border-bottom` |

**철회(3)**: **A2** compact `md-tabs` — 목적지가 셋 미만이면 가이드가 **탭을 지시**한다.
**A3** 74ch — 큰 화면 천장은 **120자**다. **A7** 비활성 지표 — magi에 `cursor:not-allowed`가
**0건**이고 실제로는 색으로 알린다. **셋 다 고쳤다면 위반을 만들었을 것**이다.

**유지(B)**: 모든 폭에서 modal 레일(가이드가 든 modal 용례 + **드로어는 폐기되어 펼침 레일이 후계자**)
· 접힌 레일의 라벨 숨김("reduced visual impact" 탈출구, `aria-label` 유지) · 108ch 트랜스크립트 ·
`--ease-emphasized`가 문서 대신 번들을 따르는 것 · 테마 토글이 스위치가 아닌 것(값이 셋이고
light/dark는 opposing options라 스위치면 **두 겹 위반**) · `MCP` 약어(도메인의 실제 이름) ·
`--casper` 다크(오류색이 아니라 **에이전트 정체성 색**).

★ **실측이 판정을 뒤집은 일곱 번**: 타겟 48px(아이콘버튼·칩 — 번들 `.touch`가 보장) ·
`.title` 툴팁(있었다) · 레일 인디케이터(pill이 있었다) · 화살표 키(`md-list`가 처리) ·
칩 outline(**안 건드려서** 올바른 기본값) · A7 커서(0건) · 탭 44px(**여기만은 `.touch`가 없어 진짜 결함**).
→ **번들을 보고 나서 판정한다.**
