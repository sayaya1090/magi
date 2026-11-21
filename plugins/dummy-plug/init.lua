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
  run_routine = true, dummy_plug = true, routine_improve = true, routine_history = true }
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
        script = script, fingerprint = magi.sha256(canon), status = fm.status }
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
local TRACE_KEEP, TRACE_ARGS_MAX, SHAPES_KEEP, RUNS_KEEP = 300, 8000, 500, 30

local function trace_key(sid) return "routine_trace:" .. tostring(sid) end

local function load_trace(sid)
  local t = stored(trace_key(sid))
  t.calls = type(t.calls) == "table" and t.calls or {}
  t.prompts = type(t.prompts) == "table" and t.prompts or {}
  t.turn = tonumber(t.turn) or 0
  t.next = tonumber(t.next) or 1
  return t
end

-- dynamic: a word that names one run's thing — a hash, a session, call or task id, a timestamp — and
-- so is an input of a routine rather than part of what it does.
local function dynamic(w)
  if #w < 7 then return false end
  if w:match("^s_%x+$") or w:match("^call_cc_%x+$") then return true end
  if w:match("^%x+$") and w:match("%a") and w:match("%d") then return true end
  if w:match("^[%w%-]+%-%x%x%x%x%x%x%x%x$") and w:match("%d") then return true end
  return w:match("^%d%d%d%d%d%d%d%dT%d%d%d%d%d%d$") ~= nil
end

local function shape_args(a)
  local s = (a:gsub("[%w_%-]+", function(w) if dynamic(w) then return "<D>" end end))
  return (s:gsub("%d+", "<N>"))
end

local function replace_plain(s, old, new)
  local out, i = {}, 1
  while true do
    local a, b = s:find(old, i, true)
    if not a then break end
    out[#out + 1] = s:sub(i, a - 1)
    out[#out + 1] = new
    i = b + 1
  end
  out[#out + 1] = s:sub(i)
  return table.concat(out)
end

-- draft_from_calls turns traced calls into a script: one call_tool line each, the run's dynamic
-- words as inputs. A word that first shows up after the first step and is not in the request likely
-- came from an earlier result, which a trace does not hold: it is an input with a TODO beside it.
local function draft_from_calls(calls, prompt)
  if #calls == 0 then return nil, "no calls in that stretch" end
  local tools, seen = {}, {}
  for i, c in ipairs(calls) do
    if not CALLABLE[c.tool] then
      return nil, "step " .. i .. ": " .. tostring(c.tool) .. " cannot be called from a routine (not in this plugin's call_tools)"
    end
    if type(c.args) ~= "string" then return nil, "step " .. i .. ": its arguments were too large to keep" end
    if not seen[c.tool] then seen[c.tool] = true; tools[#tools + 1] = c.tool end
  end
  table.sort(tools)
  local params, by, earlier = {}, {}, prompt or ""
  for i, c in ipairs(calls) do
    for w in c.args:gmatch("[%w_%-]+") do
      if dynamic(w) and not by[w] then
        local p = { name = "p" .. (#params + 1), value = w, step = i }
        p.from_result = i > 1 and not earlier:find(w, 1, true)
        params[#params + 1] = p
        by[w] = p
      end
    end
    earlier = earlier .. "\n" .. c.args
  end
  local longest = {}
  for _, p in ipairs(params) do longest[#longest + 1] = p end
  table.sort(longest, function(a, b) return #a.value > #b.value end)
  local lines = {
    "local function fill(s)",
    "  return (s:gsub(\"{{([%w_]+)}}\", function(k) return json_encode(tostring(input[k])):sub(2, -2) end))",
    "end",
  }
  local todo = {}
  for i, c in ipairs(calls) do
    local a = c.args
    for _, p in ipairs(longest) do a = replace_plain(a, p.value, "{{" .. p.name .. "}}") end
    for _, p in ipairs(params) do
      if p.step == i and p.from_result then
        lines[#lines + 1] = "-- TODO: input." .. p.name .. " (" .. p.value .. ") first appears here and is not in the request; it" ..
          " likely came from an earlier step's result. Take it from that result before relying on this routine."
        todo[#todo + 1] = p.name
      end
    end
    lines[#lines + 1] = string.format("local r%d, e%d = call_tool(%q, json_decode(fill(%q)))", i, i, c.tool, a)
    if c.ok == false then
      lines[#lines + 1] = "-- step " .. i .. " failed when it was traced; its failure does not stop the routine"
    else
      lines[#lines + 1] = string.format("if r%d == nil then error(%q .. tostring(e%d)) end", i, "step " .. i .. " (" .. c.tool .. ") failed: ", i)
    end
  end
  lines[#lines + 1] = string.format("print(%q)", "done: " .. #calls .. " steps")
  local props, req, ex = {}, {}, {}
  for _, p in ipairs(params) do
    props[#props + 1] = string.format("%q:{\"type\":\"string\"}", p.name)
    req[#req + 1] = string.format("%q", p.name)
    ex[#ex + 1] = string.format("%q:%s", p.name, magi.json_encode(p.value))
  end
  return { script = table.concat(lines, "\n"), tools = tools, todo = todo,
    inputs = '{"type":"object","properties":{' .. table.concat(props, ",") .. '},"required":[' .. table.concat(req, ",") .. "]}",
    example = "{" .. table.concat(ex, ",") .. "}" }
end

local function canon(s)
  local ok, v = pcall(magi.json_decode, s)
  if not ok or v == nil then return tostring(s) end
  return magi.json_encode(v)
end

-- dry_check runs a script with no tool reached and compares the calls it would make with expect (the
-- traced calls, in order). Without expect (a dummy_plug run, whose later calls may read earlier
-- results a dry run leaves empty) it only reports how far the script got.
local function dry_check(script, example, tools, expect)
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
    if got.tool ~= c.tool or canon(got.args) ~= canon(c.args) then
      return "failed at step " .. i .. ": would call " .. tostring(got.tool) .. " " .. tostring(got.args), false
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
  if d.example then fm[#fm + 1] = "example: " .. d.example end
  fm[#fm + 1] = "source: " .. source
  fm[#fm + 1] = "dry_check: " .. dry
  fm[#fm + 1] = "---"
  local note = draft and "A draft: review it, resolve each TODO, then delete the status line; run_routine refuses a draft.\n"
    or "Review it before its first run; that run asks for approval.\n"
  local ok, err = magi.write_file(path, table.concat(fm, "\n") .. "\n" .. note .. "\n```lua\n" .. d.script .. "\n```\n")
  if not ok then return nil, "could not write " .. path .. ": " .. tostring(err) end
  return path
end

local function draft_trace(name, calls, prompt, source, description)
  local d, err = draft_from_calls(calls, prompt)
  if not d then return nil, err end
  local dry, ok = dry_check(d.script, d.example, d.tools, calls)
  local draft = #d.todo > 0 or not ok
  local path, werr = save_routine(name, description, d, source, dry, draft)
  if not path then return nil, werr end
  local why = "saved " .. path .. " (" .. #calls .. " steps) — dry check " .. dry
  if #d.todo > 0 then why = why .. "; TODO " .. table.concat(d.todo, ", ") .. ": may come from an earlier result" end
  if draft then why = why .. "; it is a draft until you review it and delete its status line" end
  return path, why .. "; its first run asks for approval"
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
-- fingerprinted with its dynamic words blanked, and one seen in a second turn becomes a candidate.
-- A turn's opening reads (read, list, glob only) are left out: in the sessions measured they were
-- most of what repeated, and a model choosing what to read is not a procedure.
local function note_repeats(sid)
  local t = load_trace(sid)
  local turn, first_n = t.turn, nil
  local segs, seg = {}, {}
  for _, c in ipairs(t.calls) do
    if c.turn == turn then
      first_n = first_n or c.n
      if JUDGE[c.tool] then
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
    local parts, tl = {}, {}
    for _, c in ipairs(s) do
      if not CALLABLE[c.tool] or type(c.args) ~= "string" then fits = false end
      if c.tool ~= "read" and c.tool ~= "list" and c.tool ~= "glob" then opening = false end
      parts[#parts + 1] = c.tool .. " " .. (type(c.args) == "string" and shape_args(c.args) or "")
      tl[#tl + 1] = c.tool
    end
    if fits and not opening then
      local key = magi.sha256(table.concat(parts, "\n")):sub(1, 12)
      local seen = type(shapes[key]) == "table" and shapes[key] or {}
      seen.turns = type(seen.turns) == "table" and seen.turns or {}
      local tk = sid .. ":" .. turn
      if not seen.turns[tk] then
        seen.turns[tk] = true
        seen.count = (tonumber(seen.count) or 0) + 1
        shapes[key], changed = seen, true
        if seen.count == 2 then
          local id = "t" .. key:sub(1, 8)
          local preview = {}
          for _, c in ipairs(s) do preview[#preview + 1] = c.tool .. " " .. c.args end
          local tools, set = {}, {}
          for _, x in ipairs(tl) do if not set[x] then set[x] = true; tools[#tools + 1] = x end end
          table.sort(tools)
          local cands = stored("routine_candidates")
          cands[id] = { id = id, kind = "trace", sequence = table.concat(tl, ">"), count = 2, tools = tools,
            goal = "repeated stretch: " .. table.concat(tl, " > "), calls = s, prompt = t.prompts[tostring(turn)] or "",
            script = table.concat(preview, "\n") }
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
  t.prompts[tostring(t.turn)] = tostring(ev.text or ""):sub(1, 4000)
  local low, keep = t.calls[1] and tonumber(t.calls[1].turn) or t.turn, {}
  for k, v in pairs(t.prompts) do
    if tonumber(k) and tonumber(k) >= low then keep[k] = v end
  end
  t.prompts = keep
  magi.store_set(trace_key(ev.session), t)
  magi.store_set("routine_last_session", ev.session)
end)

magi.on("tool_finished", function(ev)
  if type(ev) ~= "table" or type(ev.session) ~= "string" or ev.session == "" or type(ev.tool) ~= "string" then return end
  local t = load_trace(ev.session)
  local args = ev.args
  if type(args) ~= "string" or #args > TRACE_ARGS_MAX then args = nil end
  t.calls[#t.calls + 1] = { n = t.next, turn = t.turn, tool = ev.tool, args = args, ok = ev.ok == "true" }
  t.next = t.next + 1
  while #t.calls > TRACE_KEEP do table.remove(t.calls, 1) end
  magi.store_set(trace_key(ev.session), t)
end)

magi.on("turn_finished", function(ev)
  if type(ev) == "table" and type(ev.session) == "string" and ev.session ~= "" then note_repeats(ev.session) end
end)

local ROUTINE_USE = [[
Run a routine by name: a code-mode script saved as a skill (frontmatter kind: routine) with the tools it may call and its inputs. You pass name and args; the script runs in a child session like dummy_plug and only what it prints comes back. The first run, and any run after the routine's script, inputs or tools changed, asks the person to approve it; a refusal comes back as an error and nothing runs.]]

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
    local all = stored("routine_approvals")
    local prev = type(all[r.name]) == "table" and all[r.name] or nil
    if not prev or prev.fingerprint ~= r.fingerprint then
      local why = prev and ("routine " .. r.name .. " changed since it was approved") or ("run routine " .. r.name .. " for the first time")
      local ok, aerr = magi.approve{
        reason = why .. " — it may call: " .. table.concat(r.tools, ", ") .. (#writes > 0 and (" — it may WRITE with " ..
          table.concat(writes, ", ") .. "; each write still asks as usual, one at a time") or ""),
        detail = { routine = r.name, path = r.path, tools = r.tools, writes = writes, inputs = r.inputs, script = r.script,
          fingerprint = r.fingerprint, previous = prev and prev.fingerprint or nil },
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
    })
    finish(rerr == nil)
    if rerr then return (out or "") .. "[" .. rerr .. "]", true end
    return out
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
    for id, c in pairs(stored("routine_candidates")) do
      lines[#lines + 1] = id .. "  " .. tostring(c.sequence) .. "  ×" .. tostring(c.count) .. "  " .. tostring(c.goal) ..
        "\n  tools: " .. table.concat(c.tools or {}, ", ") .. "\n  script:\n" .. tostring(c.script)
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
    local cands = stored("routine_candidates")
    local c = id and cands[id]
    if type(c) ~= "table" then return "no routine candidate " .. tostring(id) .. " (see /routine-candidates)" end
    local path, why
    if c.kind == "trace" then
      path, why = draft_trace(name, c.calls or {}, c.prompt, "repeated stretch " .. id, c.goal)
    else
      path, why = draft_script(name, c.script, c.tools, "routine candidate " .. id, c.goal)
    end
    if not path then return why end
    cands[id] = nil
    magi.store_set("routine_candidates", cands)
    magi.log("[routine] " .. why)
  end,
}

magi.register_command{
  name = "routine-trace",
  description = "List this conversation's recent tool calls and dummy_plug runs, to pick from for /routine-draft",
  execute = function(args)
    local n = tonumber(words(args)[1]) or 30
    local sid = magi.store_get("routine_last_session")
    local lines = {}
    if type(sid) == "string" and sid ~= "" then
      local t = load_trace(sid)
      for i = math.max(1, #t.calls - n + 1), #t.calls do
        local c = t.calls[i]
        lines[#lines + 1] = string.format("#%d turn %s %s%s %s", c.n, tostring(c.turn), tostring(c.tool),
          c.ok == false and " (failed)" or "", (tostring(c.args or "(too large to keep)"):sub(1, 120)))
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
      local sid = magi.store_get("routine_last_session")
      if type(sid) ~= "string" or sid == "" then return "no conversation traced yet" end
      local t = load_trace(sid)
      local calls, turns, prompts = {}, {}, {}
      for _, c in ipairs(t.calls) do
        if c.n >= a and c.n <= b then
          if JUDGE[c.tool] then return "call #" .. c.n .. " (" .. tostring(c.tool) .. ") is not a step a routine repeats" end
          calls[#calls + 1] = c
          if not turns[c.turn] then turns[c.turn] = true; prompts[#prompts + 1] = t.prompts[tostring(c.turn)] or "" end
        end
      end
      if #calls ~= b - a + 1 then return "calls #" .. a .. "-" .. b .. " are not all in the trace any more (see /routine-trace)" end
      path, why = draft_trace(name, calls, table.concat(prompts, "\n"), "trace " .. sid .. " #" .. a .. "-" .. b,
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
      if c.tool == "explore" then cost.model_calls = cost.model_calls + 1 end
    end
    if unv then return "unverified" end
    local exp = type(tk.expect) == "table" and type(tk.expect.evidence) == "table" and tk.expect.evidence or {}
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

  local IMPROVE_USE = [[
Compare a candidate version of a routine with its active version, and record the attempt. You pass the candidate's whole script, the problem it addresses and your hypothesis (read routine_history first: earlier attempts, and why they were rejected). The candidate is checked against the routine's frozen contract (fixed fixtures, run dry: control flow only), then paired with the active version on the same frozen tasks. Each task is judged on its own — newly solved, newly regressed, still failing, kept — and the verdict is applicable, held (gain too thin, unstable or regressions to review) or rejected (contract broken, a protected task lost, tools or inputs changed). It never changes the active routine, its evals or its tools; a person adopts an applicable candidate.]]

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
        tools = { type = "array", items = { type = "string" }, description = "only if it differs from the active version's (it will be rejected: tools are the person's to change)" },
        inputs = { type = "object", description = "only if it differs from the active version's (rejected likewise)" },
      },
      required = { "name", "script", "hypothesis" },
    },
    execute = function(a)
      a = a or {}
      local r, err = load_routine(a.name)
      if not r then return err, true end
      if type(a.script) ~= "string" or a.script == "" then return "script is required", true end
      if type(a.hypothesis) ~= "string" or a.hypothesis == "" then
        return "hypothesis is required: what this change should improve, and why", true
      end
      local dir = rdir(r.name)
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
      local hold_solved = 0
      local cost = { active = { calls = 0, model_calls = 0 }, candidate = { calls = 0, model_calls = 0 } }
      local reps = math.max(1, math.floor(tonumber(tasks.repeats) or 3))
      if not reject then
        for _, tk in ipairs(type(tasks.tasks) == "table" and tasks.tasks or {}) do
          local b = task_result(tk, r.script, r.tools, reps, cost.active)
          local c = task_result(tk, a.script, tools, reps, cost.candidate)
          local tr = transition(b, c)
          n[tr] = n[tr] + 1
          local id = tostring(tk.id)
          rows[#rows + 1] = { id = id, split = tostring(tk.split or "dev"), protected = tk.protected == true or nil,
            active = b, candidate = c, transition = tr }
          if tr == "regressed" then
            if tk.protected == true then plost[#plost + 1] = id else lost[#lost + 1] = id end
            diffs[#diffs + 1] = -1
          elseif tr == "solved" then
            diffs[#diffs + 1] = 1
            if tk.split == "holdout" then hold_solved = hold_solved + 1 end
          elseif tr == "kept" or tr == "still_failing" then
            diffs[#diffs + 1] = 0
          else
            shaky[#shaky + 1] = id .. " (" .. tr .. ")"
          end
        end
      end

      local effect
      if #diffs > 0 then
        local m, v = 0, 0
        for _, d in ipairs(diffs) do m = m + d end
        m = m / #diffs
        for _, d in ipairs(diffs) do v = v + (d - m) ^ 2 end
        local se = (#diffs > 1 and math.sqrt(v / (#diffs - 1)) or 0) / math.sqrt(#diffs)
        effect = { mean = m, low = m - 1.96 * se, high = m + 1.96 * se, tasks = #diffs,
          sign_p = sign_p(n.solved, n.solved + n.regressed) }
      end

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
        if cost.candidate.calls < cost.active.calls then
          verdict, reason = "applicable", "same task results with fewer calls"
        else
          verdict, reason = "held", "no task newly solved"
        end
      elseif effect.sign_p > IMPROVE_P then
        verdict, reason = "held", string.format("evidence too thin: %d newly solved, 0 regressed over %d tasks (sign test p=%.3f > %.2f); add tasks before adopting",
          n.solved, #diffs, effect.sign_p, IMPROVE_P)
      elseif hold_solved == 0 then
        verdict, reason = "held", "no gain on the holdout tasks; the gain may fit the dev tasks only"
      else
        verdict, reason = "applicable", string.format("solves %d tasks (%d holdout), regresses none (sign test p=%.3f)",
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
        baseline = { version = base, path = r.path },
        candidate = { version = ver, path = vpath, change = change, tools = tools },
        problem = a.problem, hypothesis = a.hypothesis, expected = a.expected, source = a.source,
        conditions = { evals = { contract = csha, tasks = tsha, frozen_at = frozen.at }, repeats = reps, tools = tools,
          mode = "dry run on fixed fixtures: no tool reached, no model",
          revision = "not recorded: fixtures stand in for the repository" },
        contract = #contract_rows > 0 and contract_rows or nil,
        tasks = #rows > 0 and rows or nil,
        summary = n, effect = effect, cost = cost,
        decision = { verdict = verdict, reason = reason, by = "routine_improve" },
      }
      local aok, aerr = append_line(dir .. "attempts.jsonl", attempt)
      if not aok then return "could not record the attempt: " .. tostring(aerr), true end

      local lines = {
        string.format("routine %s: candidate %s vs active %s (%s)", r.name, ver, base, change),
        "verdict: " .. verdict .. " — " .. reason,
      }
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
          lines[#lines + 1] = string.format("effect: %+.2f per task (95%% interval %+.2f..%+.2f over %d tasks), sign test p=%.3f",
            effect.mean, effect.low, effect.high, effect.tasks, effect.sign_p)
        end
        lines[#lines + 1] = string.format("cost (dry, x%d): active %d calls / %d explore, candidate %d calls / %d explore", reps,
          cost.active.calls, cost.active.model_calls, cost.candidate.calls, cost.candidate.model_calls)
      end
      if #writes > 0 then lines[#lines + 1] = "writes (" .. table.concat(writes, ", ") .. "): compared dry only; effects on files are not evaluated" end
      lines[#lines + 1] = "recorded as attempt " .. attempt.id .. " in " .. dir .. "attempts.jsonl; " ..
        (verdict == "applicable" and ("a person may adopt it with /routine-adopt " .. r.name .. " " .. ver) or "it cannot be adopted")
      lines[#lines + 1] = "checked: control flow on fixed fixtures, run dry — not the accuracy of real explore answers, not effects on files, not new tasks"
      return table.concat(lines, "\n")
    end,
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
      local r, err = load_routine(name)
      if not r then return err end
      local dir, att = rdir(name), nil
      for _, a in ipairs(read_lines(dir .. "attempts.jsonl")) do
        if type(a.candidate) == "table" and a.candidate.version == ver then att = a end
      end
      if not att then return "no recorded attempt for candidate " .. ver .. " of " .. name .. " (see /routine-history " .. name .. ")" end
      local d = type(att.decision) == "table" and att.decision or {}
      if d.verdict ~= "applicable" then
        return "candidate " .. ver .. " was " .. tostring(d.verdict) .. ": " .. tostring(d.reason) .. "; only an applicable candidate can be adopted"
      end
      local base = r.fingerprint:sub(1, 12)
      if type(att.baseline) ~= "table" or att.baseline.version ~= base then
        return "candidate " .. ver .. " was compared with " .. tostring(type(att.baseline) == "table" and att.baseline.version) ..
          ", but the active version is now " .. base .. "; compare it again"
      end
      local vtext, cur = magi.read_file(dir .. "versions/" .. ver .. ".md"), magi.read_file(r.path)
      if type(vtext) ~= "string" or type(cur) ~= "string" then return "the candidate's or the active version's file is missing" end
      local ok1, e1 = magi.write_file(dir .. "versions/" .. base .. ".md", cur)
      if not ok1 then return "could not keep the active version: " .. tostring(e1) end
      local ok2, e2 = magi.write_file(r.path, vtext)
      if not ok2 then return "could not write " .. r.path .. ": " .. tostring(e2) end
      local stack = stored("routine_rollback")
      local list = type(stack[name]) == "table" and stack[name] or {}
      list[#list + 1] = { from = base, to = ver, path = r.path }
      stack[name] = list
      magi.store_set("routine_rollback", stack)
      append_line(dir .. "decisions.jsonl", { at = magi.time(), action = "adopted", routine = name, version = ver, replaced = base,
        attempt = att.id, by = "person" })
      magi.log("[routine] adopted " .. ver .. " as " .. r.path .. " (was " .. base .. "); its next run asks for approval, and /routine-rollback " ..
        name .. " restores " .. base)
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
      local ok, e = magi.write_file(last.path, text)
      if not ok then return "could not write " .. tostring(last.path) .. ": " .. tostring(e) end
      list[#list] = nil
      stack[name] = list
      magi.store_set("routine_rollback", stack)
      append_line(rdir(name) .. "decisions.jsonl", { at = magi.time(), action = "rolled back", routine = name,
        version = last.from, replaced = last.to, by = "person" })
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
