---
name: architecture-tactics
description: Meet quality attribute requirements with architecture tactics - quality attribute scenarios, then tactics for Availability (Redundancy, heartbeat, failover), performance, modifiability, security, testability, usability and scalability, and how to record them in design and deployment views.
clients: coding
---

# Architecture tactics

Part of the `ooad` family. Non-functional requirements (the supplementary specification) are quality attributes; architecture tactics are design decisions that influence one quality attribute response. A pattern bundles several tactics. Every tactic trades one quality against others: name the cost.

## Quality attribute scenarios

State each requirement as a scenario before choosing tactics:

| Part | Example (availability) |
|---|---|
| Source | Internal hardware |
| Stimulus | Application server crashes |
| Artifact | Order service |
| Environment | Normal operation, peak hours |
| Response | Requests fail over to another instance |
| Response measure | No more than 5 s of unavailability; no lost orders |

Number the scenarios (QA-01, QA-02, ...) and trace each tactic and pattern to the scenario it serves.

## Tactics by quality attribute

### Availability

- Detect faults: heartbeat, ping/echo, monitor, timestamp, sanity checking, exception detection.
- Recover: Redundancy (active, passive, spare), failover, retry with backoff, rollback, degradation, reconfiguration.
- Reintroduce: shadow operation, state resynchronization, checkpoint and rollback.
- Prevent: removal from service, transactions, predictive model.
- Cost: extra hardware and synchronization; consistency during failover.

### Performance

- Control resource demand: manage sampling and event rate, limit event response, prioritize events, reduce overhead, bound execution time, increase efficiency.
- Manage resources: increase resources, introduce concurrency, maintain multiple copies of computations or data (caching, replicas), bound queue sizes, schedule resources.
- Cost: complexity, consistency of replicas and caches.

### Modifiability

- Reduce module size (split), increase cohesion, reduce coupling (encapsulate, use an intermediary, restrict dependencies, abstract common services), defer binding (configuration, plug-ins, dependency injection, publish-subscribe).
- Cost: indirection and run-time overhead.

### Security

- Detect attacks (intrusion detection, verify message integrity), resist attacks (authenticate and authorize actors, limit access and exposure, encrypt data, separate entities), react (revoke access, lock accounts, inform actors), recover (audit trail, restore).
- Cost: latency and usability.

### Testability

- Control and observe state (specialized interfaces, record and playback, sandbox, executable assertions), limit complexity (limit structural complexity, limit nondeterminism).

### Usability

- Support user initiative (cancel, undo, pause and resume, aggregate), support system initiative (maintain task, user and system models).

### Scalability and deployability

- Horizontal scaling behind a load balancer, stateless services, partitioning, asynchronous messaging, auto-scaling; scripted deployment, rolling and blue-green releases, feature toggles.

## Recording tactics in the design

- In the class diagram, name the pattern or tactic in a note with its scenario ("Observer - QA-03 modifiability").
- In component and deployment diagrams (`uml-architecture`), show redundancy (`[2]`), load balancers, replicas and separated stores, and label which scenario each serves.
- In the architecture document, list for each scenario the tactics chosen, the alternatives rejected and the cost accepted.
