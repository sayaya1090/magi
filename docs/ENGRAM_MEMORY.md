# Engram consolidation, forgetting and daemon sharing

[한국어](ENGRAM_MEMORY.ko.md) · [Documentation map](README.md)

Status: **target design for implementation handoff, 2026-09-11.** The requirements are to reduce repeated knowledge and share skills/memories between daemons. These contracts are targets, not completed behavior. Git is an optional export/audit mechanism.

## 1. Implementation review

| Evidence | Current behavior | Gap |
|---|---|---|
| [engram](../plugins/engram/init.lua) | Writes ledger lessons, local skills and D13 copies separately. Includes token deduplication, row-number correction and default seven-day unused-skill archiving. | Correction, cancellation and forgetting do not cover every copy. |
| [Experience store](../internal/adapter/experience/git/store.go) | Content-based memory names, optional cosine deduplication (default 0.93), same-name skill replacement. | Similarity does not prove identity. Replacement loses the previous body; concurrent writes can lose observation counts. |
| [Layers](../internal/adapter/experience/layered/store.go) | Combined project/team/global retrieval capped at five memories and three skills; optional embedding search. | Needs a token budget, canonical-ID deduplication and Unicode search. Default git-store tokenization retains ASCII only and can miss Korean queries. |
| [exp-sync](../cmd/magi/expsync.go) | TLS replication between admitted machines, using set union for team memories/wiki revisions every five minutes; path/content checks. | Does not replicate skills or memory withdrawal. Teams are not currently tenant authorization boundaries. |
| [Wiki](../internal/adapter/experience/git/wiki.go) | Immutable revisions determine current pages; local usage affects retrieval retirement. | Reuse its storage/replication foundation, but chronological winner selection does not resolve semantic conflicts. |

**Priority defects:** engram analyzes guard/error/unverified turns and saves a returned skill without rechecking host outcome. Cancellation and seven-day archiving change only local files, leaving D13 skills retrievable. Ledger correction does not withdraw old D13 memories. Physically deleted shared memories can return through another machine's exp-sync. Establish these four paths as regression cases before migration.

Targeted experience-store/Lua tests matching Engram/Experience/Skill/Memory/Retrieve/Propose passed during this review. These findings come from code paths, not a multi-user deployment test. The existing README's “review queue” description also disagrees with immediate storage.

## 2. Responsibilities and authority

Engram **observes and extracts candidates**. The core experience service is the **single writer for identity, consolidation, state and retrieval**. The fleet door handles **authentication, authorization and replication**. Clients use the same inventory/correction/withdrawal API. Do not create a separate knowledge-maintenance system for each workspace daemon.

The installation's experience service coordinates short per-store write locks. Implement it as a module in existing Go processes, without requiring another installed service. Local recording/retrieval works without the fleet door; sharing waits. Do not hold storage locks during analysis, embedding or network calls.

Versioned knowledge objects and immutable operations are authoritative. SESSION_SUMMARY.md, SKILL.md and wiki pages become generated **views**. Detect human edits by file hash and import them as revision candidates; generators must not overwrite them. Preserve unmanaged file regions. Do not independently recreate one observation in both local and shared stores.

```mermaid
flowchart LR
  O[Observation] --> C[Candidate]
  C --> G[Evidence and scope gate]
  G --> P[Merge plan]
  P --> V[Heads and permission check]
  V --> L[Immutable operation log]
  L --> R[Canonical retrieval and views]
  L <--> S[TLS daemon replication]
  U[Correction or withdrawal] --> V
```

## 3. Data contract

| Field | Meaning |
|---|---|
| namespace_id, object_id | Sharing-space UUID and knowledge UUID, stable across renames, paraphrases and file moves. |
| kind | lesson, fact or procedure. Wiki pages may assemble related objects. |
| revision_id, parents[] | SHA-256 of the canonical full payload and parent revisions. Hash state, scope and evidence as well as body. |
| claim, applies_when, excludes, procedure, verification | Assertion, applicability, exclusions, steps and checks. OS/product/version/project constraints participate in consolidation. |
| evidence[] | observation_id, source turn/session reference, host outcome, observation time and contributor. Do not share raw conversations or secrets by default. |
| state, pinned, authored_by, visibility | active/conflicted/superseded/withdrawn, retention pin, human/automatic authorship and explicit read/write policy. Local archiving is separate. |
| aliases[], supersedes[], derived_from[] | Canonical duplicate links, corrections and provenance across scopes. Copy with provenance rather than merging across scopes. |
| operation_id, actor_id, expected_heads[], reason | Retry deduplication, authenticated contributor, concurrency check and change reason. |

Identical content may have additional independent evidence. Replaying an observation_id must not increase usage/success counts. Track retrieval, loading, application and verified success separately. A successful turn that loaded a skill does not by itself prove the skill caused success.

Operations are propose, reinforce, merge, correct, withdraw and restore. Every change records actor, reason and parents. Local hiding, archiving and pins are not automatically shared. Automatic widening of visibility is prohibited.

Proposed host ports are `Propose(observation)`, `PlanMerge(ids, heads)`, `Apply(plan, expectedHeadsById)`, `Withdraw(id, heads, reason)` and `Recall(query, principal, budget)`. Return operation_id, current heads, local completion and propagation state. Distinguish stale-head, permission-denied, evidence-rejected and pending-dependencies instead of reporting success. These are proposed interfaces.

### 3.1 Storage layout and file responsibilities

**Use a local JSON operation log, SQLite indexes and Markdown views.** Start without an external vector database or Git server. Store all authoritative namespaces beneath the platform-resolved existing `<config>` directory. Workspaces contain namespace bindings and explicitly exported views, reducing accidental Git publication of private generated knowledge.

```text
<config>/knowledge/v2/
  namespaces/<namespace-uuid>/
    manifest.json
    operations/<sha256-prefix>/<operation-sha256>.json
    blobs/<sha256-prefix>/<blob-sha256>
    snapshots/<snapshot-sha256>.json
    quarantine/
    incoming/
    views/objects/<object-uuid>.md
  local/
    state.sqlite
    index.sqlite
    locks/<namespace-uuid>.lock
<workspace>/.magi/knowledge.json
<workspace>/.claude/skills/<slug>/SKILL.md
<workspace>/SESSION_SUMMARY.md
```

| Location | Contents and replication |
|---|---|
| manifest.json | Rebuildable namespace/schema/current-policy-head view. Signed policy operations are authoritative; editing this file cannot grant access. |
| operations/ | Immutable JSON change transactions. Authoritative and replicated to authorized peers, including corrections and withdrawals. |
| blobs/ | Hashed chunks for bodies/attachments too large for a 64 KiB operation. Replicate only referenced chunks with the operation's ACL. No automatic raw-conversation attachments. |
| snapshots/ | Verifiable heads/state checkpoints over a specified operation set. Do not replace/delete the log before retention requirements are met. |
| quarantine/, incoming/ | Unverified files and incomplete batches. Exclude from retrieval, views and forwarding. |
| views/ | Object-ID Markdown for people; rebuildable from the log. |
| local/state.sqlite | Local pins, hiding, actual-use timestamps, import manifest and sync acknowledgments. Not replicated; requires local backup and must not be discarded as a cache. |
| local/index.sqlite | Current objects, search tokens/n-grams and embedding cache. Rebuildable and not replicated. |
| .magi/knowledge.json | Workspace→project namespace UUID and connected team namespace UUIDs. Contains no credentials; received bindings do not authorize access. |

Project/team/global become namespace scopes and bindings rather than authoritative directory tiers. Receiving a project binding through Git still requires namespace admission. Each OS user has their own config and local databases. Other users/devices communicate through daemon APIs rather than opening shared database files.

SQLite is **local-disk only**. Do not share a WAL database over a network filesystem. Following the [SQLite WAL documentation](https://www.sqlite.org/wal.html), network workspaces keep logs/databases in local config and only export views into the workspace. exp-sync does not replicate database files.

Work package A selects and pins a Go SQLite driver after verifying FTS5, Windows/macOS/Linux packaging and build compatibility. An untested driver choice is not a completed dependency decision.

### 3.2 Concrete JSON and local database shape

This is a **shape example**, with placeholder UUIDs, hashes and signatures. A verified outcome is accepted only when supplied and validated by the host.

```json
{
  "schema": "magi.knowledge.operation.v2",
  "operation_id": "<UUID>",
  "namespace_id": "<namespace-UUID>",
  "actor_id": "<registered-key-ID>",
  "type": "propose",
  "parents": [],
  "expected_heads": {},
  "changes": [
    {
      "object_id": "<object-UUID>",
      "kind": "procedure",
      "state": "active",
      "claim": "Wait for a feature probe with a bounded deadline before reading its output.",
      "applies_when": {
        "os": [
          "windows",
          "macos",
          "linux"
        ],
        "component": "core-feature-probe",
        "version_range": null
      },
      "excludes": [
        "interactive child process"
      ],
      "procedure": [
        "Start the probe.",
        "Bound its completion time.",
        "Read the completed output."
      ],
      "verification": [
        "A silent probe returns within the deadline."
      ],
      "evidence": [
        {
          "observation_id": "<stable-observation-UUID>",
          "outcome": "verified",
          "source_ref": "<opaque-source-ID>",
          "observed_at": "2026-09-11T00:00:00Z"
        }
      ],
      "visibility": {
        "policy_id": "<namespace-policy-ID>",
        "policy_revision": "<policy-hash>"
      },
      "authored_by": "automatic",
      "derived_from": []
    }
  ],
  "reason": "verified observation",
  "signature": {
    "algorithm": "<negotiated-algorithm>",
    "key_id": "<registered-key-ID>",
    "value": "<signature>"
  }
}
```

The operation filename hash is SHA-256 of canonical full-payload bytes excluding `signature`; sign those same bytes. JSON is UTF-8, rejects duplicate keys, sorts object keys and allows integers only; schema-defined strings represent large numbers. Sort set fields such as parents/target IDs, but preserve meaningful order such as procedure steps. Do not normalize string content during hashing. Share golden serialization-byte fixtures across Go and every producer.

An object revision_id hashes its changes entry and that object's parent revisions. operation_id is a UUID retained on retries, distinct from the file hash. Reinforce operations add new observations instead of copying all historical evidence into every revision. Views show aggregates and source pointers.

The initial logical `index.sqlite` schema follows. Every key/query includes namespace_id because the database contains multiple namespaces.

| Table | Main columns and constraints |
|---|---|
| objects | (namespace_id, object_id) PK; heads_digest, kind, state, policy_id, current_payload, exact_fingerprint |
| aliases | (namespace_id, alias_id) PK; canonical_id. Reject cycles on receipt/application. |
| evidence | (namespace_id, object_id, observation_id) UNIQUE; source_ref, outcome |
| search_text | FTS5 over claim/conditions/procedure/verification. |
| search_grams | namespace_id, object_id, field, gram, count; short Korean queries and identifier-fragment candidate search. |
| embeddings | (namespace_id, object_id, revision_id, model_digest, dimensions, input_digest) PK; float32 vector |
| indexed_operations | (namespace_id, operation_hash) PK; replay missing operations after restart. |

Distinguish commit acknowledgment from indexing completion. Read-after-write waits for that operation's indexing or uses a log overlay. Apply ACL/withdrawal changes immediately in final state/permission checks even before indexing catches up.

## 4. Consolidation rules

1. Narrow by authorized scope/ACL and retain kind/applicability for comparison. Matching team names do not establish namespace identity.
2. Find at most 20 candidates using Unicode normalization/token search and optional embeddings. Korean/English lexical retrieval and exact deduplication must work without an embedding service.
3. Repeated observation_id is a no-op. Exact semantic-field matches reinforce the canonical object. Exclude source/time from content equality while preserving them as evidence.
4. Similar records receive an analyzer proposal: duplicate/refinement/contradiction/related/independent, with supporting spans. Never delete or overwrite based solely on a score such as 0.93.
5. Automatic consolidation is limited to automatically authored objects in the same namespace/ACL whose claims, conditions, commands and verification steps are identical; only evidence is added. Summarizing away unique conditions, human-edited/pinned objects and differing ACLs require review.
6. Corrections name an existing ID and expected_heads. “A fails on Windows” and “A succeeds on Linux” are conditional knowledge, not duplicates. Contradictions under the same conditions become conflicted, preserving both sources. Neither similarity nor a recent timestamp decides truth.
7. Recheck heads immediately before applying. Recompute against changes, at most three attempts, then defer. Plans expire after ten minutes; manual approval must also show a current diff.

One merge operation contains the resulting revision and the originals' superseded links. Retrieval returns each canonical object once. The first release proposes semantic-summary merges without automatically applying them. Expand automation only after evaluating human-labeled cases.

### 4.1 Similarity pipeline

**Use exact fingerprints, BM25/character n-gram/embedding candidate retrieval, structural comparison, then model-proposed relationships.** Only exact equivalence enables automatic consolidation. Embeddings are optional candidate discovery, never sufficient reason to discard a memory.

| Stage | Method | Initial parameters/result |
|---|---|---|
| Normalize | NFC, whitespace cleanup and English case-folding for natural-language search; retain originals. Preserve code, commands, paths, versions, numbers and negation. | Version normalization rules. Do not alter code through NFKC or remove negation as stopwords. |
| Exact comparison | SHA-256 over conservatively normalized JSON containing kind, claim, applicability/exclusions, procedure, verification and visibility. Do not case-fold natural language or substitute synonyms for this fingerprint. | Within the same namespace/ACL, matching fingerprints plus semantic-field equality reinforce evidence only. |
| Lexical candidates | FTS5 unicode61 BM25 over claim/conditions/procedure/verification, initially weighted 5/3/2/2. | Top 40; preserve exact identifier/error-code matches separately. |
| Korean/substring candidates | Unicode character 2-gram/3-gram inverted index, weighted Jaccard; tokenize numbers and identifiers separately too. | Top 40; support Korean suffix variations and two-character queries without an initial morphological-analyzer dependency. |
| Semantic candidates | Existing Embedder with a fixed conditions/claim/procedure/verification template; cosine over normalized float32 vectors. | Top 40 from the same model/dimensions/template. Fall back to lexical retrieval on failure. |
| Rank fusion | RRF: add `Σ 1/(60 + rank)`, starting ranks at one. | Do not add raw BM25/Jaccard/cosine scores. Send the merged top 20 to structural comparison. |
| Relationship | Analyze condition/command/verification diffs and source spans. | equivalent/refinement/contradiction/related/independent/uncertain. Missing conditions or evidence mean uncertain. |

[Official FTS5 documentation](https://www.sqlite.org/fts5.html) establishes unicode61 tokenization and BM25 support. Do not assume unicode61 alone handles Korean suffixes or two-character queries; retain the n-gram path. Weights, candidate counts and RRF constants above are initial project design values, not measured optima.

Applicability helps classify relationships. Do not exclude every relevant counterexample merely because its OS differs. **Filter authorization before search**; use kind/conditions to decide exact-merge eligibility and candidate relationships. Even within a namespace, do not send unauthorized bodies/embeddings to the relationship analyzer.

Weighted Jaccard is `Σ min(wA,wB) / Σ max(wA,wB)` over gram weights. Weight rare grams through IDF. Handle one-character queries through an explicitly bounded short-query path, not an unlimited scan. Preserve command-flag, numeric and prohibition/permission differences in structural diffs despite similar n-grams.

Initially use **exact cosine scans** over active/cold vectors allowed by namespace/ACL. Raw storage for 10,000 768-dimensional float32 vectors is about 30.7 MB, excluding metadata. Introduce ANN only if the actual corpus misses M09, measuring candidate recall against exact search. Cache by revision/model/template hashes instead of re-embedding all content. Dimension 768 is a sizing example, not a model contract.

### 4.2 Final decisions and tuning

Return `relation, target_ids, compared_heads, matched_spans, unique_conditions, contradictory_spans, proposed_changes, reason`. Model confidence is diagnostic and grants no automatic-application authority. The earlier §4 label duplicate means equivalent in this contract.

| Observation | Decision |
|---|---|
| Same content, conditions and commands in different turns | Reinforce with independent observations, without increasing canonical count. |
| Same purpose, more detailed commands | Refinement proposal; retain existing content pending approval of the unique-step diff. |
| “Finishes within five seconds” / “Five seconds is insufficient” | Contradiction under the same environment; uncertain if environment information is missing. |
| Same lesson in Korean and English | Discover through embeddings or multilingual candidates, then propose equivalence. No automatic semantic merge in the first release. |
| Same tool, different problem | Related; retain separate canonical objects. |

Before release, label at least 200 pairs: 40 each for exact duplicates, paraphrases, counterexamples, added conditions and independent cases. Include Korean/English, negation, OS/version/numeric/command differences. Split train/dev/test by task/source so paraphrases of one incident do not leak across splits. Initial candidate recall@20 target is 95%; separately report per-class relationship precision/recall and unique-condition loss.

Enable automatic application only for exact comparison, requiring zero false merges on its counterexample fixtures. Passing 200 pairs does not guarantee future correctness. Reevaluate a fixed holdout after model/template/normalization/weight changes. Improve candidate counts or lexical processing when recall is low; do not loosen semantic decisions simply to eliminate more duplicates at the cost of lost knowledge.

### Example

Twelve paraphrases of “install dependencies before testing” become one candidate group. Compare version-specific commands and failure conditions. Equivalent entries yield one canonical object and twelve independent observations. “Skip installation offline” adds a condition and must not be dropped. “Installation damaged project files” is counterevidence, not another success count.

## 5. Forgetting and recall

**Forgetting first means removal from automatic recall.** Distinguish it from withdrawing incorrect content and permanently deleting stored data.

| Object | Default policy: initial values to validate |
|---|---|
| Duplicates/superseded revisions | Link to the canonical revision and immediately exclude from automatic recall. History and restoration remain available. |
| Automatically authored, unused objects | Cold after 30 days without actual application; local archive candidates after 90 days. Replaces seven-day automatic skill moves. |
| Human edits, pins, active-task references | Exempt from automatic archiving; may receive a review reminder. |
| Version-expired facts | Mark expired and exclude by default; revalidate when needed. Missing dates do not imply permanent truth. |
| Conflicted/withdrawn | Conflicts return both sources with a warning. Withdrawn objects are excluded except from explicit audit queries. |
| Permanent deletion | Disabled by default. Requires an explicit namespace-admin action, replication acknowledgments and retention policy. |

Cold/archive are local usage decisions and must not withdraw knowledge another user needs. A newly joined device gets a 30-day observation window instead of immediately archiving old shared objects. Explicit restoration or verified actual use returns local state to active. Search exposure alone does not refresh age.

Default automatic recall returns at most five canonical memories and three skills within 3,000 tokens total. Deduplicate local/team copies and generated views through provenance. Oversized objects return a summary pointer retaining conditions/source, with explicit full retrieval. Archives require explicit queries; cold objects are a fallback when active results are insufficient. Do not summarize across incompatible ACLs.

## 6. Consistent cancellation, archiving and deletion

Save notifications identify object_id, revision_id and operation_id. Cancellation is a compensating operation. If another contributor has edited meanwhile, propose a revert diff instead of restoring an old file. Engram's short N cancellation window uses this same API.

After sharing, cancellation propagates withdrawal to the original, managed views, retrieval indexes and authorized replicas. Independently copied objects in another scope receive a source-withdrawal warning; do not delete them without authority. Tombstones suppress retransmitted old revisions. Restoration requires an authorized explicit operation naming the current tombstone as parent.

Do not promise physical erasure of peer copies or external backups. Show local completion, pending propagation and acknowledged-device count separately. Retain deletion tombstones until all admitted replicas acknowledge or unresponsive devices are revoked. Rejoining revoked devices must bootstrap from a current snapshot. Git exports are outside deletion-propagation guarantees.

## 7. Daemon sharing

Reuse the TLS fleet door and device identity with an experience-v2 capability. Notify on changes, transfer batches and retain five-minute anti-entropy for repair. Local writes finish offline; sharing remains pending.

**Multiple users:** Do not reuse “my admitted machine may access every team” for another user's invitation. A namespace administrator grants read/contribute/curate/admin to user/device keys. Show invitation scope on acceptance and verify registered keys and operation signatures. An actor string supplied by the caller is not identity. Contributions stay within grants; semantic edits to existing knowledge and shared withdrawal require curate or above. Revoke new sync/write access when keys are revoked; do not promise erasure of existing copies.

Sign the complete operation and validate SHA-256, schema, size, parents and ACL. Quarantine incoming content before retrieval; unverified objects never enter model context. External-memory instructions remain lower-trust data than user/system instructions. Before sharing, scan for secrets and minimize provenance; suspicious items remain private locally. Detection is not guaranteed complete.

Replicate immutable operation sets. Exchange heads and missing operation_ids; retries are idempotent. Derive current state from the DAG so equal sets produce equal results regardless of delivery order. Independent evidence combines, concurrent semantic edits conflict, and merges wait for parents. A fast clock does not win. Quarantine cyclic aliases/missing parents and request dependencies.

For concurrent withdrawal and editing, suppress automatic recall while retaining the edit in conflict history. Restoration resolves only tombstones it names; an unseen withdrawal keeps the object inactive. Automatic maintenance uses narrowly delegated namespace permissions, and user cancellation also checks authorization and current heads.

Initial limits: 4 MiB requests, 2 MiB responses, 64 KiB operations and 400 operations per batch. Chunk large bodies by hash and expose them only after every chunk verifies. Use per-namespace cursors, backpressure and retry jitter. Never send unreadable content to an embedding service.

Do not export v2-managed objects to v1 peers. Bidirectional sharing with tombstone-unaware peers resurrects deleted content. Show upgrade-required state; manual export is separate. The default acceptance path is create→retrieve→correct→withdraw on two machines with no Git.

## 8. Storage, concurrency and recovery

Under a namespace lock, check expected_heads, write a temporary operation file, fsync, atomically rename and ensure directory durability. A multi-object merge still has one operation file as its visibility boundary. Reuse platform-correct atomic-file support and test Windows locking/replacement behavior on the actual OS.

Build indexes and view files after commit; both are rebuildable from authoritative operations. Embedding caches include model, dimensions and content hash. State/ACL changes invalidate retrieval caches, and final return rechecks permission and state.

The analyzer cannot verify a turn. Automatic procedure promotion requires the host's verified outcome and traceable evidence. guard/error/unverified/ungated must not become active procedures merely because the model says success. Record manual authorship/approval separately. Preserve observations on analysis failure and retry; do not label failure as “no duplicates” or “merge completed.”

Initial maintenance limits are one worker per namespace per installation, batches of 100 objects, at most 20 analyzed pairs and 60 seconds per run. Schedule daily by default; queues must not block conversation work. Resume by operation_id without learning the same observation twice. Raw-session retention and learned-object retention are separate settings.

## 9. Migration, ownership and acceptance

Start with dry-run. Snapshot source hashes/provenance from ledger, skills, D13 and wiki, recording path→object_id→revision in an import manifest. Allocate IDs on first import and share that manifest rather than issuing fresh IDs on each device. Reconcile independently imported objects as exact-duplicate candidates. Preserve human edits; uncertain provenance/verification is unknown.

Unify readers before ending engram's multiple writes. Use the manifest to prevent generated legacy files from being imported as new knowledge. Block legacy writers for v2-enabled namespaces. Rollback preserves v2 read-only and uses explicit export; automatic downgrade that discards tombstones is prohibited.

| Work | Owned modules and deliverables | Depends on |
|---|---|---|
| A | Ports/experience service: object/operation schema, atomic storage, heads/ACL/outcome gate | None |
| B | Engram/Lua bridge: observation IDs, single propose, correction/cancellation/forgetting API | A |
| C | Retrieval/consolidation: Unicode, candidate decisions, provenance deduplication, budgets, dry-run report | A |
| D | Fleet door/identity: namespace grants, v2 operations/tombstones, offline repair | A |
| E | Console: merge diff, evidence, scope, conflicts, restore, propagation status, human-edit import | A–D |
| F | Migration/live QA: legacy data, multiple processes, Windows/macOS/Linux acceptance | A–E |

| Case | Required result |
|---|---|
| M01 Repeated learning | Replaying one observation 100 times creates one evidence entry. Twenty independent equivalent observations yield one canonical object and twenty evidence entries. |
| M02 Conditions/conflicts | Korean/English synonyms, negation and OS/version differences do not silently lose distinct claims. |
| M03 False success | Injecting skill JSON into guard/error analysis cannot activate a procedure. |
| M04 Cancellation/correction | After save→D13 recall→cancel/correct, managed views and subsequent recall exclude the old revision. |
| M05 Offline | A withdraws while B edits offline; reconnect, reordered and duplicated delivery cannot resurrect the object, while conflict evidence survives. |
| M06 Forgetting | Loading alone does not extend lifetime. Pins, human edits and another user's activity are not automatically withdrawn. |
| M07 Races/crashes | Concurrent merge/cancel and termination at every commit boundary recover to one canonical state or an explicit conflict. |
| M08 Authorization | Reject wrong namespace, forged actor, revoked key, ACL escalation, malicious paths and modified hashes. Old peers cannot bypass tombstones. |
| M09 Scale | Initial target: warm local candidate search p95 200 ms for 10,000 objects. Report remote embedding time separately and enforce the 3,000-token recall budget. |
| M10 Migration | Repeated imports, regenerated views, human edits and rollback preserve content/provenance without relearning duplicates. |

Release reports include canonical/duplicate/conflict counts, incorrect automatic merges, post-withdrawal reappearance, scope leaks, retrieval misses and propagation latency. Fewer sentences alone do not establish success. M01–M08 and M10 are mandatory; report hardware, model and corpus for M09 performance.
