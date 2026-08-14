# What an IDE has that this console does not — and which of it is worth taking

Status: **survey and shortlist.** §2.1 is built (MANUAL §12.4); the rest is not. The point is to decide what belongs in a
console for **supervising companions** rather than to reproduce an editor.

The filter used throughout: magi's console exists so that a person can (a) understand what an agent
did, (b) intervene precisely while it works, and (c) check the result before it lands. A feature
earns its place by serving one of those three. "IntelliJ has it" is not a reason — an IDE is for a
person writing every line; here the person is reading, judging and steering, and the code is written
by something that never needs a minimap.

---

## 1. What the console already has

Worth writing down first, because half of an IDE list is already answered and the rest of this
document is only about the gaps.

| IDE feature | Where it already is in magi |
|---|---|
| Project tree | the workspace pane — one directory per request, only the folders you opened, kept for a few seconds, ⟳ to re-read |
| Open files in tabs | the card slot's tab strip, with the facts card as the first tab; a name shared by two files carries its directory |
| Editor | the file view's edit mode: a coloured ghost in flow sizes the box, a bare textarea over it, the reading view's gutter |
| Save with conflict detection | the save is a patch against what was opened; a file changed underneath is refused with the reason, and your text stays in the box |
| Find in files | the find dialog — names or contents, results replace the tree until you clear them |
| Source control panel | the git card: branch, ahead/behind, staged and unstaged groups, stage · unstage · discard · diff per file, pull · push · stash |
| Commit UI | the commit workbench: the file list, the diff, the message box, and a model draft of the message |
| Pull requests | the PR workbench: base, commits, diff, push warning, a model draft of title and body |
| Diff viewer | unified diff, coloured, per file (staged / unstaged / untracked) |
| Terminal | `!command` in the composer, run in the companion's workspace, its output in the transcript |
| Search history | `/search` across this workspace's past sessions, by word |
| Task runner | the companion. It is the whole point |

---

## 2. The shortlist — worth building, in order

### 2.1 A command palette — one keystroke to anything · **BUILT** (2026-08-14)

Every IDE has it (`Cmd+P`, `Cmd+Shift+P`) and this console has eleven screens, four panels, a rail,
two tab strips and a growing number of row menus. The controls are discoverable by eye and slow by
hand: switching to another companion is rail → list → row; opening a file is pane → tree → scroll.

What it should reach: a companion by name, a file in the open workspace, a meeting, a past session,
and the verbs that have no home on the current screen (interrupt, compact, convene, draft a commit).

Cheap, because everything it lists is already a function the page calls. The work is the index and
the keyboard handling, not new behaviour.

**Why it is first:** it is the only item on this list that makes every other feature faster to reach,
including the ones that already exist and nobody finds.

### 2.2 Hunk-level staging and discard

Today a file is staged or not. An agent that changed six things in one file, of which five are right,
gives a person one control: take all of it, or throw all of it away. IDEs solved this with gutter
actions on each hunk (`Stage this hunk` / `Revert this hunk`), and it is exactly the reviewer's loop
this console is for.

The diff is already rendered per file and per side. What is missing is the hunk boundary as a thing
the page knows about, and `git apply --cached` with a patch built from the selection.

**Why it matters here more than in an editor:** the code was not written by the person reading it.
Partial acceptance is the normal case, not the exception.

### 2.3 A problems panel, fed by what the companion already runs

Build and test output is in the transcript, as text, inside a fold. A person who wants to know
"what is broken right now" reads a wall of it. Every IDE turns the same output into a list: file,
line, message, click to open.

magi has the pieces: the tool result is captured, the workspace is readable, and the file view opens
at a line already. The work is parsing the compiler and test output of a handful of toolchains, which
is a known, bounded and slightly boring job — and the panel must say **which run** it came from and
when, because a stale problem list is worse than none.

**The magi-specific half:** the same panel should carry the council's objections and the guard's
notes. They are the same question — "what is wrong with this work" — from a different reader.

### 2.4 Blame, and the turn that wrote the line

An IDE's blame answers "which commit, which person". Here the interesting question is different and
magi is the only thing that can answer it: **which turn wrote this line, and what was it asked to
do?** The session log holds every edit with its arguments; the guard records before and after.

Line → the turn → the prompt that produced it → the council round that let it land. That is a
supervision tool an editor cannot have, and it is built from records this console already reads.

**Cost:** the mapping from a file's current lines to the edits that produced them is real work
(edits move lines), and it should be honest when it cannot answer rather than guessing.

### 2.5 A file's own timeline, with restore

IntelliJ's Local History, and the same idea as 2.4 seen along the time axis: every version of this
file that a turn produced, with a diff between any two and a restore. The store already holds them —
the edit tool's arguments are the patches.

It is the answer to the most common thing a supervisor says out loud, which is "it was fine an hour
ago". Today that is `git` if it happened to be committed, and nothing if it was not.

### 2.6 Keyboard shortcuts for the supervision loop

Not an IDE keymap. Six or seven keys that make the loop fast: next companion waiting on you, answer
yes / answer no, interrupt, jump to the workspace, jump to the conversation, open the palette. The
page already has the actions and the notion of "who is waiting"; what is missing is that a person
supervising four companions currently drives them with a mouse.

### 2.7 Side-by-side diff, as an option

The unified diff is the right default in an 18rem-to-half-screen slot. On a wide screen a side-by-side
view reads a rewrite much better, and a rewrite is what an agent produces. One layout switch over the
same parsed diff, remembered like the pane handles are.

---

## 3. Worth having later, or in a smaller form

| Feature | The honest version for this console |
|---|---|
| Go to definition / find references | needs a language server per language; the agent already answers "where is this used" with grep, and does it across the tree. Worth it only if the console becomes a place people write code, which is not the plan |
| Outline / structure view | cheap with tree-sitter for a few languages, and mostly useful while writing. A reader jumps by search |
| Multi-cursor, refactorings | the agent does these. A console that also did them would be two ways to change a file, one of them unobserved by the companion |
| Global search **and replace** | search is here. Replace across files is a mutation the companion should make, in its own log, where it can be reviewed |
| Debugger | a person debugging is a person writing code. Out of scope; the agent runs the tests |
| Minimap, code folding, bracket matching | editor comfort for long sessions of typing. Not the job |
| Extensions / plugin marketplace | magi has plugins on the engine side (Lua). A second extension surface in the page would be a second security boundary for little gain |
| Split editors | the console is already two panes plus a card slot; a third split is width nobody has |
| Run configurations | the companion is the runner. A saved command list is `.magi/skills` |

---

## 4. What this list is missing on purpose

Nothing here adds a way for a **person** to change the workspace that the companion does not see.
Every mutation the console offers today — save, stage, discard, commit, delete — is written into the
companion's own log so its next step reads a tree it knows about. Any feature from an IDE that
breaks that rule (an editor that autosaves silently, a refactoring that rewrites twenty files, a
formatter on save) would make the agent's context wrong in a way it cannot detect. That is the one
line this survey will not cross.

---

## 5. Suggested order

1. ~~**Command palette** (2.1)~~ — built: verbs, companions, and files once there is a query
2. **Hunk-level staging** (2.2) — the reviewer's loop, and the biggest daily saving
3. **Problems panel** (2.3) — turns output already on the screen into something to act on
4. **Keyboard shortcuts** (2.6) — small, and multiplies 1–3
5. **Blame → the turn** (2.4) and **file timeline** (2.5) — the two an editor cannot have, and the
   reason to build the rest first is that they are the most work
6. **Side-by-side diff** (2.7) — a layout switch, whenever the diff is next opened
