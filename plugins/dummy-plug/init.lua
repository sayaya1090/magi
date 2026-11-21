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
        if not READ_ONLY[t] then
          return nil, path .. ": " .. t .. " is not read-only; this version runs routines of read, grep, glob, list, explore and recall_memory only"
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
        script = script, fingerprint = magi.sha256(canon) }
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
    if not READ_ONLY[t] then return nil end
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
    local bad = check_inputs(r.inputs, a.args)
    if bad then return "routine " .. r.name .. ": " .. bad, true end
    local all = stored("routine_approvals")
    local prev = type(all[r.name]) == "table" and all[r.name] or nil
    if not prev or prev.fingerprint ~= r.fingerprint then
      local why = prev and ("routine " .. r.name .. " changed since it was approved") or ("run routine " .. r.name .. " for the first time")
      local ok, aerr = magi.approve{
        reason = why .. " — it may call: " .. table.concat(r.tools, ", "),
        detail = { routine = r.name, path = r.path, tools = r.tools, inputs = r.inputs, script = r.script,
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
      child = "routine " .. r.name, tools = r.tools,
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
  description = "Register a reviewed routine candidate as .magi/skills/<name>.md (/routine-register <id> <name>)",
  execute = function(args)
    local w = words(args)
    local id, name = w[1], w[2]
    local cands = stored("routine_candidates")
    local c = id and cands[id]
    if type(c) ~= "table" then return "no routine candidate " .. tostring(id) .. " (see /routine-candidates)" end
    if not name or not name:match("^[%w_%-]+$") then return "usage: /routine-register <id> <name> (name: letters, digits, - or _)" end
    local path = ".magi/skills/" .. name .. ".md"
    if type(magi.read_file(path)) == "string" then return path .. " already exists; pick another name" end
    local text = "---\nkind: routine\ndescription: " .. tostring(c.goal):gsub("\n", " ") ..
      "\ntools: " .. table.concat(c.tools or {}, ", ") .. "\ninputs: {\"type\":\"object\",\"properties\":{}}\n---\n" ..
      "Registered from routine candidate " .. id .. " (" .. tostring(c.sequence) .. "). Run it with run_routine{name=\"" .. name ..
      "\"}; its first run asks for approval.\n\n```lua\n" .. tostring(c.script) .. "\n```\n"
    local ok, err = magi.write_file(path, text)
    if not ok then return "could not write " .. path .. ": " .. tostring(err) end
    cands[id] = nil
    magi.store_set("routine_candidates", cands)
    magi.log("[routine] registered " .. path .. "; its first run with run_routine asks for approval")
  end,
}

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
    local cand = note_sequence(goal, script, seq)
    if cand then
      out = (out or "") .. "\n[routine candidate " .. cand.id .. ": this sequence of calls (" .. cand.sequence ..
        ") has now run " .. cand.count .. " times. The person can review it with /routine-candidates and register it with /routine-register " ..
        cand.id .. " <name>; it is not registered until they do.]"
    end
    return out
  end,
}
