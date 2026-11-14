---
name: uml-class
description: Draw a Design Class Diagram (object design) - attribute and operation syntax, visibility, the relationships from dependency to composition plus generalization and Realization, association details, abstract classes vs interfaces, mapping to code and visibility between objects, with Mermaid classDiagram syntax.
clients: coding
---

# Design Class Diagram

Part of the `ooad` family; load `ooad` for where this artifact sits, `uml-domain-model` for the analysis view and `uml-sequence` for the interactions that produce its operations.

The class diagram has two perspectives: conceptual (the domain model, analysis) and design (this one). Each class has three compartments: name, attributes, operations.

## Members

- Visibility: `+` public, `-` private, `#` protected, `~` package.
- Attribute: `visibility name : Type [multiplicity] = default {property}`, for example `-tags : String [0..*] {ordered}`. `/name` is derived; underlined members are static; properties include `{readOnly}`, `{unique}`, `{ordered}`.
- Operation: `visibility name(direction param : Type, ...) : ReturnType {property}`, with parameter direction `in`, `out` or `inout`.
- An operation is a declaration; a method is its implementation.
- Getters and setters are usually omitted (n attributes would add 2n uninteresting operations). Many of them signal weak encapsulation.

## Relationships, weakest to strongest

| Relationship | Meaning | UML | Mermaid |
|---|---|---|---|
| Dependency | uses briefly (parameter or local) | dashed arrow | `A ..> B` |
| Association | works with for a prolonged time; the default; equivalent to an attribute of that type | solid line, optional navigability arrow | `A --> B` or `A -- B` |
| Aggregation | whole-part; parts exist on their own and may be shared | hollow diamond at the whole | `Whole o-- Part` |
| Composition | parts live and die with one whole; whole multiplicity at most 1 | filled diamond at the whole | `Whole *-- Part` |
| Generalization | is-a; subclass inherits attributes, operations and associations; transitive | hollow triangle to the superclass | `Super <\|-- Sub` |
| Realization | a class implements an interface | dashed line, hollow triangle | `Impl ..\|> Iface` |

## Associations

- Name, reading direction, role names and multiplicity at each end. Put multiplicity on every association: `A "1" --> "1..*" B : contains`.
- Navigability: an arrow means the source holds a reference to the target; an X forbids it; unmarked ends are often read as two-way. Mutual references (A and B holding each other) need a reason; otherwise make the association one-way.
- Data types (dates, money, strings) as attribute text; other classes as association lines. Both mean the same; the line gives visual weight.
- An association with its own data (typically many-to-many) becomes an association class, for example `Employment { salary, startDate }` between `Company` and `Person`.

## Abstract classes, interfaces and special classes

- Abstract class: `{abstract}` or italic name; cannot be instantiated; single inheritance; may hold fields and implemented methods.
- Interface: `<<interface>>`; a contract with no fields; a class may realize several. An interface with no realizing class is an unfinished design. Name interfaces consistently (for example `I` + capitalized name, or a capability adjective) and keep implementation names consistent.
- Singleton class: underlined static `instance` attribute and `getInstance()`, a `1` in the corner.
- Active class: owns its own thread of control (double side borders).

## From sequences to classes

Every message in a design sequence diagram becomes an operation on the receiver's class; classes discovered while drawing interactions are summarized here. Draw both concurrently, letting the sequences lead. Check the reverse direction too: an operation no sequence uses is dead.

## Visibility between objects and mapping to code

For A to send a message to B, B must be visible to A:

1. Attribute visibility: B is an attribute of A (permanent, most common).
2. Parameter visibility: B is passed as a parameter (temporary; often turned into attribute visibility in a constructor).
3. Local visibility: B is created or returned inside a method.
4. Global visibility: rare; prefer a Singleton.

The class diagram gives class definitions (fields and method signatures); sequence diagrams give method bodies (with the declarations, conditions and initialization they imply). Implement from the least coupled classes to the most coupled (value and description classes first, then the classes that aggregate them, then controllers).

## Patterns in the diagram

When a design pattern is applied, name it in a note next to the classes involved and say which responsibility or quality attribute it serves (load `design-patterns` and `architecture-tactics`).

## Mermaid

```text
classDiagram
  class Sale {
    -dateTime : DateTime
    +total() Money
    +makeLineItem(desc, qty)
  }
  class Item {
    <<interface>>
    +price() Money
  }
  Sale "1" *-- "1..*" SalesLineItem : contains
  Register "1" --> "1" Sale : captures
  Store o-- Register
  Register ..> ProductCatalog : uses
  Book ..|> Item
  Payment <|-- CashPayment
```

Mermaid has no association classes, navigability Xs, `in/out` parameter markers or underlining for static members (use `$` after a member for static and `*` for abstract); say so when the diagram needs them.

## Checklist

- Every message in the sequences exists as an operation on its receiver; no operation is unused.
- Relationship notation and multiplicities are correct; interfaces have realizations; mutual references are justified.
- Applied patterns are named with what they serve.
