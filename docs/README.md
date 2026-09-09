# magi Documentation Map

[English](README.md) · [한국어](README.ko.md) · [↑ Project README](../README.md)

A sitemap of all documents under `docs/`. Every document carries a standard header: title, language switcher, and a status line indicating whether it is an active reference or a historical design record.

## Core Guides

| Document | Description |
|---|---|
| [MANUAL](MANUAL.md) · [한국어](MANUAL.ko.md) | **User Manual**: Complete guide to installation, execution, configuration, TUI, and web console. |
| [CONTEXT](CONTEXT.md) | **Orientation (한국어)**: Summary of project goals, core architecture, and the turn execution loop. |
| [SECURITY](../SECURITY.md) · [한국어](../SECURITY.ko.md) | **Trust Boundaries**: Tool gate, workspace isolation, authentication model, and defended perimeters. |

## Technical References (As-Built Implementation)

| Document | Description |
|---|---|
| [ARCHITECTURE](ARCHITECTURE.md) · [한국어](ARCHITECTURE.ko.md) | **As-Built System Architecture**: Hexagonal layers, agent loop, verification gate, guardrails, and extension points. If earlier design docs conflict, this document takes precedence. |
| [DIAGRAMS](DIAGRAMS.md) · [한국어](DIAGRAMS.ko.md) | **Architecture Diagrams**: Visual architecture from process boundaries (L0) down to class diagrams (L5–L9) in Mermaid. |
| [UI](UI.md) · [한국어](UI.ko.md) | **UI Design Specs**: Layout and design guidelines for web console (`clients/web/server`) and terminal UI (`internal/adapter/tui`). |
| [CLIENTS](CLIENTS.md) · [한국어](CLIENTS.ko.md) | **Client Platform Integrations**: Architecture and socket contracts for terminal, web console, JetBrains, Visual Studio, and Office add-ins. |
| [EXTENDING](EXTENDING.md) · [한국어](EXTENDING.ko.md) | **Extensibility Guide**: Practical integration steps for external tools (MCP) and shared team memory/skills. |

## Design History & Archived Records

Preserved to record early architectural decisions and rationales. These are historical records; where they conflict with current code or the ARCHITECTURE/MANUAL documents, the active documentation takes precedence.

| Document | Description |
|---|---|
| [SPEC](SPEC.md) · [한국어](SPEC.ko.md) | **Original Specification (Archived)**: Initial feature specification prior to the council and loop redesign. |
| [DESIGN](DESIGN.md) · [한국어](DESIGN.ko.md) | **Initial Design (Archived)**: Milestone 1 design intent and architectural decision history. |

## Proposals

The `docs/proposals/` directory contains dated design proposals from specific decision points. They serve as historical decision contexts rather than active specifications.

