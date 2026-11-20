-- Dummy plug: a program pilots the tools instead of the model, one call at a time.
--
-- The model enters the mode, hands over a Lua script, and gets back only what the script printed.
-- Every call the script makes runs through the host (magi.run_script -> ToolEnv.CallTool): the
-- permission gate, the start fact and the result apply, and each call is recorded under its own
-- name — but kept out of the model's history, which is where the saving is.
--
-- The mode is per conversation and ends with the turn. Tools stay listed either way (a session's
-- tool list is fixed for its prompt cache); outside the mode dummy_plug_run refuses.

local inside = {} -- session id -> true while the plug is in

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
  local function report(force)
    local out = {}
    for _, st in ipairs(run.stages) do
      out[#out + 1] = { id = st.id, label = st.name, kind = st.kind, state = st.state,
        done = st.done, total = st.total, running = st.running }
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

local GUIDE = [[
Dummy plug mode is on. Send a Lua script to dummy_plug_run; only what it print()s comes back to you.

In the script:
- call_tool(name, args) runs a tool and returns its text, or nil and the reason. Open tools: read, grep, glob, list, explore, recall_memory (read-only).
- call_all({ {name, args}, ... }) runs several calls at the same time (at most 8 at once) and returns { {ok=true, text=} or {ok=false, error=} } in the order asked; one failure does not stop the rest.
- plan({name, ...}) lists the steps ahead (call it first): the person watching sees them all from the start, each updated in place. stage(name) opens the next step (a planned one by that name, or a new one). call_all fills the open step (or opens one) with its tool and k/n.
- progress(text) reports where the script is (for example "step 2/3: grep"); call_all reports k/n as calls finish.
- print(...) is the only output; keep it to what you need.
- json_encode(v) / json_decode(s), and Lua's string, table and math libraries. No files, processes or modules of its own.

Limits: 200 tool calls per run, 60 s by default (timeout up to 300), 16000 bytes of output by default (output up to 64000).
Use it for loops, filtering and many reads whose contents you do not need to see. Leave with dummy_plug_exit; the mode also ends with the turn.]]

magi.register_tool{
  name = "dummy_plug_enter",
  description = "Enter dummy plug mode: from here dummy_plug_run runs Lua scripts that call read-only tools for you and returns only what they print. Use it when a step needs many reads, loops or filtering you do not need to see call by call.",
  schema = { type = "object", properties = {} },
  execute = function()
    local sid = magi.session_id()
    if sid == "" then return "dummy plug: no conversation to enter", true end
    inside[sid] = true
    return GUIDE
  end,
}

magi.register_tool{
  name = "dummy_plug_run",
  description = "In dummy plug mode, run a Lua script. call_tool(name, args) runs read, grep, glob, list, explore or recall_memory; call_all runs several at once; stage(name) opens a step of the live run graph; progress(text) reports the step; only what the script print()s is returned.",
  schema = {
    type = "object",
    properties = {
      script = { type = "string", description = "Lua source to run" },
      timeout = { type = "number", description = "seconds, default 60, at most 300" },
      output = { type = "number", description = "bytes of printed output kept, default 16000, at most 64000" },
    },
    required = { "script" },
  },
  execute = function(args)
    local sid = magi.session_id()
    if not inside[sid] then
      return "not in dummy plug mode: call dummy_plug_enter first", true
    end
    local script = args and args.script
    if type(script) ~= "string" or script == "" then return "script is required", true end
    local globals, on_call, finish = track()
    local out, err = magi.run_script(script, {
      timeout = args.timeout, output = args.output, globals = globals, on_call = on_call,
    })
    finish(err == nil)
    if err then
      return (out or "") .. "[" .. err .. "]", true
    end
    return out
  end,
}

magi.register_tool{
  name = "dummy_plug_exit",
  description = "Leave dummy plug mode and go back to calling tools directly.",
  schema = { type = "object", properties = {} },
  execute = function()
    inside[magi.session_id()] = nil
    return "dummy plug mode is off; call tools directly"
  end,
}

magi.on("turn_finished", function(e)
  if e and e.session then inside[e.session] = nil end
end)
