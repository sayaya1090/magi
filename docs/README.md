# magi documentation

[English](README.md) · [한국어](README.ko.md) · [↑ Project README](../README.md)

The map of everything under `docs/`. Every document carries the same header: a title, a language
switcher, and a one-line **status** — whether it is a current reference or a historical record kept
for its rationale.

## Start here

| Document | What it is |
|---|---|
| [MANUAL](MANUAL.md) · [한국어](MANUAL.ko.md) | **User guide.** Install, run, configure; the TUI and the web console, end to end. |
| [CONTEXT](CONTEXT.md) | **Orientation (한국어).** A short summary of the goal, the architecture, and the turn loop. |

## Reference — the as-built system

| Document | What it is |
|---|---|
| [ARCHITECTURE](ARCHITECTURE.md) · [한국어](ARCHITECTURE.ko.md) | **The as-built reference** for developing on magi: hexagonal layering, the agent loop, the finish gate, guardrails, tools, extension points. Where the design documents disagree with this file, this file wins. |
| [DIAGRAMS](DIAGRAMS.md) · [한국어](DIAGRAMS.ko.md) | The visual companion to ARCHITECTURE — one axis from the process boundary (L0) down to the class diagrams (L5–L9), all mermaid. |
| [UI](UI.md) · [한국어](UI.ko.md) | The two surfaces — the web console (`cmd/magi-web`) and the terminal UI (`internal/adapter/tui`): what is on each screen, the design rules they keep, and why. |
| [EXTENDING](EXTENDING.md) · [한국어](EXTENDING.ko.md) | The practical procedure for attaching **external tools (MCP)** and **team-shared memory/skills** (the experience store) to magi. |

## Design & history

These are kept for the record of what was decided and on what grounds. They are **not** the current
reference — where they disagree with ARCHITECTURE/MANUAL, those win.

| Document | What it is |
|---|---|
| [SPEC](SPEC.md) · [한국어](SPEC.ko.md) | **Historical.** The original feature specification with test cases, from before the council/loop redesign. |
| [DESIGN](DESIGN.md) · [한국어](DESIGN.ko.md) | **Historical.** The detailed design intent as of the M1 start. |

## Proposals

`docs/proposals/` holds dated design proposals — point-in-time records of a direction being weighed,
not current documentation. Read them for the reasoning behind a change, dated by when it was written.
