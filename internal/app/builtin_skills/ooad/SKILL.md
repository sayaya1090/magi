---
name: ooad
description: Produce object-oriented analysis and design artifacts (use cases, system sequence diagrams, domain model, design sequence diagrams, design class diagram, traceability) in the Unified Process style of Larman's Applying UML and Patterns, and review them for the usual lane and notation mistakes.
---

# Object-oriented analysis and design (UP style)

Use this when asked for OOAD or UML artifacts, when deciding which artifact comes next, or when reviewing a design for consistency. Load `visual-explanation` for how to present diagrams in this client. Mermaid is not complete UML: say where a diagram departs from standard notation.

## Core principle: let the dynamic model drive

- Draw the interaction (sequence diagram) first and let the class diagram follow from it. Messages become operations; classes found while drawing interactions are summarized in the class diagram.
- Draw both views concurrently and iteratively, but the dynamic view leads. A class diagram invented alone and animated afterwards places responsibilities by guess and is costly to maintain.
- If a sequence cannot be turned into a method body, the design is not finished.

## Unified Process in brief

Iterative (short time-boxed iterations, each a small waterfall), use-case driven, architecture-centric and risk-driven.

| Phase | Goal | Artifacts started |
|---|---|---|
| Inception | Vision, scope, feasibility; most actors and use cases named, most in brief form | Use-case model, vision, supplementary specification (non-functional requirements), glossary |
| Elaboration | Core risky architecture built and tested; most requirements stabilized | Domain model, design model (sequence diagrams and design class diagram), architecture document, data model |
| Construction | Remaining lower-risk elements built incrementally, with tests and refactoring | Refined design and data model; package, component and deployment diagrams |
| Transition | Deployment | |

Disciplines (requirements, design, implementation, test and others) run across all phases; a project records which practices and artifacts it uses.

## Keep each artifact in its lane

- **Analysis (what):** use-case model, system sequence diagrams with the system as a black box, domain model of conceptual classes with no operations.
- **Design (how, at object level):** design sequence diagrams of collaborating objects or components, design class diagram with operations, visibility and navigable associations.
- **Implementation:** establish visibility between objects and map the design to code.

Most drafting mistakes are lane violations: operations in a domain model, internal components in a system sequence diagram, user-interface steps in a use case ("types the ID in a dialog" instead of "identifies self"), implementation wording in a black-box artifact ("writes to the database" instead of "records the sale").

## Artifact order and traceability

Requirements list → use-case diagram → use-case descriptions → system sequence diagrams → domain model → design sequence diagrams → design class diagram, with a traceability table and a glossary kept current throughout.

- Number requirements and trace each one: requirement → use case → system operation → sequence diagram → class and operation. Flag requirements no use case covers and use cases no requirement asks for.
- Use cases may be evident (an actor sees them) or hidden (system-internal, no actor). Do not attach a hidden use case to an actor to make it look complete.
- Describe main, alternative and exceptional courses. A use case that always succeeds is incomplete (a login that can never fail has no retry or lockout path). Mark each step with who acts, actor or system.

## System Sequence Diagram (analysis)

- One scenario of one use case: external actors and the system as a single black-box participant. No internal objects.
- Its messages are system operations; across all use cases they form the system interface and are the input to object design. Name them at the level of intention (`enterItem(itemID, quantity)`), not of devices (`scan`).
- Keep the main success flow outside frames. Put only the deviations in frames: a retry that returns to the main flow is `loop`, an early exit is `break`, an optional step is `opt`. Do not wrap the main flow in an `alt` with a "success" guard.
- Keep message names identical to the use-case steps and to the first message of the design sequence diagram that realizes each operation.

## Domain Model (analysis)

- A visual dictionary of conceptual classes, their attributes and associations with multiplicity. No operations and no implementation elements (database, screens). `Sale { dateTime }` is conceptual; `Sale { date, time, print() }` is a software class.
- Find candidates by noun and noun-phrase identification in the use-case text.
- There is no single correct domain model; usefulness matters more than correctness. Its value is understanding the domain and giving domain-layer classes their names (a small representational gap).

## Design Sequence Diagram

- Starts from exactly one system operation and shows the objects (controller, domain objects, persistence) that fulfil it. Show the system's internal messages, not only the actor's: a diagram that skips them cannot be turned into method bodies.
- Synchronous call: filled arrowhead, the caller waits; reply: dashed line, may be omitted when obvious; asynchronous: open arrowhead.
- Message syntax: `return = message(parameter : Type) : ReturnType`. Creation points at the new lifeline; destruction ends it with an X.
- Combined fragments, by meaning: `alt` alternatives with disjoint guards (`[else]` allowed); `opt` one optional operand; `loop` repetition with a guard; `break` runs its operand and leaves the enclosing fragment, so use it only for exception-like exits; `par` concurrent operands; `critical` an atomic region. Write guards as concrete conditions (`[status == ok]`), not prose ("if it works").

## Design Class Diagram

Visibility markers: `+` public, `-` private, `#` protected, `~` package. Static members are underlined; `/name` is derived. An operation is a declaration, a method its implementation. Getters and setters are usually omitted; many of them signal weak encapsulation.

Relationships from weakest to strongest:

| Relationship | Meaning | UML | Mermaid |
|---|---|---|---|
| Dependency | uses briefly (parameter or local) | dashed arrow | `A ..> B` |
| Association | works with for a prolonged time; the default; equivalent to an attribute of that type | solid line, optional navigability arrow | `A --> B` or `A -- B` |
| Aggregation | whole-part; parts exist on their own and may be shared | hollow diamond at the whole | `Whole o-- Part` |
| Composition | parts live and die with one whole; whole multiplicity at most 1 | filled diamond at the whole | `Whole *-- Part` |
| Generalization | is-a; subclass inherits attributes, operations and associations | hollow triangle to the superclass | `Super <|-- Sub` |
| Realization | a class implements an interface | dashed line, hollow triangle | `Impl ..|> Iface` |

- Show data types as attribute text and other classes as association lines. Put multiplicity on every association (`A "1" --> "1..*" B : label`).
- An association with its own data (typically many-to-many) becomes an association class.
- An interface with no realizing class is an unfinished design. Mutual references (A and B holding each other) need a reason; otherwise make the association one-way.
- When a design pattern is applied, name it in a note and the quality attribute it serves.

## From design to code

- For A to send a message to B, B must be visible to A: attribute visibility (most common, permanent), parameter visibility (temporary, often turned into an attribute in a constructor), local visibility (created or returned inside a method), global visibility (rare; prefer a singleton).
- The class diagram gives class definitions (fields and method signatures); sequence diagrams give method bodies.
- Implement from the least coupled classes to the most coupled.

## Drawing them in Mermaid

| Artifact | Mermaid |
|---|---|
| Use-case diagram | No Mermaid notation: use a labelled `flowchart` (actors and use cases as nodes) or a table, and say so |
| System Sequence Diagram | `sequenceDiagram` with `actor` participants and one `participant Sys as :System` |
| Design Sequence Diagram | `sequenceDiagram`; `->>` synchronous, `-->>` reply, `-)` asynchronous, `+`/`-` on a message for activation, `create participant` and `destroy` for lifetimes; frames `alt`/`else`, `opt`, `loop`, `break`, `par`/`and`, `critical` |
| Domain Model | `classDiagram` with attributes only and labelled associations with multiplicity |
| Design Class Diagram | `classDiagram` with visibility markers, operations, the relationship arrows in the table above and `<<interface>>` |
| Lifecycle of one object | `stateDiagram-v2` |

Mermaid draws a guard as the frame label text; write it in brackets so it reads as a guard (`alt [item found]`).

## Architecture views

- Package diagram: logical grouping, suited to layered architecture.
- Component diagram: components with provided and required interfaces and connectors; logical structure only, no physical placement.
- Deployment diagram: hardware nodes and the software deployed on them, showing how qualities such as availability and scalability are met (redundancy, multiple servers).

## Review checklist

- Every requirement maps to a use case and back; the traceability table has no gaps.
- Use cases name the actor or system for each step and have alternative and exceptional courses.
- The domain model has no operations or implementation elements.
- Each system sequence diagram shows only actors and the black-box system; the main flow is not wrapped in a frame.
- Each design sequence diagram realizes one system operation and shows internal messages; fragments match their meaning; guards are concrete; replies appear where values return.
- Every message in a sequence diagram exists as an operation on the receiver's class, and no operation is unused by any sequence.
- Interfaces have realizations; relationship notation and multiplicities are correct; mutual references are justified.
- Names match across artifacts: system operation = first design message = class operation.
