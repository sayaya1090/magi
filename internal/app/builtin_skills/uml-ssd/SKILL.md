---
name: uml-ssd
description: Write a System Sequence Diagram (analysis) - actors and the system as one black box, system operations named by intention, how main, alternative and exceptional use-case courses map onto messages and frames, and the Mermaid sequenceDiagram form.
clients: coding
---

# System Sequence Diagram (analysis)

Part of the `ooad` family; load `ooad` for where this artifact sits.

## What it shows

- The events external actors generate, their order and inter-system events, for **one scenario of one use case**.
- Participants: external actors (people and external systems) and the system as a single **black box** (`:System`). No internal objects, components or databases: those belong to design sequence diagrams (`uml-sequence`).
- UML has no separate "system sequence diagram": it is ordinary sequence notation used from the black-box viewpoint.

## System operations

- The messages into the system are system operations. Across all use cases they form the system interface, and they are the input to object design: each design sequence diagram realizes one system operation.
- Name them at the level of intention, not of devices: `enterItem(itemID, quantity)`, not `scan(itemID)`; `endSale()`, not `pressEnter()`.
- Return messages carry what the actor learns (`description, total`); keep them in domain words.
- Keep message names identical to the use-case steps and to the first message of the design sequence that realizes each operation.

## From use-case courses to messages

| Use-case course | In the diagram |
|---|---|
| Main success scenario | Messages on the main line, outside any frame |
| Repetition that returns to the main flow (more items, retry) | `loop` |
| Optional step | `opt` |
| Exceptional course that ends the scenario early | `break` |
| Real alternative paths | `alt` with disjoint guards |

- Do not wrap the main flow in an `alt` with a "success" guard: that makes the normal scenario conditional.
- A time trigger ("when the reservation time arrives") is a step of the main flow, not a guard.
- Keep the use-case text at the same black-box level; mention internal processing only when it explains an observable result.

## Mermaid

```text
sequenceDiagram
  actor C as Cashier
  participant Sys as :System
  C->>Sys: makeNewSale()
  loop [more items]
    C->>Sys: enterItem(itemID, quantity)
    Sys-->>C: description, running total
  end
  C->>Sys: endSale()
  Sys-->>C: total with taxes
  C->>Sys: makePayment(amount)
  break [payment declined]
    Sys-->>C: declined
  end
  Sys-->>C: change due, receipt
```

Mermaid draws a guard as frame label text; write it in brackets so it reads as a guard.

## Checklist

- One scenario of one use case; only actors and `:System`.
- Every system operation is named by intention and appears in the traceability table.
- Alternative and exceptional courses of the use case are present, each in the frame that matches its meaning.
