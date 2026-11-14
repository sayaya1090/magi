---
name: uml-domain-model
description: Build a Domain Model (analysis) - conceptual classes with attributes and associations but no operations, found by noun-phrase identification, with multiplicity, and drawn as a Mermaid classDiagram.
clients: coding
---

# Domain Model (analysis)

Part of the `ooad` family; load `ooad` for where this artifact sits and `uml-class` for the design class diagram it later informs.

## What it is

- A visual dictionary of **conceptual classes** (things in the real situation), their attributes and their associations with multiplicity.
- Drawn as a class diagram with **no operations**, and not of software objects: no methods, no databases, windows or controllers. `Sale { dateTime }` is conceptual; `Sale { date, time, print() }` is a software class and does not belong here.
- There is no single correct domain model: usefulness matters more than correctness. It is a tool for understanding and communication.

## Why

- Learning the domain and its vocabulary.
- Naming the domain-layer software classes after it, which keeps the representational gap between the problem and the code small.

## How

1. Read the use-case text and list nouns and noun phrases as candidate classes and attributes (adjectives suggest attribute values; verbs become operations later, in design).
2. Keep a candidate as a class if it has its own identity or behaviour in the domain; simple data values (dates, amounts, names) become attributes.
3. Add associations that must be remembered ("Sale is paid by Payment", "Register records Sale"), each with a name, reading direction and multiplicity at both ends.
4. Add attributes the requirements need; do not add foreign keys or identifiers for the sake of storage.
5. Mark conceptual classes with a stereotype if the team uses one (for example `<<business object>>`).

Associations to look for: physical or logical part of, contained in, recorder of, described by, member of, uses or manages, owned by, communicates with, related to a transaction.

## Mermaid

```text
classDiagram
  class Sale {
    dateTime
    total
  }
  class SalesLineItem {
    quantity
  }
  class ProductDescription {
    description
    price
    itemID
  }
  class Register
  class Payment {
    amount
  }
  Register "1" -- "1..*" Sale : records
  Sale "1" -- "1..*" SalesLineItem : contains
  SalesLineItem "0..*" -- "1" ProductDescription : described by
  Sale "1" -- "1" Payment : paid by
```

Use plain lines (`--`) without arrowheads: navigability is a design decision. Mermaid needs at least one member or an empty body to show a class box.

## Checklist

- No operations, no visibility markers, no implementation elements.
- Every association has a name and multiplicity at both ends.
- Class names come from the use-case vocabulary and the glossary.
