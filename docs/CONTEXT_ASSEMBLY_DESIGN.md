# Context assembly review and refactoring design

Status: stages 1–4 implemented and automatically validated. Reviewed at `d6cfed8f` (2026-09-28). [한국어](CONTEXT_ASSEMBLY_DESIGN.ko.md)

## 1. Decision and scope

The repository already has `port.ContextProvider` for retrieved context. Introducing an abstract base class for tools, skills, and memory would duplicate existing infrastructure. Use Go interfaces and internal value types to describe the fragments being assembled, while retaining existing extension points.

This design covers application-level model request assembly. Tool execution and authorization, memory stores, skill file formats, and adapter message roles remain unchanged. It does not replace the plugin API or introduce a general framework.

First separate the structure without changing behavior. Then add guidance based on actual request tools and fragment diagnostics. Token optimization, automatic deduplication, and trust-based role changes remain separate work.

## 2. Existing structure

| Component | Evidence | Current contract |
|---|---|---|
| Retrieved context extension | `internal/port/port.go`: ContextProvider, ContextQuery, ContextChunk | Provide(ctx, query), returning Source/Text |
| Registration and collection | `internal/app/app_plugin_api.go`: RegisterContextProvider, gatherContext | Registration order; five-second context timeout per provider; skip errors; output limit |
| Lua integration | `internal/adapter/plugin/lua/context_provider.go`, `bridge.go` | Adapts the existing port |
| System body | `internal/app/prompt.go`: systemFor | Project instructions, agent instructions, environment, output guide |
| Language and skill hints | `internal/app/loop.go`: buildStepSystem, skillBlockFor | Language prepended; skill list appended and frozen per session |
| Project instructions | `internal/app/memory.go`: projectMemory | Global AGENTS.md, project AGENTS.md, .magi/AGENTS.md; file-state cache |
| Skill loading | `internal/app/skills.go`: loadSkills | Existing name deduplication, ordering, and description extraction; body loaded on demand |
| Prefix freezing | `internal/app/prompt_frozen.go` | System per turn; tools per session and agent key |
| Variable context | `internal/app/prompt.go`: volatileContext | Plan, recall hints, retrieval, editor buffer; run state; elapsed time |
| Final request | `internal/app/loop.go`: buildStepRequest | Event reconstruction, compaction, ephemeral trailing message, tool array |
| Usage reporting | `internal/app/context_state.go`, prompt_frozen.go | System/tools/talk/calls/results estimates; no provider breakdown |

Assembly entry points and caching already exist. The missing contract concerns fragment identity, provenance, placement, and dependencies on tools actually advertised in the request.

## 3. Findings that constrain the design

1. ContextProvider is a retrieval interface. Do not force project instructions, tool schemas, and event history through it.
2. The 8000 limit in gatherContext measures Go string **bytes**, despite the character wording in its comment. Slicing can split UTF-8. Correct this separately from structural extraction.
3. The five-second timeout requires provider cooperation with context cancellation. It does not forcibly stop an uncooperative provider. Sequential providers can accumulate latency.
4. volatileContext describes retrieval as top-level-only, but no such branch is visible in that function. Capture actual call-path behavior and correct the comment; do not introduce a filter during extraction.
5. Some recall hints check agent.allows, not the frozen request catalog. Skill arrivals use another path. Allowlisting and actual registration must be distinguished.
6. Office default instructions and bundled skills feed these paths. SeedInstructions preserves existing nonempty instructions. Updating source does not remove old instructions from installed workspaces.

## 4. Design decisions

### 4.1 Internal application value type

Add an unexported type in `internal/app/context_fragment.go`. Do not change the public port in the first stage.

```go
type contextFragment struct {
    id       string // stable identity assigned by the application
    source   string // diagnostic provenance, not automatically sent to the model
    text     string // includes existing headings and whitespace
    lane     contextLane
    requires []string // actual tool names required by generated guidance
}
```

contextLane has four values: turnSystem, volatileStable, volatileRun, and volatileClock. Existing skillBlockFor owns session-frozen skill hints; its result enters turnSystem. The assembler does not create persisted arrival events.

Application-generated IDs include system/project-memory, system/agent, tail/plan, and tail/retrieved/<registration-index>/<chunk-index>. A plugin Source label is neither a unique ID nor an authorization claim. Do not deduplicate different chunks with the same Source.

### 4.2 Pure assembly and existing providers

Add pure ordered joining functions in `internal/app/context_assembly.go`. They perform no file access, retrieval, event writes, tool execution, or cache mutation. Initially retain caller order without priority sorting.

Existing systemFor and volatileContext collect data and pass fragments to the assembler. Do not create a class for every short source. Adapt existing retrieved ContextChunks to internal fragments and preserve Provide and Lua registration APIs.

```text
Existing readers/retrieval -> contextFragment[] -> pure assembly -> frozen/variable paths
Existing sessionToolSpecs ---------------------------------------> ChatRequest.Tools
Existing reconstruction/compaction ------------------------------> ChatRequest.Messages
```

Tools remain structured specs. Availability checks use exact Name membership in the same []port.ToolSpec sent with the request, not another registry query or an enabled setting.

### 4.3 Preserve lifetimes and side effects

- Keep stepSystemFor and sessionToolSpecs as the only freeze entry points.
- Usage inspection must not trigger freezing. Retain tests preventing an early UI reading from freezing tools before attachment.
- Project instruction, language, or skill changes must not alter the system prefix within a turn.
- Preserve the existing session/agent tool key and lifetime. Newly registered tools still require a subsequent session under the current contract.
- Keep arrival events and memory-arrival queue consumption in buildStepRequest. Reassembly after compaction must not duplicate events.
- Variable context remains the existing trailing user-role message, absent from the event log. Do not combine this refactor with role or trust-policy changes.

### 4.4 Match guidance to actual tools

After structural extraction, obtain sessionToolSpecs once in buildStepRequest and pass that same snapshot to generated guidance and the final request. Moving this call earlier requires dedicated initial attachment and freeze timing tests. If the ordering is unsafe, pause this stage and investigate connection ordering instead of adding a second supposedly read-only catalog.

Generate skill-loading guidance only when skill is present, memory hints only when recall_memory is present, and compacted-history hints only when recall_context is present. Apply the same rule to arrival notices. Record omission reasons diagnostically. Do not regex-delete user instructions or retrieved text.

Completion guidance belongs to generators that know the exact tool name and contract. Do not request council when it is absent. Mention land only when it exists; otherwise describe ordinary result reporting. Do not identify arbitrary MCP tools by a matching suffix.

The assembler cannot resolve every natural-language contradiction. Existing Office user instructions need a separate migration or reviewable update notice. Automatic overwriting is out of scope.

### 4.5 Limits, diagnostics, and provenance

Preserve existing path-specific limits and ordering initially. A later fix must truncate at the last complete UTF-8 code point while preserving the 8000-byte cap. Any truncation marker must fit within that cap.

Diagnostics contain ID, lane, byte count, included/skipped/truncated state, and reasons such as missing_tool. Do not log bodies, absolute file paths, or search queries by default. Preserve the PromptShape event schema and UI totals. Provenance metadata does not grant system authority or trust.

## 5. Sequential implementation instructions

| Stage | Files and responsibility | Acceptance |
|---|---|---|
| 1. Capture baseline | Application tests only | Capture frozen/variable bytes, tool specs, message roles/order with fixed time and retrieval inputs |
| 2. Extract assembly | context_fragment.go, context_assembly.go, prompt.go, optionally app_plugin_api.go | Byte-identical output; existing caches, ports, events, compaction unchanged |
| 3. Gate generated guidance | loop.go, prompt.go, guidance tests | One frozen tool snapshot; restricted agents, session ownership, absent completion tools covered |
| 4. Limits and diagnostics | app_plugin_api.go and assembly tests | Valid UTF-8 for Korean/emoji boundaries, byte limit preserved, omission reasons visible |

Use separate commits per stage. Stages 1–2 preserve behavior; stages 3–4 intentionally change behavior. Implementers must not invent token policy, trust policy, or plugin API changes. Record evidence and impact if actual code differs from this review.

## 6. Validation and acceptance

Retain prompt_one_door_test.go, skill_head_frozen_test.go, retrieval_cache_test.go, context_provider_test.go, allowlist_advert_test.go, and memory/compaction tests. Do not delete assertions or mass-update expected output to manufacture equivalence.

Add coverage for:

- Identical system bytes and tool array across two steps of a turn; only the variable tail changes.
- Project instruction updates on the next turn; existing tool snapshot lifetime preserved.
- No tool or recall-guidance leakage across agents and owning sessions.
- No completion invocation guidance when a setting is enabled but the request tool is absent.
- No duplicate one-shot notification consumption or event writes across compaction.
- Retrieval failures, cached empty results, registration invalidation, and provider order preserved.
- Korean/emoji truncation boundaries, duplicate Source labels, and cooperative cancellation.
- Fragment extraction does not rewrite language selection or user-authored instructions.

Run affected tests at each implementation stage. Final checks: `go test ./internal/app ./internal/adapter/plugin/lua ./internal/arch`, `go test -race ./internal/app`, and `go test ./clients/office/helper/...`. Always run `node clients/vscode/tools/transcript-test.mjs` for implementation and record results. Implementation passed application, Lua, architecture and Office tests, the application race suite, and seven Playwright tests. Sub-agent review identified diagnostic gaps and repeated stall-guidance behavior; those were corrected and affected tests rerun. Distinguish native Office/IDE acceptance from automated checks.

## 7. Implementation result and remaining scope

Baseline tests were committed as `34497294`, assembly extraction as `19db9858`. The frozen request tool catalog now gates skill, recall and completion guidance. Retrieval truncation preserves UTF-8. contextDiagnostics is an internal application accessor, with no public API or UI. It returns metadata for system, variable, retrieval and excluded arrival guidance without their bodies.

User-instruction migration, automatic natural-language conflict resolution, token-policy changes and plugin API replacement remain outside this work. Automated core validation does not claim native Office or IDE acceptance.
