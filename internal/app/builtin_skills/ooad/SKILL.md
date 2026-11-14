---
name: ooad
description: Hub for object-oriented analysis and design in the Unified Process style (Larman, Applying UML and Patterns) - phases, analysis/design/implementation lanes, artifact order, traceability and a review checklist; points to the per-diagram skills, design patterns and architecture tactics.
clients: coding
---

# Object-oriented analysis and design (UP style)

Use this when asked for OOAD or UML artifacts, when deciding which artifact comes next, or when reviewing a design for consistency. Load the skill for the artifact at hand:

| Artifact or topic | Skill |
|---|---|
| System Sequence Diagram, system operations | `uml-ssd` |
| Design sequence diagrams, combined fragments | `uml-sequence` |
| Domain Model (conceptual classes) | `uml-domain-model` |
| Design Class Diagram, relationships, visibility, mapping to code | `uml-class` |
| Package, component and deployment diagrams | `uml-architecture` |
| GRASP and GoF patterns, where to put a responsibility | `design-patterns` |
| Quality attributes and the tactics that meet them | `architecture-tactics` |

Load `visual-explanation` for how to present diagrams in this client. Mermaid is not complete UML: say where a diagram departs from standard notation.

## Why OOAD

Structured analysis and design decomposes a problem top-down into functions (data flow diagrams, state machines). Object-oriented analysis and design organizes it as objects that hold data and operations and cooperate by messages, giving encapsulation, inheritance and polymorphism. Larman's summary: identify the requirements, create a domain model, add methods to the appropriate classes and define the messaging between objects that fulfils the requirements.

## Core principle: let the dynamic model drive

- Draw the interaction (sequence diagram) first and let the class diagram follow. Messages become operations; classes found while drawing interactions are summarized in the class diagram.
- Draw both views concurrently and iteratively, but the dynamic view leads. A class diagram invented alone and animated afterwards places responsibilities by guess and is costly to maintain.
- Implementation follows mechanically: the class diagram gives class definitions, sequence diagrams give method bodies. If a sequence cannot be turned into a method body, the design is not finished.

## Unified Process in brief

Iterative (short time-boxed iterations, each a small waterfall), use-case driven, architecture-centric and risk-driven.

| Phase | Goal | Artifacts started |
|---|---|---|
| Inception | Vision, scope, feasibility; most actors and use cases named, most in brief form (10-20% fully dressed); riskiest requirements identified | Use-case model, vision, supplementary specification (non-functional requirements), glossary |
| Elaboration | Core risky architecture built and tested; most requirements stabilized; risks mitigated | Domain model, design model (sequence diagrams and design class diagram), architecture document, data model |
| Construction | Remaining lower-risk elements built incrementally, with tests and refactoring | Refined design and data model; package, component and deployment diagrams |
| Transition | Deployment | |

Disciplines run across all phases: business modelling, requirements, design, implementation, test, deployment, configuration and change management, project management, environment. A development case records which practices and artifacts a project uses.

UML is used in three modes: sketch (conversation, early analysis), blueprint (detailed design, code generation) and programming language (full generation, still immature). UML 2 has 13 diagrams: structure (class, object, component, composite structure, deployment, package) and behaviour (activity, use case, state machine, and the interaction diagrams sequence, communication, interaction overview, timing). Most design work needs use case, sequence and class, plus package, component and deployment.

## Keep each artifact in its lane

- **Analysis (what):** use-case model, system sequence diagrams with the system as a black box, domain model of conceptual classes with no operations.
- **Design (how, at object level):** design sequence diagrams of collaborating objects or components, design class diagram with operations, visibility and navigable associations.
- **Implementation:** establish visibility between objects and map the design to code.

Most drafting mistakes are lane violations: operations in a domain model, internal components in a system sequence diagram, user-interface steps in a use case ("types the ID in a dialog" instead of "identifies self"), implementation wording in a black-box artifact ("writes to the database" instead of "records the sale").

## Use cases

- Formats: brief (one paragraph), casual (several paragraphs, main and alternative scenarios), fully dressed (every step with preconditions, success guarantee, main success scenario, extensions).
- Number requirements and map them to use cases. Use cases are evident (an actor sees them) or hidden (system-internal, no actor). Do not attach a hidden use case to an actor to make it look complete.
- Describe main, alternative and exceptional courses, marking each step with who acts (actor or system). A use case that always succeeds is incomplete (a login that can never fail has no retry or lockout path).
- Write at the level of goals, not user-interface mechanics.
- Mermaid has no use-case notation: draw a labelled `flowchart` (actors and use cases as nodes) or a table, and say so.

## Artifact order and traceability

Requirements list -> use-case diagram -> use-case descriptions -> system sequence diagrams -> domain model -> design sequence diagrams -> design class diagram, with a traceability table and a glossary kept current throughout.

Keep traceability end to end: requirement -> use case -> system operation -> sequence diagram -> class and operation. Flag requirements no use case covers and use cases no requirement asks for. Names must match across artifacts: system operation = first message of its design sequence = operation on the receiving class.

## Review checklist

- Every requirement maps to a use case and back; the traceability table has no gaps.
- Use cases name the actor or system for each step and have alternative and exceptional courses.
- The domain model has no operations or implementation elements.
- Each system sequence diagram shows only actors and the black-box system; the main flow is not wrapped in a frame.
- Each design sequence diagram realizes one system operation and shows internal messages; fragments match their meaning; guards are concrete; replies appear where values return.
- Every message in a sequence diagram exists as an operation on the receiver's class, and no operation is unused by any sequence.
- Interfaces have realizations; relationship notation and multiplicities are correct; mutual references are justified.
- Applied patterns are named with the responsibility or quality attribute they serve.
