---
name: uml-architecture
description: Draw architecture views - Package diagram for layers, Component diagram with provided and required interfaces, ports and connectors, Deployment diagram with nodes, artifacts and the quality attributes it addresses - and how to approximate them in Mermaid.
clients: coding
---

# Architecture views

Part of the `ooad` family; load `ooad` for where these sit (started in elaboration, refined in construction) and `architecture-tactics` for the qualities they serve.

## Package diagram

- Groups logically related elements (classes, use cases, other packages) and shows dependencies between groups.
- Suited to a layered architecture: presentation, application, domain, technical services, foundation, with dependencies pointing downward only. A dependency upward or a cycle between packages is a design smell to explain or remove.

## Component diagram

- Components are replaceable units with interfaces. A provided interface is drawn as a ball (lollipop), a required interface as a socket.
- Ports group the interfaces of a component; an assembly connector joins a required to a provided interface; a delegation connector forwards from a component's port to an inner part.
- Shows logical structure only, not physical placement. Note whether a connection is an in-process call between layers or network communication between servers.
- Design sequence diagrams at component level use the same components and interfaces.

## Deployment diagram

- The run-time configuration: hardware nodes (`<<device>>`), execution environments (`<<executionEnvironment>>` such as an application server or container runtime), artifacts (`<<artifact>>`) and their `<<deploy>>` relations, with communication paths and protocols.
- Use it to show how qualities are met: redundancy (`[2]` or more instances), multiple application servers behind a load balancer, separated databases, auto-scaled services.

## Mermaid

Mermaid has no package, component or deployment diagram types. Approximate them and say so:

- Package diagram: `flowchart TD` with a `subgraph` per package and dashed arrows (`-.->`) for dependencies.
- Component diagram: `flowchart LR` with components as nodes, interfaces as small round nodes (`I1((IPayment))`), and labelled edges for provided and required.
- Deployment diagram: nested `subgraph` blocks for nodes and execution environments, artifacts inside them, edges labelled with protocols.

```text
flowchart LR
  subgraph Web["<<device>> Web server x2"]
    App["<<artifact>> app.war"]
  end
  subgraph DB["<<device>> Database server"]
    Store[(orders)]
  end
  LB[Load balancer] -->|HTTPS| Web
  App -->|JDBC| Store
```

## Checklist

- Layers depend downward only; cycles are explained.
- Every required interface is connected to a provider.
- Deployment shows which quality each redundancy or separation serves.
