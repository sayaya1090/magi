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

local USE = [[
Run one Lua script that calls tools for you, as a subagent no model drives: only what it print()s comes back.

In the script:
- call_tool(name, args) runs a tool and returns its text, or nil and the reason. Open tools: read, grep, glob, list, explore, recall_memory, bash, edit, multiedit, write. Each call follows the conversation's permission mode, as the model's own call would: one the person must approve is asked in the conversation and the script waits for the answer; a refusal comes back as nil and the reason. glob and list return a JSON array: json_decode it.
- call_all({ {name, args}, ... }) runs several calls at the same time (at most 8 at once) and returns { {ok=true, text=} or {ok=false, error=} } in the order asked; one failure does not stop the rest.
- call_graph({{id="a", tool="glob", args={pattern="docs/*.md"}}, {id="b", tool="read", after={"a"}, args={path="README.md"}}, ...}) runs tasks once their named predecessors succeed. Declare predecessors first. Independent safe calls overlap; a task with multiple predecessors joins their branches. The call returns output by task ID, or stops the script after a failure and skips its dependents.
- plan({name, ...}) lists the steps ahead (call it first): the person watching sees them all from the start, each updated in place. stage(name) opens the next step (a planned one by that name, or a new one). call_all fills the open step (or opens one) with its tool and k/n.
- progress(text) reports where the script is; print(...) is the only output, so keep it to what you need.
- json_encode(v) / json_decode(s), and Lua's string, table and math libraries. No files, processes or modules of its own.

Limits: 200 tool calls per run, 60 s by default (timeout up to 300), 16000 bytes of output by default (output up to 64000).
Use it for loops, filtering and many reads whose contents you do not need to see.]]

-- Routines: a code-mode script kept as a skill and run again by name (run_routine).
--
-- A routine is a skill file whose frontmatter says `kind: routine`: .magi/skills/<name>.md or
-- .claude/skills/<name>/SKILL.md. Besides `description:` it names the tools the script may call
-- (`tools: recall_memory, explore`) and its inputs as a one-line JSON Schema (`inputs: {...}`); the
-- script is the first ```lua block of the body and reads its arguments from `input`.
--
-- The person approves what runs: the first run, and any run after the script, the input schema or
-- the tool list changed, shows all three in the conversation's permission prompt (magi.approve). What
-- was approved is kept by fingerprint in this plugin's store, so it survives a restart. Approving the
-- routine grants nothing else — each call the script makes passes the permission gate as before, and
-- run_script refuses any tool outside the approved list. This version runs read-only routines only.
local READ_ONLY = { read = true, grep = true, glob = true, list = true, explore = true, recall_memory = true }
-- The tools a routine may call: this plugin's call_tools. Those that may write run one at a time
-- (run_script serial), and each still passes its own permission gate.
local CALLABLE = { read = true, grep = true, glob = true, list = true, explore = true, recall_memory = true,
  bash = true, edit = true, multiedit = true, write = true }
local WRITES = { bash = true, edit = true, multiedit = true, write = true }
-- Calls that carry a judgement (or are routines themselves): a stretch to repeat ends at them.
local JUDGE = { ask_user = true, council = true, todowrite = true, route_interjection = true, remember = true,
  label = true, hand_off = true, rate_handoff = true, skill = true, diagram = true, recall_context = true,
  run_routine = true, dummy_plug = true, routine_improve = true, routine_history = true, routine_autoimprove = true, routine_compare_writes = true, routine_compare_explore = true, routine_trace_path = true, routine_task_families = true, routine_compare_trace_policy = true }
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

local function load_routine(name)
  if type(name) ~= "string" or not name:match("^[%w_%-]+$") then
    return nil, "routine name must be letters, digits, - or _"
  end
  for _, path in ipairs({ ".magi/skills/" .. name .. ".md", ".claude/skills/" .. name .. "/SKILL.md" }) do
    local text = magi.read_file(path)
    if type(text) == "string" then
      local fm, body = frontmatter(text)
      if not fm or fm.kind ~= "routine" then
        return nil, path .. " is a skill, not a routine (its frontmatter has no kind: routine)"
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
        script = script, fingerprint = magi.sha256(canon), file_sha256 = magi.sha256(text), status = fm.status }
    end
  end
  return nil, "no routine named " .. name .. " (.magi/skills/" .. name .. ".md or .claude/skills/" .. name .. "/SKILL.md)"
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
  t.prompts = nil
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
    local files = magi.list_files(".magi/skills") or {}
    for _, file in ipairs(files) do
      if file:match("%.md$") then
        local path = ".magi/skills/" .. file
        local text = magi.read_file(path)
        local header = type(text) == "string" and text:match("^%-%-%-\n(.-)\n%-%-%-\n")
        if header and (header:find("\nsource: trace ", 1, true) or header:find("\nsource: repeated stretch ", 1, true))
          and not header:find("(synthetic arguments; observed completion order)", 1, true) then
          magi.log("[routine] saved draft may contain legacy arguments: " .. path)
        end
      end
    end
  end,
}

local function draft_from_calls(calls)
  if #calls == 0 then return nil, "no calls in that stretch" end
  if #calls > 1 then
    for _, c in ipairs(calls) do
      if not c.order_known then
        return nil, "call #" .. tostring(c.n) .. " has no verified sequential order; draft it separately and review dependencies"
      end
    end
  end
  local tools, seen, lines, props, req, fixtures = {}, {}, {}, {}, {}, {}
  for i, c in ipairs(calls) do
    if not CALLABLE[c.tool] then
      return nil, "step " .. i .. ": " .. tostring(c.tool) .. " cannot be called from a routine (not in this plugin's call_tools)"
    end
    if not seen[c.tool] then seen[c.tool] = true; tools[#tools + 1] = c.tool end
    local key = "step" .. i
    props[key] = { type = "object" }
    req[i] = key
    fixtures[key] = { fixture_step = i }
    lines[#lines + 1] = "-- TODO: supply arguments for step " .. i .. " and review its order and dependencies."
    lines[#lines + 1] = string.format("local r%d, e%d = call_tool(%q, input.%s)", i, i, c.tool, key)
    if c.ok ~= false then
      lines[#lines + 1] = string.format("if r%d == nil then error(%q .. tostring(e%d)) end", i, "step " .. i .. " (" .. c.tool .. ") failed: ", i)
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
    if got.tool ~= c.tool or got.args ~= magi.json_encode(fixtures["step" .. i]) then
      return "failed at step " .. i .. ": would call " .. tostring(got.tool), false
    end
  end
  if #calls > #expect then return "failed: made " .. #calls .. " calls, the trace has " .. #expect, false end
  return "passed " .. #expect .. "/" .. #expect, true
end

-- save_routine writes the routine file. Saving is choosing what to keep; running it is approved
-- separately, by its first run_routine. A draft (status: draft) is refused by run_routine until the
-- person has reviewed it and deleted that line.
local function save_routine(name, description, d, source, dry, draft)
  if type(name) ~= "string" or not name:match("^[%w_%-]+$") then return nil, "name: letters, digits, - or _" end
  local path = ".magi/skills/" .. name .. ".md"
  if type(magi.read_file(path)) == "string" then return nil, path .. " already exists; pick another name" end
  local fm = { "---", "kind: routine" }
  if draft then fm[#fm + 1] = "status: draft" end
  fm[#fm + 1] = "description: " .. (tostring(description or name):gsub("\n", " "))
  fm[#fm + 1] = "tools: " .. table.concat(d.tools, ", ")
  fm[#fm + 1] = "inputs: " .. d.inputs
  fm[#fm + 1] = "source: " .. source
  fm[#fm + 1] = "dry_check: " .. dry
  fm[#fm + 1] = "---"
  local note = draft and "A draft: review it, resolve each TODO, then delete the status line; run_routine refuses a draft.\n"
    or "Review it before its first run; that run asks for approval.\n"
  local ok, err = magi.write_file(path, table.concat(fm, "\n") .. "\n" .. note .. "\n```lua\n" .. d.script .. "\n```\n")
  if not ok then return nil, "could not write " .. path .. ": " .. tostring(err) end
  return path
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
  local path, werr = save_routine(name, description, d, source, dry, false)
  if not path then return nil, werr end
  return path, "saved " .. path .. " — dry run " .. dry .. "; its first run asks for approval"
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

-- note_repeats looks at the turn that just ended: each stretch between judgements and failures is
-- fingerprinted by tool name in observed completion order, and one seen in a second turn becomes a candidate.
-- A turn's opening reads (read, list, glob only) are left out: in the sessions measured they were
-- most of what repeated, and a model choosing what to read is not a procedure.
local function note_repeats(sid)
  local t = load_trace(sid)
  local turn, first_n = t.turn, nil
  local segs, seg = {}, {}
  for _, c in ipairs(t.calls) do
    if c.turn == turn then
      first_n = first_n or c.n
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
  for _, s in ipairs(segs) do
    local fits, opening = #s >= 2 and #s <= 12, s[1].n == first_n
    local tl = {}
    for _, c in ipairs(s) do
      if not CALLABLE[c.tool] then fits = false end
      if c.tool ~= "read" and c.tool ~= "list" and c.tool ~= "glob" then opening = false end
      tl[#tl + 1] = c.tool
    end
    if fits and not opening then
      local key = magi.sha256(table.concat(tl, "\n")):sub(1, 12)
      local seen = type(shapes[key]) == "table" and shapes[key] or {}
      seen.turns = type(seen.turns) == "table" and seen.turns or {}
      local tk = sid .. ":" .. turn
      if not seen.turns[tk] then
        seen.turns[tk] = true
        seen.count = (tonumber(seen.count) or 0) + 1
        shapes[key], changed = seen, true
        if seen.count == 2 then
          local id = "t" .. key:sub(1, 8)
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
    if n > SHAPES_KEEP then shapes[next(shapes)] = nil end
    magi.store_set("routine_shapes", shapes)
  end
end

magi.on("user_message", function(ev)
  if type(ev) ~= "table" or type(ev.session) ~= "string" or ev.session == "" then return end
  local t = load_trace(ev.session)
  t.turn = t.turn + 1
  magi.store_set(trace_key(ev.session), t)
  magi.store_set("routine_last_session", ev.session)
end)

magi.on("tool_finished", function(ev)
  if type(ev) ~= "table" or type(ev.session) ~= "string" or ev.session == "" or type(ev.tool) ~= "string" then return end
  local t = load_trace(ev.session)
  t.calls[#t.calls + 1] = { n = t.next, turn = t.turn, tool = ev.tool, ok = ev.ok == "true",
    call_id = ev.call_id, parallel_batch = ev.parallel_batch,
    order_known = ev.order_known == "true" and (ev.parallel_batch or "") == "" }
  t.next = t.next + 1
  while #t.calls > TRACE_KEEP do table.remove(t.calls, 1) end
  magi.store_set(trace_key(ev.session), t)
end)

magi.on("turn_finished", function(ev)
  if type(ev) == "table" and type(ev.session) == "string" and ev.session ~= "" then note_repeats(ev.session) end
end)

local ROUTINE_USE = [[
Run a routine by name: a code-mode script saved as a skill (frontmatter kind: routine) with the tools it may call and its inputs. You pass name and args; the script runs in a child session like dummy_plug and only what it prints comes back. The first run, and any run after the routine's script, inputs or tools changed, asks the person to approve it; a refusal comes back as an error and nothing runs.]]

local observe_routine_result
magi.register_tool{
  name = "run_routine",
  description = ROUTINE_USE,
  schema = {
    type = "object",
    properties = {
      name = { type = "string", description = "the routine's name (its skill file name)" },
      args = { type = "object", description = "the routine's inputs, as its inputs schema says" },
      timeout = { type = "number", description = "seconds, default 60, at most 300" },
      output = { type = "number", description = "bytes of printed output kept, default 16000, at most 64000" },
    },
    required = { "name" },
  },
  execute = function(a)
    local r, err = load_routine(a and a.name)
    if not r then return err, true end
    if r.status == "draft" then
      return "routine " .. r.name .. " is a draft (status: draft in " .. r.path .. "): review it, resolve its TODOs and " ..
        "delete the status line first; nothing ran", true
    end
    local writes = {}
    for _, t in ipairs(r.tools) do
      if WRITES[t] then writes[#writes + 1] = t end
    end
    local bad = check_inputs(r.inputs, a.args)
    if bad then return "routine " .. r.name .. ": " .. bad, true end
    local explore_scope = stored("routine_explore_scopes")[r.name .. ":" .. r.fingerprint:sub(1, 12)]
    if explore_scope and not (magi.script_features and magi.script_features.explore_read_scope) then
      return "scoped explore requires an updated magi host; nothing ran", true
    end
    local all = stored("routine_approvals")
    local prev = type(all[r.name]) == "table" and all[r.name] or nil
    if not prev or prev.fingerprint ~= r.fingerprint then
      local why = prev and ("routine " .. r.name .. " changed since it was approved") or ("run routine " .. r.name .. " for the first time")
      local ok, aerr = magi.approve{
        reason = why .. " — it may call: " .. table.concat(r.tools, ", ") .. (#writes > 0 and (" — it may WRITE with " ..
          table.concat(writes, ", ") .. "; each write still asks as usual, one at a time") or ""),
        detail = { routine = r.name, path = r.path, tools = r.tools, writes = writes, inputs = r.inputs, script = r.script,
          fingerprint = r.fingerprint, previous = prev and prev.fingerprint or nil, explore_read_scope = explore_scope },
      }
      if ok == nil then return "routine " .. r.name .. " could not be approved: " .. tostring(aerr), true end
      if not ok then return "routine " .. r.name .. " was not approved; nothing ran", true end
      all[r.name] = { fingerprint = r.fingerprint, tools = r.tools, path = r.path, at = magi.time() }
      magi.store_set("routine_approvals", all)
    end
    local globals, on_call, finish = track()
    local src = "local input = json_decode(" .. string.format("%q", magi.json_encode(a.args or {})) .. ")\n" .. r.script
    local out, rerr = magi.run_script(src, {
      timeout = a.timeout, output = a.output, globals = globals, on_call = on_call,
      child = "routine " .. r.name, tools = r.tools, serial = writes,
      explore_read_scope = explore_scope,
    })
    finish(rerr == nil)
    if observe_routine_result and not rerr then
      local observed, why = pcall(observe_routine_result, r, a.args or {}, out or "")
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
  description = "Save a reviewed routine candidate as .magi/skills/<name>.md, checked dry (/routine-register <id> <name>)",
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
            or (not c.order_known and " (order unknown)" or ""))
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


  local function rdir(name) return ".magi/routines/" .. name .. "/" end

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

  local function dry_run(script, input, fixtures, tools)
    local src = "local input = json_decode(" .. string.format("%q", magi.json_encode(input or {})) .. ")\n" .. script
    local out, err, calls = magi.run_script(src, { dry = true, fixtures = fixtures or {}, tools = tools, timeout = 10,
      globals = noop_globals() })
    calls = type(calls) == "table" and calls or {}
    local unverified = false
    for _, c in ipairs(calls) do
      if c.unverified then unverified = true end
    end
    return tostring(out or ""), err, calls, unverified
  end

  local function check_case(c, script, tools)
    local out, err, calls, unv = dry_run(script, c.input, c.fixtures, tools)
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
  local TRACE_DIR = ".magi/routines/" .. TRACE_FAMILY .. "/"
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
      out[#out + 1] = string.format("%s %s %s (replaced %s) by %s", tostring(d.at), tostring(d.action), tostring(d.version),
        tostring(d.replaced), tostring(d.by))
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
    local vtext = "---\nkind: routine\ndescription: " .. (tostring(r.description):gsub("\n", " ")) .. "\ntools: " ..
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
  local GENERATE_SYSTEM = [[You propose one improved version of a magi code-mode routine (a Lua script run by a dummy plug). Change the script only and preserve its tool allowlist and input schema. Inputs are already available in the local Lua table input. Read an optional field with input.field, for example local query = input.query or "default". Use call_tool(name, args) for tool calls. It returns text on success or nil and an error on failure. Use print to return findings. Use the development feedback from earlier rounds to address problems. Reply with exactly one line "HYPOTHESIS: <what this change should improve and why>" followed by the whole script in one ```lua block, and nothing else.]]

  local autoimprove_tool = {
    name = "routine_autoimprove",
    description = "Generate routine candidates and screen them on development cases. Compare the first passing candidate on the full frozen evaluation, including holdout, and apply it only within a /routine-auto delegation.",
    schema = { type = "object", properties = {
      name = { type = "string", description = "the routine" },
      problem = { type = "string", description = "what keeps going wrong, with the runs that showed it" },
      rounds = { type = "number", description = "candidates to try at most, default 3, at most 5" },
    }, required = { "name", "problem" } },
    execute = function(a)
      a = a or {}
      local r, err = load_routine(a.name)
      if not r then return err, true end
      if type(a.problem) ~= "string" or a.problem == "" then return "problem is required: what keeps going wrong", true end
      local rounds = math.max(1, math.min(5, math.floor(tonumber(a.rounds) or 3)))
      local out, feedback = {}, {}
      local last_failed_script
      for i = 1, rounds do
        local prompt = "Routine " .. r.name .. "\ntools: " .. table.concat(r.tools, ", ") .. "\ninputs: " .. magi.json_encode(r.inputs) ..
          "\n\nProblem: " .. a.problem .. "\n\nActive script:\n```lua\n" .. r.script ..
          "\n```\n\nDevelopment feedback from this run:\n" .. table.concat(feedback, "\n")
        if last_failed_script then
          prompt = prompt .. "\n\nMost recent candidate that failed development screening. Use the feedback above to repair it:\n```lua\n" ..
            last_failed_script:sub(1, 12000) .. "\n```"
        end
        local ok, raw, gerr = pcall(magi.analyze, { system = GENERATE_SYSTEM, text = prompt, label = "routine-candidate" })
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

  local pending = {}
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

  magi.on("turn_finished", function(ev)
    local session = ev.session
    local job = pending[session]
    if not job then return end
    pending[session] = nil
    local r = load_routine(job.name)
    if not r or r.fingerprint ~= job.fingerprint then return end
    local delegation = delegated_development(r)
    if not delegation or delegation.id ~= job.delegation then return end
    if not job.retry then
      if stored(job.key).started or not magi.store_set(job.key, { started = true }) then return end
    end
    local _, _, transient = autoimprove_tool.execute{ name = job.name, problem = job.problem, rounds = 1 }
    if transient and not job.retry then
      job.retry = true
      pending[session] = job
    end
  end)

  observe_routine_result = function(r, args, output)
    local session = magi.session_id()
    if session == "" or pending[session] then return end
    local delegation, frozen, text = delegated_development(r)
    if not delegation then return end
    local tasks = decode(text)
    if not tasks or type(tasks.tasks) ~= "table" then return end
    local input = magi.json_encode(args)
    local input_hash = magi.sha256(input)
    local matched
    for _, task in ipairs(tasks.tasks) do
      if type(task) == "table" and magi.json_encode(task.input or {}) == input then
        if task.split == "holdout" or matched then return end
        if task.split == "dev" then matched = task end
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
    local problem = "Two runs of frozen development case " .. tostring(matched.id):sub(1, 120) ..
      " omitted expected evidence: " .. table.concat(missing, ", "):sub(1, 1200) ..
      ". Development input: " .. input:sub(1, 1200)
    pending[session] = { name = r.name, fingerprint = r.fingerprint, key = state_key,
      delegation = delegation.id, problem = problem }
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
