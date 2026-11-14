---
name: uml-sequence
description: Draw design sequence diagrams (object design) - one system operation per diagram, lifelines, message kinds and syntax, creation and destruction, all twelve combined fragments by meaning, related interaction diagrams, and the Mermaid sequenceDiagram syntax.
clients: coding
---

# Design sequence diagram

Part of the `ooad` family; load `ooad` for where this artifact sits and `uml-ssd` for the analysis-level system sequence diagram.

## Scope

- Models how objects collaborate over time **in one scenario**, starting from exactly one system operation (the first message, named as in the system sequence diagram).
- Levels: object level (controller, domain objects, persistence) or component level (components with ports and provided/required interfaces; external systems outside the boundary).
- Show the system's internal messages, not only the actor's. A diagram that skips them cannot be turned into method bodies: write it close to code.

## Notation

- Lifeline box: `:Sale` (unnamed instance) or `s1:Sale` (named). A `1` in the corner marks a singleton, reached through the Singleton pattern.
- Execution specification (activation bar): the period an object is handling a message.
- Found message: a message whose sender is not shown.
- Message kinds: synchronous (filled arrowhead, caller waits), reply (dashed, may be omitted when obvious), asynchronous (open arrowhead, caller does not wait).
- Message syntax: `return = message(parameter : Type) : ReturnType`.
- Creation: a message (conventionally `create`) pointing at the new lifeline's box placed at that height. Destruction: `<<destroy>>` and an X ending the lifeline.
- A slanted message with a duration constraint (`{2..3 days}`) shows a message that takes time; by default transmission is instantaneous.

## Combined fragments

Frames nest freely. Guards are boolean conditions in brackets; write them concretely (`[status == ok]`), not as prose ("if it works").

| Group | Operator | Meaning |
|---|---|---|
| Branching and loops | `alt` | Alternatives; disjoint guards, `[else]` allowed |
| | `opt` | One optional operand (if without else) |
| | `loop` | Repetition: `loop(min, max)`, `loop(*)` without upper bound; the guard is checked each iteration |
| | `break` | If the guard holds, run the operand and leave the enclosing fragment; use only for exception-like exits |
| Concurrency and order | `par` | Operands run in parallel |
| | `seq` | Weak sequencing (default ordering per lifeline) |
| | `strict` | Strict sequencing across lifelines |
| | `critical` | Atomic region, not interleaved |
| Filters and assertions | `ignore` | Listed messages may appear and are ignored |
| | `consider` | Only listed messages are considered |
| | `assert` | The operand is the only valid continuation |
| | `neg` | The operand describes an invalid trace |

Do not replace every `alt` with `break`, and do not use `break` where nothing exceptional happens.

## Related interaction diagrams

- Communication diagram: the same interaction with numbered messages (`1:`, `1.1:`) on links, emphasizing structure over time; better for explanation than for design.
- Interaction overview: an activity diagram whose nodes are interactions.
- Timing diagram: state of lifelines along a time axis.

## Mermaid

```text
sequenceDiagram
  actor C as Cashier
  participant R as :Register
  C->>+R: enterItem(id, qty)
  create participant S as :Sale
  R->>S: create()
  alt [item found]
    R->>S: makeLineItem(desc, qty)
  else [else]
    R-->>C: error
  end
  opt [coupon given]
    R->>S: applyCoupon(c)
  end
  par
    R-)S: log()
  and
    R-)S: audit()
  end
  critical [lock held]
    R->>S: endSale()
  end
  destroy S
  R-xS: close
  R-->>-C: total
```

`->>` synchronous, `-->>` reply, `-)` asynchronous, `+`/`-` on a message open and close activation. Mermaid supports `alt`, `opt`, `loop`, `break`, `par`, `critical` and `rect` highlighting; `seq`, `strict`, `ignore`, `consider`, `assert` and `neg` have no Mermaid syntax: use a `rect` with a `Note` naming the operator, and say so.

## Checklist

- Starts from one system operation whose name matches the system sequence diagram.
- Internal messages are present; every message exists as an operation on its receiver's class (`uml-class`).
- Fragments match their meaning; guards are concrete; replies appear where values return.
