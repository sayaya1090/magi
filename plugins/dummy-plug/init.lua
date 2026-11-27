-- Dummy plug: a program pilots the tools instead of the model, one call at a time.
--
-- The model hands over a Lua script in one call and gets back only what the script printed. The
-- script runs as a child the host opens for it (magi.run_script with child): a subagent no model
-- drives. Every call it makes goes through the host — the permission gate, the start fact and the
-- result apply — and is recorded in the child's log, so this conversation keeps only the call and
-- what was printed. There is no mode to enter or leave.

-- The run's steps: where a script is, reported as a list the screen keeps in place. The host only
-- hands over facts (plan() and stage() are ours, on_call tells each call's start and end); what a
-- step means is this plugin's, and how it looks is the screen's. Each step has an id fixed when it is
-- made, a kind (tool, mcp, web, harness, subagent) the screen picks an icon for, and a state
-- (waiting, running, done, failed). No fixed words travel: the screen says "start" in its language.
local SUBAGENT = { explore = true, hand_off = true, s2_compare = true, s2_checkpoint = true }
local HARNESS = {
  recall_memory = true, recall_context = true, remember = true, search_sessions = true,
  s2_search = true, s2_report = true, s2_validate = true,
  companions = true, companion_can = true, companion_call = true, rate_handoff = true,
  diagram = true, todowrite = true, schedule = true, label = true, council = true,
}

local function kind_of(tool)
  if tool == nil or tool == "" then return nil end
  if tool:sub(1, 5) == "mcp__" then return "mcp" end
  if tool == "websearch" or tool == "webfetch" then return "web" end
  if SUBAGENT[tool] then return "subagent" end
  if HARNESS[tool] then return "harness" end
  return "tool"
end

-- clip keeps a name the script wrote on one line, at most 60 characters.
local function clip(v)
  local s = tostring(v or ""):gsub("[\r\n\t]", " "):gsub("%s+", " ")
  s = s:gsub("^ ", ""):gsub(" $", "")
  local out, n = {}, 0
  for ch in s:gmatch("[%z\1-\127\194-\244][\128-\191]*") do
    n = n + 1
    if n > 60 then out[#out + 1] = "…"; break end
    out[#out + 1] = ch
  end
  return table.concat(out)
end

-- track keeps one run's steps from plan(), stage() and the host's call facts, and reports them. It
-- forces a report when a step opens, a batch starts or ends, and when the run ends; between those the
-- host keeps a gap so a hundred quick reads do not report a hundred times.
local function track()
  local run = { stages = {}, current = 0, made = 0 }
  local batches = {}
  local graphs = {}
  local function report(force)
    local out = {}
    -- A graph goes right after the step that ran it, so a step planned after it is drawn after it;
    -- one run before any step goes first.
    local function graphs_of(owner)
      for _, graph in ipairs(graphs) do
        if graph.owner == owner then
          for _, task in ipairs(graph.tasks) do out[#out + 1] = task end
        end
      end
    end
    graphs_of(nil)
    for _, st in ipairs(run.stages) do
      out[#out + 1] = { id = st.id, label = st.name, kind = st.kind, state = st.state,
        done = st.done, total = st.total, running = st.running }
      graphs_of(st)
    end
    local cur = run.stages[run.current]
    magi.progress(cur and cur.name or "", { stages = out, force = force })
  end
  local function make(name)
    run.made = run.made + 1
    return { id = "s" .. run.made, name = clip(name), state = "waiting", done = 0, total = 0, running = 0 }
  end
  -- open runs the next planned step with this name, or a new one right after the current step.
  local function open(name)
    local cur = run.stages[run.current]
    if cur and cur.state == "running" then cur.state = "done" end
    if cur then cur.handed = nil end
    local want, at = clip(name), nil
    for i = run.current + 1, #run.stages do
      if run.stages[i].state == "waiting" and run.stages[i].name == want then at = i; break end
    end
    if not at then
      at = run.current + 1
      table.insert(run.stages, at, make(name))
    end
    run.current = at
    run.stages[at].state = "running"
  end
  local globals = {
    plan = function(list)
      if type(list) ~= "table" then return end
      for _, name in ipairs(list) do run.stages[#run.stages + 1] = make(name) end
    end,
    stage = function(name) open(name); report(true) end,
  }
  local function on_call(e)
    if e.phase == "graph" then
      local graph = {}
      local prefix = "g" .. e.graph_id .. ":"
      for _, task in ipairs(e.graph) do
        local deps = {}
        for _, dep in ipairs(task.after) do deps[#deps + 1] = prefix .. dep end
        graph[#graph + 1] = { id = prefix .. task.id, label = clip(task.label), kind = kind_of(task.tool), state = task.state,
          after = #deps > 0 and deps or nil, graph = true }
      end
      local owner = graphs[e.graph_id] and graphs[e.graph_id].owner or run.stages[run.current]
      graphs[e.graph_id] = { owner = owner, tasks = graph }
      -- Once every branch of the graph has started, the step that ran it has done its part: the
      -- graph's own nodes show the rest, so the step does not stay running until the next one opens.
      local started = true
      for _, task in ipairs(e.graph) do
        if #task.after == 0 and task.state == "waiting" then started = false end
      end
      -- handed says why it is done: it is still the script's current step, and a call_all that
      -- follows in it fills it again rather than opening a step the script never named.
      if started and owner and owner.state == "running" then owner.state = "done"; owner.running = 0; owner.handed = true end
      report(true)
      return
    end
    if e.batch == 0 then return end -- a single call_tool is not a step of its own
    local i = batches[e.batch]
    if not i then
      local cur = run.stages[run.current]
      if not cur or cur.total > 0 or (cur.state ~= "running" and not cur.handed) then open(e.tool) end
      cur = run.stages[run.current]
      if cur.handed then cur.state = "running"; cur.handed = nil end
      cur.kind, cur.total = kind_of(e.tool), e.size
      i = run.current
      batches[e.batch] = i
    end
    local st = run.stages[i]
    if e.phase == "start" then
      st.running = st.running + 1
      report(st.running == 1 and st.done == 0)
    else
      st.running = st.running - 1
      st.done = st.done + 1
      report(st.done == st.total)
    end
  end
  local function finish(ok)
    local cur = run.stages[run.current]
    if cur and (cur.state == "running" or cur.handed) then
      -- A step handed to a graph is still the script's current step: a failure after the graph is a
      -- failure in it, so it ends failed like a running one. The graph's nodes keep their own states.
      cur.state = ok and "done" or "failed"; cur.running = 0; cur.handed = nil
    elseif not ok and (#run.stages > 0 or next(graphs) ~= nil) then
      -- No step is left to carry the failure (a script with only a graph, or one that failed after
      -- its steps ended): a step of its own says the script failed, after what did succeed.
      local st = make("script failed")
      st.state = "failed"
      run.stages[#run.stages + 1] = st
    elseif #run.stages == 0 then
      return
    end
    report(true)
  end
  return globals, on_call, finish
end

magi.register_judge()

local USE = [[
Run one Lua script that calls tools for you, as a subagent no model drives: only what it print()s comes back.

In the script:
- call_tool(name, args) runs a tool and returns its text, or nil and the reason. Open tools: read, grep, glob, list, explore, recall_memory, bash, edit, multiedit, write. Each call follows the conversation's permission mode, as the model's own call would: one the person must approve is asked in the conversation and the script waits for the answer; a refusal comes back as nil and the reason. glob and list return a JSON array: json_decode it.
- judge(question, choices_or_schema) returns a validated value or nil and an error. Use Lua for exact rules. Only send the evidence needed for an ambiguous decision.
- call_all({ {name, args}, ... }) runs several calls at the same time (at most 8 at once) and returns { {ok=true, text=} or {ok=false, error=} } in the order asked; one failure does not stop the rest.
- call_graph({{id="a", tool="glob", args={pattern="docs/*.md"}}, {id="b", tool="read", after={"a"}, args={path="README.md"}}, ...}) runs tasks once their named predecessors succeed. Declare predecessors first. Independent safe calls overlap; a task with multiple predecessors joins their branches. The call returns output by task ID, or stops the script after a failure and skips its dependents.
- plan({name, ...}) lists the steps ahead (call it first): the person watching sees them all from the start, each updated in place. stage(name) opens the next step (a planned one by that name, or a new one). call_all fills the open step (or opens one) with its tool and k/n.
- progress(text) reports where the script is; print(...) is the only output, so keep it to what you need.
- json_encode(v) / json_decode(s), and Lua's string, table and math libraries. No files, processes or modules of its own.

Limits: 200 tool calls per run, 60 s by default (timeout up to 300), 16000 bytes of output by default (output up to 64000).
Use it for loops, filtering and many reads whose contents you do not need to see.]]

-- Routines: a code-mode script kept as a skill and run again by name (run_routine).
--
-- A routine has `kind: routine` in its frontmatter. New definitions sit beside their evaluation
-- and history files under .magi/routines/<name>/. The older locations are still accepted.
-- Besides `description:` it names the tools the script may call
-- (`tools: recall_memory, explore`) and its inputs as a one-line JSON Schema (`inputs: {...}`); the
-- script is the first ```lua block of the body and reads its arguments from `input`.
--
-- The person approves what runs: the first run, and any run after the script, the input schema or
-- the tool list changed, shows all three in the conversation's permission prompt (magi.approve). What
-- was approved is kept by fingerprint in this plugin's store, so it survives a restart. Approving the
-- routine grants nothing else — each call the script makes passes the permission gate as before, and
-- run_script refuses any tool outside the approved list. This version runs read-only routines only.
local READ_ONLY = { read = true, grep = true, glob = true, list = true, explore = true, recall_memory = true }
local AUTO_READERS = { read = true, grep = true, glob = true, list = true }
local AUTO_ROUTINE_LIMIT = 20
-- The tools a routine may call: this plugin's call_tools. Those that may write run one at a time
-- (run_script serial), and each still passes its own permission gate.
local CALLABLE = { judge = true, read = true, grep = true, glob = true, list = true, explore = true, recall_memory = true,
  bash = true, edit = true, multiedit = true, write = true }
local WRITES = { bash = true, edit = true, multiedit = true, write = true }
-- Calls that carry a judgement (or are routines themselves): a stretch to repeat ends at them.
local JUDGE = { judge = true, ask_user = true, council = true, todowrite = true, route_interjection = true, remember = true,
  label = true, hand_off = true, rate_handoff = true, skill = true, diagram = true, recall_context = true,
  run_routine = true, routine_catalog = true, dummy_plug = true, routine_improve = true, routine_history = true, routine_autoimprove = true, routine_compare_writes = true, routine_compare_explore = true, routine_trace_path = true, routine_task_families = true, routine_compare_trace_policy = true }
local CANDIDATE_AFTER = 3

local function frontmatter(text)
  local head, body = text:match("^%-%-%-\n(.-)\n%-%-%-\n?(.*)$")
  if not head then return nil end
  local fm = {}
  for line in head:gmatch("[^\n]+") do
    local k, v = line:match("^%s*([%w_]+)%s*:%s*(.-)%s*$")
    if k then fm[k] = v end
  end
  return fm, body
end

local ROUTINES_DIR = ".magi/routines"
local LEGACY_SKILLS_DIR = ".magi/skills"
local CLAUDE_SKILLS_DIR = ".claude/skills"

local function rdir(name) return ROUTINES_DIR .. "/" .. name .. "/" end

local function routine_paths(name)
  return { rdir(name) .. "routine.md", LEGACY_SKILLS_DIR .. "/" .. name .. ".md",
    CLAUDE_SKILLS_DIR .. "/" .. name .. "/SKILL.md" }
end

local function routine_names()
  local names, seen = {}, {}
  local function collect(dir, pattern)
    for _, entry in ipairs(magi.list_files(dir) or {}) do
      local name = entry:match(pattern)
      if name and not seen[name] then
        names[#names + 1], seen[name] = name, true
      end
    end
  end
  collect(ROUTINES_DIR, "^([%w_%-]+)/$")
  collect(LEGACY_SKILLS_DIR, "^([%w_%-]+)%.md$")
  collect(CLAUDE_SKILLS_DIR, "^([%w_%-]+)/$")
  table.sort(names)
  return names
end

local function load_routine(name)
  if type(name) ~= "string" or not name:match("^[%w_%-]+$") then
    return nil, "routine name must be letters, digits, - or _"
  end
  for _, path in ipairs(routine_paths(name)) do
    local text = magi.read_file(path)
    if type(text) == "string" then
      local fm, body = frontmatter(text)
      if not fm or fm.kind ~= "routine" then
        return nil, path .. " is a skill, not a routine (its frontmatter has no kind: routine)"
      end
      if fm.status == "learned-write" and (fm.source or ""):match("^result value reused %x+$") then
        return nil, "legacy result link needs fresh evidence from two model responses"
      end
      local script = body:match("```lua\n(.-)\n```")
      if not script then return nil, path .. " has no ```lua block" end
      local tools, seen = {}, {}
      for t in (fm.tools or ""):gmatch("[%w_]+") do
        if not seen[t] then seen[t] = true; tools[#tools + 1] = t end
      end
      table.sort(tools)
      if #tools == 0 then return nil, path .. ": tools: names no tool" end
      for _, t in ipairs(tools) do
        if not CALLABLE[t] then
          return nil, path .. ": " .. t .. " cannot be called from a routine (not in this plugin's call_tools)"
        end
      end
      local inputs = { type = "object", properties = {} }
      if fm.inputs and fm.inputs ~= "" then
        local v = magi.json_decode(fm.inputs)
        if type(v) ~= "table" then return nil, path .. ": inputs is not a JSON object" end
        inputs = v
      end
      local canon = script .. "\n\0inputs\0" .. magi.json_encode(inputs) .. "\0tools\0" .. table.concat(tools, ",")
      return { name = name, path = path, description = fm.description or "", tools = tools, inputs = inputs,
        script = script, fingerprint = magi.sha256(canon), file_sha256 = magi.sha256(text), source = fm.source, status = fm.status }
    end
  end
  return nil, "no routine named " .. name .. " (looked in " .. table.concat(routine_paths(name), ", ") .. ")"
end

local function check_inputs(schema, args)
  if args ~= nil and type(args) ~= "table" then return "args must be an object" end
  args = args or {}
  local props = type(schema.properties) == "table" and schema.properties or {}
  for _, k in ipairs(type(schema.required) == "table" and schema.required or {}) do
    if args[k] == nil then return "missing input " .. tostring(k) end
  end
  for k, v in pairs(args) do
    local p = props[k]
    if type(p) ~= "table" then return "unknown input " .. tostring(k) end
    local want, got = p.type, type(v)
    local ok = want == nil or (want == "string" and got == "string") or ((want == "number" or want == "integer") and got == "number")
      or (want == "boolean" and got == "boolean") or ((want == "array" or want == "object") and got == "table")
    if not ok then return "input " .. tostring(k) .. " must be " .. tostring(want) end
  end
  return nil
end

local function stored(key)
  local v = magi.store_get(key)
  return type(v) == "table" and v or {}
end

local function lifecycle_key()
  return "routine_lifecycle:" .. magi.sha256(magi.workdir())
end

local function lifecycle_now()
  return tonumber(magi.time("unix"))
end

local function lifecycle_entry(state, r, now)
  local entry = state[r.name]
  if type(entry) ~= "table" or entry.version ~= r.fingerprint then
    entry = { version = r.fingerprint, since = now }
    state[r.name] = entry
  end
  return entry
end

local function lifecycle_state(entry, r)
  if type(entry) == "table" and entry.version == r.fingerprint then return entry.status end
end

-- What a routine is worth: the model steps the stretch it was learned from took, less the run_routine call
-- that replaces them and any judge calls inside it. Measured once per version when it is saved (from the
-- response identities on the recorded calls); an edited version adds or removes the call sites it changed,
-- as an estimate. Nothing here calls a model.
local RV = {}
function RV.key() return "routine_saving:" .. magi.sha256(magi.workdir()) end
function RV.count(script, pattern)
  local n = 0
  for _ in (script or ""):gmatch(pattern) do n = n + 1 end
  return n
end
function RV.sites(script) return RV.count(script, "call_tool%s*%(") + RV.count(script, "run_pipeline_v1%s*%(") end
function RV.steps(calls)
  local seen, n = {}, 0
  for _, c in ipairs(calls or {}) do
    if not c.response then return nil end
    if not seen[c.response] then seen[c.response], n = true, n + 1 end
  end
  return n > 0 and n or nil
end
function RV.record(name, calls, fallback, occurrences)
  local ok, r = pcall(load_routine, name)
  if not ok or not r then return end
  local steps, basis = RV.steps(calls), "observed"
  if not steps then steps, basis = fallback, "estimated" end
  if not steps then return end
  local all = stored(RV.key())
  local sig = {}
  for _, c in ipairs(calls or {}) do sig[#sig + 1] = c.tool end
  -- learned from two observed occurrences of the same stretch
  all[name] = { version = r.fingerprint, steps = steps, sites = RV.sites(r.script), basis = basis,
    sig = #sig > 0 and sig or nil, occ = occurrences or 2 }
  magi.store_set(RV.key(), all)
end
function RV.of(r)
  local s = stored(RV.key())[r.name]
  local judges = RV.count(r.script, "judge%s*%(")
  if type(s) ~= "table" then
    local d = r.description or ""
    if r.status == "learned-write" and (d:find("^Shell pipeline:") or d:find("^Conditional shell workflow:")) then
      return { steps = 1, saved = 0 - judges, basis = "observed" } -- learned from one bash call: one model step
    end
    return nil
  end
  if s.version == r.fingerprint then
    return { steps = s.steps, saved = s.steps - 1 - judges, basis = s.basis, sig = s.sig, occ = s.occ }
  end
  local steps = math.max(1, s.steps + RV.sites(r.script) - (s.sites or 0))
  return { steps = steps, saved = steps - 1 - judges, basis = "estimated" }
end
-- Successful runs and listings, each halving every 14 days.
function RV.decay(v, at, now)
  v, at = tonumber(v) or 0, tonumber(at)
  if at and now and now > at then v = v * 0.5 ^ ((now - at) / (14 * 86400)) end
  return v
end
function RV.score(e, now) return RV.decay(e.score, e.at, now) end
-- Listings are charged in the same unit as savings: a listed line costs its tokens, and one saved model
-- call is counted as STEP_TOKENS tokens. That number is an assumption (a step re-sends at least the
-- conversation so far), not a measurement.
RV.STEP_TOKENS = 2000
-- A learned routine with few runs of its own borrows half of the mean run score of routines here; the
-- borrowed part shrinks as its own runs grow.
RV.SHRINK = 0.5
function RV.prior(idx, now)
  local sum, n = 0, 0
  for _, e in pairs(idx) do
    if type(e) == "table" then
      local score = RV.score(e, now)
      if score > 0 then sum, n = sum + score, n + 1 end
    end
  end
  return n > 0 and sum / n or 0
end
function RV.value(e, sv, now, prior)
  local score = RV.score(e, now)
  local listing = RV.decay(e.xscore, e.xat, now) * (tonumber(e.line_tokens) or 0) / RV.STEP_TOKENS
  local base = score
  if sv then base = sv.saved * (score + RV.SHRINK * (prior or 0) / (1 + (tonumber(e.runs) or 0))) end
  return base - (tonumber(e.failed) or 0) - listing
end
-- Whether the tool sequence short occurs contiguously inside the longer one.
function RV.contains(long, short)
  if #short >= #long then return false end
  for i = 1, #long - #short + 1 do
    local same = true
    for j = 1, #short do
      if long[i + j - 1] ~= short[j] then same = false; break end
    end
    if same then return true end
  end
  return false
end

local function record_routine_use(r, succeeded)
  if r.status ~= "auto-review" and r.status ~= "learned-write" then return end
  local now = lifecycle_now()
  if not now then return end
  local key = lifecycle_key()
  local state = stored(key)
  local entry = lifecycle_entry(state, r, now)
  entry.last_used = now
  entry.runs = (entry.runs or 0) + 1
  entry.failures = succeeded and 0 or (entry.failures or 0) + 1
  magi.store_set(key, state)
end

-- note_sequence counts a successful dummy plug run's calls by their sequence of tools (repeats of one
-- tool folded). A read-only sequence seen CANDIDATE_AFTER times becomes a routine candidate the
-- person can review and register; nothing is registered on the model's say-so.
local function note_sequence(goal, script, seq)
  if #seq < 2 then return nil end
  local parts, tools, set = {}, {}, {}
  for _, t in ipairs(seq) do
    if not CALLABLE[t] then return nil end
    if parts[#parts] ~= t then parts[#parts + 1] = t end
    if not set[t] then set[t] = true; tools[#tools + 1] = t end
  end
  table.sort(tools)
  local key = table.concat(parts, ">")
  local seen = stored("routine_seen")
  local s = type(seen[key]) == "table" and seen[key] or { count = 0 }
  s.count, s.goal, s.script, s.tools = (tonumber(s.count) or 0) + 1, goal, script, tools
  seen[key] = s
  magi.store_set("routine_seen", seen)
  if s.count ~= CANDIDATE_AFTER then return nil end
  local cands = stored("routine_candidates")
  local id = "c" .. magi.sha256(key):sub(1, 8)
  cands[id] = { id = id, sequence = key, goal = goal, script = script, tools = tools, count = s.count }
  magi.store_set("routine_candidates", cands)
  return cands[id]
end

-- Traces and drafts. tool_finished and user_message say what this conversation's tools did, turn by
-- turn; a person picks a stretch of it (/routine-trace, /routine-draft) or a finished dummy_plug run,
-- and it is saved as a routine draft checked dry: run with no tool reached, its calls compared with
-- what was traced. A stretch that runs again in another turn is offered as a candidate on the same
-- path. Nothing here needs Engram or a model.
local TRACE_KEEP, SHAPES_KEEP, RUNS_KEEP = 300, 500, 30

local function trace_key(sid) return "routine_trace:" .. tostring(sid) end

local function load_trace(sid)
  local t = stored(trace_key(sid))
  t.calls = type(t.calls) == "table" and t.calls or {}
  local legacy = t.prompts ~= nil
  t.prompts, t.file_loop = nil, nil
  for _, call in ipairs(t.calls) do
    if call.args ~= nil then legacy = true; call.args = nil end
  end
  t.turn = tonumber(t.turn) or 0
  t.next = tonumber(t.next) or 1
  if legacy then
    local ok, err = magi.store_set(trace_key(sid), t)
    if not ok then magi.log("[routine] could not clean legacy trace: " .. tostring(err)) end
  end
  return t
end

local function load_candidates()
  local cands, changed = stored("routine_candidates"), false
  for _, c in pairs(cands) do
    if type(c) == "table" and c.kind == "trace" then
      if c.prompt ~= nil or c.script ~= nil then changed = true end
      c.prompt, c.script = nil, nil
      for _, call in ipairs(c.calls or {}) do
        if call.args ~= nil then changed = true; call.args = nil end
      end
    end
  end
  if changed then
    local ok, err = magi.store_set("routine_candidates", cands)
    if not ok then magi.log("[routine] could not clean legacy candidates: " .. tostring(err)) end
  end
  return cands
end

local function clean_traces()
  if type(magi.store_prune_fields) == "function" then
    local ok, err = magi.store_prune_fields("routine_trace:", { { "prompts" }, { "calls", "*", "args" } })
    if not ok then magi.log("[routine] could not clean stored traces: " .. tostring(err)) end
  else
    local sid = magi.store_get("routine_last_session")
    if type(sid) == "string" and sid ~= "" then load_trace(sid) end
  end
  load_candidates()
end

clean_traces()

magi.register_command{
  name = "routine-trace-clean",
  description = "Clean legacy stored traces and list saved drafts that may contain copied values",
  execute = function()
    clean_traces()
    for _, name in ipairs(routine_names()) do
      for _, path in ipairs(routine_paths(name)) do
        local text = magi.read_file(path)
        local header = type(text) == "string" and text:match("^%-%-%-\n(.-)\n%-%-%-\n")
        if header and (header:find("\nsource: trace ", 1, true) or header:find("\nsource: repeated stretch ", 1, true))
          and not header:find("(synthetic arguments;", 1, true) then
          magi.log("[routine] saved draft may contain legacy arguments: " .. path)
        end
      end
    end
  end,
}

local function draft_from_calls(calls, automatic, parallel)
  if #calls == 0 then return nil, "no calls in that stretch" end
  if #calls > 1 and not parallel then
    for _, c in ipairs(calls) do
      if not c.order_known then
        return nil, "call #" .. tostring(c.n) .. " has no verified sequential order; draft it separately and review dependencies"
      end
    end
  end
  local tools, seen, lines, props, req, fixtures = {}, {}, {}, {}, {}, {}
  if parallel then lines[1] = "local results = call_all({" end
  for i, c in ipairs(calls) do
    if not CALLABLE[c.tool] then
      return nil, "step " .. i .. ": " .. tostring(c.tool) .. " cannot be called from a routine (not in this plugin's call_tools)"
    end
    if not seen[c.tool] then seen[c.tool] = true; tools[#tools + 1] = c.tool end
    local key = "step" .. i
    props[key] = { type = "object" }
    req[i] = key
    fixtures[key] = { fixture_step = i }
    if automatic then
      props[key].description = "Arguments for " .. c.tool .. " from the current request at step " .. i
    else
      lines[#lines + 1] = "-- TODO: supply arguments for step " .. i .. " and review its order and dependencies."
    end
    if parallel then
      lines[#lines + 1] = string.format("  {name=%q, args=input.%s},", c.tool, key)
    else
      lines[#lines + 1] = string.format("local r%d, e%d = call_tool(%q, input.%s)", i, i, c.tool, key)
      if c.ok ~= false then
        lines[#lines + 1] = string.format("if r%d == nil then error(%q .. tostring(e%d)) end", i, "step " .. i .. " (" .. c.tool .. ") failed: ", i)
      end
      if automatic then lines[#lines + 1] = string.format("print(%q, r%d)", "Step " .. i .. ": " .. c.tool, i) end
    end
  end
  if parallel then
    lines[#lines + 1] = "})"
    for i, c in ipairs(calls) do
      lines[#lines + 1] = string.format("if not results[%d].ok then error(%q .. tostring(results[%d].error)) end", i, "step " .. i .. " (" .. c.tool .. ") failed: ", i)
      lines[#lines + 1] = string.format("print(%q, results[%d].text)", "Step " .. i .. ": " .. c.tool, i)
    end
  end
  table.sort(tools)
  lines[#lines + 1] = string.format("print(%q)", "done: " .. #calls .. " steps")
  return { script = table.concat(lines, "\n"), tools = tools, fixtures = fixtures,
    inputs = magi.json_encode({ type = "object", properties = props, required = req }) }
end

-- dry_check runs a script with no tool reached and compares the calls it would make with expect (the
-- traced tool names in completion order and synthetic argument fixtures). Without expect (a dummy_plug run, whose later calls may read earlier
-- results a dry run leaves empty) it only reports how far the script got.
local function dry_check(script, example, tools, expect, fixtures)
  local src = "local input = json_decode(" .. string.format("%q", example or "{}") .. ")\n" .. script
  local _, err, calls = magi.run_script(src, { dry = true, tools = tools, timeout = 10 })
  calls = type(calls) == "table" and calls or {}
  if not expect then
    if err then return "stopped after " .. #calls .. " call(s): " .. tostring(err), true end
    return "ran " .. #calls .. " call(s)", true
  end
  for i, c in ipairs(expect) do
    local got = calls[i]
    if not got then
      return "failed: made " .. #calls .. " of " .. #expect .. " calls" .. (err and (" (" .. tostring(err) .. ")") or ""), false
    end
    local decoded, args = pcall(magi.json_decode, got.args)
    if got.tool ~= c.tool or not decoded or magi.json_encode(args) ~= magi.json_encode(fixtures["step" .. i]) then
      return "failed at step " .. i .. ": would call " .. tostring(got.tool), false
    end
  end
  if #calls > #expect then return "failed: made " .. #calls .. " calls, the trace has " .. #expect, false end
  if err then return "failed: " .. tostring(err), false end
  return "passed " .. #expect .. "/" .. #expect, true
end

local function pipeline_filter(description)
  local source, operands, include, exclude = description:match(
    "^Shell pipeline: ([%w_%.%-]+) ([^|]+) | grep %-E (%S+) | grep %-v (%S+)$")
  if not source then return end
  local roles = {}
  include = include:match("^{(s%d+_arg%d+)}$")
  exclude = exclude:match("^{(s%d+_arg%d+)}$")
  if include then roles[include] = "extended regular expression for lines to keep" end
  if exclude then
    roles[exclude] = include == exclude and "shared pattern for both the include and exclude filters"
      or "basic regular expression for lines to exclude"
  end
  if source == "git" then
    local path = operands:match(" %-%- {(s%d+_arg%d+)}$")
    if path and not roles[path] then roles[path] = "operand after Git's -- separator" end
  end
  return {source=source, roles=roles, purpose="Filter " .. source ..
    " output: keep lines matching the include regex, then remove lines matching the exclude pattern."}
end

-- Derive the help shown for old receipts from their saved template too. This
-- keeps the execution fingerprint independent of changes to catalog wording.
local function routine_usage(description, schema, source)
  local filter = pipeline_filter(description)
  local variable = (source or ""):match("^repeated variable%-width pipeline ") ~= nil
  local roles, example = {}, {}
  for k,p in pairs(schema.properties or {}) do roles[k] = p.description or p.type end
  for k,v in pairs(filter and filter.roles or {}) do roles[k]=v end
  local revision = description:match("^Shell pipeline: git show %-%-format= {(s%d+_arg%d+)} %-%- {items%.%.%.}")
  local include = description:match(" | grep %-E {(s%d+_arg%d+)} | grep %-v ")
  local includes_changes = include or description:find(" | grep -E ^[-+] | ",1,true)
  local review = variable and revision and includes_changes and filter and
    description:sub(-#("grep -v ^+++\\|^---")) == "grep -v ^+++\\|^---"
  local usage = {
    when = filter and filter.purpose or description,
    parameters = roles,
    output = "Text printed by the routine; output is bounded by run_routine's output limit.",
    errors = "Missing, unknown or mistyped inputs are rejected. Each tool call uses the current permission policy."
  }
  if description:match("^Shell pipeline:") or description:match("^Conditional shell workflow:") then
    usage.output="Bash result with exit status and command output; tool failures are reported to the caller."
  end
  if description:match("^Parallel readers:") or description:match("^Read%-only sequence:") then
    usage.when="Use when the requested reads and all their arguments are already known. "..description
    usage.output="Each step prints its tool name and result in the saved step order."
    usage.errors=usage.errors.." The first run or a changed read scope needs approval; a failed step identifies its tool."
  elseif description=="Read every file returned by glob" then
    usage.when="Use to read every matching file under one workspace subdirectory."
    roles.search="Glob arguments with the current subdirectory path and filename pattern"
    usage.output="Matching paths and their read results in glob order. Zero matches produce no file output."
    usage.errors=usage.errors.." More than 64 matches or a path outside the approved scope stops the run."
  end
  if variable then
    roles.items="1 to 32 literal operands after --, in order"
    roles.tail="Optional array of argv arrays matching an observed output pipeline. Omit for the core output."
    usage.errors=usage.errors.." Invalid operand counts, option-like scalar operands and unknown tail layouts are rejected before Bash."
  end
  if review then
    usage.when="Review a known commit's added and removed lines for selected files, after listing changed files."
    roles[revision]="Git revision to inspect (for example HEAD or a commit hash)"
    if include then roles[include]="Extended regex selecting diff lines; ^[-+] selects additions and deletions" end
    roles.items="1 to 32 current Git paths, one array element per path"
    usage.output="Bash result containing matching diff lines without +++/--- file headers; optional tail further filters or limits output."
    example[revision],example.items="HEAD",{"docs/example.md"}
    if include then example[include]="^[-+]" end
    usage.example=example
  end
  return usage, review and revision, review and include
end

local function usage_markdown(u)
  local lines={"**When to use.** "..u.when,"","**Inputs**",""}
  local keys={}
  for k in pairs(u.parameters) do keys[#keys+1]=k end
  table.sort(keys)
  for _,k in ipairs(keys) do lines[#lines+1]="- `"..k.."` — "..u.parameters[k] end
  lines[#lines+1]="\n**Output.** "..u.output
  lines[#lines+1]="\n**Errors.** "..u.errors
  if u.example then lines[#lines+1]="\nExample arguments: `"..magi.json_encode(u.example).."`" end
  return table.concat(lines,"\n").."\n"
end

-- save_routine writes the routine file. Saving is choosing what to keep; running it is approved
-- separately, by its first run_routine. A draft (status: draft) is refused by run_routine until the
-- person has reviewed it and deleted that line.
local function save_routine(name, description, d, source, dry, draft, automatic, learned_write)
  if type(name) ~= "string" or not name:match("^[%w_%-]+$") then return nil, "name: letters, digits, - or _" end
  local paths = routine_paths(name)
  local path = paths[1]
  for _, existing in ipairs(paths) do
    if type(magi.read_file(existing)) == "string" then return nil, existing .. " already exists; pick another name" end
  end
  local fm = { "---", "kind: routine" }
  if draft then fm[#fm + 1] = "status: draft" end
  if automatic then fm[#fm + 1] = "status: auto-review" end
  if learned_write then fm[#fm + 1] = "status: learned-write" end
  fm[#fm + 1] = "description: " .. (tostring(description or name):gsub("\n", " "))
  fm[#fm + 1] = "tools: " .. table.concat(d.tools, ", ")
  fm[#fm + 1] = "inputs: " .. d.inputs
  fm[#fm + 1] = "source: " .. source
  fm[#fm + 1] = "dry_check: " .. dry
  fm[#fm + 1] = "---"
  local note = learned_write and "Supply arguments from the current request. Each tool call follows the conversation's permission policy.\n"
    or automatic and "Supply arguments from the current request and use only when each step can run with those arguments. The first run asks for approval.\n"
    or draft and "A draft: review it, resolve each TODO, then delete the status line; run_routine refuses a draft.\n"
    or "Review it before its first run; that run asks for approval.\n"
  if d.purpose then note = d.purpose .. "\n" .. note end
  if automatic or learned_write then
    note = usage_markdown(routine_usage(description,magi.json_decode(d.inputs),source)) .. "\n" .. note
  end
  local body = table.concat(fm, "\n") .. "\n" .. note .. "\n```lua\n" .. d.script .. "\n```\n"
  local ok, err = magi.write_file(path, body)
  if not ok then return nil, "could not write " .. path .. ": " .. tostring(err) end
  return path, nil, magi.sha256(body)
end

local function reader_capacity()
  local active, total = 0, 0
  local state = stored(lifecycle_key())
  for _, name in ipairs(routine_names()) do
    if name:match("^auto%-t%x+$") then
      local ok, r = pcall(load_routine, name)
      if ok and r and r.status == "auto-review" then
        total = total + 1
        if not lifecycle_state(state[name], r) then active = active + 1 end
      end
    end
  end
  return active < AUTO_ROUTINE_LIMIT and total < 100
end

local function save_repeated_readers(id, calls, sequence, sid, parallel)
  if not reader_capacity() then return end
  local d = draft_from_calls(calls, true, parallel)
  if not d then return end
  local dry, passed = dry_check(d.script, magi.json_encode(d.fixtures), d.tools, calls, d.fixtures)
  if not passed then return end
  local name = "auto-" .. id
  local description = (parallel and "Parallel readers: " or "Read-only sequence: ") .. sequence
  local check = parallel and " (synthetic arguments; unordered dispatch group)" or " (synthetic arguments; observed completion order)"
  local path = save_routine(name, description, d, "repeated stretch " .. id, dry .. check, false, true)
  if path then RV.record(name, calls, parallel and 1 or #calls) end
  if path then
    local message = "[routine] saved " .. path .. "; review at its first run"
    magi.log(message)
    if sid and sid ~= "" then pcall(magi.notify, sid, message) end
  end
end

local function draft_trace(name, calls, source, description)
  local d, err = draft_from_calls(calls)
  if not d then return nil, err end
  local dry = dry_check(d.script, magi.json_encode(d.fixtures), d.tools, calls, d.fixtures)
  local path, werr = save_routine(name, description, d, source, dry .. " (synthetic arguments; observed completion order)", true)
  if not path then return nil, werr end
  return path, "saved " .. path .. " — dry check " .. dry .. " with synthetic arguments; review order and supply arguments before enabling"
end

local function draft_script(name, script, tools, source, description)
  if type(script) ~= "string" or script == "" then return nil, "no script to save" end
  local list = {}
  for _, t in ipairs(tools or {}) do
    if not CALLABLE[t] then return nil, tostring(t) .. " cannot be called from a routine (not in this plugin's call_tools)" end
    list[#list + 1] = t
  end
  table.sort(list)
  if #list == 0 then return nil, "that run called no tool" end
  local d = { script = script, tools = list, inputs = '{"type":"object","properties":{}}' }
  local dry = dry_check(script, "{}", list, nil)
  local hints = magi.judge_candidates(script)
  if #hints > 0 then
    dry = dry .. "; possible judge at source line(s) " .. table.concat(hints, ", ") ..
      ": reuse the existing question if its evidence is already supplied; keep explore when it needs to read files"
  end
  local path, werr = save_routine(name, description, d, source, dry, false)
  if not path then return nil, werr end
  return path, "saved " .. path .. " — dry run " .. dry .. "; its first run asks for approval"
end

-- Describe literal simple commands without retaining argument values. Shell constructs
-- that require evaluation or a full parser use the exact-command digest.
local function literal_shell_command(command)
  command = command:match("^[ \t]*(.-)[ \t]*$")
  local assignments = {}
  while #assignments < 16 do
    local name, rest = command:match("^([%a_][%w_]*)=[%w_./:%-]*[ \t]+(.+)$")
    if not name then break end
    assignments[#assignments + 1] = "assignment:" .. name
    command = rest
  end
  local verb, rest = command:match("^[ \t]*([%a_][%w_.%-]*)[ \t]+(.*)$")
  if not verb then
    verb = command:match("^[ \t]*([%a_][%w_.%-]*)[ \t]*$")
    if not verb then return nil end
    rest = ""
  end
  local shape = { "literal shell", verb }
  for _, assignment in ipairs(assignments) do shape[#shape + 1] = assignment end
  local subcommand = rest:match("^([%a][%w%-]*)[ \t]+%-")
  if subcommand then shape[#shape + 1] = "leading word:" .. subcommand end
  local options = true
  while rest ~= "" do
    local word, tail = rest:match("^([^ \t]+)(.*)$")
    if not word then return nil end
    local quote = rest:sub(1, 1)
    if quote == "'" or quote == '"' then
      word, tail = rest:match("^" .. quote .. "([^" .. quote .. "]*)" .. quote .. "(.*)$")
      if not word or (tail ~= "" and not tail:match("^[ \t]")) then return nil end
      if quote == '"' and word:find("[`$\\]") then return nil end
      shape[#shape + 1] = "operand"
    else
      if word:find("[;&|<>()`$\\%*%?%[%]{}\"'#~]") then return nil end
      if options and word == "--" then
        shape[#shape + 1], options = "--", false
      elseif options then
        local flag = word:match("^(%-%-?[%a][%w%-]*)=")
        shape[#shape + 1] = flag and flag .. "=operand" or word:match("^%-%-?[%a][%w%-]*$") or "operand"
      else
        shape[#shape + 1] = "operand"
      end
    end
    rest = tail:gsub("^[ \t]+", "")
  end
  return table.concat(shape, "\n"), verb
end

local function shell_pattern(args)
  if type(args) ~= "string" or #args > 65536 then return end
  local ok, body = pcall(magi.json_decode, args)
  if not ok or type(body) ~= "table" or type(body.command) ~= "string" then return end
  local command = body.command
  local exact = magi.sha256(command)
  if magi.shell_shape then
    local digest, label, dependencies, pipelines, graphs = magi.shell_shape(command)
    return digest or exact, label, dependencies, pipelines, graphs
  end
  -- Split literal pipelines and lists. Continuations and shell expansions keep
  -- their exact fingerprint.
  if command:find("[\r\\]") then return exact end
  command = command:gsub("^[ \t\n]+", ""):gsub("[ \t\n]+$", "")
  local shape, verbs, quote, start, i = {}, {}, nil, 1, 1
  while i <= #command do
    local char = command:sub(i, i)
    if quote then
      if char == quote then quote = nil end
    elseif char == "'" or char == '"' then
      quote = char
    elseif char == "|" or char == "&" or char == ";" or char == "\n" then
      local operator = command:sub(i, i + 1)
      if operator ~= "&&" and operator ~= "||" then operator = char end
      local width = #operator
      if operator == "\n" then operator = ";" end
      if operator == "&" or #shape >= 30 then return exact end
      local part, verb = literal_shell_command(command:sub(start, i - 1))
      if not part then return exact end
      shape[#shape + 1], shape[#shape + 2] = part, operator
      verbs[#verbs + 1], verbs[#verbs + 2] = verb, operator
      i = i + width - 1
      while command:sub(i + 1, i + 1):match("[ \t\n]") do i = i + 1 end
      start = i + 1
    end
    i = i + 1
  end
  if quote then return exact end
  local part, verb = literal_shell_command(command:sub(start))
  if not part then return exact end
  shape[#shape + 1], verbs[#verbs + 1] = part, verb
  return magi.sha256(table.concat(shape, "\0")), table.concat(verbs, " ")
end

local function observe_request(entry, terms, turn, request_id)
  if not terms or not next(terms) or entry.request_turn == turn then return false end
  local common = {}
  for term in pairs(terms) do
    if entry.last_terms and entry.last_terms[term] then common[term] = true end
  end
  entry.last_terms, entry.request_turn, entry.request_id = terms, turn, request_id
  if next(common) then entry.common_terms = common end
  return true
end

local function request_overlap(entry, terms, request_id)
  if request_id and entry and request_id == entry.request_id then return 50 end
  local count = 0
  for term in pairs(entry and entry.common_terms or {}) do
    if terms[term] then count = count + 1 end
  end
  return count
end

-- Generated synopses retain option spelling and slot identity, including in older receipts.
-- Describe only stages whose complete operands are known.
local function filter_intent(prompt, source)
  local words = " " .. prompt:lower():gsub("[^%w_.%-]+", " ") .. " "
  local named = words:find(" " .. source:lower() .. " ", 1, true)
  if source == "git" then
    named = named or prompt:find("변경 내역", 1, true) or prompt:find("변경 이력", 1, true)
      or prompt:find("커밋", 1, true) or words:find(" commit ", 1, true) or words:find(" commits ", 1, true)
  end
  return named and (prompt:find("추려", 1, true) or prompt:find("걸러", 1, true)
    or prompt:find("제외", 1, true) or prompt:find("포함", 1, true) or words:find(" filter ", 1, true)
    or words:find(" exclude ", 1, true) or words:find(" excluding ", 1, true))
end

local function save_shell_chain(dependency)
  if magi.platform() == "windows" then return end
  -- A leading open slot in a multi-argument stage could select the
  -- operation itself. Keep the observation without minting that template.
  for _,stage in ipairs(dependency.chain) do
    local slots = stage.slots or {}
    if #slots > 1 and type(slots[1]) ~= "string" then return end
  end
  local id = magi.sha256("shell chain\n" .. magi.workdir() .. "\n" .. dependency.key ..
    "\n" .. magi.json_encode(dependency.chain)):sub(1, 20)
  local name = "write-" .. id
  local receipt = stored("routine_learned_write:" .. id)
  if receipt.workspace == magi.workdir() and receipt.digest then
    local ok, r = pcall(load_routine, name)
    if ok and r and r.file_sha256 == receipt.digest then return name end
  end
  local total = 0
  for _, existing in ipairs(routine_names()) do
    if existing:match("^write%-%x+$") then total = total + 1 end
  end
  if total >= 100 then return end
  local pipeline = (dependency.pipes or 0) > 0
  local join = pipeline and " | " or " && "
  local props, required, fixture, commands, example, keys, synopsis = {}, {}, {}, {}, {}, {}, {}
  for stage_index, stage in ipairs(dependency.chain) do
    local words, sample = {string.format("%q", stage.name)}, {stage.name}
    for at, slot in ipairs(stage.slots or {}) do
      if type(slot) == "string" then
        words[#words + 1], sample[#sample + 1] = string.format("%q", slot), slot
      else
        local key = keys[slot]
        if not key then
          key = "s" .. stage_index .. "_arg" .. at
          keys[slot] = key
          props[key] = { type = "string", description = "Current argument " .. at .. " for " .. stage.name .. " in stage " .. stage_index }
          required[#required + 1], fixture[key] = key, key
        end
        words[#words + 1], sample[#sample + 1] = "quote(input." .. key .. ")", "'" .. key .. "'"
      end
    end
    commands[#commands + 1] = "table.concat({" .. table.concat(words, ", ") .. "}, ' ')"
    example[#example + 1] = table.concat(sample, " ")
    synopsis[#synopsis + 1] = table.concat(sample, " "):gsub("'(s%d+_arg%d+)'", "{%1}")
  end
  local script = [[local function quote(value)
  if type(value) ~= "string" or value:find("%z") then error("expected a literal shell argument") end
  return "'" .. value:gsub("'", "'\\''") .. "'"
end
local command = ]] .. table.concat(commands, " .. " .. string.format("%q", join) .. " .. ") .. [[

local out, err = call_tool("bash", {command=command})
if out == nil then error(tostring(err)) end
print(out)]]
  local description = (pipeline and "Shell pipeline: " or "Conditional shell workflow: ") .. table.concat(synopsis, join)
  local filter = pipeline_filter(description)
  if filter then
    for key, role in pairs(filter.roles) do props[key].description = role end
  end
  local d = { purpose = filter and filter.purpose, script = script, tools = {"bash"}, inputs = magi.json_encode{
    type = "object", properties = props, required = required } }
  local dry, passed = dry_check(script, magi.json_encode(fixture), d.tools, {{tool="bash"}},
    {step1={command=table.concat(example, join)}})
  if not passed then
    magi.log("[routine] shell workflow dry check: " .. dry)
    return
  end
  local path, _, digest = save_routine(name, description, d,
    "repeated shell graph " .. dependency.key, dry .. " (synthetic arguments)", false, false, true)
  if path then
    magi.store_set("routine_learned_write:" .. id, {digest=digest, workspace=magi.workdir()})
    return name
  end
end

-- Only a literal -- in the first stage establishes the variable-width boundary.
-- The stored layout contains command syntax and input positions, never observed paths.
local function pipeline_slot(value, index, stage)
  if type(value) ~= "string" then return {arg=true} end
  local program = stage[1]
  if program == "head" or program == "tail" then
    if #stage == 2 and index == 2 and value:match("^%-%d+$") then return {numeric="-"} end
    if #stage == 3 and index == 3 and stage[2] == "-n" and value:match("^%d+$") then return {numeric=""} end
  elseif program == "cut" and #stage == 2 and index == 2 and value:match("^%-c%d[%d%-]*$") then
    return {numeric="-c"}
  end
  if value:match("^%-%-?[%a][%w%-]*=?$") or value == "--" or
      (index == 2 and value:match("^[%a_][%w_.%-]*$") and type(stage[3]) == "string" and stage[3]:sub(1,1) == "-") then
    return {word=value}
  end
  -- A punctuation-only regex may become fixed after independent observations.
  if #value <= 64 and value:match("^[%[%]%^%$%+%*%.%-%?%(%)|\\]+$") then
    return {arg=true, pattern=value}
  end
  return {arg=true}
end

local function pipeline_layout(row)
  local core, tail, syntax, patterns, width = {}, {}, {}, {}, 0
  for i, stage in ipairs(row) do
    local layout = {stage[1]}
    local separated = false
    for j=2,#stage do
      if separated then
        width = width + 1
      else
        local slot = pipeline_slot(stage[j], j, stage)
        local key = "s" .. i .. "_arg" .. (j-1)
        if i <= 3 and slot.pattern then patterns[key] = slot.pattern end
        slot.pattern = nil
        layout[#layout+1] = slot
        if i == 1 and stage[j] == "--" then separated = true end
      end
    end
    if i == 1 then layout[#layout+1] = {items=true} end
    if i <= 3 then
      core[#core+1] = layout
      syntax[#syntax+1] = stage[1]
    else tail[#tail+1] = layout end
  end
  return core, tail, patterns, width, table.concat(syntax," | ")
end

local function save_pipeline_family(id, core, evidence)
  local name = "write-" .. id
  local count = 0
  for _, n in ipairs(routine_names()) do if n:match("^write%-%x+$") then count=count+1 end end
  if count >= 100 then return end
  local props = {items={type="array",items={type="string"},description="Current operands after --, in order"},
    tail={type="array",description="Optional previously observed output pipeline, each stage a literal argv array"}}
  local required, sample = {"items"}, {items={"one","two"}}
  local expected, synopsis = {}, {}
  for i,stage in ipairs(core) do
    local words = {stage[1]}
    for j=2,#stage do
      local slot, key = stage[j], "s"..i.."_arg"..(j-1)
      if evidence.patterns[key] then slot.word=evidence.patterns[key] end
      if slot.items then
        words[#words+1],words[#words+2]="one","two"
      elseif slot.word then words[#words+1]=slot.word
      else
        props[key]={type="string",description="Operand "..(j-1).." for "..stage[1]}
        required[#required+1]=key
        sample[key]=slot.numeric and (slot.numeric.."1") or key
        words[#words+1]=sample[key]
      end
    end
    local summary = {stage[1]}
    for j=2,#stage do
      local slot, key = stage[j], "s"..i.."_arg"..(j-1)
      summary[#summary+1] = slot.items and "{items...}" or slot.word or ("{"..key.."}")
      if props[key] and stage[1]=="grep" and j==3 then
        if stage[2].word=="-E" then props[key].description="Extended regex for lines to keep" end
        if stage[2].word=="-v" then props[key].description="Basic regex for lines to exclude" end
      end
    end
    synopsis[#synopsis+1] = table.concat(summary," ")
    for k,v in ipairs(words) do words[k]="'"..v:gsub("'","'\\''").."'" end
    expected[#expected+1]=table.concat(words," ")
  end
  local script="local spec=json_decode("..string.format("%q",magi.json_encode({core=core,tails=evidence.tails}))..")\nprint(run_pipeline_v1(spec, input))"
  local d={script=script,tools={"bash"},inputs=magi.json_encode({type="object",properties=props,required=required})}
  local dry,passed=dry_check(script,magi.json_encode(sample),d.tools,{{tool="bash"}},{step1={command=table.concat(expected," | ")}})
  if not passed then return end
  local path,_,digest=save_routine(name,"Shell pipeline: "..table.concat(synopsis," | "),d,
    "repeated variable-width pipeline "..id,dry,false,false,true)
  if path then
    magi.store_set("routine_learned_write:"..id,{digest=digest,workspace=magi.workdir()})
    return name
  end
end

local function observe_pipeline_families(rows, ev, trace)
  if not rows or #rows == 0 or magi.platform()=="windows" then return end
  local store_key="routine_pipeline_families:"..magi.sha256(magi.workdir())
  local state=stored(store_key)
  local occurrence=ev.session..":"..(ev.call_id ~= "" and ev.call_id or tostring(trace.next))
  local changed=false
  local covered={}
  for _,row in ipairs(rows or {}) do
    local core,tail,patterns,width,label=pipeline_layout(row)
    if #core >= 3 then
      local id=magi.sha256("variable pipeline v1\n"..magi.workdir().."\n"..magi.json_encode(core)):sub(1,20)
      local e=state[id]
      if e and e.name and not magi.read_file(routine_paths(e.name)[1]) then
        state[id], e, changed = nil, nil, true
      end
      if not e then
        local n=0
        for _ in pairs(state) do n=n+1 end
        if n < 200 then e={first=occurrence,width=width,patterns=patterns,tails={},label=label}; state[id]=e end
      end
      if e and not e.name then
        changed=true
        for key,v in pairs(e.patterns) do
          if patterns[key] ~= v then e.patterns[key] = nil end
        end
        if width ~= e.width then e.varied=true end
        if #tail>0 and #e.tails<8 then
          local seen=false
          for _,form in ipairs(e.tails) do if magi.json_encode(form)==magi.json_encode(tail) then seen=true end end
          if not seen then e.tails[#e.tails+1]=tail end
        end
        -- Not saved: one bash call is one model response, so a routine for it saves no model call.
      end
      -- A saved family covers only the observed input layout and output stages.
      if e and e.name and row.graph then
        local fits=true
        for k,v in pairs(e.patterns) do if patterns[k]~=v then fits=false end end
        if #tail>0 then
          local found=false
          for _,form in ipairs(e.tails) do if magi.json_encode(form)==magi.json_encode(tail) then found=true; break end end
          fits=fits and found
        end
        if fits then covered[row.graph]=e.name end
      end
    end
  end
  if changed then magi.store_set(store_key,state) end
  return covered
end

-- Only a complete standalone invocation can teach a producer/consumer edge.
-- Literal operands must come from this result, or be fixed punctuation patterns.
local function inventory_consumer(target, raw)
  local digest, _, _, rows=shell_pattern(raw)
  if not rows or #rows~=1 or not digest or rows[1].graph~=digest then return end
  local row=rows[1]
  local core,tail,_,width=pipeline_layout(row)
  if width~=#target.items then return end
  local first=row[1]
  for i,v in ipairs(target.items) do
    if first[#first-width+i]~=v then return end
  end
  for _,stage in ipairs(tail) do core[#core+1]=stage end
  -- shape: output settings such as cut -c1-200 or head -60. They are not part of the edge's identity, so
  -- consumers that differ only in width are one routine; the latest value fills the next plan.
  local bindings,patterns,shape={},{},{}
  for i,stage in ipairs(core) do
    if #stage>2 and not stage[2].word then return end
    for j=2,#stage do
      local slot=stage[j]
      if slot.numeric then
        shape["s"..i.."_arg"..(j-1)]=row[i][j]
      elseif not slot.word and not slot.items then
        local value=row[i][j]
        if type(value)~="string" then return end
        local key="s"..i.."_arg"..(j-1)
        local field
        for k,v in pairs(target.fields) do
          if v==value then
            if field then return end
            field=k
          end
        end
        if field then bindings[key]=field
        elseif pipeline_slot(value,j,row[i]).pattern then patterns[key]=value
        else return end
      end
    end
  end
  return core,bindings,patterns,shape
end

-- A previously saved variable-width routine may already implement this call.
-- Compare its fixed patterns and bind any remaining pattern inputs explicitly.
local function existing_inventory_family(core,patterns)
  if #core~=3 then return end
  local id=magi.sha256("variable pipeline v1\n"..magi.workdir().."\n"..magi.json_encode(core)):sub(1,20)
  local family=stored("routine_pipeline_families:"..magi.sha256(magi.workdir()))[id]
  if not family or not family.name then return end
  local r=load_routine(family.name)
  local receipt=stored("routine_learned_write:"..id)
  if not r or receipt.workspace~=magi.workdir() or r.file_sha256~=receipt.digest then return end
  for k,value in pairs(family.patterns or {}) do if patterns[k]~=value then return end end
  local constants={}
  for k,value in pairs(patterns) do if r.inputs.properties[k] then constants[k]=value end end
  return r.name, constants
end

magi.on("inventory_routine",function(ev)
  if magi.platform()=="windows" then return end
  local target=magi.json_decode(ev.inventory)
  local store_key="routine_inventory_edges_v2:"..magi.sha256(magi.workdir())
  local edges=stored(store_key)
  local edge=edges[target.source]
  if ev.consumer~="" then
    local calls=magi.json_decode(ev.consumer)
    if calls.command then calls={calls} end
    local core,bindings,patterns,shape
    for _,call in ipairs(calls) do
      local stages,fields,fixed,form=inventory_consumer(target,magi.json_encode(call))
      if stages then
        if core then return end
        core,bindings,patterns,shape=stages,fields,fixed,form
      end
    end
    if not core then return end
    local id=magi.sha256("inventory edge v1\n"..magi.workdir().."\n"..magi.json_encode({core,bindings,patterns})):sub(1,20)
    if edge and edge.id~=id then
      edge.ambiguous=true
    elseif not edge then
      local count=0
      for _ in pairs(edges) do count=count+1 end
      if count>=200 then return end
      local first={}
      for _,field in pairs(bindings) do first[field]=magi.sha256(magi.workdir().."\0"..target.fields[field]) end
      edges[target.source]={id=id,width=#target.items,first=first,bindings=bindings,shape=shape}
    elseif not edge.ambiguous and not edge.name and edge.width~=#target.items then
      for field,hash in pairs(edge.first) do
        if magi.sha256(magi.workdir().."\0"..target.fields[field])==hash then return end
      end
      edge.name,edge.constants=existing_inventory_family(core,patterns)
      if not edge.name then
        local name="write-"..id
        local r=load_routine(name)
        local receipt=stored("routine_learned_write:"..id)
        if r and receipt.workspace==magi.workdir() and receipt.digest==r.file_sha256 then edge.name=name
        else
          -- Output pipelines observed for the same three stages (one-call runs are observed, not saved).
          local family=#core==3 and stored("routine_pipeline_families:"..magi.sha256(magi.workdir()))[
            magi.sha256("variable pipeline v1\n"..magi.workdir().."\n"..magi.json_encode(core)):sub(1,20)]
          edge.name=save_pipeline_family(id,core,{patterns=patterns,tails=type(family)=="table" and family.tails or {}})
        end
      end
      edge.shape=shape
    elseif not edge.ambiguous then edge.shape=shape
    else return end
    magi.store_set(store_key,edges)
    return
  end
  if not edge or edge.ambiguous or not edge.name then return end
  local r=load_routine(edge.name)
  if not r then edges[target.source]=nil; magi.store_set(store_key,edges); return end
  local receipt=stored("routine_learned_write:"..edge.name:sub(7))
  if receipt.workspace~=magi.workdir() or receipt.digest~=r.file_sha256 or lifecycle_state(stored(lifecycle_key())[r.name],r) then return end
  local args={items=target.items}
  for key,field in pairs(edge.bindings) do args[key]=target.fields[field] end
  for key,value in pairs(edge.constants or {}) do args[key]=value end
  for key,value in pairs(edge.shape or {}) do
    if (r.inputs.properties or {})[key] then args[key]=value end
  end
  return magi.json_encode({name=r.name,args=args,expected_digest=r.file_sha256})
end)

-- Compare independent turns for the same request, model and exact generated
-- definition. An argument-free template is fully bound before a suggestion.
local function record_shell_cost(name, arm, ev, trace)
  if not name or not trace.request_id or (ev.authored_model or "") == "" then return end
  local r=load_routine(name)
  if not r or not (r.source or ""):match("^repeated shell graph ") or next(r.inputs.properties or {}) then return end
  local receipt=stored("routine_learned_write:"..name:sub(7))
  if receipt.digest~=r.file_sha256 or receipt.workspace~=magi.workdir() then return end
  local key="routine_call_cost:"..name
  local record=stored(key)
  if record.digest~=r.file_sha256 or record.request~=trace.request_id or record.model~=ev.authored_model then
    record={digest=r.file_sha256,request=trace.request_id,model=ev.authored_model}
  end
  local stats=record[arm] or {count=0,errors=0}
  local turn=ev.session..":"..trace.turn
  if stats.last==turn then return end
  stats.last,stats.count=turn,stats.count+1
  if ev.ok~="true" then stats.errors=stats.errors+1 end
  local tokens=tonumber(ev.authored_tokens) or 0
  if tokens>0 then
    stats.metered=(stats.metered or 0)+1
    stats.min=math.min(stats.min or tokens,tokens)
    stats.max=math.max(stats.max or tokens,tokens)
  end
  record[arm]=stats
  magi.store_set(key,record)
end

local function measured_shell_match(name,r,request,model)
  if not request or not model or model=="" or next(r.inputs.properties or {}) then return false end
  local record=stored("routine_call_cost:"..name)
  local direct,wrapper=record.direct,record.wrapper
  if record.digest~=r.file_sha256 or record.request~=request or record.model~=model or not direct or not wrapper then return false end
  if direct.count<2 or wrapper.count<2 or wrapper.errors>0 then return false end
  return direct.errors>=2 or ((direct.metered or 0)>=2 and (wrapper.metered or 0)>=2 and wrapper.max<direct.min)
end

local function observe_shell_dependencies(session, call, dependencies, trace, covered)
  if type(dependencies) ~= "table" or #dependencies == 0 then return end
  local key = "routine_shell_dependencies:" .. magi.sha256(magi.workdir()):sub(1, 20)
  local state = stored(key)
  local entries = type(state.entries) == "table" and state.entries or {}
  local order = type(state.order) == "table" and state.order or {}
  local occurrence = session .. ":" .. call
  for _, dependency in ipairs(dependencies) do
    local id = dependency.key
    local entry = entries[id]
    local layout = dependency.chain and magi.sha256(magi.json_encode(dependency.chain))
    if layout then
      local variant = magi.sha256(dependency.key .. ":" .. layout)
      if entries[variant] or (entry and entry.layout and entry.layout ~= layout) then
        id, entry = variant, entries[variant]
      end
    end
    if not entry then
      entry = { label = dependency.label, hits = 0 }
      entries[id] = entry
      order[#order + 1] = id
    end
    if entry.routine and not magi.read_file(routine_paths(entry.routine)[1]) then
      entry.routine, entry.layout_occurrence = nil, occurrence
    end
    if entry.last ~= occurrence then
      entry.hits, entry.last = entry.hits + 1, occurrence
    end
    entry.conditions = dependency.conditions
    entry.file_edges = dependency.file_edges
    entry.pipes = dependency.pipes
    if (tonumber(entry.conditions) or 0) >= 2 or (tonumber(entry.file_edges) or 0) >= 2 or (entry.pipes or 0) >= 2 then
      observe_request(entry, trace.request_terms, session .. ":" .. trace.turn, trace.request_id)
    end
    if layout then
      local requests = entry.runnable_requests or {}
      if requests.last ~= occurrence then
        requests.hits, requests.last = (requests.hits or 0) + 1, occurrence
        observe_request(requests, trace.request_terms, session .. ":" .. trace.turn, trace.request_id)
      end
      entry.runnable_requests = requests
      entry.covered_by = covered and covered[dependency.key]
      -- The layout is remembered for hints and graphs but not saved as a routine: the chain was one bash
      -- call, one model response, so running it as a routine would save no model call.
      if not entry.routine then
        entry.layout, entry.layout_occurrence = layout, occurrence
      end
    end
  end
  while #order > 200 do entries[table.remove(order, 1)] = nil end
  magi.store_set(key, { entries = entries, order = order })
end

local function note_run(goal, script, seq)
  local runs = stored("routine_runs")
  local list = type(runs.list) == "table" and runs.list or {}
  local tools, set = {}, {}
  for _, t in ipairs(seq) do
    if not set[t] then set[t] = true; tools[#tools + 1] = t end
  end
  table.sort(tools)
  local id = "r" .. magi.sha256(script .. tostring(magi.time()) .. tostring(magi.nonce())):sub(1, 8)
  list[#list + 1] = { id = id, goal = goal, script = script, tools = tools, at = magi.time() }
  while #list > RUNS_KEEP do table.remove(list, 1) end
  runs.list = list
  magi.store_set("routine_runs", runs)
  return id
end

local function request_terms(text)
  local terms, count = {}, 0
  if type(text) ~= "string" then return terms end
  local prefix = magi.workdir() .. "\0request\0"
  for word in text:sub(1, 4096):lower():gmatch("[^%s%p]+") do
    if #word >= 4 and #word <= 40 then
      local key = magi.sha256(prefix .. word):sub(1, 20)
      if not terms[key] then
        terms[key], count = true, count + 1
        if count == 48 then break end
      end
    end
  end
  local request_id
  if count >= 2 then request_id = magi.sha256(prefix .. text):sub(1, 24) end
  return terms, request_id
end

local function observe_write(shapes, sid, calls, writer_shape, writer_labels, same_file, terms, request_id)
  local key = magi.sha256("write\n" .. magi.workdir() .. "\n" .. table.concat(writer_shape, "\n")):sub(1, 20)
  local seen = type(shapes[key]) == "table" and shapes[key] or {}
  local previous = seen.write_seen
  local turn = sid .. ":" .. calls[1].turn
  local updated = false
  if #calls > 1 and observe_request(seen, terms, turn, request_id) then
    shapes[key], updated = seen, true
  end
  if not seen.write_saved and (not previous or previous.session ~= sid or previous.last < calls[1].n) then
    if previous or seen.write_turn then
      local name = "write-" .. key
      local existing = magi.read_file(routine_paths(name)[1])
      if type(existing) == "string" then
        seen.write_saved = true
      else
        local d = draft_from_calls(calls, true)
        if d and same_file then
          d.fixtures.step1.path, d.fixtures.step2.path = "fixture.txt", "fixture.txt"
          local schema = magi.json_decode(d.inputs)
          schema.required = {"step1"}
          schema.properties.step2.description = "Optional read arguments. The path defaults to step1.path."
          d.inputs = magi.json_encode(schema)
          d.script = [[
input.step2 = input.step2 or {}
if input.step2.path == nil then input.step2.path = input.step1.path end
if type(input.step1.path) ~= "string" or input.step1.path ~= input.step2.path then
  error("the write and read must target the same file")
end
]] .. d.script
        end
        local dry, passed
        if d then dry, passed = dry_check(d.script, magi.json_encode(d.fixtures), d.tools, calls, d.fixtures) end
        local path, _, digest
        if passed then
          local description = same_file and "Write and read the same file: " or "Writing sequence: "
          description = description .. table.concat(writer_labels, " > ")
          if same_file then description = description .. " (supply step1; read reuses its path)" end
          path, _, digest = save_routine(name, description,
            d, "repeated stretch " .. key, dry .. " (synthetic arguments; observed completion order)", false, false, true)
          if path then RV.record(name, calls, #calls) end
        end
        if path then
          seen.write_saved = true
          magi.store_set("routine_learned_write:" .. key, { digest = digest, workspace = magi.workdir() })
          magi.log("[routine] learned writing routine " .. path)
        end
      end
    end
    seen.write_seen = { session = sid, last = calls[#calls].n }
    shapes[key] = seen
    return true
  end
  return updated
end

-- Inspect each suffix when its last call completes. A suffix never crosses a turn,
-- failed call, or unknown order. Overlapping occurrences cannot confirm each other.
local function learn_completed_writes(sid, trace)
  local last = trace.calls[#trace.calls]
  if not last.ok or not last.order_known or JUDGE[last.tool] or not CALLABLE[last.tool] then return end
  local shapes, changed = stored("routine_shapes"), false
  if last.file_source then
    for i=#trace.calls-1,1,-1 do
      local c=trace.calls[i]
      if c.n==last.file_source then
        changed=observe_write(shapes,sid,{c,last},{"same-file",c.tool,"read"},{c.tool,"read"},true,trace.request_terms,trace.request_id)
        break
      end
    end
  end
  local calls, tokens, labels, writes = {}, {}, {}, false
  for i = #trace.calls, math.max(1, #trace.calls - 11), -1 do
    local c = trace.calls[i]
    if c.turn ~= last.turn or not c.ok or not c.order_known or JUDGE[c.tool] or not CALLABLE[c.tool]
      or (c.tool == "bash" and (not c.shell_pattern or c.pipe_graphs)) then break end
    table.insert(calls, 1, c)
    table.insert(tokens, 1, c.tool .. ":" .. (c.shell_pattern or ""))
    table.insert(labels, 1, c.shell_verb and (c.tool .. " (" .. c.shell_verb .. ")") or c.tool)
    writes = writes or WRITES[c.tool] == true
    if writes and #calls >= 2
      and observe_write(shapes, sid, calls, tokens, labels, false, trace.request_terms, trace.request_id) then changed = true end
  end
  if changed then
    local count = 0
    for _ in pairs(shapes) do count = count + 1 end
    while count > SHAPES_KEEP do
      shapes[next(shapes)] = nil
      count = count - 1
    end
    magi.store_set("routine_shapes", shapes)
  end
end

-- Manual candidates require separate turns. Automatic readers count distinct stretches
-- and complete parallel dispatches, including those observed within the same turn.
-- A turn's opening reads (read, list, glob only) are left out: in the sessions measured they were
-- most of what repeated, and a model choosing what to read is not a procedure.
local function note_repeats(sid)
  local t = load_trace(sid)
  local turn, first_n = t.turn, nil
  local segs, seg, batches, batch_order = {}, {}, {}, {}
  for _, c in ipairs(t.calls) do
    if c.turn == turn then
      first_n = first_n or c.n
      if c.parallel_batch and c.parallel_batch ~= "" then
        local batch = batches[c.parallel_batch]
        if not batch then
          batch = {}
          batches[c.parallel_batch] = batch
          batch_order[#batch_order + 1] = c.parallel_batch
        end
        batch[#batch + 1] = c
      end
      if JUDGE[c.tool] or not c.order_known then
        if #seg > 0 then segs[#segs + 1] = seg end
        seg = {}
      else
        seg[#seg + 1] = c
        if c.ok == false then segs[#segs + 1] = seg; seg = {} end
      end
    end
  end
  if #seg > 0 then segs[#segs + 1] = seg end
  local shapes, changed = stored("routine_shapes"), false
  local function observe_reader(key, calls, tools, occurrence, parallel)
    local seen = type(shapes[key]) == "table" and shapes[key] or {}
    if (tonumber(seen.read_successes) or 0) >= 2 or seen.reader_occurrence == occurrence then return end
    seen.reader_occurrence = occurrence
    seen.read_successes = (tonumber(seen.read_successes) or 0) + 1
    shapes[key], changed = seen, true
    if seen.read_successes == 2 then
      save_repeated_readers("t" .. key:sub(1, 8), calls, table.concat(tools, parallel and ", " or ">"), sid, parallel)
    end
  end
  for _, batch_id in ipairs(batch_order) do
    local calls = batches[batch_id]
    local total = #calls
    local eligible, ids, tools = total >= 2 and total <= 8, {}, {}
    for _, c in ipairs(calls) do
      if c.parallel_size ~= total or not c.ok or not AUTO_READERS[c.tool]
        or not c.call_id or c.call_id == "" or ids[c.call_id] then eligible = false end
      if c.call_id then ids[c.call_id] = true end
    end
    if eligible then
      table.sort(calls, function(a, b) return a.tool == b.tool and a.call_id < b.call_id or a.tool < b.tool end)
      for i, c in ipairs(calls) do tools[i] = c.tool end
      local key = magi.sha256("parallel\n" .. table.concat(tools, "\n")):sub(1, 12)
      observe_reader(key, calls, tools, sid .. ":batch:" .. batch_id, true)
    end
  end
  for _, s in ipairs(segs) do
    local fits, opening = #s >= 2 and #s <= 12, s[1].n == first_n
    local tl, readers = {}, true
    for _, c in ipairs(s) do
      if not CALLABLE[c.tool] then fits = false end
      if not c.ok or not AUTO_READERS[c.tool] then readers = false end
      if c.tool ~= "read" and c.tool ~= "list" and c.tool ~= "glob" then opening = false end
      tl[#tl + 1] = c.tool
    end
    if fits and not opening then
      local key = magi.sha256(table.concat(tl, "\n")):sub(1, 12)
      if readers then
        observe_reader(key, s, tl, sid .. ":stretch:" .. turn .. ":" .. s[1].n, false)
      end
      local seen = type(shapes[key]) == "table" and shapes[key] or {}
      seen.turns = type(seen.turns) == "table" and seen.turns or {}
      local tk = sid .. ":" .. turn
      if not seen.turns[tk] then
        seen.turns[tk] = true
        seen.count = (tonumber(seen.count) or 0) + 1
        shapes[key], changed = seen, true
        local id = "t" .. key:sub(1, 8)
        if seen.count == 2 then
          local tools, set = {}, {}
          for _, x in ipairs(tl) do if not set[x] then set[x] = true; tools[#tools + 1] = x end end
          table.sort(tools)
          local cands = load_candidates()
          cands[id] = { id = id, kind = "trace", sequence = table.concat(tl, ">"), count = 2, tools = tools,
            goal = "repeated stretch: " .. table.concat(tl, " > "), calls = s }
          magi.store_set("routine_candidates", cands)
          magi.log("[routine] candidate " .. id .. ": " .. cands[id].sequence .. " ran in 2 turns — review with " ..
            "/routine-candidates, save with /routine-register " .. id .. " <name>")
        end
      end
    end
  end
  if changed then
    local n = 0
    for _ in pairs(shapes) do n = n + 1 end
    while n > SHAPES_KEEP do
      shapes[next(shapes)] = nil
      n = n - 1
    end
    magi.store_set("routine_shapes", shapes)
  end
end

-- Learn the data dependency, counting complete consumption in glob order.
local EACH_FILE = [[local files = assert(json_decode(assert(call_tool("glob", input.search))))
if #files > 64 then error("glob result exceeds 64 files") end
for _, path in ipairs(files) do
  local text = assert(call_tool("read", {path=path}))
  print(path, text)
end]]

local function observe_file_loop(t, ev, digest)
  local workspace = magi.sha256(magi.workdir())
  local sources = t.glob_workspace == workspace and t.glob_sources or {}
  t.glob_sources, t.glob_workspace = sources, workspace
  for i = #sources, 1, -1 do
    if t.next - sources[i].n > 2048 or t.turn - sources[i].turn > 8 then table.remove(sources, i) end
  end
  local ordered = ev.ok == "true" and ev.order_known == "true" and (ev.parallel_batch or "") == "" and ev.background ~= "true"
  if ev.tool == "glob" and (ev.glob_items or "") ~= "" then
    local ok, items = pcall(magi.json_decode, ev.glob_items)
    if ok and type(items) == "table" and #items >= 2 and #items <= 64 then
      -- Even an unordered result can make attribution to an older search ambiguous.
      sources[#sources + 1] = {n=t.next, turn=t.turn, items=items, index=1, ordered=ordered}
      if #sources > 16 then table.remove(sources, 1) end
    end
    return
  end
  if not digest then return end
  local pending, position
  local matches = 0
  for i = #sources, 1, -1 do
    local source = sources[i]
    for _, item in ipairs(source.items) do
      if item == digest then
        matches = matches + 1
        position = i
        pending = table.remove(sources, i)
        break
      end
    end
  end
  if matches ~= 1 or ev.tool ~= "read" or not ordered or not pending.ordered or pending.items[pending.index] ~= digest then return end
  local args = magi.json_decode(ev.args)
  for key in pairs(args) do if key ~= "path" then return end end
  if pending.index < #pending.items then
    pending.index = pending.index + 1
    table.insert(sources, position, pending)
    return pending.n
  end
  return pending.n, pending
end

local function learn_file_loop(pending, ev)
  local id = "t" .. magi.sha256("glob-each-read-v1"):sub(1,8)
  local key = "routine_loop:" .. magi.sha256(magi.workdir()) .. ":" .. id
  local previous = stored(key)
  if previous.saved then return end
  if not previous.count then magi.store_set(key,{count=#pending.items}); return end
  if previous.count == #pending.items or not reader_capacity() then return end
  local def = {script=EACH_FILE, tools={"glob","read"}, inputs=magi.json_encode{
    type="object", properties={search={type="object",description="glob arguments including a subdirectory path and pattern"}},required={"search"}}}
  local _, err, replay = magi.run_script('local input={search={path="docs",pattern="*"}}\n' .. EACH_FILE,
    {dry=true, tools=def.tools, fixtures={{tool="glob", text='["docs/a","docs/b"]'}}})
  if err or not replay or #replay ~= 3 or replay[1].tool ~= "glob" then return end
  for i,path in ipairs({"docs/a","docs/b"}) do
    if replay[i+1].tool ~= "read" or magi.json_decode(replay[i+1].args).path ~= path then return end
  end
  local path = save_routine("auto-" .. id,"Read every file returned by glob",def,
    "glob result consumed in order at lengths " .. previous.count .. " and " .. #pending.items,
    "passed glob and both dependent reads",false,true)
  if path then
    magi.store_set(key,{saved=true})
    local msg="[routine] saved " .. path .. "; reads follow the files matched by glob"
    magi.log(msg)
    pcall(magi.notify,ev.session,msg)
  end
end

-- Workspace patterns contain roles and argument positions only. Trace numbers
-- identify observations locally and are never part of a pattern's identity.
local graph_observe, graph_recall
do
  local function store_key() return "routine_graph_index_v1:" .. magi.sha256(magi.workdir()) end

  local function node(graph, role, title, parents)
    graph[#graph+1] = {role=role, title=title, parents=parents or {}}
    return #graph
  end

  -- Backward slice. Parent IDs can point to much older calls in the trace.
  -- Compact IDs are topological, with the requested result last.
  local function slice(graph, root)
    local result, ids = {}, {}
    local function visit(i)
      if ids[i] then return ids[i] end
      local v, parents = graph[i], {}
      for _, edge in ipairs(v.parents) do
        parents[#parents+1] = {visit(edge[1]), edge[2]}
      end
      ids[i] = node(result, v.role, v.title, parents)
      return ids[i]
    end
    visit(root)
    return result
  end

  -- Two rounds of directed WL refinement. Incoming neighborhoods stay
  -- comparable when a later observation adds more downstream consumers.
  local function colors(graph)
    local labels = {}
    for i,v in ipairs(graph) do labels[i] = v.role end
    for round=1,2 do
      local next_labels = {}
      for i,v in ipairs(graph) do
        local incoming = {}
        for _,e in ipairs(v.parents) do incoming[#incoming+1] = e[2] .. ":" .. labels[e[1]] end
        table.sort(incoming)
        next_labels[i] = magi.sha256(labels[i] .. "\0" .. table.concat(incoming,"\0")):sub(1,24)
      end
      labels = next_labels
    end
    return labels
  end

  -- VF2-style directed frontier search starting at the result. Edge labels
  -- include operand positions. Injectivity preserves shared-input identity.
  local function embeds(pattern, host, root)
    local mapping,used,frontier = {},{},{{#pattern,root}}
    while #frontier>0 do
      local pair=table.remove(frontier)
      local p,h=pair[1],pair[2]
      if mapping[p] then
        if mapping[p]~=h then return false end
      else
        if used[h] then return false end
        local a,b=pattern[p],host[h]
        if a.role~=b.role or #a.parents~=#b.parents then return false end
        mapping[p],used[h]=h,true
        -- Position labels make every predecessor choice unique. Only the
        -- initial host vertex has alternatives (enumerated through the index).
        local targets={}
        for _,edge in ipairs(b.parents) do targets[edge[2]]=edge[1] end
        for _,edge in ipairs(a.parents) do
          local target=targets[edge[2]]
          if not target then return false end
          frontier[#frontier+1]={edge[1],target}
        end
      end
    end
    return true
  end

  local function index_graph(state, graph, sid, first, call, trace)
    if #graph < 2 or #graph > 40 then return end
    local labels = colors(graph)
    local root = labels[#labels]
    local same, partial
    for i,label in ipairs(labels) do
      for _,id in ipairs(state.buckets[label] or {}) do
        local entry = state.entries[id]
        if #entry.graph <= #graph and embeds(entry.graph,graph,i) then
          if i == #graph and #entry.graph == #graph then same=id
          elseif (entry.session~=sid or entry.last<first) and (not partial or #entry.graph > #state.entries[partial].graph) then partial=id end
        end
      end
    end
    local id = same
    if not id then
      if #state.order >= 96 then
        local old = table.remove(state.order,1)
        local bucket = state.buckets[state.entries[old].root]
        for i,v in ipairs(bucket) do if v == old then table.remove(bucket,i); break end end
        if #bucket == 0 then state.buckets[state.entries[old].root] = nil end
        state.entries[old] = nil
      end
      id = magi.sha256(magi.json_encode(graph)):sub(1,24)
      state.order[#state.order+1] = id
      local bucket = state.buckets[root] or {}
      bucket[#bucket+1],state.buckets[root] = id,bucket
      state.entries[id] = {graph=graph,root=root,hits=0}
    end
    local entry = state.entries[id]
    if entry.session ~= sid or first > entry.last then
      entry.hits = entry.hits + 1
      entry.session,entry.last = sid,call.n
      observe_request(entry,trace.request_terms,sid..":"..trace.turn,trace.request_id)
    end
    if entry.hits >= 2 or partial then
      call.graph_matches = call.graph_matches or {}
      call.graph_matches[#call.graph_matches+1] = (same and "repeat " or partial and "contains " or "pattern ") .. (same or partial or id)
    end
    return entry
  end

  graph_observe = function(trace,sid)
    local calls,call = trace.calls,trace.calls[#trace.calls]
    if not call.ok or not (call.pipe_graphs or call.glob_source or call.job_source or (call.tool=="read" and call.file_digest)) then return end
    local sources,by_number = {},{}
    for i=#calls-1,1,-1 do
      local c = calls[i]
      by_number[c.n]=c
      if c.n == call.glob_source then sources.glob=c end
      if c.n == call.job_source then sources.job=c end
      if not sources.inventory and call.inventory_read and c.inventory_produced == call.inventory_read then sources.inventory=c end
    end
    if call.tool == "read" and call.file_digest and call.order_known then
      for i=#calls-1,1,-1 do
        local c=calls[i]
        if not c.ok or not c.order_known or not (AUTO_READERS[c.tool] or (WRITES[c.tool] and c.file_digest)) then break end
        if WRITES[c.tool] and c.file_digest == call.file_digest then sources.file=c; break end
      end
    end
    if not next(sources) and not call.pipe_graphs then return end
    local state = stored(store_key())
    state.order,state.entries,state.buckets = state.order or {},state.entries or {},state.buckets or {}
    local graph,parents,first = {},{},call.n
    local prior,updated={},false
    local function ancestor(c)
      if prior[c.n] then return prior[c.n] end
      local links={}
      for _,kind in ipairs({"file","glob","job"}) do
        local parent=by_number[c[kind.."_source"]]
        if parent and parent.n<c.n and parent.ok then links[#links+1]={ancestor(parent),kind} end
      end
      first=math.min(first,c.n)
      prior[c.n]=node(graph,c.tool..":"..(c.shell_pattern or ""),c.tool,links)
      return prior[c.n]
    end
    for _,kind in ipairs({"file","glob","job"}) do
      local c=sources[kind]
      if c and c.ok then parents[#parents+1]={ancestor(c),kind} end
    end
    if #parents>0 then
      local root=node(graph,call.tool..":"..(call.shell_pattern or ""),call.tool,parents)
      updated=index_graph(state,slice(graph,root),sid,first,call,trace)~=nil
      if sources.file then call.file_source=sources.file.n end
    end
    for _,pipe in ipairs(call.pipe_graphs or {}) do
      local stages,slots={},{}
      graph,prior,first={},{},call.n
      local producer = sources.inventory
      local source = producer and ancestor(producer)
      local prior_matches=#(call.graph_matches or {})
      local linked=false
      for _,stage in ipairs(pipe.stages) do
        parents={}
        if #stages>0 then parents[#parents+1]={stages[#stages],"pipe"} end
        local selected={}
        for _,slot in ipairs(stage.item_slots or {}) do selected[slot]=true end
        for j,slot in ipairs(stage.slots or {}) do
          if slot>0 then
            if not slots[slot] then slots[slot]=node(graph,"argument","input") end
            parents[#parents+1]={slots[slot],"arg:"..j}
            if source and selected[slot] then
              parents[#parents+1]={source,(stage.matched==stage.total and "all:" or "selected:")..j}
              linked=true
            end
          end
        end
        stages[#stages+1]=node(graph,"stage:"..(stage.shape or stage.command),stage.command,parents)
        if pipe.matchable and #graph<=40 and (#stages>=3 or linked) then
          local entry=index_graph(state,slice(graph,stages[#stages]),sid,linked and first or call.n,call,trace)
          updated=updated or entry~=nil
          if entry and #stages==#pipe.stages and pipe.routine then entry.routine=pipe.routine end
        end
      end
      if call.graph_matches and #call.graph_matches>prior_matches+1 then
        local longest=call.graph_matches[#call.graph_matches]
        while #call.graph_matches>prior_matches do table.remove(call.graph_matches) end
        call.graph_matches[#call.graph_matches+1]=longest
      end
    end
    if updated then magi.store_set(store_key(),state) end
  end

  graph_recall = function(terms,request_id)
    local best,score,routine
    for _,entry in pairs(stored(store_key()).entries or {}) do
      local overlap=request_overlap(entry,terms,request_id)
      if entry.hits>=2 and overlap>=2 then
        local titles,cross,selected={},false,false
        for _,v in ipairs(entry.graph) do
          if v.role~="argument" then titles[#titles+1]=v.title end
          for _,edge in ipairs(v.parents) do
            if edge[2]:match("^selected:") then selected=true end
            if edge[2]~="pipe" and not edge[2]:match("^arg:") then cross=true end
          end
        end
        local rank=overlap*100+#titles
        local text=table.concat(titles," → ")
        if selected then text=text.." (use the items selected for the current task)" end
        if cross and (not score or rank>score or (rank==score and text<best)) then best,score,routine=text,rank,entry.routine end
      end
    end
    if routine then
      local ok,r=pcall(load_routine,routine)
      local receipt=stored("routine_learned_write:"..routine:sub(7))
      if ok and r and receipt.workspace==magi.workdir() and receipt.digest==r.file_sha256 and not lifecycle_state(stored(lifecycle_key())[routine],r) then
        best=best..". For the pipeline step, inspect routine_catalog with query "..routine
      end
    end
    return best
  end
end

magi.on("user_message", function(ev)
  if type(ev) ~= "table" or type(ev.session) ~= "string" or ev.session == "" then return end
  local t = load_trace(ev.session)
  t.turn = t.turn + 1
  t.request_terms, t.request_id = request_terms(ev.text)
  t.suggestion_prompt = type(ev.text) == "string" and magi.sha256(magi.workdir() .. "\0suggestion\0" .. ev.text) or nil
  magi.store_set(trace_key(ev.session), t)
  magi.store_set("routine_last_session", ev.session)
end)

magi.on("tool_finished", function(ev)
  if type(ev) ~= "table" or type(ev.session) ~= "string" or ev.session == "" or type(ev.tool) ~= "string" then return end
  local t = load_trace(ev.session)
  local pattern, verb, dependencies, pipe_graphs
  local file_digest
  if ev.tool == "read" or ev.tool == "write" or ev.tool == "edit" or ev.tool == "multiedit" then
    local ok, args = pcall(magi.json_decode, ev.args or "")
    if ok and type(args) == "table" and type(args.path) == "string" and args.path ~= "" then
      file_digest = magi.sha256(magi.workdir() .. "\0" .. args.path)
    end
  end
  local issued_job, job_source
  if ev.job_ref and ev.job_ref ~= "" then
    if ev.tool == "bash" and ev.background == "true" then
      issued_job = ev.job_ref
    elseif ev.tool == "wait_for" or ev.tool == "bash_output" then
      for i = #t.calls, 1, -1 do
        if t.calls[i].issued_job == ev.job_ref then job_source = t.calls[i].n; break end
      end
    end
  end
  local glob_source, complete = observe_file_loop(t, ev, file_digest)
  if complete then learn_file_loop(complete, ev) end
  if ev.tool == "bash" and ev.ok == "true" and ev.background ~= "true" then
    local pipelines
    pattern, verb, dependencies, pipelines, pipe_graphs = shell_pattern(ev.args)
    local inventory=ev.inventory_items and ev.inventory_items~="" and magi.json_decode(ev.inventory_items)
    for _,graph in ipairs(pipe_graphs or {}) do
      local items={}
      for _,value in ipairs(inventory or {}) do items[value]=true end
      for _,node in ipairs(graph.stages) do
        if inventory then
          local seen={}
          for j,operand in ipairs(node.operands) do
            local key=magi.sha256(magi.workdir().."\0"..operand)
            seen[key]=true
            if items[key] and node.slots then
              node.item_slots=node.item_slots or {}
              node.item_slots[#node.item_slots+1]=node.slots[#node.slots-#node.operands+j]
            end
          end
          local hits=0
          for _,value in ipairs(inventory) do if seen[value] then hits=hits+1 end end
          if hits>0 then node.matched=hits; node.total=#inventory end
        end
        node.operands=nil
      end
    end
    local covered=observe_pipeline_families(pipelines, ev, t)
    for _,pipe in ipairs(pipe_graphs or {}) do pipe.routine=covered and covered[pipe.shape] end
    observe_shell_dependencies(ev.session, ev.call_id ~= "" and ev.call_id or t.next, dependencies, t, covered)
  end
  if (ev.authored_model or "") ~= "" and t.request_id then
    if ev.tool=="run_routine" then
      local ok,a=pcall(magi.json_decode,ev.args)
      if ok and type(a)=="table" and type(a.name)=="string" and a.name:match("^write%-%x+$") and (a.args == nil or type(a.args)=="table" and not next(a.args)) then
        record_shell_cost(a.name,"wrapper",ev,t)
      end
    elseif ev.tool=="bash" and ev.background~="true" then
      local measured_pattern, edges = pattern, dependencies
      if not measured_pattern then local label; measured_pattern,label,edges=shell_pattern(ev.args) end
      for _,d in ipairs(edges or {}) do
        if d.key==measured_pattern and d.chain then
          local id=magi.sha256("shell chain\n"..magi.workdir().."\n"..d.key.."\n"..magi.json_encode(d.chain)):sub(1,20)
          record_shell_cost("write-"..id,"direct",ev,t)
          break
        end
      end
    end
  end
  t.calls[#t.calls + 1] = { n = t.next, turn = t.turn, tool = ev.tool, ok = ev.ok == "true",
    pipe_graphs=pipe_graphs and #pipe_graphs>0 and pipe_graphs or nil, shell_pattern = pattern, shell_verb = verb, file_digest = file_digest, glob_source = glob_source,
    inventory_read = ev.inventory_read ~= "" and ev.inventory_read or nil, inventory_produced = ev.inventory_produced ~= "" and ev.inventory_produced or nil,
    issued_job = issued_job, job_source = job_source,
    call_id = ev.call_id, parallel_batch = ev.parallel_batch, parallel_size = tonumber(ev.parallel_size),
    response = (ev.response_id or "") ~= "" and ev.response_id or nil,
    order_known = ev.order_known == "true" and (ev.parallel_batch or "") == "" }
  graph_observe(t,ev.session)
  t.next = t.next + 1
  while #t.calls > TRACE_KEEP do table.remove(t.calls, 1) end
  magi.store_set(trace_key(ev.session), t)
  learn_completed_writes(ev.session, t)
end)

magi.on("turn_finished", function(ev)
  if type(ev) ~= "table" or type(ev.session) ~= "string" or ev.session == "" then return end
  note_repeats(ev.session)

end)

local ROUTINE_USE = [[
Run a saved code-mode routine by name. A matching harness suggestion can supply the complete call. Otherwise use routine_catalog for use cases, inputs, results and errors. For a routine discovered from a tool sequence, use it only if you can supply every step's arguments from the current request and known context. If a later step needs an earlier result to determine its arguments, use ordinary tools instead. Learned writing routines use the normal tool permissions. Other routines ask for approval on first run and when changed.]]

magi.register_tool{
  name = "routine_catalog",
  description = "Find saved routines, when to use them, input schemas, expected outputs and failure conditions. Pass query to search names, tools, descriptions, and wording associated with learned tasks. Returns up to 30 matches.",
  schema = { type = "object", properties = {query = {type = "string"}} },
  execute = function(args)
    local query = args and args.query or ""
    if type(query) ~= "string" then return "query must be a string", true end
    local words = {}
    for word in query:lower():gmatch("%S+") do words[#words + 1] = word end
    local associated, represented = {}, {}
    local terms,request_id=request_terms(query)
    local graphs=stored("routine_shell_dependencies:"..magi.sha256(magi.workdir()):sub(1,20))
    local state=stored(lifecycle_key())
    for _,entry in pairs(graphs.entries or {}) do
      if entry.routine then
        local representative
        if entry.covered_by then
          local r=load_routine(entry.covered_by)
          local receipt=stored("routine_learned_write:"..entry.covered_by:sub(7))
          if r and receipt.workspace==magi.workdir() and receipt.digest==r.file_sha256 and not lifecycle_state(state[r.name],r) then
            represented[entry.routine],representative=true,r.name
          end
        end
        local score=request_overlap(entry.runnable_requests,terms,request_id)
        local name=representative or entry.routine
        if score>=2 then associated[name]=math.max(score,associated[name] or 0) end
      end
    end
    if next(terms) then
      for id,entry in pairs(stored("routine_shapes")) do
        local score=request_overlap(entry,terms,request_id)
        if entry.write_saved and score>=2 then associated["write-"..id]=score end
      end
    end
    local routines = {}
    local lifecycle = state
    local names = routine_names()
    if #words > 0 then
      local exact = #words == 1 and words[1]
      table.sort(names, function(a, b)
        local left, right = a:lower() == exact, b:lower() == exact
        if left ~= right then return left end
        local sa, sb = associated[a] or 0, associated[b] or 0
        if sa ~= sb then return sa > sb end
        return a < b
      end)
    end
    for _, name in ipairs(names) do
      local ok, r = pcall(load_routine, name)
      if ok and r and r.status ~= "draft" and not lifecycle_state(lifecycle[name], r) and (not represented[name] or query==name) then
        local matches = true
        if #words > 0 then
          local text = (name .. " " .. r.description .. " " .. table.concat(r.tools, " ")):lower()
          for _, word in ipairs(words) do
            if not text:find(word, 1, true) then matches = false; break end
          end
        end
        local filter = r.status == "learned-write" and pipeline_filter(r.description)
        if filter then
          for key, role in pairs(filter.roles) do
            local prop = (r.inputs.properties or {})[key]
            if prop then prop.description = role end
          end
        end
        if matches or associated[name] or (filter and filter_intent(query, filter.source)) then
          local usage=routine_usage(r.description,r.inputs,r.source)
          routines[#routines + 1] = { name = name, usage=usage, description = filter and (usage.when .. " " .. r.description) or r.description, tools = r.tools, inputs = r.inputs, status = r.status }
          if #routines == 30 then break end
        end
      end
    end
    return magi.json_encode(routines)
  end,
}

local function reader_intents(prompt)
  prompt = prompt:lower()
  local words = " " .. prompt:gsub("[^a-z]+", " ") .. " "
  return {
    read = words:find(" read ", 1, true) or prompt:find("읽", 1, true),
    grep = words:find(" grep ", 1, true) or words:find(" search ", 1, true) or prompt:find("검색", 1, true),
    glob = words:find(" glob ", 1, true) or words:find(" matching files ", 1, true) or prompt:find("파일 찾아", 1, true),
    list = words:find(" list ", 1, true) or prompt:find("목록", 1, true),
  }
end

-- The context provider can also be queried by tooling. Only announce a
-- suggestion for the user prompt that actually opened this session turn.
local function note_suggestion(query, name, description)
  if not query.session_id or query.session_id == "" or type(magi.notify) ~= "function" then return end
  local t = load_trace(query.session_id)
  if t.suggestion_prompt ~= magi.sha256(magi.workdir() .. "\0suggestion\0" .. query.prompt) then return end
  if t.suggested_name == name or t.suggested_turn == t.turn then return end
  local ok, sent = pcall(magi.notify, query.session_id, "[routine] Suggested: " .. description .. " (" .. name .. ")")
  if ok and sent then
    t.suggested_name, t.suggested_turn = name, t.turn
    magi.store_set(trace_key(query.session_id), t)
  end
end

-- Older hosts and stripped-down rigs may omit context providers. The catalog tool remains available.
do
local function catalog_provide(query)
    if not query or query.workdir ~= magi.workdir() or type(query.prompt) ~= "string" then return {} end
    local intents = reader_intents(query.prompt)
    local prompt = query.prompt:lower()
    local words = " " .. prompt:gsub("[^a-z]+", " ") .. " "
    local writing = words:find(" write ", 1, true) or words:find(" edit ", 1, true)
      or words:find(" create ", 1, true) or words:find(" update ", 1, true)
      or prompt:find(" 수정", 1, true) or prompt:find(" 생성", 1, true) or prompt:find("만들", 1, true)
    local command_words = {}
    for word in prompt:gmatch("[%w_.%-]+") do command_words[word] = true end
    local state = stored(lifecycle_key())
    local terms, request_id = request_terms(query.prompt)
    local shapes = stored("routine_shapes")
    local candidates = {}
    local note_name, note_description
    for _, name in ipairs(routine_names()) do
      if name:match("^auto%-t%x+$") then
        local ok, r = pcall(load_routine, name)
        if ok and r and r.status == "auto-review" and not lifecycle_state(state[name], r) then
          local matches = true
          for _, tool in ipairs(r.tools) do if not intents[tool] then matches = false; break end end
          if matches and (#r.tools > 1 or (r.inputs.properties or {}).step2) then
            candidates[#candidates + 1] = name .. ": " .. r.description
            if #candidates == 3 then break end
          end
        end
      end
    end
    if #candidates < 3 then
      local writing_matches = {}
      for _, name in ipairs(routine_names()) do
        if name:match("^write%-%x+$") then
          local ok, r = pcall(load_routine, name)
          local shell = ok and r and r.description:match("bash %(([^)]+)%)")
          local matches_shell = false
          local overlap = request_overlap(shapes[name:sub(7)], terms, request_id)
          if shell then
            for verb in shell:gmatch("[%w_.%-]+") do
              if command_words[verb:lower()] then matches_shell = true; break end
            end
          end
          if ok and r and r.status=="learned-write" and not lifecycle_state(state[name],r) and measured_shell_match(name,r,request_id,query.model) then
            writing_matches[#writing_matches+1]={name=name,description=r.description,overlap=50,ready=true}
          elseif ok and r and r.status == "learned-write" and (r.inputs.properties or {}).step2
            and not r.description:find("^Conditional shell workflow:") and not r.description:find("^Shell pipeline:")
            and (overlap >= 2 or matches_shell or (writing and not shell and not r.description:find("bash", 1, true)
              and (not r.description:find("^Write and read the same file:") or intents.read))) then
            writing_matches[#writing_matches + 1] = { name = name, description = r.description, overlap = overlap,
              same_file = r.description:find("^Write and read the same file:") ~= nil,
              single_path = r.inputs.required and #r.inputs.required == 1 and r.inputs.required[1] == "step1" }
          end
        end
      end
      table.sort(writing_matches, function(left, right)
        if left.overlap ~= right.overlap then return left.overlap > right.overlap end
        if left.same_file ~= right.same_file then return left.same_file end
        return left.name < right.name
      end)
      for _, match in ipairs(writing_matches) do
        local writer = match.description:match("^Write and read the same file: (%w+) > read")
        if match.ready then
          candidates[#candidates+1]="For this request the measured routine needs no arguments: run_routine "..magi.json_encode({name=match.name})..". "..match.description
          note_name,note_description=match.name,match.description
          break
        elseif writer and match.single_path then
          candidates[#candidates + 1] = "For a request to " .. writer ..
            " and read back the same file, use one run_routine tool call. Set name to " .. match.name ..
            " and args.step1 to the arguments for " .. writer .. ". The result includes the file read back from disk."
          if match.overlap >= 2 and intents.read then
            note_name, note_description = match.name, match.description
            break
          end
        else
          candidates[#candidates + 1] = match.name .. ": " .. match.description
        end
        if #candidates == 3 then break end
      end
    end
    if #candidates == 0 then
      local flow=graph_recall(terms,request_id)
      if flow then return {{source="automatic-routine-catalog",text="Earlier matching tasks used this observed dependency flow: "..flow..". Choose current inputs from this request."}} end
      return {}
    end
    if note_name then note_suggestion(query, note_name, note_description) end
    return {{source="automatic-routine-catalog",text="Use run_routine when all steps and inputs match.\n"..table.concat(candidates,"\n")}}
end

-- The routines worth listing, ranked by value: what reaches the model as skills (sync_routine_skills) and
-- what /routine-skills shows. Read from disk each time. Capped at 10 entries or 1.5KB, whichever comes
-- first; idle maintenance keeps the active set inside that budget. count: this is a request, so each
-- listed routine is charged one exposure (once per request).
local function routine_index(count)
  local key = "routine_index:" .. magi.sha256(magi.workdir())
  local idx = stored(key)
  local life = stored(lifecycle_key())
  local rows = {}
  for _, name in ipairs(routine_names()) do
    local ok, r = pcall(load_routine, name)
    if ok and r and r.status ~= "draft" and not lifecycle_state(life[name], r) then
      local e = type(idx[name]) == "table" and idx[name] or {}
      if e.version ~= r.fingerprint then e = { version = r.fingerprint } end
      idx[name] = e
      local sv = RV.of(r)
      -- a learned routine that saves no model call is not worth a line in every turn
      if not e.dropped and not (sv and sv.saved <= 0) then
        rows[#rows + 1] = { name = name, r = r, e = e, sv = sv }
      end
    end
  end
  if #rows == 0 then return nil, {} end
  local now = lifecycle_now()
  local prior = RV.prior(idx, now)
  for _, row in ipairs(rows) do row.value = RV.value(row.e, row.sv, now, prior) end
  -- Overlapping learned routines are counted once: when one learned stretch contains another, list only
  -- the one worth more over its occurrences (observed occurrences plus recent runs, times saved calls).
  -- Decided heaviest first, and only a routine already kept can hide another: one that is hidden itself
  -- does not take a third out of the list with it.
  for _, row in ipairs(rows) do
    row.weight = row.sv and row.sv.saved * ((row.sv.occ or 0) + RV.score(row.e, now)) or 0
  end
  local order = {}
  for i, row in ipairs(rows) do order[i] = row end
  table.sort(order, function(a, b)
    if a.weight ~= b.weight then return a.weight > b.weight end
    return a.name < b.name
  end)
  local keep = {}
  for _, a in ipairs(order) do
    local hidden = false
    for _, b in ipairs(keep) do
      if a.sv and b.sv and a.sv.sig and b.sv.sig and
        (RV.contains(a.sv.sig, b.sv.sig) or RV.contains(b.sv.sig, a.sv.sig)) then hidden = true; break end
    end
    if not hidden then keep[#keep + 1] = a end
  end
  rows = keep
  table.sort(rows, function(a, b)
    if a.value ~= b.value then return a.value > b.value end
    return a.name < b.name
  end)
  local head = "# Saved routines (call run_routine by name when one fits this task; routine_catalog searches every saved routine)"
  local lines, size, shown, listed = { head }, #head, 0, {}
  for _, row in ipairs(rows) do
    local d = (row.r.description or ""):gsub("%s+", " ")
    if #d > 100 then d = (d:sub(1, 97):gsub("[\192-\255][\128-\191]*$", "")) .. "..." end
    local line = "- " .. row.name .. ": " .. d
    if row.sv then
      line = line .. " (saves " .. row.sv.saved .. " model call" .. (row.sv.saved == 1 and "" or "s") .. " per run" ..
        (row.sv.basis == "observed" and "" or ", estimated") .. ")"
    end
    if shown == 10 or size + 1 + #line + 80 > 1500 then break end
    lines[#lines + 1], size, shown = line, size + 1 + #line, shown + 1
    listed[#listed + 1] = row
    row.e.line_tokens = math.ceil(#line / 4)
    if count then
      row.e.exposed = (row.e.exposed or 0) + 1
      row.e.xscore, row.e.xat = RV.decay(row.e.xscore, row.e.xat, now) + 1, now
    end
  end
  if shown < #rows then lines[#lines + 1] = "(" .. (#rows - shown) .. " more: search with routine_catalog)" end
  magi.store_set(key, idx)
  return table.concat(lines, "\n"), listed
end

-- Routines reach the model as skills: each listed routine under .magi/routines gets a one-line skill file
-- .magi/skills/<name>.routine.md that says to call run_routine directly. The host freezes the skill list
-- at session start and re-freezes it at compaction; a file added in between is announced in one line and
-- a file removed in between stays named until then, so the head is not rewritten mid-session. A routine
-- that already is a skill file is not listed twice. Only files carrying the mark are ever removed.
-- Arguments taken from a previous result. The idle review reads past turns (magi.recent_tool_steps) and
-- finds a value the model read off one call's result and passed to the next call: a token on a fixed spot
-- (line and field near the top, or right after a fixed word). Seen twice with different values, the pair
-- becomes a two-step routine that saves the model response in between. Only hashes are stored.
do
  local LK = { TOOLS = { read = true, grep = true, glob = true, list = true, bash = true }, PH = "{{value}}" }
  LK.EXTRACT = [[local function extract(text, rule)
  if rule.pattern then
    local ok, v = pcall(string.match, text or "", rule.pattern)
    return ok and v or nil
  end
  local l = 0
  for line in ((text or "") .. "\n"):gmatch("([^\n]*)\n") do
    l = l + 1
    local fs = {}
    for f in line:gmatch("%S+") do fs[#fs + 1] = f end
    if rule.line and l == rule.line then return fs[rule.field] end
    if rule.after then
      for k = 1, #fs - 1 do if fs[k] == rule.after then return fs[k + 1] end end
    end
  end
end
]]
  -- The same reading the routine script does (LK.EXTRACT), for checking a proposed rule here.
  function LK.extract(text, rule)
    if rule.pattern then
      local ok, v = pcall(string.match, text or "", rule.pattern)
      return ok and v or nil
    end
    local l = 0
    for line in ((text or "") .. "\n"):gmatch("([^\n]*)\n") do
      l = l + 1
      local fs = {}
      for f in line:gmatch("%S+") do fs[#fs + 1] = f end
      if rule.line and l == rule.line then return fs[rule.field] end
      if rule.after then
        for k = 1, #fs - 1 do if fs[k] == rule.after then return fs[k + 1] end end
      end
    end
  end
  -- No fixed spot fits: ask the model for a rule, from the results it was taken from. Kept only if it
  -- reads every observed value back exactly; a failure, refusal or malformed answer is dropped.
  function LK.ask(list)
    local parts = {}
    for i, o in ipairs(list) do
      if i > 3 then break end
      parts[#parts + 1] = "Result " .. i .. ":\n" .. o.output:sub(1, 2000) .. "\nValue taken from it: " .. o.value
    end
    local ok, raw = pcall(magi.analyze, { label = "routine-link", text = table.concat(parts, "\n\n"),
      system = "Each result below is a tool output and the value a later call took from it. Give one rule that " ..
        "reads that value from any such output, as JSON only: {\"line\":N,\"field\":K} (whitespace fields), " ..
        "{\"after\":\"word\"} (the token after a fixed word) or {\"pattern\":\"Lua pattern with one capture\"}." })
    if not ok or type(raw) ~= "string" then return end
    local text = raw:match("%b{}")
    if not text then return end
    local okd, rule = pcall(magi.json_decode, text)
    if not okd or type(rule) ~= "table" then return end
    local clean = {}
    if type(rule.pattern) == "string" and #rule.pattern <= 200 then clean.pattern = rule.pattern
    elseif type(rule.after) == "string" and #rule.after <= 24 and not rule.after:find("%s") then clean.after = rule.after
    elseif tonumber(rule.line) and tonumber(rule.field) then
      clean.line, clean.field = math.floor(tonumber(rule.line)), math.floor(tonumber(rule.field))
    else return end
    for _, o in ipairs(list) do
      if LK.extract(o.output, clean) ~= o.value then return end
    end
    return clean
  end
  function LK.position(text, value)
    local l = 0
    for line in ((text or "") .. "\n"):gmatch("([^\n]*)\n") do
      l = l + 1
      if l > 40 then return end
      local fs = {}
      for f in line:gmatch("%S+") do fs[#fs + 1] = f end
      for k, f in ipairs(fs) do
        if f == value then
          if l <= 5 then return { line = l, field = k } end
          local w = fs[k - 1]
          if w and #w <= 24 and w:match("^%a[%w_%-]*:?$") then return { after = w } end
          return
        end
      end
    end
  end
  -- The second call's arguments with the value as a placeholder; the value must be one whole token of one field.
  function LK.template(args, value)
    local key, out, hits = nil, {}, 0
    for k, v in pairs(args) do
      out[k] = v
      if type(v) == "string" and v:find(LK.PH, 1, true) then return end
      if type(v) == "string" then
        for start, tok, stop in v:gmatch("()(%S+)()") do
          if tok == value then
            hits, key = hits + 1, k
            out[k] = v:sub(1, start - 1) .. LK.PH .. v:sub(stop)
          end
        end
      end
    end
    if hits == 1 then return key, out end
  end
  function LK.candidates(steps)
    local found = {}
    for j, b in ipairs(steps) do
      if LK.TOOLS[b.name] and type(b.args) == "table" and not b.pending and not b.failed then
        local said = {}
        for i = 1, j - 1 do
          if steps[i].turn == b.turn then said[#said + 1] = magi.json_encode(steps[i].args or {}) end
        end
        local done = false
        for _, v in pairs(b.args) do
          if done or type(v) ~= "string" then goto next_field end
          for tok in v:gmatch("%S+") do
            if done then break end
            if #tok >= 4 and #tok <= 200 and tok:match("%w") and not tok:match("^%-") then
              local known = false
              for _, text in ipairs(said) do if text:find(tok, 1, true) then known = true; break end end
              if not known then
                for i = j - 1, 1, -1 do
                  local a = steps[i]
                  if a.turn ~= b.turn then break end
                  if LK.TOOLS[a.name] and not a.failed and not a.pending and type(a.output) == "string" then
                    if not a.response or not b.response or a.response == b.response
                      or (a.result_order or 0) <= 0 or a.result_order >= (b.call_order or 0) then break end
                    local rule = LK.position(a.output, tok)
                    local key, tmpl = LK.template(b.args, tok)
                    if rule and LK.extract(a.output, rule) ~= tok then rule = nil end
                    if key and b.name == "bash" and (key ~= "command" or magi.platform()=="windows"
                      or not magi.bind_shell_operand(tmpl.command, tok)) then key = nil end
                    if key and (rule or a.output:find(tok, 1, true)) then
                      found[#found + 1] = { a = a, b = b, rule = rule, key = key, tmpl = tmpl, value = tok,
                        output = a.output }
                      done = true
                    end
                    break
                  end
                end
              end
            end
          end
          ::next_field::
        end
      end
    end
    return found
  end
  function LK.script(c)
    local bind = c.b.name == "bash" and 'args.command = assert(bind_shell_operand(s, value))'
      or ("args[" .. string.format("%q", c.key) .. "] = s:sub(1, i - 1) .. value .. s:sub(i + " .. #LK.PH .. ")")
    return LK.EXTRACT .. "local first = assert(call_tool(" .. string.format("%q", c.a.name) .. ", json_decode(" ..
      string.format("%q", magi.json_encode(c.a.args or {})) .. ")))\n" ..
      "local value = extract(first, json_decode(" .. string.format("%q", magi.json_encode(c.rule)) .. "))\n" ..
      "if not value then error(\"no value at the learned position of the " .. c.a.name .. " result\") end\n" ..
      "local args = json_decode(" .. string.format("%q", magi.json_encode(c.tmpl)) .. ")\n" ..
      "local s = args[" .. string.format("%q", c.key) .. "]\nlocal i = s:find(" .. string.format("%q", LK.PH) .. ", 1, true)\n" ..
      bind .. "\n" ..
      "print(first)\nprint(assert(call_tool(" .. string.format("%q", c.b.name) .. ", args)))"
  end
  function LK.save(c, sig, occ)
    local name = "link-" .. sig:sub(1, 12)
    local script = LK.script(c)
    local tools = { c.a.name }
    if c.b.name ~= c.a.name then tools[2] = c.b.name end
    local fixture = c.output
    if LK.extract(fixture, c.rule) ~= c.value then return end
    local _, err, replay = magi.run_script(script, { dry = true, tools = tools, fixtures = { { tool = c.a.name, text = fixture } } })
    if err or type(replay) ~= "table" or #replay ~= 2 or replay[2].tool ~= c.b.name then return end
    local expected = {}
    for k, v in pairs(c.b.args) do expected[k] = v end
    if c.b.name == "bash" then
      expected.command = magi.bind_shell_operand(c.tmpl.command, c.value)
      if not expected.command then return end
    end
    local ok, got = pcall(magi.json_decode, replay[2].args)
    if not ok or magi.json_encode(got) ~= magi.json_encode(expected) then return end
    local where = c.rule.line and ("line " .. c.rule.line .. " field " .. c.rule.field)
      or c.rule.after and ("the token after " .. c.rule.after) or "the match of a pattern"
    local def = { script = script, tools = tools, inputs = '{"type":"object","properties":{}}',
      purpose = "Runs " .. c.a.name .. ", takes " .. where .. " of its result and passes it to " .. c.b.name .. "." }
    local path = save_routine(name, "Value from a result: " .. c.a.name .. " then " .. c.b.name .. " (" .. c.key .. " from " .. where .. ")",
      def, "result value reused v2 " .. sig:sub(1, 20), "passed: the second call received the value", false, false, true)
    if not path then return end
    RV.record(name, {{tool=c.a.name, response=c.a.response}, {tool=c.b.name, response=c.b.response}}, nil, occ)
    magi.log("[routine] saved " .. path .. "; it takes an argument from the previous result")
    return true
  end
  function LK.learn(state)
    local ok, past = pcall(magi.recent_tool_steps, 8)
    if not ok or type(past) ~= "table" then return end
    state.links, state.seen = type(state.links) == "table" and state.links or {}, type(state.seen) == "table" and state.seen or {}
    local n = 0
    for _ in pairs(state.seen) do n = n + 1 end
    if n > 2000 then state.seen = {} end
    local pend = {}
    for _, sess in ipairs(past) do
      for _, c in ipairs(LK.candidates(sess.steps or {})) do
        local sig = magi.sha256(magi.json_encode({ "causal-v2", c.a.name, c.a.args or {}, c.b.name, c.key, c.tmpl, c.rule or "model" }))
        if not c.rule then
          -- kept in memory for this review only, to show the model where the values came from
          local list = pend[sig] or {}
          local vh, dup = magi.sha256(c.value):sub(1, 16), false
          for _, o in ipairs(list) do if o.vh == vh then dup = true end end
          if not dup then list[#list + 1] = { output = c.output, value = c.value, vh = vh, c = c } end
          pend[sig] = list
        end
        local at = magi.sha256(tostring(sess.session) .. "#" .. tostring(c.b.turn) .. "#" .. c.b.call_order .. "#" .. sig):sub(1, 20)
        if not state.seen[at] then
          state.seen[at] = true
          local link = type(state.links[sig]) == "table" and state.links[sig] or { values = {}, n = 0 }
          link.values[magi.sha256(c.value):sub(1, 16)] = true
          link.n = link.n + 1
          local distinct = 0
          for _ in pairs(link.values) do distinct = distinct + 1 end
          if c.rule and distinct >= 2 and not link.saved then link.saved = LK.save(c, sig, link.n) end
          state.links[sig] = link
        end
      end
    end
    for sig, list in pairs(pend) do
      local link = state.links[sig]
      if #list >= 2 and type(link) == "table" and not link.saved and not link.asked then
        link.asked = true
        local rule = LK.ask(list)
        if rule then
          local last = list[#list]
          last.c.rule = rule
          link.saved = LK.save(last.c, sig, link.n)
        end
      end
    end
  end
  magi.on("idle", function()
    if magi.workdir() == "" then return end
    local key = "routine_links_v2:" .. magi.sha256(magi.workdir())
    local state = stored(key)
    local now = lifecycle_now()
    if now and state.last and now - state.last < 3600 then return end
    state.last = now
    pcall(LK.learn, state)
    magi.store_set(key, state)
  end)
end

local SKILL_MARK = "source: routine-skill"
local function routine_skill_text(row)
  local d = (row.r.description or ""):gsub("%s+", " ")
  if #d > 100 then d = (d:sub(1, 97):gsub("[\192-\255][\128-\191]*$", "")) .. "..." end
  local saves = ""
  if row.sv then
    saves = " (saves " .. row.sv.saved .. " model call" .. (row.sv.saved == 1 and "" or "s") .. " per run" ..
      (row.sv.basis == "observed" and "" or ", estimated") .. ")"
  end
  local names = {}
  for k in pairs(type(row.r.inputs.properties) == "table" and row.r.inputs.properties or {}) do names[#names + 1] = k end
  table.sort(names)
  local args = #names > 0 and (" with args " .. table.concat(names, ", ")) or ""
  return "---\ndescription: Saved routine: " .. d .. saves .. ". Run it directly: run_routine " ..
    magi.json_encode({ name = row.name }) .. args .. "; no need to load this skill.\n" .. SKILL_MARK .. "\n---\n" ..
    "Advertises the saved routine " .. row.name .. " (" .. row.r.path .. "). Call run_routine with its name.\n"
end
local function is_routine_skill(text)
  local fm = type(text) == "string" and frontmatter(text)
  return fm and fm.source == "routine-skill"
end
local function sync_routine_skills(listed)
  local want = {}
  for _, row in ipairs(listed or {}) do
    if row.r.path:sub(1, #ROUTINES_DIR + 1) == ROUTINES_DIR .. "/" then want[row.name] = routine_skill_text(row) end
  end
  for name, text in pairs(want) do
    local path = LEGACY_SKILLS_DIR .. "/" .. name .. ".routine.md"
    local previous = magi.read_file(path)
    if previous == nil then
      magi.write_file(path, text, true)
    elseif previous ~= text and is_routine_skill(previous) then
      magi.write_file(path, text)
    end
  end
  for _, entry in ipairs(magi.list_files(LEGACY_SKILLS_DIR) or {}) do
    local name = entry:match("^([%w_%-]+)%.routine%.md$")
    if name and not want[name] then
      local path = LEGACY_SKILLS_DIR .. "/" .. entry
      local text = magi.read_file(path)
      if is_routine_skill(text) then magi.remove_file(path) end
    end
  end
end

magi.on("user_message", function(ev)
  if type(ev) ~= "table" or type(ev.session) ~= "string" or ev.session == "" then return end
  pcall(function()
    local _, listed = routine_index(true)
    sync_routine_skills(listed)
  end)
end)

magi.register_command{
  name = "routine-skills",
  description = "Show the saved routines offered to the model as skills, in value order",
  execute = function()
    local ok, text = pcall(routine_index, false)
    magi.log(ok and text or "# Saved routines: none listed")
  end,
}

pcall(magi.register_context_provider, { name = "automatic-routine-catalog", provide = catalog_provide })
end

local observe_routine_result
local function plan_reader_scopes(r, src, args)
  if r.script == EACH_FILE then
    local search = args and args.search
    if type(search) ~= "table" or type(search.pattern) ~= "string" then return nil, "glob pattern required" end
    local scope, err = magi.reader_scope("glob", magi.json_encode(search))
    if not scope then return nil, err end
    return {scope, "read-matched:" .. scope:sub(6) .. ":" .. magi.sha256(magi.json_encode(search))}
  end
  if type(magi.reader_scope) ~= "function" then return nil, "automatic routines require an updated host" end
  local _, err, calls = magi.run_script(src, { dry = true, tools = r.tools, timeout = 10 })
  if err or type(calls) ~= "table" or #calls == 0 then return nil, "automatic routine could not plan its file reads" end
  local result, seen = {}, {}
  for _, call in ipairs(calls) do
    local scope, why = magi.reader_scope(call.tool, call.args)
    if not scope then return nil, why end
    if not seen[scope] then result[#result + 1], seen[scope] = scope, true end
  end
  table.sort(result)
  return result
end

local function covers_reader_scopes(approved, planned)
  if type(approved) ~= "table" then return false end
  for _, scope in ipairs(planned) do
    local covered = false
    for _, limit in ipairs(approved) do
      local kind, path = scope:match("^([^:]+):(.+)$")
      local allowed_kind, allowed_path = limit:match("^([^:]+):(.+)$")
      if scope == limit or (path and allowed_path and allowed_kind ~= "file"
        and (kind == allowed_kind or (allowed_kind == "dir" and kind == "file"))
        and path:sub(1, #allowed_path + 1) == allowed_path .. "/") then
        covered = true
        break
      end
    end
    if not covered then return false end
  end
  return true
end

magi.register_tool{
  name = "run_routine",
  description = ROUTINE_USE,
  schema = {
    type = "object",
    properties = {
      expected_digest = { type = "string", description = "Optional exact definition digest for a planned call" },
      name = { type = "string", description = "the routine's name (its skill file name)" },
      args = { type = "object", description = "the routine's inputs, as its inputs schema says" },
      timeout = { type = "number", description = "seconds, default 60, at most 300" },
      reuse_read_directory = { type = "boolean", description = "Only when the user explicitly requests reusable folder permission for an automatic routine, ask them to approve the folder instead of each file. Omit for file-specific approval." },
      output = { type = "number", description = "bytes of printed output kept, default 16000, at most 64000" },
    },
    required = { "name" },
  },
  execute = function(a)
    local r, err = load_routine(a and a.name)
    if not r then return err, true end
    if a.expected_digest and a.expected_digest ~= r.file_sha256 then return "routine changed since inventory", true end
    if r.status == "draft" then
      return "routine " .. r.name .. " is a draft (status: draft in " .. r.path .. "): review it, resolve its TODOs and " ..
        "delete the status line first; nothing ran", true
    end
    local writes = {}
    for _, t in ipairs(r.tools) do
      if r.status == "auto-review" and not AUTO_READERS[t] then return "automatic routines only support file readers", true end
      if WRITES[t] then writes[#writes + 1] = t end
    end
    local bad = check_inputs(r.inputs, a.args)
    if bad then return "routine " .. r.name .. ": " .. bad, true end
    local src = "local input = json_decode(" .. string.format("%q", magi.json_encode(a.args or {})) .. ")\n" .. r.script
    local reader_scopes
    if r.status == "auto-review" then
      reader_scopes, bad = plan_reader_scopes(r, src, a.args)
      if not reader_scopes then return bad, true end
    end
    local approval_scopes = reader_scopes
    if reader_scopes and a.reuse_read_directory == true then
      local seen = {}
      approval_scopes = {}
      for _, scope in ipairs(reader_scopes) do
        local parent = scope:match("^file:(.+)/[^/]+$")
        local grant = parent and "dir:" .. parent or scope
        if not seen[grant] then
          approval_scopes[#approval_scopes + 1], seen[grant] = grant, true
        end
      end
      table.sort(approval_scopes)
    end
    local explore_scope = stored("routine_explore_scopes")[r.name .. ":" .. r.fingerprint:sub(1, 12)]
    if explore_scope and not (magi.script_features and magi.script_features.explore_read_scope) then
      return "scoped explore requires an updated magi host; nothing ran", true
    end
    local all = stored("routine_approvals")
    local prev = type(all[r.name]) == "table" and all[r.name] or nil
    local learned_key = r.status == "learned-write" and r.name:match("^write%-(%x+)$")
    local learned = learned_key and stored("routine_learned_write:" .. learned_key)
    local generated = learned and learned.workspace == magi.workdir() and learned.digest == r.file_sha256
    local learned_graph = generated and r.source and r.source:match("^repeated shell graph (%x+)$")
    if learned_graph then
      local _, dry_error, calls = magi.run_script(src, {dry=true,tools=r.tools})
      local matched = false
      if not dry_error and calls and #calls == 1 and calls[1].tool == "bash" then
        local _, _, graphs = shell_pattern(calls[1].args)
        for _, graph in ipairs(graphs or {}) do
          if graph.key == learned_graph and graph.chain then matched = true end
        end
      end
      if not matched then return "arguments change the observed shell command structure; nothing ran", true end
    end
    local scope_changed = approval_scopes and (not prev or prev.reader_scope_version ~= 2 or prev.workspace ~= magi.workdir()
      or not covers_reader_scopes(prev.reader_scopes, approval_scopes))
    if not generated and (not prev or prev.fingerprint ~= r.fingerprint or scope_changed) then
      local why = prev and ("routine " .. r.name .. " changed since it was approved") or ("run routine " .. r.name .. " for the first time")
      if prev and prev.fingerprint == r.fingerprint and scope_changed then why = "approve new read scope for routine " .. r.name end
      if approval_scopes then why = why .. " within " .. table.concat(approval_scopes, ", ") end
      if r.script == EACH_FILE then
        why = why .. "; read each file returned by the proposed glob search"
      end
      local ok, aerr = magi.approve{
        reason = why .. " — it may call: " .. table.concat(r.tools, ", ") .. (#writes > 0 and (" — it may WRITE with " ..
          table.concat(writes, ", ") .. "; each write still asks as usual, one at a time") or ""),
        detail = { routine = r.name, path = r.path, tools = r.tools, writes = writes, inputs = r.inputs, script = r.script,
          proposed_args = r.status == "auto-review" and a.args or nil,
          reader_scopes = approval_scopes,
          fingerprint = r.fingerprint, previous = prev and prev.fingerprint or nil, explore_read_scope = explore_scope },
      }
      if ok == nil then return "routine " .. r.name .. " could not be approved: " .. tostring(aerr), true end
      if not ok then return "routine " .. r.name .. " was not approved; nothing ran", true end
      all[r.name] = { fingerprint = r.fingerprint, tools = r.tools, path = r.path, at = magi.time(),
        workspace = approval_scopes and magi.workdir() or nil, reader_scopes = approval_scopes,
        reader_scope_version = approval_scopes and 2 or nil }
      magi.store_set("routine_approvals", all)
    end
    local globals, on_call, finish = track()
    local out, rerr = magi.run_script(src, {
      timeout = a.timeout, output = a.output, globals = globals, on_call = on_call,
      child = "routine " .. r.name, tools = r.tools, serial = writes,
      explore_read_scope = explore_scope,
      reader_scopes = reader_scopes,
    })
    finish(rerr == nil)
    record_routine_use(r, rerr == nil)
    do -- value: successful runs decay; a failed run costs the call it wasted
      local key = "routine_index:" .. magi.sha256(magi.workdir())
      local idx, now = stored(key), lifecycle_now()
      local e = type(idx[r.name]) == "table" and idx[r.name] or {}
      if e.version ~= r.fingerprint then e = { version = r.fingerprint } end
      e.score, e.at = RV.score(e, now) + (rerr == nil and 1 or 0), now
      -- only a run that succeeded brings a dropped routine back to the per-turn index
      if rerr == nil then e.dropped = nil end
      e.runs = (tonumber(e.runs) or 0) + (rerr == nil and 1 or 0)
      if rerr ~= nil then e.failed = (tonumber(e.failed) or 0) + 1 end
      idx[r.name] = e
      magi.store_set(key, idx)
    end
    if observe_routine_result then
      local observed, why = pcall(observe_routine_result, r, a.args or {}, rerr and "" or (out or ""))
      if not observed then magi.log("routine failure observation failed: " .. tostring(why)) end
    end
    if rerr then return (out or "") .. "[" .. rerr .. "]", true end
    return out
  end,
}

magi.register_tool{
  name = "routine_compare_writes",
  description = "Compare the active and candidate scripts on separate copies of the same explicit text fixtures. Runs local read/write/edit/multiedit only and reports errors and changed file hashes.",
  schema = { type = "object", properties = {
    name = { type = "string" }, script = { type = "string" },
    files = { type = "object", additionalProperties = { type = "string" } },
    args = { type = "object" },
  }, required = { "name", "script", "files" } },
  execute = function(a)
    if not (magi.script_features and magi.script_features.file_fixture) then
      return "isolated writes require an updated magi host; nothing ran", true
    end
    local r, err = load_routine(a and a.name)
    if not r then return err, true end
    if type(a.script) ~= "string" or type(a.files) ~= "table" then return "candidate script and file fixtures required", true end
    local writable = false
    for _, tool in ipairs(r.tools) do
      if tool == "write" or tool == "edit" or tool == "multiedit" then writable = true
      elseif tool ~= "read" then return "file experiments do not support " .. tool, true end
    end
    if not writable then return "the routine declares no supported writing tool", true end
    local bad = check_inputs(r.inputs, a.args)
    if bad then return bad, true end
    local prefix = "local input = json_decode(" .. string.format("%q", magi.json_encode(a.args or {})) .. ")\n"
    local active, active_err, active_files, active_failures = magi.run_script(prefix .. r.script, { file_fixture = a.files, tools = r.tools })
    if not active_files then return tostring(active_err), true end
    local candidate, candidate_err, candidate_files, candidate_failures = magi.run_script(prefix .. a.script, { file_fixture = a.files, tools = r.tools })
    if not candidate_files then return tostring(candidate_err), true end
    local changed = {}
    for path, hash in pairs(active_files) do if candidate_files[path] ~= hash then changed[#changed + 1] = path end end
    for path in pairs(candidate_files) do if not active_files[path] then changed[#changed + 1] = path end end
    table.sort(changed)
    local result = { active = { output = active, error = active_err, files = active_files, failed_calls = active_failures },
      candidate = { output = candidate, error = candidate_err, files = candidate_files, failed_calls = candidate_failures },
      differing_files = changed, same_file_state = #changed == 0 }
    return magi.json_encode(result)
  end,
}

local function words(args)
  if type(args) == "table" then
    local out = {}
    for _, v in ipairs(args) do for w in tostring(v):gmatch("%S+") do out[#out + 1] = w end end
    return out
  end
  local out = {}
  for w in tostring(args or ""):gmatch("%S+") do out[#out + 1] = w end
  return out
end

magi.register_command{
  name = "routine-candidates",
  description = "List routine candidates found in repeated dummy plug runs (review before /routine-register)",
  execute = function(_)
    local lines = {}
    for id, c in pairs(load_candidates()) do
      lines[#lines + 1] = id .. "  " .. tostring(c.sequence) .. "  ×" .. tostring(c.count) .. "  " .. tostring(c.goal) ..
        "\n  tools: " .. table.concat(c.tools or {}, ", ") .. (c.script and ("\n  script:\n" .. c.script) or "")
    end
    table.sort(lines)
    if #lines == 0 then magi.log("[routine] no routine candidates"); return end
    magi.log("[routine] candidates:\n" .. table.concat(lines, "\n\n"))
  end,
}

magi.register_command{
  name = "routine-register",
  description = "Save a reviewed candidate as .magi/routines/<name>/routine.md after a dry check (/routine-register <id> <name>)",
  execute = function(args)
    local w = words(args)
    local id, name = w[1], w[2]
    local cands = load_candidates()
    local c = id and cands[id]
    if type(c) ~= "table" then return "no routine candidate " .. tostring(id) .. " (see /routine-candidates)" end
    local path, why
    if c.kind == "trace" then
      path, why = draft_trace(name, c.calls or {}, "repeated stretch " .. id, c.goal)
    else
      path, why = draft_script(name, c.script, c.tools, "routine candidate " .. id, c.goal)
    end
    if not path then return why end
    cands[id] = nil
    magi.store_set("routine_candidates", cands)
    magi.log("[routine] " .. why)
  end,
}

-- The conversation a trace command reads: the one the command was typed in (an IDE or web client
-- names it), else, in the TUI, the conversation that last finished a tool call.
local function command_sid()
  local sid = type(magi.command_session) == "function" and magi.command_session() or ""
  if sid ~= "" then return sid end
  return magi.store_get("routine_last_session")
end

magi.register_command{
  name = "routine-trace",
  description = "List this conversation's recent tool calls and dummy_plug runs, to pick from for /routine-draft",
  execute = function(args)
    local n = tonumber(words(args)[1]) or 30
    local sid = command_sid()
    local lines = {}
    if type(sid) == "string" and sid ~= "" then
      local t = load_trace(sid)
      for i = math.max(1, #t.calls - n + 1), #t.calls do
        local c = t.calls[i]
        lines[#lines + 1] = string.format("#%d turn %s %s%s%s", c.n, tostring(c.turn), tostring(c.tool),
          c.ok == false and " (failed)" or "",
          c.parallel_batch and c.parallel_batch ~= "" and (" (parallel batch " .. c.parallel_batch .. ")")
            or (not c.order_known and " (order unknown)" or "")) ..
          (c.glob_source and (" (path matched glob #" .. c.glob_source .. ")") or "") ..
          (c.job_source and (" (job issued by bash #" .. c.job_source .. ")") or "")
        for _,match in ipairs(c.graph_matches or {}) do lines[#lines+1]="  dependency graph "..match end
        for _,graph in ipairs(c.pipe_graphs or {}) do
          local stages={}
          for _,node in ipairs(graph.stages) do
            stages[#stages+1]=node.command..(node.matched and (" ["..node.matched.."/"..node.total.." inventory items]") or "")
          end
          lines[#lines+1]="  #"..c.n.." statement "..graph.statement..": "..table.concat(stages," | ")
        end
      end
    end
    local runs = stored("routine_runs")
    for _, r in ipairs(type(runs.list) == "table" and runs.list or {}) do
      lines[#lines + 1] = "run " .. tostring(r.id) .. " " .. tostring(r.goal or "") .. " (" .. table.concat(r.tools or {}, ", ") .. ")"
    end
    if #lines == 0 then magi.log("[routine] nothing traced yet"); return end
    magi.log("[routine] trace:\n" .. table.concat(lines, "\n"))
  end,
}

magi.register_command{
  name = "routine-draft",
  description = "Save tool calls <from>-<to> of /routine-trace, or a dummy_plug run (run:<id>), as a routine draft checked dry (/routine-draft <name> <from>-<to> | run:<id>)",
  execute = function(args)
    local w = words(args)
    local name, what = w[1], w[2]
    local usage = "usage: /routine-draft <name> <from>-<to> | run:<id> (numbers and ids from /routine-trace)"
    if not name or not what then return usage end
    local path, why
    local rid = what:match("^run:(r%x+)$")
    if rid then
      local run
      local runs = stored("routine_runs")
      for _, r in ipairs(type(runs.list) == "table" and runs.list or {}) do
        if r.id == rid then run = r end
      end
      if not run then return "no dummy_plug run " .. rid .. " (see /routine-trace)" end
      path, why = draft_script(name, run.script, run.tools, "run " .. rid, run.goal)
    else
      local a, b = what:match("^(%d+)%-(%d+)$")
      if not a then a = what:match("^(%d+)$"); b = a end
      a, b = tonumber(a), tonumber(b)
      if not a or not b or b < a then return usage end
      local sid = command_sid()
      if type(sid) ~= "string" or sid == "" then return "no conversation traced yet" end
      local t = load_trace(sid)
      local calls = {}
      for _, c in ipairs(t.calls) do
        if c.n >= a and c.n <= b then
          if JUDGE[c.tool] then return "call #" .. c.n .. " (" .. tostring(c.tool) .. ") is not a step a routine repeats" end
          calls[#calls + 1] = c
        end
      end
      if #calls ~= b - a + 1 then return "calls #" .. a .. "-" .. b .. " are not all in the trace any more (see /routine-trace)" end
      path, why = draft_trace(name, calls, "trace " .. sid .. " #" .. a .. "-" .. b,
        "calls #" .. a .. "-" .. b .. " of " .. sid)
    end
    if not path then return why end
    magi.log("[routine] " .. why)
  end,
}

-- Improving a routine. A candidate version (its script; never its tools, inputs or evals) is checked
-- against the routine's frozen contract — fixed fixtures, run dry, so control flow is what is checked —
-- and only then paired with the active version on the same frozen tasks. Each task's transition is
-- judged on its own (newly solved, newly regressed, still failing, kept, unstable, unverified); a
-- protected task lost or a broken contract rejects, a thin or unstable gain holds, and only an
-- applicable candidate can be adopted, by a person, with the previous version kept for rollback.
-- Every attempt is appended to .magi/routines/<name>/attempts.jsonl and read back for the next one.
-- Nothing here needs Engram or a model.
do
  local IMPROVE_P = 0.10
  local EVAL_POLICY = "holdout-v1"
  -- Data reach: the arguments a script passes to the tools that read data. A delegation covers the
  -- reach the person saw; explore takes free text, so its reach is not compared.
  local REACH = { read = true, grep = true, glob = true, list = true, recall_memory = true }
  local REACH_KEYS = { "path", "pattern", "glob", "include", "query", "file", "dir" }
  local function reach_key(tool, args)
    local ok, v = pcall(magi.json_decode, tostring(args or ""))
    local parts = { tostring(tool) }
    if ok and type(v) == "table" then
      for _, k in ipairs(REACH_KEYS) do
        if v[k] ~= nil then parts[#parts + 1] = k .. "=" .. tostring(v[k]) end
      end
    else
      parts[#parts + 1] = tostring(args)
    end
    return table.concat(parts, " ")
  end



  local function decode(text)
    if type(text) ~= "string" then return nil end
    local ok, v = pcall(magi.json_decode, text)
    if ok and type(v) == "table" then return v end
    return nil
  end

  local function read_lines(path)
    local text, out = magi.read_file(path), {}
    if type(text) ~= "string" then return out end
    for line in text:gmatch("[^\n]+") do
      local v = decode(line)
      if v then out[#out + 1] = v end
    end
    return out
  end

  local function append_line(path, rec)
    local text = magi.read_file(path)
    if type(text) ~= "string" then text = "" end
    return magi.write_file(path, text .. magi.json_encode(rec) .. "\n")
  end

  local function version_of(script, inputs, tools)
    local canon = script .. "\n\0inputs\0" .. magi.json_encode(inputs) .. "\0tools\0" .. table.concat(tools, ",")
    return magi.sha256(canon):sub(1, 12)
  end

  local function noop_globals()
    local g, out = track(), {}
    for k in pairs(g) do out[k] = function() end end
    return out
  end

  local function dry_run(script, input, fixtures, tools, timeout)
    local src = "local input = json_decode(" .. string.format("%q", magi.json_encode(input or {})) .. ")\n" .. script
    local out, err, calls = magi.run_script(src, { dry = true, fixtures = fixtures or {}, tools = tools, timeout = timeout or 10,
      globals = noop_globals() })
    calls = type(calls) == "table" and calls or {}
    local unverified = false
    for _, c in ipairs(calls) do
      if c.unverified then unverified = true end
    end
    return tostring(out or ""), err, calls, unverified
  end

  local function check_case(c, script, tools, timeout)
    local out, err, calls, unv = dry_run(script, c.input, c.fixtures, tools, timeout)
    local why = {}
    if err then why[#why + 1] = "script failed: " .. tostring(err) end
    local want_calls = type(c.calls) == "table" and c.calls or {}
    local want_out = type(c.print_contains) == "table" and c.print_contains or {}
    if #want_calls > 0 then
      if #calls ~= #want_calls then why[#why + 1] = "made " .. #calls .. " calls, expected " .. #want_calls end
      for i, e in ipairs(want_calls) do
        local got = calls[i]
        if got and (got.tool ~= e.tool or (e.contains and not tostring(got.args):find(e.contains, 1, true))) then
          why[#why + 1] = "call " .. i .. " is " .. tostring(got.tool) .. (e.contains and (", expected one carrying " .. e.contains) or "")
        end
      end
    end
    for _, s in ipairs(want_out) do
      if not out:find(s, 1, true) then why[#why + 1] = "output lacks " .. s end
    end
    if #why > 0 then return "fail", table.concat(why, "; ") end
    if unv then return "unverified", "a call no fixture answers" end
    if #want_calls == 0 and #want_out == 0 then return "unverified", "nothing checked" end
    return "pass", ""
  end

  local function task_once(tk, script, tools, cost)
    local out, err, calls, unv = dry_run(script, tk.input, tk.fixtures, tools)
    cost.calls = cost.calls + #calls
    for _, c in ipairs(calls) do
      if c.tool == "explore" then cost.simulated_explore_calls = cost.simulated_explore_calls + 1 end
      if REACH[c.tool] and cost.reach then cost.reach[reach_key(c.tool, c.args)] = true end
      -- explore takes free text: its arguments are kept whole and compared exactly.
      if c.tool == "explore" and cost.explore then cost.explore[tostring(c.args)] = true end
    end
    if unv then return "unverified" end
    local exp = type(tk.expect) == "table" and type(tk.expect.evidence) == "table" and tk.expect.evidence or {}
    if #exp == 0 then return "unverified" end
    local ok = err == nil
    for _, e in ipairs(exp) do
      if not out:find(e, 1, true) then ok = false end
    end
    return ok and "pass" or "fail"
  end

  local function lifecycle_log(r, action, reason, canonical)
    return append_line(rdir(r.name) .. "decisions.jsonl", { at = magi.time(), action = action,
      reason = reason, routine = r.name, version = r.fingerprint:sub(1, 12), canonical = canonical, by = "lifecycle" })
  end

  local function comparable_fixtures(r)
    local frozen = stored("routine_evals_frozen")[r.name]
    local dir = rdir(r.name)
    local contract, tasks = magi.read_file(dir .. "contract.json"), magi.read_file(dir .. "tasks.json")
    if not frozen or not contract or not tasks or magi.sha256(contract) ~= frozen.contract
      or magi.sha256(tasks) ~= frozen.tasks then return end
    contract, tasks = decode(contract), decode(tasks)
    if not contract or type(contract.cases) ~= "table" or #contract.cases == 0 or #contract.cases > 4
      or not tasks or type(tasks.tasks) ~= "table" or #tasks.tasks < 2 or #tasks.tasks > 4 then return end
    local dev, holdout = false, false
    for _, task in ipairs(tasks.tasks) do
      if type(task) ~= "table" then return end
      dev = dev or task.split == "dev"
      holdout = holdout or task.split == "holdout"
    end
    if dev and holdout then return contract.cases, tasks.tasks, frozen.contract .. ":" .. frozen.tasks end
  end

  local function equivalent_on_fixtures(a, b, contracts, tasks)
    for _, c in ipairs(contracts) do
      if type(c) ~= "table" or check_case(c, a.script, a.tools, 0.05) ~= "pass"
        or check_case(c, b.script, b.tools, 0.05) ~= "pass" then return false end
    end
    for _, task in ipairs(tasks) do
      local expected = type(task.expect) == "table" and task.expect.evidence
      if type(expected) ~= "table" or #expected == 0 then return false end
      local first, err, calls, unverified = dry_run(a.script, task.input, task.fixtures, a.tools, 0.05)
      local second, other_err, other_calls, other_unverified = dry_run(b.script, task.input, task.fixtures, b.tools, 0.05)
      if err or other_err or unverified or other_unverified or #calls == 0 or first ~= second
        or magi.json_encode(calls) ~= magi.json_encode(other_calls) then return false end
      for _, evidence in ipairs(expected) do
        if type(evidence) ~= "string" or evidence == "" or not first:find(evidence, 1, true) then return false end
      end
    end
    return true
  end

  magi.on("idle", function()
    local now = lifecycle_now()
    if not now or magi.workdir() == "" then return end
    local key = lifecycle_key()
    local state = stored(key)
    if state.last_review and now - state.last_review < 3600 then return end
    state.last_review = now
    do -- listed 20 times and worth nothing by the same value the index ranks by: leave the per-turn index
      -- (still runs by name, still in routine_catalog; a later run brings it back)
      local ikey = "routine_index:" .. magi.sha256(magi.workdir())
      local idx, dropped = stored(ikey), false
      local prior = RV.prior(idx, now)
      for name, e in pairs(idx) do
        if type(e) == "table" and not e.dropped and (e.exposed or 0) >= 20 then
          local ok, r = pcall(load_routine, name)
          local sv = ok and r and RV.of(r) or nil
          if RV.value(e, sv, now, prior) <= 0 then e.dropped, dropped = true, true end
        end
      end
      if dropped then magi.store_set(ikey, idx) end
    end
    local approvals = stored("routine_approvals")
    local groups, compared, seen = {}, false, {}
    for _, name in ipairs(routine_names()) do
      local ok, r = pcall(load_routine, name)
      if ok and r and name:match("^auto%-t%x+$") and r.status == "auto-review" then
        seen[name] = true
        local entry = lifecycle_entry(state, r, now)
        if (entry.status == "paused" or entry.status == "archived") and entry.last_used
          and entry.last_used >= (entry.hidden_at or 0) and entry.failures == 0
          and lifecycle_log(r, "catalog restored", "explicit execution succeeded") then
          entry.status, entry.hidden_at = nil, nil
        end
        local status, reason
        if not entry.keep then
          if (entry.failures or 0) >= 3 then status, reason = "paused", "three consecutive execution failures"
          elseif now - (entry.last_used or entry.since) >= 30 * 86400 then status, reason = "archived", "no execution for 30 days" end
        end
        if status and entry.status == nil and lifecycle_log(r, "catalog " .. status, reason) then
          entry.status, entry.canonical, entry.hidden_at = status, nil, now
        end
        local scope = approvals[name]
        if not scope or scope.fingerprint ~= r.fingerprint then scope = {} end
        local group = magi.sha256(magi.json_encode({ tools = r.tools, inputs = r.inputs, workspace = scope.workspace,
          scopes = scope.reader_scopes }))
        entry.group = group
        if not entry.status then
          local peers = groups[group] or {}
          groups[group] = peers
          for _, peer in ipairs(peers) do
            local canonical = peer.r
            local identical = canonical.fingerprint == r.fingerprint
            local equivalent
            if not identical and not compared and not entry.keep then
              local ac, at, ah = comparable_fixtures(canonical)
              local bc, bt, bh = comparable_fixtures(r)
              local comparison = ah and bh and magi.sha256(canonical.fingerprint .. r.fingerprint .. ah .. bh)
              entry.comparisons = entry.comparisons or {}
              if comparison and entry.comparisons[canonical.name] ~= comparison then
                compared = true
                equivalent = equivalent_on_fixtures(canonical, r, ac, at) and equivalent_on_fixtures(canonical, r, bc, bt)
                if lifecycle_log(r, "catalog compared", equivalent and "same calls and results on both frozen suites" or "equivalence not established", canonical.name) then
                  entry.comparisons[canonical.name] = comparison
                end
              end
            end
            if not entry.keep and (identical or equivalent) and lifecycle_log(r, "catalog alias",
              identical and "same script, inputs, tools and approval scope" or "same calls and results on both frozen suites", canonical.name) then
              entry.status, entry.canonical, entry.canonical_version = "alias", canonical.name, canonical.fingerprint
              break
            end
          end
          if not entry.status then peers[#peers + 1] = { r = r } end
        end
      end
    end
    for name, entry in pairs(state) do
      if name ~= "last_review" then
        if not seen[name] then state[name] = nil
        elseif entry.status == "alias" then
          local canonical = state[entry.canonical]
          if not canonical or canonical.status or canonical.version ~= entry.canonical_version or canonical.group ~= entry.group then
            entry.status, entry.canonical, entry.canonical_version, entry.comparisons = nil, nil, nil, nil
          end
        end
      end
    end
    magi.store_set(key, state)
  end)

  magi.register_command{
    name = "routine-lifecycle",
    description = "Show automatic routine catalog state, or restore a hidden entry (/routine-lifecycle [restore <name>])",
    execute = function(args)
      local w = words(args)
      local key = lifecycle_key()
      local state = stored(key)
      if w[1] == "restore" and w[2] and not w[3] then
        local r, err = load_routine(w[2])
        if not r then return err end
        local entry = state[r.name]
        if not lifecycle_state(entry, r) then return "routine is already visible" end
        if not lifecycle_log(r, "catalog restored", "requested by user") then return "could not record restoration" end
        entry.status, entry.canonical, entry.canonical_version, entry.hidden_at, entry.keep = nil, nil, nil, nil, true
        if not magi.store_set(key, state) then return "could not save restoration" end
        magi.log("[routine] restored " .. r.name .. " to the catalog")
        return
      end
      if w[1] then return "usage: /routine-lifecycle [restore <name>]" end
      local lines = {}
      for name, entry in pairs(state) do
        if type(entry) == "table" then
          lines[#lines + 1] = name .. "  " .. (entry.status or "active") ..
            (entry.canonical and (" → " .. entry.canonical) or "") .. "  runs=" .. tostring(entry.runs or 0) ..
            "  consecutive failures=" .. tostring(entry.failures or 0)
        end
      end
      table.sort(lines)
      magi.log(#lines > 0 and table.concat(lines, "\n") or "no automatic routine lifecycle records")
    end,
  }

  local function task_result(tk, script, tools, reps, cost)
    local first, stable = nil, true
    for _ = 1, reps do
      local res = task_once(tk, script, tools, cost)
      if first == nil then first = res elseif first ~= res then stable = false end
    end
    if not stable then return "unstable" end
    return first
  end

  local function transition(b, c)
    if b == "unstable" or c == "unstable" then return "unstable" end
    if b == "unverified" or c == "unverified" then return "unverified" end
    if b == "fail" and c == "pass" then return "solved" end
    if b == "pass" and c == "fail" then return "regressed" end
    if b == "fail" then return "still_failing" end
    return "kept"
  end

  local function live_evidence(out, scope)
    local found, invalid, checked = {}, {}, {}
    for line in out:gmatch("[^\r\n]+") do
      if not line:match("^%s*#") and not line:match("^%s*%(no answer:") then
        for path, number in line:gmatch("([%w_./%-]+):(%d+)") do
          local ref = path .. ":" .. number
          if not checked[ref] then
            checked[ref] = true
            local local_path = path:sub(1, 1) ~= "/"
            for part in path:gmatch("[^/]+") do
              if part == ".." or part == "." then local_path = false end
            end
            local n = tonumber(number)
            if local_path and n and n <= 1048576 and magi.scoped_line_exists(scope, path, n) then found[ref] = true end
            if not found[ref] then invalid[#invalid + 1] = ref end
          end
        end
      end
    end
    table.sort(invalid)
    return found, invalid
  end

  local TRACE_FAMILY = "client-daemon-call-trace"
  local TRACE_DIR = rdir(TRACE_FAMILY)
  local TRACE_LESSON = "cite-executable-handoff"
  local TRACE_LESSON_DETAILS = {
    id = TRACE_LESSON,
    condition = "The task must establish a call between two components or verify a claim about an executable path.",
    technique = "Cite the actual call at each handoff, with its scope-relative path and line.",
    benefit = "Separates a dispatch label from the statement that performs the action.",
    limitation = "A valid citation cannot establish whether a particular runtime action succeeded.",
  }
  local TRACE_CLIENTS = { web = true, vscode = true, jetbrains = true }
  local TRACE_ACTIONS = { recovery = true, cancellation = true, approval = true }
  local TRACE_FAMILIES = {
    [TRACE_FAMILY] = { description = "Trace a client action through its handlers and daemon calls.",
      actions = TRACE_ACTIONS, instruction = "Cite the exact executable calls as workspace-relative path:line." },
    ["implementation-evidence"] = { description = "Verify a claim about implemented behavior against the executable statements.",
      actions = { claim = true }, instruction = "Cite the executable statement for each check as workspace-relative path:line. State what remains unverified." },
  }

  local function trace_lessons()
    return decode(magi.read_file(TRACE_DIR .. "lessons.json")) or {}
  end

  local function trace_run(id)
    if type(id) ~= "string" or not id:match("^%x+$") then return nil end
    return decode(magi.read_file(TRACE_DIR .. "runs/" .. id .. ".json"))
  end

  local function trace_runs()
    local runs = {}
    for _, file in ipairs(magi.list_files(TRACE_DIR .. "runs") or {}) do
      local id = file:match("^(%x+)%.json$")
      local run = id and trace_run(id)
      if run then runs[#runs + 1] = run end
    end
    return runs
  end

  magi.register_tool{
    name = "routine_task_families",
    description = "Find read-only routine task families and applicable lessons before planning a request. Does not run a routine or call a model.",
    schema = { type = "object", properties = {} },
    execute = function()
      local promoted = trace_lessons()[TRACE_LESSON]
      local families = {}
      for id, family in pairs(TRACE_FAMILIES) do
        local actions = {}
        for action in pairs(family.actions) do actions[#actions + 1] = action end
        table.sort(actions)
        families[#families + 1] = { id = id, description = family.description, actions = actions,
          lesson = (id == TRACE_FAMILY or promoted) and TRACE_LESSON or nil,
          lesson_status = promoted and "corroborated" or (id == TRACE_FAMILY and "bundled" or nil),
          guidance = (id == TRACE_FAMILY or promoted) and TRACE_LESSON_DETAILS or nil }
      end
      table.sort(families, function(a, b) return a.id < b.id end)
      return magi.json_encode(families)
    end,
  }

  local TRACE_POLICY = { ["whole-v1"] = true, ["handoff-v1"] = true }
  -- A run uses the policy it read before approval. Replay uses the same decision function.
  local function trace_next(policy, proved, total)
    if policy == "whole-v1" then return proved == 0 and "whole" or "done" end
    return proved < total and "handoff" or "done"
  end

  local function trace_policy()
    local raw = magi.read_file(TRACE_DIR .. "policy.json")
    if raw == nil then return { active = "whole-v1", previous = {} } end
    local config = decode(raw)
    if config and TRACE_POLICY[config.active] and type(config.previous) == "table" then return config end
    return nil
  end

  local TRACE_SCRIPT = [[
local checked = 0
while true do
  local decision = next_trace_step(checked)
  if decision == "done" then break end
  local q = {input.question, input.instruction}
  if decision == "whole" then
    q[#q + 1] = "Trace these handoffs: " .. table.concat(input.handoffs, "; ")
  else
    q[#q + 1] = "Investigate only this handoff: " .. input.handoffs[checked + 1]
  end
  local answer, err = call_tool("explore", { question = table.concat(q, "\n"), depth = "quick" })
  if not answer then error(err or "explore returned no answer") end
  print(answer)
  if decision == "whole" then
    check_trace_evidence(answer, 0)
    break
  end
  if not check_trace_evidence(answer, checked + 1) then break end
  checked = checked + 1
end
]]

  local trace_tool = {
    name = "routine_trace_path",
    description = "Trace a client-to-daemon call path once with the active, approved read-only policy. Supply the task-specific question and ordered handoffs with source references to check, and use the existing task budget.",
    schema = { type = "object", properties = {
      question = { type = "string" }, directory = { type = "string" },
      family = { type = "string", enum = { TRACE_FAMILY, "implementation-evidence" } },
      client = { type = "string", enum = { "web", "vscode", "jetbrains" } },
      action = { type = "string", enum = { "recovery", "cancellation", "approval", "claim" } },
      handoffs = { type = "array", items = { type = "object", properties = {
        label = { type = "string" }, evidence = { type = "string", description = "Expected scope-relative path:line, if known" },
      }, required = { "label" } } },
      request_limit = { type = "integer", minimum = 1, maximum = 64 },
      timeout = { type = "number", minimum = 1, maximum = 300 },
      reported_token_limit = { type = "integer", minimum = 0, maximum = 1000000 },
    }, required = { "question", "directory", "client", "action", "handoffs", "request_limit", "timeout" } },
    execute = function(a, policy_override, split)
      if not (magi.script_features and magi.script_features.explore_request_limit) then
        return "call-path tracing requires an updated magi host", true
      end
      a = a or {}
      local family_id = a.family or TRACE_FAMILY
      local family = TRACE_FAMILIES[family_id]
      if not family or not TRACE_CLIENTS[a.client] or not family.actions[a.action] then
        return "a supported client and action are required", true
      end
      local scope, timeout, cap = a.directory, a.timeout, a.request_limit
      local tokens = a.reported_token_limit or 0
      if type(scope) ~= "string" or not scope:match("^[%w_.%-%/]+$") or scope:sub(1, 1) == "/" or scope == "." then
        return "a workspace subdirectory is required", true
      end
      for part in scope:gmatch("[^/]+") do
        if part == ".." or part == "." then return "use a workspace subdirectory", true end
      end
      if type(cap) ~= "number" or cap % 1 ~= 0 or cap < 1 or cap > 64 or type(timeout) ~= "number" or timeout < 1 or timeout > 300
        or type(tokens) ~= "number" or tokens % 1 ~= 0 or tokens < 0 or tokens > 1000000 then
        return "invalid execution budget", true
      end
      if type(a.question) ~= "string" or a.question == "" or #a.question > 1600 or type(a.handoffs) ~= "table"
        or #a.handoffs < 1 or #a.handoffs > 4 then
        return "provide a question and one to four ordered handoffs", true
      end
      local labels, expected, planned = {}, {}, {}
      for i, step in ipairs(a.handoffs) do
        if type(step) ~= "table" or type(step.label) ~= "string" or step.label == "" or #step.label > 200
          or (step.evidence ~= nil and (type(step.evidence) ~= "string" or not step.evidence:match("^[%w_./%-]+:%d+$"))) then
          return "invalid handoff " .. i, true
        end
        labels[i], expected[i] = step.label, step.evidence or false
        planned[i] = step.evidence and "check-reference" or "investigate"
      end
      local config = trace_policy()
      if not config then return "invalid call-path policy file", true end
      local policy = policy_override or config.active
      local id = magi.nonce()
      local lesson = (family_id == TRACE_FAMILY or trace_lessons()[TRACE_LESSON]) and TRACE_LESSON or nil
      local instruction = family.instruction
      if lesson and family_id ~= TRACE_FAMILY then
        instruction = instruction .. " Follow each handoff to the actual call, beyond its dispatch label."
      end
      local version = magi.sha256(TRACE_SCRIPT .. "\0" .. family_id .. "\0" .. instruction)
      local key = magi.sha256(magi.workdir() .. "\n" .. scope .. "\n" .. version .. "\n" .. policy)
      local approvals = stored("trace_policy_approvals")
      if not policy_override and approvals[key] ~= true then
        local ok, why = magi.approve{ reason = "Run read-only call-path policy " .. policy .. " in " .. scope,
          detail = { family = family_id, lesson = lesson, policy = policy, scope = scope, script = TRACE_SCRIPT, instruction = instruction, version = version, tools = {"explore"} } }
        if not ok then return tostring(why or "call-path policy was not approved"), true end
        approvals[key] = true
        local saved, err = magi.store_set("trace_policy_approvals", approvals)
        if not saved then return tostring(err), true end
      end

      local path = TRACE_DIR .. "runs/" .. id .. ".json"
      local record = { id = id, phase = "started", family = family_id, policy = policy, source_routine = "routine_trace_path",
        routine_version = version, lesson = lesson, session = magi.session_id(), planned = planned,
        started_at = magi.time(),
        client = a.client, action = a.action, scope = scope, evaluation_split = split,
        request_limit = cap, timeout = timeout, reported_token_limit = tokens }
      local saved, err = magi.write_file(path, magi.json_encode(record))
      if not saved then return "could not record call-path plan: " .. tostring(err), true end

      local decisions, verified, checked, citations, diagnostics = {}, {}, {}, {}, {}
      local src = "local input = json_decode(" .. string.format("%q", magi.json_encode{ question = a.question, handoffs = labels, instruction = instruction }) .. ")\n" .. TRACE_SCRIPT
      local out, failure, metrics = magi.run_script(src, {
        tools = {"explore"}, explore_read_scope = scope, measure_explore = true,
        explore_request_limit = cap, explore_token_limit = tokens, timeout = timeout,
        globals = {
          next_trace_step = function(count)
            local decision = trace_next(policy, count, #labels)
            decisions[#decisions + 1] = decision
            return decision
          end,
          check_trace_evidence = function(answer, index)
            local found, invalid = live_evidence(answer, scope)
            local rejected = {}
            for _, ref in ipairs(invalid) do rejected[ref] = true end
            local refs = {}
            for ref in pairs(found) do refs[#refs + 1] = ref end
            table.sort(refs)
            local function preview(items)
              local result = {}
              for i = 1, math.min(#items, 8) do result[i] = items[i]:sub(1, 240) end
              return result
            end
            local observation = { handoff = index, resolved = preview(refs), rejected = preview(invalid),
              resolved_count = #refs, rejected_count = #invalid, checks = {} }
            diagnostics[#diagnostics + 1] = observation
            local function check(i)
              checked[i] = true
              if expected[i] then
                verified[i] = found[expected[i]] == true
              else
                verified[i] = next(found) ~= nil
              end
              observation.checks[#observation.checks + 1] = { handoff = i,
                expected = expected[i] and expected[i]:sub(1, 240) or nil,
                outcome = verified[i] and "verified" or
                  (rejected[expected[i]] and "rejected" or "missing") }
              if verified[i] then
                if expected[i] then citations[i] = expected[i]
                else
                  local refs = {}
                  for ref in pairs(found) do refs[#refs + 1] = ref end
                  table.sort(refs)
                  citations[i] = refs[1]
                end
              end
              return verified[i]
            end
            if index == 0 then
              for i = 1, #labels do check(i) end
              return true
            end
            -- The next handoff needs a citation that resolves in the scoped tree.
            return check(index)
          end,
        },
      })
      record.phase = failure and "failed" or "completed"
      record.decisions, record.handoffs, record.evidence = decisions, {}, {}
      record.completed_at = magi.time()
      record.citation_diagnostics = diagnostics
      record.script_timed_out = failure ~= nil and failure:match("^script stopped at its time limit") ~= nil
      for i = 1, #labels do
        record.handoffs[i] = not checked[i] and "unchecked" or (verified[i] and "verified_reference" or "unverified")
        if citations[i] then record.evidence[#record.evidence + 1] = { handoff = i, citation = citations[i] } end
        if record.phase == "completed" and not verified[i] then record.phase = "partial" end
      end
      if metrics then
        metrics.answers = nil
        record.metrics = metrics
        record.request_limit_reached = type(metrics.model_calls) == "number" and metrics.model_calls >= cap
      end
      saved, err = magi.write_file(path, magi.json_encode(record))
      if not saved then return "call-path run " .. id .. " ended but its outcome could not be recorded: " .. tostring(err), true end
      local message = (out or "") .. "\n[call-path run " .. id .. " · " .. policy .. "] " .. table.concat(record.handoffs, ", ")
      if failure then return message .. "\n" .. failure, true, record end
      return message, false, record
    end,
  }

  magi.register_tool(trace_tool)

  magi.register_command{
    name = "routine-trace-policy",
    description = "Inspect, replay or change the call-path policy (/routine-trace-policy [freeze|replay <run-id>|adopt <comparison-id>|use <version>|rollback])",
    execute = function(args)
      local w, config = words(args), trace_policy()
      if not config then return "invalid call-path policy file" end
      if w[1] == "replay" and w[2] and w[2]:match("^%x+$") then
        local run = decode(magi.read_file(TRACE_DIR .. "runs/" .. w[2] .. ".json"))
        if not run or run.evaluation_split == "holdout" or type(run.handoffs) ~= "table" then return "no finished call-path run " .. w[2] end
        for _, version in ipairs({"whole-v1", "handoff-v1"}) do
          local choices, passed = {}, 0
          for _, status in ipairs(run.handoffs) do
            local step = trace_next(version, passed, #run.handoffs)
            choices[#choices + 1] = step
            if step == "whole" or status ~= "verified_reference" then break end
            passed = passed + 1
          end
          magi.log(version .. ": " .. table.concat(choices, ", ") .. " (decisions only; no tools called)")
        end
        return
      end
      if w[1] == "freeze" and #w == 1 then
        local raw = magi.read_file(TRACE_DIR .. "policy-tasks.json")
        local tasks = decode(raw)
        if not tasks or type(tasks.tasks) ~= "table" then return "write policy-tasks.json with a tasks array first" end
        local saved, err = magi.write_file(TRACE_DIR .. "policy-tasks.sha256", magi.sha256(raw))
        if not saved then return tostring(err) end
        magi.log("call-path policy tasks frozen")
        return
      end
      if w[1] == "adopt" and #w == 2 and w[2]:match("^%x+$") then
        local result = decode(magi.read_file(TRACE_DIR .. "comparisons/" .. w[2] .. ".json"))
        local raw = magi.read_file(TRACE_DIR .. "policy-tasks.json")
        if not result or result.phase ~= "completed" or result.split ~= "holdout" or not raw
          or result.tasks_hash ~= magi.sha256(raw) or result.tasks_hash ~= magi.read_file(TRACE_DIR .. "policy-tasks.sha256")
          or result.holdout_use ~= 1 or result.active ~= config.active or not TRACE_POLICY[result.candidate]
          or result.regressed ~= 0 or type(result.solved) ~= "number" or result.solved < 1 then
          return "adoption requires a fresh holdout comparison with a gain and no regressions"
        end
        for _, row in ipairs(result.rows or {}) do
          if row.protected and row.candidate ~= "completed" then return "a protected task is unresolved" end
        end
        config.previous[#config.previous + 1] = config.active
        config.active = result.candidate
      elseif w[1] == "use" and TRACE_POLICY[w[2]] then
        if config.active ~= w[2] then
          config.previous[#config.previous + 1] = config.active
          config.active = w[2]
        end
      elseif w[1] == "rollback" then
        local previous = table.remove(config.previous)
        if not TRACE_POLICY[previous] then return "no prior call-path policy" end
        config.active = previous
      elseif w[1] == nil then
        magi.log("call-path policy: " .. config.active .. " (available: whole-v1, handoff-v1)")
        return
      else
        return "usage: /routine-trace-policy [freeze|replay <run-id>|adopt <comparison-id>|use <whole-v1|handoff-v1>|rollback]"
      end
      local ok, err = magi.write_file(TRACE_DIR .. "policy.json", magi.json_encode(config))
      if not ok then return tostring(err) end
      magi.log("call-path policy: " .. config.active)
    end,
  }

  magi.register_command{
    name = "routine-lesson",
    description = "Review a shared lesson or promote it with two reviewed run IDs (/routine-lesson [promote <run-id> <run-id>|withdraw])",
    execute = function(args)
      local w, lessons = words(args), trace_lessons()
      if w[1] == nil then
        local distinct, outcomes = {}, {}
        local count, calls, tokens, known = 0, 0, 0, 0
        for _, run in ipairs(trace_runs()) do
          if run.lesson == TRACE_LESSON and run.evaluation_split ~= "holdout" then
            count = count + 1
            local task = tostring(run.client) .. "/" .. tostring(run.action)
            distinct[task] = true
            outcomes[run.phase] = (outcomes[run.phase] or 0) + 1
            if run.metrics then
              calls = calls + (run.metrics.model_calls or 0)
              tokens = tokens + (run.metrics.input_tokens or 0) + (run.metrics.output_tokens or 0)
              if run.metrics.tokens_known then known = known + 1 end
            end
            magi.log(tostring(run.id) .. " " .. task .. " " .. tostring(run.phase))
          end
        end
        local task_count = 0
        for _ in pairs(distinct) do task_count = task_count + 1 end
        magi.log(magi.json_encode({ lesson = TRACE_LESSON_DETAILS, promotion = lessons[TRACE_LESSON],
          runs = count, client_action_groups = task_count, outcomes = outcomes, model_calls = calls,
          reported_tokens = tokens, runs_with_complete_usage = known }))
        return
      elseif w[1] == "promote" and #w == 3 then
        local a, b = trace_run(w[2]), trace_run(w[3])
        local function anchored(run)
          if not run or run.family ~= TRACE_FAMILY or run.lesson ~= TRACE_LESSON or run.phase ~= "completed"
            or run.evaluation_split == "holdout" or type(run.planned) ~= "table" or #run.planned == 0 then return false end
          for _, step in ipairs(run.planned) do if step ~= "check-reference" then return false end end
          return true
        end
        if not anchored(a) or not anchored(b) or (a.client == b.client and a.action == b.action) then
          return "promotion needs two completed, reference-checked runs from different client/action tasks"
        end
        lessons[TRACE_LESSON] = { status = "corroborated", runs = {a.id, b.id}, at = magi.time() }
      elseif w[1] == "withdraw" and #w == 1 then
        lessons[TRACE_LESSON] = nil
      else
        return "usage: /routine-lesson [promote <run-id> <run-id>|withdraw]"
      end
      local ok, err = magi.write_file(TRACE_DIR .. "lessons.json", magi.json_encode(lessons))
      if not ok then return tostring(err) end
      magi.log(TRACE_LESSON .. (lessons[TRACE_LESSON] and " promoted" or " withdrawn"))
    end,
  }

  magi.register_command{
    name = "routine-trace-runs",
    description = "List recorded call-path runs, or show one (/routine-trace-runs [run-id])",
    execute = function(args)
      local id = words(args)[1]
      if id then
        if not id:match("^%x+$") then return "invalid call-path run ID" end
        local raw = magi.read_file(TRACE_DIR .. "runs/" .. id .. ".json")
        if not raw then return "no call-path run " .. id end
        magi.log(raw)
        return
      end
      for _, run in ipairs(trace_runs()) do
        magi.log(tostring(run.id) .. " " .. tostring(run.family) .. " " .. tostring(run.policy) .. " " .. tostring(run.phase))
      end
    end,
  }

  magi.register_tool{
    name = "routine_compare_trace_policy",
    description = "Explicitly compare whole-v1 and handoff-v1 on frozen policy tasks using one Git snapshot and separate, equal budgets. Does not activate either policy.",
    schema = { type = "object", properties = { split = { type = "string", enum = { "dev", "holdout" } } } },
    execute = function(a)
      local split = a and a.split or "dev"
      if split ~= "dev" and split ~= "holdout" then return "invalid policy comparison split", true end
      local config = trace_policy()
      if not config then return "invalid call-path policy configuration", true end
      local baseline = config.active
      local candidate = baseline == "whole-v1" and "handoff-v1" or "whole-v1"
      if type(magi.with_explore_comparison) ~= "function" then return "policy comparison requires an updated host", true end
      local path = TRACE_DIR .. "policy-tasks.json"
      local raw = magi.read_file(path)
      local frozen = magi.read_file(TRACE_DIR .. "policy-tasks.sha256")
      if not raw or magi.sha256(raw) ~= frozen then return "freeze current policy tasks with /routine-trace-policy freeze", true end
      local data = decode(raw)
      local selected, seen, inputs = {}, {}, {}
      local request_total, timeout_total = 0, 0
      for _, task in ipairs(data and data.tasks or {}) do
        if type(task) ~= "table" or type(task.id) ~= "string" or task.id == "" or seen[task.id]
          or type(task.input) ~= "table" or (task.split ~= "dev" and task.split ~= "holdout") then
          return "policy tasks need distinct IDs, inputs and an explicit split", true
        end
        seen[task.id] = true
        local fingerprint = magi.sha256(magi.json_encode({ question = task.input.question, handoffs = task.input.handoffs }))
        if inputs[fingerprint] and inputs[fingerprint] ~= task.split then return "a policy task appears in both splits", true end
        inputs[fingerprint] = task.split
        if task.split == split then
          local input = task.input
          if type(input.handoffs) ~= "table" or #input.handoffs < 1 or #input.handoffs > 4
            or type(input.request_limit) ~= "number" or input.request_limit % 1 ~= 0 or input.request_limit < 1
            or type(input.timeout) ~= "number" or input.timeout < 1 or input.timeout > 300 then
            return "policy comparison requires valid handoffs and budgets", true
          end
          for _, handoff in ipairs(input.handoffs) do
            if type(handoff) ~= "table" or type(handoff.evidence) ~= "string" or not handoff.evidence:match("^[%w_./%-]+:%d+$") then
              return "policy comparison requires a reference for every handoff", true
            end
          end
          request_total, timeout_total = request_total + 2 * input.request_limit, timeout_total + 2 * input.timeout
          selected[#selected + 1] = task
        end
      end
      if #selected == 0 or #selected > 4 then return "select one to four frozen tasks", true end
      if request_total > 32 or timeout_total > 600 then return "paired comparison exceeds 32 requests or 600 seconds of total budget", true end
      local approved, approval_err = magi.approve{
        reason = "Run a paired, read-only comparison of call-path policies on " .. #selected .. " " .. split .. " tasks",
        detail = { split = split, tasks_hash = frozen, tasks = selected, tools = {"explore"},
          request_limit = request_total, timeout = timeout_total,
          policies = {baseline, candidate}, script = TRACE_SCRIPT },
      }
      if not approved then return tostring(approval_err or "policy comparison was not approved"), true end
      local report, err = magi.with_explore_comparison(function(revision, tree)
        local result = { id = magi.nonce(), phase = "started", split = split, revision = revision, tree = tree, tasks_hash = frozen,
          active = baseline, candidate = candidate, rows = {}, solved = 0, regressed = 0 }
        local comparisons = TRACE_DIR .. "comparisons/"
        if split == "holdout" then
          result.holdout_use = 1
          for _, file in ipairs(magi.list_files(comparisons) or {}) do
            if file:match("^%x+%.json$") then
              local previous = decode(magi.read_file(comparisons .. file))
              if previous and previous.split == "holdout" and previous.tasks_hash == frozen then
                result.holdout_use = result.holdout_use + 1
              end
            end
          end
        end
        local result_path = comparisons .. result.id .. ".json"
        local started, start_err = magi.write_file(result_path, magi.json_encode(result))
        if not started then return nil, tostring(start_err) end
        for i, task in ipairs(selected) do
          local function arm(policy)
            local message, _, run = trace_tool.execute(task.input, policy, split)
            return run, not run and message or nil
          end
          local base, trial, problem
          if i % 2 == 1 then
            base, problem = arm(result.active)
            if base then trial, problem = arm(result.candidate) end
          else
            trial, problem = arm(result.candidate)
            if trial then base, problem = arm(result.active) end
          end
          if not base or not trial then return nil, problem end
          local b, c = base.phase == "completed", trial.phase == "completed"
          if b and not c then result.regressed = result.regressed + 1 end
          if c and base.phase == "partial" then result.solved = result.solved + 1 end
          result.rows[#result.rows + 1] = { id = task.id, baseline_run = base.id, candidate_run = trial.id,
            active = base.phase, candidate = trial.phase, protected = task.protected == true }
        end
        if magi.read_file(path) ~= raw or magi.read_file(TRACE_DIR .. "policy-tasks.sha256") ~= frozen then
          return nil, "policy tasks changed during comparison"
        end
        result.phase = "completed"
        local saved, why = magi.write_file(result_path, magi.json_encode(result))
        if not saved then return nil, tostring(why) end
        return magi.json_encode(result)
      end)
      if not report then return tostring(err), true end
      return report
    end,
  }

  local function conclusion_marker(text)
    local found
    for line in text:gmatch("[^\r\n]+") do
      local marker = line:match("^%s*CONCLUSION:%s*(%a+)%s*$")
      if marker then
        if (found and found ~= marker) or (marker ~= "SUPPORTED" and marker ~= "UNVERIFIED") then return nil end
        found = marker
      end
    end
    return found
  end

  magi.register_tool{
    name = "routine_compare_explore",
    description = "Run a read-only routine and a candidate on the same frozen tasks using real scoped explore calls. Records paired citation checks and timings without adopting either version.",
    schema = { type = "object", properties = {
      name = { type = "string" }, script = { type = "string" }, directory = { type = "string" },
      split = { type = "string", enum = { "dev", "holdout" } },
      aa = { type = "boolean", description = "Run the active script in both arms on development cases" },
    }, required = { "name", "directory" } },
    execute = function(a)
      if type(magi.with_explore_comparison) ~= "function" then
        return "live comparison requires an updated magi host", true
      end
      local r, err = load_routine(a and a.name)
      if not r then return err, true end
      local scope, split = a.directory, a.split or "dev"
      local aa = a.aa == true
      if aa and split ~= "dev" then return "A/A uses development tasks only", true end
      local script = aa and r.script or a.script
      if type(script) ~= "string" or script == "" or type(scope) ~= "string"
        or not scope:match("^[%w_.%-%/]+$") or scope:sub(1, 1) == "/" or scope == "." then
        return "candidate script and workspace subdirectory required", true
      end
      for part in scope:gmatch("[^/]+") do
        if part == ".." or part == "." then return "use a workspace subdirectory", true end
      end
      if split ~= "dev" and split ~= "holdout" then return "split must be dev or holdout", true end
      local explore = false
      for _, tool in ipairs(r.tools) do
        if tool == "explore" then explore = true
        elseif tool ~= "recall_memory" then return "live comparison does not support " .. tool, true end
      end
      if not explore then return "routine does not declare explore", true end

      local dir = rdir(r.name)
      local ctext, ttext = magi.read_file(dir .. "contract.json"), magi.read_file(dir .. "tasks.json")
      local frozen = stored("routine_evals_frozen")[r.name]
      if type(frozen) ~= "table" or type(ctext) ~= "string" or type(ttext) ~= "string"
        or frozen.contract ~= magi.sha256(ctext) or frozen.tasks ~= magi.sha256(ttext) then
        return "freeze the current contract and tasks before live comparison", true
      end
      local tasks = decode(ttext)
      local selected = {}
      for _, task in ipairs(tasks and tasks.tasks or {}) do
        if (task.split or "dev") == split then selected[#selected + 1] = task end
      end
      if #selected == 0 or #selected > 12 then return "live comparison requires 1 to 12 tasks in the selected split", true end
      local path = dir .. "live-explore.jsonl"
      local use = 1
      if split == "holdout" then
        for _, prior in ipairs(read_lines(path)) do
          if prior.phase == "started" and prior.split == "holdout" and prior.tasks_hash == frozen.tasks then use = use + 1 end
        end
      end
      local id = magi.nonce()
      local report = { id = id, phase = "started", split = split, tasks_hash = frozen.tasks, contract_hash = frozen.contract,
        baseline = r.fingerprint, candidate = version_of(script, r.inputs, r.tools), directory = scope, mode = aa and "A/A" or "A/B",
        declared_revision = tasks.revision, declared_model = tasks.model, holdout_use = split == "holdout" and use or nil }
      local result, failure = magi.with_explore_comparison(function(revision, tree)
        report.revision, report.source_tree = revision, tree
        local ok, why = append_line(path, report)
        if not ok then return "could not record live comparison: " .. tostring(why), true end

        local function arm(task, script)
          local src = "local input = json_decode(" .. string.format("%q", magi.json_encode(task.input or {})) .. ")\n" .. script
          local out, failure, metrics = magi.run_script(src, {
            globals = noop_globals(), tools = r.tools, explore_read_scope = scope, measure_explore = true, timeout = 90,
          })
          metrics = metrics or { calls = 0, failures = 0 }
          local answer = out or ""
          local observations = table.concat(metrics.answers or {}, "\n")
          local found, invalid = live_evidence(answer, scope)
          local observed = live_evidence(observations, scope)
          local expect = task.expect or {}
          local missing, expected = {}, expect.evidence or {}
          for _, ref in ipairs(expected) do if not found[ref] or not observed[ref] then missing[#missing + 1] = ref end end
          local ran = not failure and metrics.calls > 0 and metrics.failures == 0
          local citation_status = (#expected == 0 or metrics.calls == 0) and "unverified"
            or (#missing == 0 and #invalid == 0 and "pass" or "fail")
          local conclusion_status, marker
          if expect.conclusion then
            marker = conclusion_marker(answer)
            conclusion_status = not ran and "unverified"
              or (marker == expect.conclusion and conclusion_marker(observations) == marker and "pass" or "fail")
          end
          local status = (citation_status == "unverified" or conclusion_status == "unverified") and "unverified"
            or (ran and citation_status == "pass" and conclusion_status ~= "fail" and "pass" or "fail")
          return { status = status, citation_status = citation_status, conclusion_status = conclusion_status, conclusion = marker,
            missing = missing, invalid = invalid, explore_calls = metrics.calls, explore_failures = metrics.failures,
            elapsed_ms = metrics.elapsed_ms, script_failed = failure ~= nil, model_calls = metrics.model_calls, usage_reports = metrics.usage_reports,
            input_tokens = metrics.input_tokens, output_tokens = metrics.output_tokens, cached_tokens = metrics.cached_tokens,
            tokens_known = metrics.tokens_known, cache_reported = metrics.cache_reported, model = metrics.model, profile = metrics.profile }
        end
        report.rows, report.protected_regressions = {}, 0
        for i, task in ipairs(selected) do
          local base, candidate
          if i % 2 == 1 then
            base = arm(task, r.script)
            candidate = arm(task, script)
          else
            candidate = arm(task, script)
            base = arm(task, r.script)
          end
          local outcome = transition(base.status, candidate.status)
          if task.protected and outcome == "regressed" then report.protected_regressions = report.protected_regressions + 1 end
          report.rows[#report.rows + 1] = { id = task.id, protected = task.protected, active = base, candidate = candidate, transition = outcome }
        end
        local active = magi.read_file(r.path)
        if magi.read_file(dir .. "contract.json") ~= ctext or magi.read_file(dir .. "tasks.json") ~= ttext
          or type(active) ~= "string" or magi.sha256(active) ~= r.file_sha256 then
          return "evaluation inputs or active routine changed during live comparison", true
        end
        report.phase = "completed"
        ok, why = append_line(path, report)
        if not ok then return "could not record live comparison results: " .. tostring(why), true end
        return magi.json_encode(report)
      end)
      if not result then return tostring(failure), true end
      return result, failure
    end,
  }

  -- sign_p: one-sided sign test over the discordant tasks, P(at least s of n solved | no effect).
  local function sign_p(s, n)
    if n == 0 then return 1 end
    local p, comb = 0, 1
    for k = 0, n do
      if k > 0 then comb = comb * (n - k + 1) / k end
      if k >= s then p = p + comb end
    end
    return p / 2 ^ n
  end

  local function diff_summary(a, b)
    local count, added, removed = {}, 0, 0
    for l in (a .. "\n"):gmatch("(.-)\n") do count[l] = (count[l] or 0) + 1 end
    for l in (b .. "\n"):gmatch("(.-)\n") do
      if (count[l] or 0) > 0 then count[l] = count[l] - 1 else added = added + 1 end
    end
    for _, n in pairs(count) do removed = removed + n end
    return string.format("+%d -%d lines", added, removed)
  end

  local function history_text(name, limit)
    local recs = read_lines(rdir(name) .. "attempts.jsonl")
    if #recs == 0 then return "no attempts recorded for " .. name end
    local out = {}
    for i = #recs, math.max(1, #recs - limit + 1), -1 do
      local a = recs[i]
      local d = type(a.decision) == "table" and a.decision or {}
      local cand = type(a.candidate) == "table" and a.candidate or {}
      local lost, gained = {}, {}
      for _, t in ipairs(type(a.tasks) == "table" and a.tasks or {}) do
        if t.transition == "regressed" then lost[#lost + 1] = tostring(t.id) end
        if t.transition == "solved" and t.split ~= "holdout" then gained[#gained + 1] = tostring(t.id) end
      end
      out[#out + 1] = string.format("%s %s candidate %s: %s — %s\n  problem: %s\n  hypothesis: %s%s%s", tostring(a.at),
        tostring(a.id), tostring(cand.version), tostring(d.verdict), tostring(d.reason), tostring(a.problem or ""),
        tostring(a.hypothesis or ""), #gained > 0 and ("\n  solved (dev): " .. table.concat(gained, ", ")) or "",
        #lost > 0 and ("\n  regressed: " .. table.concat(lost, ", ")) or "")
    end
    for _, d in ipairs(read_lines(rdir(name) .. "decisions.jsonl")) do
      if d.action == "generation interrupted" then
        out[#out + 1] = string.format("%s %s %s (attempt %s)", tostring(d.at), d.action, tostring(d.version), tostring(d.attempt))
      elseif d.by == "lifecycle" then
        out[#out + 1] = tostring(d.at) .. " " .. tostring(d.action) .. " " .. tostring(d.version) ..
          (d.canonical and (" → " .. d.canonical) or "") .. ": " .. tostring(d.reason)
      else
        out[#out + 1] = string.format("%s %s %s (replaced %s) by %s", tostring(d.at), tostring(d.action), tostring(d.version),
          tostring(d.replaced), tostring(d.by))
      end
    end
    return table.concat(out, "\n")
  end

  -- adopt_version makes an applicable candidate the active version and keeps the replaced one for
  -- rollback. by is "person" (/routine-adopt) or "auto" (inside a delegation a person gave once,
  -- /routine-auto). It approves no run: the adopted version asks for run approval on its next run.
  local function adopt_version(name, ver, by, delegation)
    local r, err = load_routine(name)
    if not r then return nil, err end
    local dir, att = rdir(name), nil
    if by == "auto" then
      for _, decision in ipairs(read_lines(dir .. "decisions.jsonl")) do
        if (decision.action == "rolled back" or decision.action == "rollback requested") and decision.replaced == ver then
          return nil, "candidate " .. ver .. " was rolled back by a person; use /routine-adopt to restore it explicitly"
        end
      end
    end
    for _, a in ipairs(read_lines(dir .. "attempts.jsonl")) do
      if type(a.candidate) == "table" and a.candidate.version == ver then att = a end
    end
    if not att then return nil, "no recorded attempt for candidate " .. ver .. " of " .. name .. " (see /routine-history " .. name .. ")" end
    local d = type(att.decision) == "table" and att.decision or {}
    if d.verdict ~= "applicable" then
      return nil, "candidate " .. ver .. " was " .. tostring(d.verdict) .. ": " .. tostring(d.reason) .. "; only an applicable candidate can be adopted"
    end
    if att.evaluation_policy ~= EVAL_POLICY then
      return nil, "candidate " .. ver .. " was evaluated under an older policy; compare it again"
    end
    local expected_evals = (att.conditions or {}).evals or {}
    for _, key in ipairs({ "contract", "tasks" }) do
      local raw = magi.read_file(dir .. key .. ".json")
      if type(raw) ~= "string" or magi.sha256(raw) ~= expected_evals[key] then
        return nil, "candidate " .. ver .. " evaluation inputs changed; compare it again"
      end
    end
    local base = r.fingerprint:sub(1, 12)
    if type(att.baseline) ~= "table" or att.baseline.version ~= base then
      return nil, "candidate " .. ver .. " was compared with " .. tostring(type(att.baseline) == "table" and att.baseline.version) ..
        ", but the active version is now " .. base .. "; compare it again"
    end
    local vtext, cur = magi.read_file(dir .. "versions/" .. ver .. ".md"), magi.read_file(r.path)
    if type(vtext) ~= "string" or type(cur) ~= "string" then return nil, "the candidate's or the active version's file is missing" end
    if magi.sha256(vtext) ~= att.candidate.file_sha256 or magi.sha256(cur) ~= att.baseline.file_sha256 then
      return nil, "candidate or active version changed on disk; compare it again"
    end
    local ok1, e1 = magi.write_file(dir .. "versions/" .. base .. ".md", cur)
    if not ok1 then return nil, "could not keep the active version: " .. tostring(e1) end
    if by == "auto" and delegation and delegation.explore_scope then
      local scopes = stored("routine_explore_scopes")
      scopes[name .. ":" .. ver] = delegation.explore_scope
      local ok, err = magi.store_set("routine_explore_scopes", scopes)
      if not ok then return nil, "could not persist the explore scope: " .. tostring(err) end
    end
    local ok2, e2 = magi.write_file(r.path, vtext)
    if not ok2 then return nil, "could not write " .. r.path .. ": " .. tostring(e2) end
    local stack = stored("routine_rollback")
    local list = type(stack[name]) == "table" and stack[name] or {}
    list[#list + 1] = { from = base, to = ver, path = r.path }
    stack[name] = list
    magi.store_set("routine_rollback", stack)
    append_line(dir .. "decisions.jsonl", { at = magi.time(), action = by == "auto" and "auto-applied" or "adopted", routine = name,
      version = ver, replaced = base, attempt = att.id, by = by, delegation = delegation,
      run_approval = "not granted: the next run of " .. ver .. " asks the person" })
    return true, base, r.path
  end

  local IMPROVE_USE = [[
Compare a candidate version of a routine with its active version, and record the attempt. You pass the candidate's whole script, the problem it addresses and your hypothesis (read routine_history first: earlier attempts, and why they were rejected). The candidate is checked against the routine's frozen contract (fixed fixtures, run dry: control flow only), then paired with the active version on the same frozen tasks. Each task is judged on its own — newly solved, newly regressed, still failing, kept — and the verdict is applicable, held (gain too thin, unstable or regressions to review) or rejected (contract broken, a protected task lost, tools or inputs changed). It never changes the active routine, its evals or its tools; a person adopts an applicable candidate.]]

  local function improve(a, dev_only)
    a = a or {}
    local r, err = load_routine(a.name)
    if not r then return err, true end
    if type(a.script) ~= "string" or a.script == "" then return "script is required", true end
    if type(a.hypothesis) ~= "string" or a.hypothesis == "" then
      return "hypothesis is required: what this change should improve, and why", true
    end
    local dir = rdir(r.name)
    -- Links: the run the person picked to start from, and the attempt this one follows. Both must
    -- exist, so an attempt never points at something that was not there.
    local source_run, follows
    if a.from_run ~= nil and a.from_run ~= "" then
      local fr = tostring(a.from_run)
      local runs = stored("routine_runs")
      for _, x in ipairs(type(runs.list) == "table" and runs.list or {}) do
        if x.id == fr then source_run = { run = fr, goal = x.goal, code_hash = magi.sha256(tostring(x.script)):sub(1, 12) } end
      end
      if not source_run then return "no run " .. fr .. " (see /routine-trace); nothing was compared", true end
    end
    if a.previous ~= nil and a.previous ~= "" then
      for _, p in ipairs(read_lines(dir .. "attempts.jsonl")) do
        if p.id == a.previous then
          local d = type(p.decision) == "table" and p.decision or {}
          follows = { attempt = p.id, verdict = d.verdict, reason = d.reason }
        end
      end
      if not follows then return "no attempt " .. tostring(a.previous) .. " of " .. r.name .. " (see routine_history); nothing was compared", true end
    end
    local ctext, ttext = magi.read_file(dir .. "contract.json"), magi.read_file(dir .. "tasks.json")
    if type(ctext) ~= "string" or type(ttext) ~= "string" then
      return "routine " .. r.name .. " has no " .. dir .. "contract.json and tasks.json; a person writes them and freezes them with /routine-evals-freeze " .. r.name .. "; nothing was compared", true
    end
    local frozen = stored("routine_evals_frozen")[r.name]
    if type(frozen) ~= "table" then
      return "evals for " .. r.name .. " are not frozen; a person reviews " .. dir .. "contract.json and tasks.json and freezes them with /routine-evals-freeze " .. r.name .. "; nothing was compared", true
    end
    local csha, tsha = magi.sha256(ctext), magi.sha256(ttext)
    if csha ~= frozen.contract or tsha ~= frozen.tasks then
      return "evals for " .. r.name .. " changed since they were frozen at " .. tostring(frozen.at) .. "; a person reviews the change and freezes them again; nothing was compared", true
    end
    local contract, tasks = decode(ctext), decode(ttext)
    if not contract or not tasks then return "contract.json or tasks.json is not a JSON object", true end
    local tools = r.tools
    if type(a.tools) == "table" then
      local seen = {}
      tools = {}
      for _, t in ipairs(a.tools) do
        t = tostring(t)
        if not seen[t] then seen[t] = true; tools[#tools + 1] = t end
      end
      table.sort(tools)
    end
    local inputs = type(a.inputs) == "table" and a.inputs or r.inputs
    local ver, base = version_of(a.script, inputs, tools), r.fingerprint:sub(1, 12)
    if ver == base then return "the candidate is the active version " .. base .. "; nothing to compare", true end

    -- Gates, not scores: what a candidate may not change.
    local reject
    local had, has, added, dropped = {}, {}, {}, {}
    for _, t in ipairs(r.tools) do had[t] = true end
    for _, t in ipairs(tools) do
      has[t] = true
      if not had[t] then added[#added + 1] = t end
    end
    for _, t in ipairs(r.tools) do
      if not has[t] then dropped[#dropped + 1] = t end
    end
    if #added > 0 or #dropped > 0 then
      reject = "tool scope changed (" .. (#added > 0 and ("adds " .. table.concat(added, ", ")) or "") ..
        ((#added > 0 and #dropped > 0) and "; " or "") .. (#dropped > 0 and ("drops " .. table.concat(dropped, ", ")) or "") ..
        "): a candidate changes its script only; its tools are the person's to change"
    elseif magi.json_encode(inputs) ~= magi.json_encode(r.inputs) then
      reject = "input schema changed: a candidate changes its script only"
    end
    local writes = {}
    for _, t in ipairs(tools) do
      if WRITES[t] then writes[#writes + 1] = t end
    end

    local contract_rows, cfail, cpass, cunv = {}, {}, 0, {}
    if not reject then
      for _, c in ipairs(type(contract.cases) == "table" and contract.cases or {}) do
        local res, why = check_case(c, a.script, tools)
        contract_rows[#contract_rows + 1] = { id = tostring(c.id), result = res, why = why ~= "" and why or nil }
        if res == "fail" then cfail[#cfail + 1] = tostring(c.id) .. " (" .. why .. ")"
        elseif res == "pass" then cpass = cpass + 1
        else cunv[#cunv + 1] = tostring(c.id) end
      end
      if #cfail > 0 then reject = "breaks the contract: " .. table.concat(cfail, "; ") end
    end

    local n = { solved = 0, regressed = 0, still_failing = 0, kept = 0, unstable = 0, unverified = 0 }
    local rows, diffs, plost, lost, shaky = {}, {}, {}, {}, {}
    local hold_solved, hold_regressed, hold_cheaper, hold_costlier = 0, 0, 0, 0
    local cost = { active = { calls = 0, simulated_explore_calls = 0, reach = {}, explore = {} }, candidate = { calls = 0, simulated_explore_calls = 0, reach = {}, explore = {} } }
    local reps = math.max(1, math.floor(tonumber(tasks.repeats) or 3))
    if not reject then
      for _, tk in ipairs(type(tasks.tasks) == "table" and tasks.tasks or {}) do
        if not dev_only or tk.split ~= "holdout" then
          local active_calls, candidate_calls = cost.active.calls, cost.candidate.calls
          local holdout = tk.split == "holdout"
          local b = task_result(tk, r.script, r.tools, reps, cost.active)
          local c = task_result(tk, a.script, tools, reps, cost.candidate)
          local tr = transition(b, c)
          n[tr] = n[tr] + 1
          local id = tostring(tk.id)
          rows[#rows + 1] = { id = id, split = tostring(tk.split or "dev"), protected = tk.protected == true or nil,
            active = b, candidate = c, transition = tr }
          if holdout and tr == "kept" then
            local delta = (cost.candidate.calls - candidate_calls) - (cost.active.calls - active_calls)
            if delta < 0 then hold_cheaper = hold_cheaper + 1 end
            if delta > 0 then hold_costlier = hold_costlier + 1 end
          end
          if tr == "regressed" then
            if tk.protected == true then plost[#plost + 1] = id else lost[#lost + 1] = id end
            if holdout then hold_regressed = hold_regressed + 1; diffs[#diffs + 1] = -1 end
          elseif tr == "solved" then
            if holdout then hold_solved = hold_solved + 1; diffs[#diffs + 1] = 1 end
          elseif tr == "kept" or tr == "still_failing" then
            if holdout then diffs[#diffs + 1] = 0 end
          else
            shaky[#shaky + 1] = id .. " (" .. tr .. ")"
          end
        end
      end
    end

    if dev_only then
      -- This feedback stays in the current generation loop. Full attempts may contain
      -- holdout results directly or through their verdict and follow-up notes.
      if reject then return reject, true end
      local problems = {}
      for _, row in ipairs(rows) do
        if row.transition == "regressed" or row.transition == "unstable" or row.transition == "unverified" then
          problems[#problems + 1] = row.id .. ": " .. row.transition
        end
      end
      if #problems > 0 then return table.concat(problems, "; "), true end
      return "passed development screening"
    end

    local effect
    if #diffs > 0 then
      local m, v = 0, 0
      for _, d in ipairs(diffs) do m = m + d end
      m = m / #diffs
      for _, d in ipairs(diffs) do v = v + (d - m) ^ 2 end
      local se = (#diffs > 1 and math.sqrt(v / (#diffs - 1)) or 0) / math.sqrt(#diffs)
      effect = { mean = m, low = m - 1.96 * se, high = m + 1.96 * se, tasks = #diffs,
        sign_p = sign_p(hold_solved, hold_solved + hold_regressed), split = "holdout" }
    end

    local reach_added, reach_counts = {}, { active = 0, candidate = 0 }
    for k in pairs(cost.active.reach) do reach_counts.active = reach_counts.active + 1 end
    for k in pairs(cost.candidate.reach) do
      reach_counts.candidate = reach_counts.candidate + 1
      if not cost.active.reach[k] then reach_added[#reach_added + 1] = k end
    end
    table.sort(reach_added)
    local explore_changed = 0
    for k in pairs(cost.candidate.explore) do
      if not cost.active.explore[k] then explore_changed = explore_changed + 1 end
    end
    for k in pairs(cost.active.explore) do
      if not cost.candidate.explore[k] then explore_changed = explore_changed + 1 end
    end
    cost.active.reach, cost.candidate.reach = nil, nil
    cost.active.explore, cost.candidate.explore = nil, nil

    local verdict, reason
    if reject then
      verdict, reason = "rejected", reject
    elseif #plost > 0 then
      verdict, reason = "rejected", "protected task regressed: " .. table.concat(plost, ", ") .. " — a gain elsewhere does not offset it"
    elseif #lost > 0 then
      verdict, reason = "held", "regressions to review: " .. table.concat(lost, ", ") .. " — not offset by what it solves"
    elseif #shaky > 0 then
      verdict, reason = "held", "results not settled on: " .. table.concat(shaky, ", ")
    elseif n.solved == 0 then
      local cost_p = sign_p(hold_cheaper, hold_cheaper + hold_costlier)
      if hold_costlier == 0 and cost_p <= IMPROVE_P then
        verdict, reason = "applicable", string.format("same task results with fewer calls on %d holdout tasks (sign test p=%.3f)", hold_cheaper, cost_p)
      else
        verdict, reason = "held", "no quality gain and insufficient holdout evidence of fewer calls"
      end
    elseif not effect or effect.sign_p > IMPROVE_P then
      verdict, reason = "held", string.format("holdout evidence too thin: %d newly solved, %d regressed over %d verified tasks (sign test p=%.3f; need <= %.2f)",
        hold_solved, hold_regressed, #diffs, effect and effect.sign_p or 1, IMPROVE_P)
    else
      verdict, reason = "applicable", string.format("solves %d tasks (%d holdout), regresses none (holdout sign test p=%.3f)",
        n.solved, hold_solved, effect.sign_p)
    end

    local vpath = dir .. "versions/" .. ver .. ".md"
    local status = r.status == "auto-review" and "status: auto-review\n" or ""
    local vtext = "---\nkind: routine\n" .. status .. "description: " .. (tostring(r.description):gsub("\n", " ")) .. "\ntools: " ..
      table.concat(tools, ", ") .. "\ninputs: " .. magi.json_encode(inputs) .. "\n---\nCandidate " .. ver .. " of " .. r.name ..
      ". Hypothesis: " .. (a.hypothesis:gsub("\n", " ")) .. "\n\n```lua\n" .. a.script .. "\n```\n"
    local wok, werr = magi.write_file(vpath, vtext)
    if not wok then return "could not write " .. vpath .. ": " .. tostring(werr), true end
    local change = diff_summary(r.script, a.script)
    local attempt = {
      id = "a" .. magi.sha256(ver .. tostring(magi.time()) .. tostring(magi.nonce())):sub(1, 8),
      at = magi.time(), routine = r.name,
      baseline = { version = base, path = r.path, file_sha256 = r.file_sha256 },
      candidate = { version = ver, path = vpath, change = change, tools = tools, file_sha256 = magi.sha256(vtext) },
      problem = a.problem, hypothesis = a.hypothesis, expected = a.expected, source = a.source,
      source_run = source_run, follows = follows,
      conditions = { evals = { contract = csha, tasks = tsha, frozen_at = frozen.at }, repeats = reps, tools = tools,
        mode = "dry run on fixed fixtures: no tool reached, no model",
        declared_revision = tasks.revision and tostring(tasks.revision) or nil,
        declared_model = tasks.model and tostring(tasks.model) or nil, actual_model_calls = 0 },
      contract = #contract_rows > 0 and contract_rows or nil,
      tasks = #rows > 0 and rows or nil,
      summary = n, effect = effect, cost = cost,
      holdout_cost = { cheaper = hold_cheaper, costlier = hold_costlier,
        sign_p = sign_p(hold_cheaper, hold_cheaper + hold_costlier) },
      evaluation_policy = EVAL_POLICY,
      data_reach = { added = #reach_added > 0 and reach_added or nil, active = reach_counts.active, candidate = reach_counts.candidate, explore_changed = explore_changed,
        compared = "read, grep, glob, list and recall_memory arguments on the tasks; explore not compared" },
      decision = { verdict = verdict, reason = reason, by = "routine_improve" },
    }
    local aok, aerr = append_line(dir .. "attempts.jsonl", attempt)
    if not aok then return "could not record the attempt: " .. tostring(aerr), true end

    local lines = {
      string.format("routine %s: candidate %s vs active %s (%s)", r.name, ver, base, change),
      "verdict: " .. verdict .. " — " .. reason,
    }
    if source_run then lines[#lines + 1] = "from run " .. source_run.run .. " (" .. tostring(source_run.goal) .. ")" end
    if follows then
      lines[#lines + 1] = "follows attempt " .. follows.attempt .. " (" .. tostring(follows.verdict) .. ": " .. tostring(follows.reason) .. ")"
    end
    if #contract_rows > 0 then
      lines[#lines + 1] = string.format("contract: pass %d, fail %d, unverified %d%s", cpass, #cfail, #cunv,
        #cunv > 0 and (" (" .. table.concat(cunv, ", ") .. ")") or "")
    end
    if #rows > 0 then
      lines[#lines + 1] = string.format("tasks (paired, x%d): solved %d, regressed %d, still failing %d, kept %d, unstable %d, unverified %d",
        reps, n.solved, n.regressed, n.still_failing, n.kept, n.unstable, n.unverified)
      for _, row in ipairs(rows) do
        lines[#lines + 1] = string.format("  %s [%s%s]: %s -> %s = %s", row.id, row.split, row.protected and ", protected" or "",
          row.active, row.candidate, row.transition)
      end
      if effect then
        lines[#lines + 1] = string.format("effect: %+.2f per task (approximate 95%% interval %+.2f..%+.2f over %d holdout tasks), sign test p=%.3f",
          effect.mean, effect.low, effect.high, effect.tasks, effect.sign_p)
      end
      lines[#lines + 1] = string.format("cost (dry, x%d; no model calls): active %d calls / %d simulated explore, candidate %d calls / %d simulated explore", reps,
        cost.active.calls, cost.active.simulated_explore_calls, cost.candidate.calls, cost.candidate.simulated_explore_calls)
    end
    if #writes > 0 then lines[#lines + 1] = "writes (" .. table.concat(writes, ", ") .. "): compared dry only; effects on files are not evaluated" end
    if #rows > 0 then
      lines[#lines + 1] = #reach_added > 0 and ("data reach: adds " .. table.concat(reach_added, "; "):sub(1, 200))
        or "data reach: no new read, grep, glob, list or recall_memory arguments"
      lines[#lines + 1] = explore_changed > 0 and ("explore questions: " .. explore_changed .. " differ from the active version's")
        or "explore questions: same as the active version's"
    end
    -- Delegated auto-apply: only an applicable candidate, only inside what the person allowed once.
    local applied, auto_line = false, nil
    if verdict == "applicable" then
      local del = stored("routine_auto")[r.name]
      if type(del) == "table" then
        local why
        if #writes > 0 then why = "it may write (" .. table.concat(writes, ", ") .. ")"
        elseif table.concat(tools, ",") ~= table.concat(type(del.tools) == "table" and del.tools or {}, ",") then why = "its tools differ from the delegated ones"
        elseif magi.json_encode(inputs) ~= tostring(del.inputs) then why = "its inputs differ from the delegated ones"
        elseif explore_changed > 0 and type(del.explore_scope) ~= "string" then
          why = "data reach differs (explore questions changed); /routine-auto " .. r.name .. " explore-text <directory> allows that within the directory"
        elseif #reach_added > 0 then
          why = "its data reach differs (adds " .. table.concat(reach_added, "; "):sub(1, 200) .. ")"
        elseif type(del.evals) ~= "table" or del.evals.contract ~= csha or del.evals.tasks ~= tsha then
          why = "the evals changed since the delegation of " .. tostring(del.at)
        end
        if why then
          auto_line = "not auto-applied: outside the delegated scope — " .. why .. "; a person may adopt it with /routine-adopt " .. r.name .. " " .. ver
        else
          local ok, prev = adopt_version(r.name, ver, "auto", { at = del.at, explore_scope = del.explore_scope })
          if ok then
            applied = true
            auto_line = "auto-applied " .. ver .. " (was " .. prev .. ") under the delegation of " .. tostring(del.at) ..
              "; its next run asks for approval; /routine-rollback " .. r.name .. " restores " .. prev
            local summary = "[routine] auto-applied " .. ver .. " to " .. r.name .. " (was " .. prev .. "): " .. reason ..
              ". Undo with /routine-rollback " .. r.name
            magi.log(summary)
            local sid = type(magi.session_id) == "function" and magi.session_id() or ""
            if sid ~= "" and type(magi.notify) == "function" then pcall(magi.notify, sid, summary) end
          else
            auto_line = "not auto-applied: " .. tostring(prev)
          end
        end
      end
    end
    if auto_line then lines[#lines + 1] = auto_line end
    lines[#lines + 1] = "recorded as attempt " .. attempt.id .. " in " .. dir .. "attempts.jsonl; " ..
      (applied and "applied" or verdict == "applicable" and ("a person may adopt it with /routine-adopt " .. r.name .. " " .. ver) or "it cannot be adopted")
    lines[#lines + 1] = "checked: control flow on fixed fixtures, run dry — not the accuracy of real explore answers, not effects on files, not new tasks"
    return table.concat(lines, "\n")
  end

  magi.register_tool{
    name = "routine_improve",
    description = IMPROVE_USE,
    schema = {
      type = "object",
      properties = {
        name = { type = "string", description = "the routine to improve" },
        script = { type = "string", description = "the candidate's whole Lua script" },
        problem = { type = "string", description = "what kept going wrong, with the runs that showed it" },
        hypothesis = { type = "string", description = "what this change should improve, and why" },
        expected = { type = "string", description = "the effect you expect on the tasks" },
        source = { type = "string", description = "the runs or attempts this candidate came from (ids)" },
        from_run = { type = "string", description = "the run this started from (an id from /routine-trace); it must exist" },
        previous = { type = "string", description = "the attempt this one follows (an id from routine_history); it must exist" },
        tools = { type = "array", items = { type = "string" }, description = "only if it differs from the active version's (it will be rejected: tools are the person's to change)" },
        inputs = { type = "object", description = "only if it differs from the active version's (rejected likewise)" },
      },
      required = { "name", "script", "hypothesis" },
    },
    execute = function(a) return improve(a, false) end,
  }

  magi.register_tool{
    name = "routine_history",
    description = "Read a routine's improvement attempts, newest first: each candidate's problem, hypothesis, verdict and why, and the tasks it solved or regressed. Read it before proposing a candidate with routine_improve.",
    schema = { type = "object", properties = {
      name = { type = "string", description = "the routine" },
      limit = { type = "number", description = "attempts to show, default 10" },
    }, required = { "name" } },
    execute = function(a)
      if type(a) ~= "table" or type(a.name) ~= "string" or not a.name:match("^[%w_%-]+$") then return "name is required", true end
      return history_text(a.name, math.max(1, math.floor(tonumber(a.limit) or 10)))
    end,
  }

  -- Select on development cases and the contract, then evaluate the selected candidate once.
  -- The final verdict is returned to the caller and never fed back to this generator.
  local GENERATE_SYSTEM = [[You propose one improved version of a magi code-mode routine (a Lua script run by a dummy plug). Change the script only and preserve its tool allowlist and input schema. Inputs are already available in the local Lua table input. Read an optional field with input.field, for example local query = input.query or "default". Use call_tool(name, args) for tool calls. It returns text on success or nil and an error on failure. If judge is allowed, consider reusing an existing bounded question about supplied evidence; prefer a Lua rule when sufficient. Keep explore for evidence gathering. Use print to return findings. Use the development feedback from earlier rounds to address problems. Reply with exactly one line "HYPOTHESIS: <what this change should improve and why>" followed by the whole script in one ```lua block, and nothing else.]]

  local function generation_request(r, problem, feedback)
    return { system = GENERATE_SYSTEM, label = "routine-candidate", text =
      "Routine " .. r.name .. "\ntools: " .. table.concat(r.tools, ", ") .. "\ninputs: " .. magi.json_encode(r.inputs) ..
      "\n\nProblem: " .. problem .. "\n\nActive script:\n```lua\n" .. r.script ..
      "\n```\n\nDevelopment feedback from this run:\n" .. table.concat(feedback, "\n") }
  end

  local autoimprove_tool = {
    name = "routine_autoimprove",
    description = "Generate routine candidates and screen them on development cases. Compare the first passing candidate on the full frozen evaluation, including holdout, and apply it only within a /routine-auto delegation.",
    schema = { type = "object", properties = {
      name = { type = "string", description = "the routine" },
      problem = { type = "string", description = "what keeps going wrong, with the runs that showed it" },
      rounds = { type = "number", description = "candidates to try at most, default 3, at most 5" },
    }, required = { "name", "problem" } },
    execute = function(a, reply)
      a = a or {}
      local r, err = load_routine(a.name)
      if not r then return err, true end
      if type(a.problem) ~= "string" or a.problem == "" then return "problem is required: what keeps going wrong", true end
      local rounds = math.max(1, math.min(5, math.floor(tonumber(a.rounds) or 3)))
      local out, feedback = {}, {}
      local last_failed_script
      for i = 1, rounds do
        local request = generation_request(r, a.problem, feedback)
        if last_failed_script then
          request.text = request.text .. "\n\nMost recent candidate that failed development screening. Use the feedback above to repair it:\n```lua\n" ..
            last_failed_script:sub(1, 12000) .. "\n```"
        end
        local ok, raw, gerr
        if reply then ok, raw, gerr = true, reply.text, reply.error
        else ok, raw, gerr = pcall(magi.analyze, request) end
        if not ok or type(raw) ~= "string" then
          out[#out + 1] = string.format("round %d: no candidate (%s)", i, tostring(ok and gerr or raw))
          local message = ok and tostring(gerr):lower() or ""
          local transient = message:find("connection refused", 1, true) or message:find("connection reset", 1, true)
            or message:find("context deadline exceeded", 1, true) or message:find("timeout", 1, true)
            or message:match("http [45]%d%d") == "http 429" or message:match("http 5%d%d")
          return table.concat(out, "\n"), false, transient ~= nil and transient ~= false
        end
        local hyp, script = raw:match("HYPOTHESIS:%s*([^\n]+)"), raw:match("```lua\n(.-)\n```")
        if not hyp or not script then
          out[#out + 1] = string.format("round %d: the generator's reply had no hypothesis or script; nothing compared", i)
        else
          local candidate = { name = r.name, script = script, hypothesis = hyp, problem = a.problem,
            source = "routine_autoimprove round " .. i }
          local result, failed = improve(candidate, true)
          if failed then
            last_failed_script = script
            local line = string.format("round %d: development screening failed — %s (hypothesis: %s)", i, result, hyp)
            feedback[#feedback + 1] = line
            out[#out + 1] = line
          else
            local final, final_failed = improve(candidate, false)
            out[#out + 1] = string.format("round %d: full evaluation\n%s", i, final)
            return table.concat(out, "\n"), final_failed
          end
        end
      end
      out[#out + 1] = "checked: control flow on fixed fixtures, run dry — not the accuracy of real explore answers"
      return table.concat(out, "\n")
    end,
  }
  magi.register_tool(autoimprove_tool)

  local function pending_key(session)
    return "routine_pending:" .. magi.sha256(magi.workdir() .. "\0" .. session)
  end
  local function delegated_development(r)
    local delegation = stored("routine_auto")[r.name]
    local frozen = stored("routine_evals_frozen")[r.name]
    if not delegation or not delegation.start_on_failure or not frozen or not delegation.evals or
      delegation.evals.tasks ~= frozen.tasks or delegation.evals.contract ~= frozen.contract or
      delegation.inputs ~= magi.json_encode(r.inputs) or magi.json_encode(delegation.tools) ~= magi.json_encode(r.tools) then return end
    local dir = rdir(r.name)
    local text = magi.read_file(dir .. "tasks.json")
    local contract = magi.read_file(dir .. "contract.json")
    if not text or not contract or magi.sha256(text) ~= frozen.tasks or magi.sha256(contract) ~= frozen.contract then return end
    return delegation, frozen, text
  end

  local active_generation
  magi.on("turn_finished", function(ev)
    local session = ev.session
    if not session or session == "" or type(magi.analyze_async) ~= "function" then return end
    local queue = pending_key(session)
    local job = stored(queue)
    if not job.name then return end
    if job.dispatched then
      if active_generation == job.dispatched then return end
      local attempt = job.retry and 2 or 1
      local state = stored(job.key)
      if (state.attempt or 0) < attempt and
        not magi.store_set(job.key, { started = true, attempt = attempt }) then return end
      local path = rdir(job.name) .. "decisions.jsonl"
      local recorded
      for _, decision in ipairs(read_lines(path)) do
        if decision.dispatch == job.dispatched then recorded = true; break end
      end
      if recorded or append_line(path, { at = magi.time(), action = "generation interrupted",
        routine = job.name, version = job.fingerprint:sub(1, 12), dispatch = job.dispatched,
        attempt = attempt, by = "auto" }) then
        magi.store_set(queue, {})
      end
      return
    end
    local r = load_routine(job.name)
    if not r then magi.store_set(queue, {}); return end
    local delegation, _, task_text = delegated_development(r)
    if not delegation or delegation.id ~= job.delegation or r.fingerprint ~= job.fingerprint then
      magi.store_set(queue, {})
      return
    end
    local state = stored(job.key)
    local attempt = job.retry and 2 or 1
    if (state.attempt or (state.started and 1) or 0) >= attempt then
      magi.store_set(queue, {})
      return
    end
    local tasks = decode(task_text)
    local task = tasks and type(tasks.tasks) == "table" and tasks.tasks[job.case_index]
    if type(task) ~= "table" or task.split ~= "dev" then magi.store_set(queue, {}); return end
    local problem = "Two runs of frozen development case " .. tostring(task.id):sub(1, 120) ..
      " omitted expected evidence: " .. job.missing ..
      ". Development input: " .. magi.json_encode(task.input or {}):sub(1, 1200) ..
      ". Use this case to identify which input the active script ignores or handles incorrectly. " ..
      "Fix how the script uses that input for all valid values. Preserve the existing defaults and avoid hard-coding the example or its expected evidence."
    job.dispatched = magi.nonce()
    if not magi.store_set(queue, job) then return end
    if not magi.store_set(job.key, { started = true, attempt = attempt }) then
      job.dispatched = nil
      magi.store_set(queue, job)
      return
    end
    local started = magi.analyze_async(generation_request(r, problem, {}), function(answer, err)
      active_generation = nil
      local current = load_routine(job.name)
      local allowed = current and delegated_development(current)
      if not allowed or allowed.id ~= job.delegation or current.fingerprint ~= job.fingerprint then
        magi.store_set(queue, {})
        return
      end
      local result, _, transient = autoimprove_tool.execute(
        { name = job.name, problem = problem, rounds = 1 }, { text = answer, error = err })
      local summary = tostring(result):match("verdict: [^\r\n]+") or tostring(result):match("^[^\r\n]+")
      if summary then magi.log("[routine] automatic generation for " .. job.name .. ": " .. summary:sub(1, 400)) end
      if transient and not job.retry then
        job.retry = true
        job.dispatched = nil
        magi.store_set(queue, job)
      else
        magi.store_set(queue, {})
      end
    end)
    if started then
      active_generation = job.dispatched
    else
      job.dispatched = nil
      magi.store_set(queue, job)
      magi.store_set(job.key, state)
    end
  end)

  observe_routine_result = function(r, args, output)
    local session = magi.session_id()
    if session == "" or type(magi.in_child_session) ~= "function" or magi.in_child_session() then return end
    local queue = pending_key(session)
    if stored(queue).name then return end
    local delegation, frozen, text = delegated_development(r)
    if not delegation then return end
    local tasks = decode(text)
    if not tasks or type(tasks.tasks) ~= "table" then return end
    local input = magi.json_encode(args)
    local input_hash = magi.sha256(input)
    local matched, case_index
    for index, task in ipairs(tasks.tasks) do
      if type(task) == "table" and magi.json_encode(task.input or {}) == input then
        if task.split == "holdout" or matched then return end
        if task.split == "dev" then matched, case_index = task, index end
      end
    end
    local evidence = matched and type(matched.expect) == "table" and matched.expect.evidence
    if type(evidence) ~= "table" or #evidence == 0 then return end
    local missing = {}
    for _, ref in ipairs(evidence) do
      if type(ref) ~= "string" or ref == "" then return end
      if not output:find(ref, 1, true) then missing[#missing + 1] = ref:sub(1, 240) end
    end
    local key = magi.sha256(magi.workdir() .. "\0" .. r.name .. "\0" .. r.fingerprint)
    local state_key = "routine_failure:" .. key
    local state = stored(state_key)
    if state.started then return end
    if #missing == 0 then
      if state.input == input_hash then magi.store_set(state_key, {}) end
      return
    end
    if state.input ~= input_hash or state.tasks ~= frozen.tasks or state.contract ~= frozen.contract
      or state.delegation ~= delegation.id then
      magi.store_set(state_key, { input = input_hash, tasks = frozen.tasks, contract = frozen.contract, delegation = delegation.id })
      return
    end
    magi.store_set(queue, { name = r.name, fingerprint = r.fingerprint, key = state_key,
      delegation = delegation.id, case_index = case_index, missing = table.concat(missing, ", "):sub(1, 1200) })
  end

  magi.register_command{
    name = "routine-history",
    description = "Show a routine's improvement attempts and adoptions (/routine-history <name>)",
    execute = function(args)
      local name = words(args)[1]
      if not name or not name:match("^[%w_%-]+$") then return "usage: /routine-history <name>" end
      magi.log("[routine] history of " .. name .. ":\n" .. history_text(name, 20))
    end,
  }

  magi.register_command{
    name = "routine-evals-freeze",
    description = "Freeze a routine's contract.json and tasks.json as reviewed; routine_improve refuses evals changed since (/routine-evals-freeze <name>)",
    execute = function(args)
      local name = words(args)[1]
      if not name or not name:match("^[%w_%-]+$") then return "usage: /routine-evals-freeze <name>" end
      local dir = rdir(name)
      local ctext, ttext = magi.read_file(dir .. "contract.json"), magi.read_file(dir .. "tasks.json")
      if not decode(ctext) or not decode(ttext) then return "need " .. dir .. "contract.json and " .. dir .. "tasks.json, each a JSON object" end
      local all = stored("routine_evals_frozen")
      all[name] = { contract = magi.sha256(ctext), tasks = magi.sha256(ttext), at = magi.time() }
      magi.store_set("routine_evals_frozen", all)
      magi.log("[routine] froze the evals of " .. name .. " (contract " .. all[name].contract:sub(1, 12) .. ", tasks " .. all[name].tasks:sub(1, 12) .. ")")
    end,
  }

  magi.register_command{
    name = "routine-adopt",
    description = "Make an applicable candidate the active version, keeping the current one for rollback (/routine-adopt <name> <version>)",
    execute = function(args)
      local w = words(args)
      local name, ver = w[1], w[2]
      if not name or not ver then return "usage: /routine-adopt <name> <version>" end
      local ok, base, path = adopt_version(name, ver, "person")
      if not ok then return base end
      magi.log("[routine] adopted " .. ver .. " as " .. path .. " (was " .. base .. "); its next run asks for approval, and /routine-rollback " ..
        name .. " restores " .. base)
    end,
  }

  -- A delegation is what a person allows once: routine_improve may then apply an applicable candidate
  -- of this routine by itself, as long as nothing the person did not see changes — no writing tool,
  -- the same tools and inputs (so the same data reach), and the same frozen evals. It is not run
  -- approval: an applied version still asks before its next run.
  magi.register_command{
    name = "routine-auto",
    description = "Delegate automatic application of read-only candidates. Add start-on-failure to also allow model generation after repeated development failures (/routine-auto <name> [explore-text <directory>] [start-on-failure] | /routine-auto <name> off)",
    execute = function(args)
      local w = words(args)
      local start_on_failure = w[#w] == "start-on-failure" and #w > 1
      if start_on_failure then table.remove(w) end
      local name, off, directory = w[1], w[2], w[3]
      if not name or not name:match("^[%w_%-]+$") then return "usage: /routine-auto <name> | /routine-auto <name> off" end
      local all = stored("routine_auto")
      if off == "off" then
        if start_on_failure or directory then return "usage: /routine-auto <name> off" end
        all[name] = nil
        magi.store_set("routine_auto", all)
        append_line(rdir(name) .. "decisions.jsonl", { at = magi.time(), action = "auto-apply revoked", routine = name, by = "person" })
        magi.log("[routine] auto-apply revoked for " .. name)
        return
      end
      local r, err = load_routine(name)
      if not r then return err end
      for _, t in ipairs(r.tools) do
        if WRITES[t] then return "routine " .. name .. " may write with " .. t .. "; auto-apply covers read-only routines only" end
      end
      local frozen = stored("routine_evals_frozen")[name]
      if type(frozen) ~= "table" then return "freeze the evals of " .. name .. " first (/routine-evals-freeze " .. name .. ")" end
      if off ~= nil and off ~= "explore-text" then return "usage: /routine-auto <name> [explore-text <directory>] [start-on-failure] | /routine-auto <name> off" end
      if off == "explore-text" then
        if not (magi.script_features and magi.script_features.explore_read_scope) then
          return "scoped explore requires an updated magi host"
        end
        if not directory or directory == "." or directory:find(":", 1, true)
          or directory:find("\\", 1, true) or directory:sub(1, 1) == "/" or w[4] then
          return "usage: /routine-auto <name> explore-text <workspace subdirectory>"
        end
        for part in directory:gmatch("[^/]+") do
          if part == ".." then return "explore scope must stay within the workspace" end
        end
      elseif directory then
        return "usage: /routine-auto <name> [explore-text <directory>]"
      end
      local scope = { tools = r.tools, inputs = magi.json_encode(r.inputs), evals = { contract = frozen.contract, tasks = frozen.tasks },
        explore_text = off == "explore-text" or nil, explore_scope = directory, at = magi.time(), by = "person",
        start_on_failure = start_on_failure or nil, id = magi.nonce() }
      all[name] = scope
      local saved, save_err = magi.store_set("routine_auto", all)
      if not saved then return "could not save the delegation: " .. tostring(save_err) end
      append_line(rdir(name) .. "decisions.jsonl", { at = scope.at, action = "auto-apply delegated", routine = name, by = "person", scope = scope })
      magi.log("[routine] auto-apply delegated for " .. name .. " (tools " .. table.concat(r.tools, ", ") .. ", evals " ..
        frozen.tasks:sub(1, 12) .. (scope.explore_text and ", explore limited to " .. scope.explore_scope or ", explore questions fixed") ..
        (start_on_failure and ", model generation on repeated development failure enabled" or "") ..
        "); /routine-auto " .. name .. " off revokes it")
    end,
  }

  magi.register_command{
    name = "routine-rollback",
    description = "Restore the version a routine had before its last adoption (/routine-rollback <name>)",
    execute = function(args)
      local name = words(args)[1]
      if not name or not name:match("^[%w_%-]+$") then return "usage: /routine-rollback <name>" end
      local stack = stored("routine_rollback")
      local list = stack[name]
      if type(list) ~= "table" or #list == 0 then return "nothing to roll back for " .. name end
      local last = list[#list]
      local text = magi.read_file(rdir(name) .. "versions/" .. tostring(last.from) .. ".md")
      if type(text) ~= "string" then return "the earlier version " .. tostring(last.from) .. " is missing" end
      -- Save the person's veto before restoring the old version, so a write failure
      -- cannot leave a restored routine eligible for automatic reapplication.
      local decisions = rdir(name) .. "decisions.jsonl"
      local recorded, record_err = append_line(decisions, { at = magi.time(), action = "rollback requested", routine = name,
        version = last.from, replaced = last.to, by = "person" })
      if not recorded then return "could not record rollback: " .. tostring(record_err) end
      local ok, e = magi.write_file(last.path, text)
      if not ok then return "could not write " .. tostring(last.path) .. ": " .. tostring(e) end
      list[#list] = nil
      stack[name] = list
      magi.store_set("routine_rollback", stack)
      local audited, audit_err = append_line(decisions, { at = magi.time(), action = "rolled back", routine = name,
        version = last.from, replaced = last.to, by = "person" })
      if not audited then magi.log("[routine] could not record rollback completion: " .. tostring(audit_err)) end
      magi.log("[routine] rolled " .. name .. " back to " .. tostring(last.from) .. "; its next run asks for approval")
    end,
  }
end

magi.register_tool{
  name = "dummy_plug",
  background = true,
  description = USE,
  schema = {
    type = "object",
    properties = {
      goal = { type = "string", description = "one line: what the script is for (the subagent's task line)" },
      script = { type = "string", description = "Lua source to run" },
      background = { type = "boolean", description = "Return immediately and deliver the script result in a completion notice. Default false." },
      timeout = { type = "number", description = "seconds, default 60, at most 300" },
      output = { type = "number", description = "bytes of printed output kept, default 16000, at most 64000" },
    },
    required = { "script" },
  },
  execute = function(args)
    local script = args and args.script
    if type(script) ~= "string" or script == "" then return "script is required", true end
    local goal = args.goal
    if type(goal) ~= "string" or goal:match("^%s*$") then goal = "dummy plug script" end
    local globals, on_call, finish = track()
    local seq, graphs_seen = {}, {}
    local function watch(e)
      if e.phase == "start" then seq[#seq + 1] = e.tool
      elseif e.phase == "graph" and not graphs_seen[e.graph_id] then
        graphs_seen[e.graph_id] = true
        for _, task in ipairs(e.graph) do seq[#seq + 1] = task.tool end
      end
      return on_call(e)
    end
    local out, err = magi.run_script(script, {
      timeout = args.timeout, output = args.output, globals = globals, on_call = watch, child = goal,
    })
    finish(err == nil)
    if err then
      return (out or "") .. "[" .. err .. "]", true
    end
    note_run(goal, script, seq)
    local cand = note_sequence(goal, script, seq)
    if cand then
      out = (out or "") .. "\n[routine candidate " .. cand.id .. ": this sequence of calls (" .. cand.sequence ..
        ") has now run " .. cand.count .. " times. The person can review it with /routine-candidates and register it with /routine-register " ..
        cand.id .. " <name>; it is not registered until they do.]"
    end
    return out
  end,
}
