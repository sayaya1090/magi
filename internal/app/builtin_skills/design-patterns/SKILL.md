---
name: design-patterns
description: Assign responsibilities with GRASP (Information Expert, Creator, Controller, Low Coupling, High Cohesion, Polymorphism, Pure Fabrication, Indirection, Protected Variations) and choose GoF patterns (creational, structural, behavioural such as Strategy, Observer, Adapter) by the problem they solve and their cost.
clients: coding
---

# Design patterns

Part of the `ooad` family. Patterns are named solutions to recurring design problems. Explain the problem a pattern solves and its cost before recommending it; do not add one for its own sake. In a class diagram, name an applied pattern in a note with the responsibility or quality attribute it serves (`uml-class`).

## GRASP: where a responsibility goes

Use these while drawing design sequence diagrams: each message is a responsibility being assigned.

| Principle | Question it answers | Answer |
|---|---|---|
| Information Expert | Who should do X? | The class that has the information X needs |
| Creator | Who creates A? | B, if B contains, aggregates, records or closely uses A, or has its initializing data |
| Controller | Who first receives a system operation from the UI? | A facade controller for the system or a use-case controller; not a UI class |
| Low Coupling | How to reduce the impact of change? | Prefer the assignment that adds fewer dependencies |
| High Cohesion | How to keep classes focused? | Prefer the assignment that keeps related responsibilities together |
| Polymorphism | How to handle alternatives by type? | Give each type the behaviour, not a switch on type |
| Pure Fabrication | Where does a responsibility go that no domain class should hold? | An invented service class (for example persistence) |
| Indirection | How to decouple two parts? | An intermediate object between them |
| Protected Variations | How to keep variation points from rippling? | Wrap them behind a stable interface |

## GoF patterns

### Creational

| Pattern | Problem | Cost |
|---|---|---|
| Factory Method | A class cannot know which subclass to instantiate | One subclass per product family |
| Abstract Factory | Create families of related objects consistently | Adding a product kind changes every factory |
| Builder | Construct a complex object step by step | Extra builder class |
| Prototype | Create by copying a configured instance | Deep-copy correctness |
| Singleton | Exactly one instance with global access | Hidden global state, harder testing; prefer dependency injection where possible |

### Structural

| Pattern | Problem | Cost |
|---|---|---|
| Adapter | Use a class whose interface does not match | One adapter per adaptee |
| Bridge | Vary abstraction and implementation independently | Two hierarchies to maintain |
| Composite | Treat individual objects and groups uniformly | Overly general component interface |
| Decorator | Add responsibilities dynamically without subclass explosion | Many small objects, identity checks break |
| Facade | Give a simple entry to a complex subsystem | Can become a god object |
| Flyweight | Share many fine-grained objects | Extrinsic state must be passed in |
| Proxy | Control access (lazy, remote, protection) | Extra indirection and latency |

### Behavioural

| Pattern | Problem | Cost |
|---|---|---|
| Strategy | Swap an algorithm at run time | Clients must know the strategies exist |
| Observer | Notify dependents when a subject changes | Update cascades, ordering and memory leaks from unregistered observers |
| Template Method | Fix an algorithm's skeleton, vary steps in subclasses | Inheritance coupling |
| Command | Treat a request as an object (queue, undo, log) | One class per command |
| State | Behaviour changes with internal state | One class per state |
| Chain of Responsibility | Let several handlers try a request | No guarantee a request is handled |
| Iterator | Traverse a collection without exposing it | Mostly built into languages |
| Mediator | Centralize complex interactions between colleagues | The mediator grows large |
| Memento | Capture and restore state without breaking encapsulation | Memory for snapshots |
| Visitor | Add operations over a fixed structure | Adding element types changes every visitor |
| Interpreter | Evaluate a simple language | Poor fit for large grammars |

## Patterns and quality attributes

- Modifiability: Strategy, Observer, Bridge, Adapter, Facade, Template Method (localize change, defer binding).
- Performance: Flyweight, Proxy (lazy loading, caching).
- Testability: Strategy and Dependency Injection instead of Singleton.

Load `architecture-tactics` for tactics at architecture level.

## Drawing a pattern

Show the participants with their roles in a `classDiagram` and a short `sequenceDiagram` of the key interaction (for Observer: `attach`, `notify`, `update`). Name the pattern in a note or title.
