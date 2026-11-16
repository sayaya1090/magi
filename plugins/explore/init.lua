-- explore: hand a reading job to a child that can only look.
--
-- Coding stays in the main conversation: a hand-off of work that depends on everything said so far
-- costs more than it saves. Reading a lot is different. What the child reads stays in the child and
-- only its findings come back, and children that can only read cannot collide, so the host runs
-- several explore calls of one step at once (readonly_children; the host refuses a writing tool).

local READERS = { "read", "grep", "glob", "list" }

-- How deep the reader digs: a quick lookup, the default, or a flow across many files.
local DEPTHS = {
  quick = { steps = 8, timeout = 120 },
  normal = { steps = 24, timeout = 300 },
  thorough = { steps = 40, timeout = 600 },
}

-- [plugins.explore] model routes readers to a cheaper model; empty means the session's model.
local MODEL = tostring(magi.store_get("model", "") or "")

local SYSTEM = table.concat({
  "You are a read-only exploration agent. You can only read, grep, glob and list files in the workspace.",
  "Answer the one question you are given by looking at the files; do not guess beyond what you read.",
  "Report what you found concisely: the answer first, then the evidence as path:line references with short quotes.",
  "Say plainly what you could not find or did not check. Do not propose or make changes.",
}, " ")

magi.register_tool{
  name = "explore",
  subagent = true,
  readonly_children = true,
  description = "Delegate a read-only investigation to a subagent that can only read, grep, glob and list, " ..
    "and get back its findings with path:line evidence. Read one or two files directly yourself — a delegation " ..
    "costs a whole child turn. Use this when there is a lot to read or search: give each call one independent " ..
    "question, and put several explore calls in one step so they run in parallel. Do not use it for writing, " ..
    "editing, running commands, or work that depends on this conversation's context. " ..
    "depth: quick to locate one thing, normal by default, thorough to trace a flow across many files.",
  schema = {
    type = "object", additionalProperties = false,
    properties = {
      question = { type = "string", maxLength = 4000, description = "One self-contained question about the workspace, with the names or paths the child needs." },
      scope = { type = "string", maxLength = 500, description = "Optional directory or file pattern to look in first." },
      depth = { type = "string", enum = { "quick", "normal", "thorough" }, description = "quick: locate one thing; normal (default); thorough: trace a flow across many files." },
    },
    required = { "question" },
  },
  execute = function(args)
    local q = type(args) == "table" and type(args.question) == "string" and args.question:match("^%s*(.-)%s*$") or ""
    if q == "" then return "explore: question is required", true end
    local prompt = q
    if type(args.scope) == "string" and args.scope:match("%S") then
      prompt = prompt .. "\n\nLook first in: " .. args.scope
    end
    local d = DEPTHS[args.depth] or DEPTHS.normal
    local spec = { system = SYSTEM, prompt = prompt, tools = READERS, max_steps = d.steps, timeout = d.timeout, lean = true }
    if MODEL ~= "" then spec.model = MODEL end
    local ok, r = pcall(magi.spawn, spec)
    if not ok then return "explore: " .. tostring(r), true end
    if type(r) ~= "table" then return "explore: no result", true end
    if r.err and r.err ~= "" then return "explore: the reader failed: " .. r.err, true end
    local text = r.text or ""
    if not text:match("%S") then return "explore: the reader returned nothing", true end
    return text
  end,
}
