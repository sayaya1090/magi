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
    for _, st in ipairs(run.stages) do
      out[#out + 1] = { id = st.id, label = st.name, kind = st.kind, state = st.state,
        done = st.done, total = st.total, running = st.running }
    end
    for _, graph in ipairs(graphs) do
      for _, task in ipairs(graph) do out[#out + 1] = task end
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
      graphs[e.graph_id] = graph
      report(true)
      return
    end
    if e.batch == 0 then return end -- a single call_tool is not a step of its own
    local i = batches[e.batch]
    if not i then
      local cur = run.stages[run.current]
      if not cur or cur.total > 0 or cur.state ~= "running" then open(e.tool) end
      cur = run.stages[run.current]
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
    if #run.stages == 0 then return end
    local cur = run.stages[run.current]
    if cur and cur.state == "running" then cur.state = ok and "done" or "failed"; cur.running = 0 end
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
    local out, err = magi.run_script(script, {
      timeout = args.timeout, output = args.output, globals = globals, on_call = on_call, child = goal,
    })
    finish(err == nil)
    if err then
      return (out or "") .. "[" .. err .. "]", true
    end
    return out
  end,
}
