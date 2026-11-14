---
name: visual-explanation
description: Explain complex software structure, UML relationships, request sequences, and state transitions with Mermaid and concise prose grounded in the code.
---

# Visual explanations

Use a diagram by default whenever the explanation involves relationships, ordering or causation with two or more steps. That includes root-cause chains, the order of an operation, a change across several files and a comparison of alternatives, not only architecture. Start with the conclusion, present one focused diagram, then explain the important arrows and tradeoffs.

Read the relevant code first. Use actual module, type and method names. Label proposals and assumptions explicitly; do not present an inferred call as an observed one. Name the source files after the diagram. Split large views into a system overview and a separate focused flow.

Choose the notation that answers the developer's question:

- Components and dependencies: `flowchart`; say whether an arrow means calls, data flow or ownership. Keep deployment nodes distinct from code modules.
- A request across components: `sequenceDiagram`; order calls and results, and use `alt`, `opt` or `loop` for actual branches.
- Causes, operation steps and alternatives: `flowchart`; label each arrow with what leads to what, and put the options being compared side by side.
- Lifecycle and recovery: `stateDiagram-v2`; show trigger, success, failure and cancellation paths.
- Types and GoF patterns: `classDiagram`; use real responsibilities and relationships. Explain the problem a pattern solves and its cost before recommending it.
- OOAD: distinguish domain concepts (analysis, no software operations) from design classes. Derive design operations from sequence messages. Keep system sequence diagrams black-box; internal objects belong in design sequences.

Mermaid is not a complete UML implementation. Do not invent unsupported use-case notation; use a labeled flowchart or prose and say what it represents. Do not claim a diagram is executable or formally verified.

Use the available Mermaid tool. Pass plain source without HTML, click callbacks, URLs or configuration. A successful preparation does not establish valid Mermaid syntax: rendering is checked by the client. Keep labels short and use stable node identifiers. Add an accessible prose summary.

In JetBrains, web and VS Code, include the fenced Mermaid source in the answer so the client can render it. The client offers zoom, source copying and selection to prepare a question. Do not claim that clicking a node executes code or navigates to a file.

When only `mermaid_preview` is available, present its local preview link and a brief explanation. Do not print the source in the terminal. Never claim the browser opened: the tool saves a page, and the developer opens the link. When no Mermaid tool is available, use text. Do not invent a preview link or install a browser.

If project-specific OOAD, architecture or design-pattern skills are listed, load the relevant one for its conventions. This built-in guide is self-contained and does not require private course documents.
