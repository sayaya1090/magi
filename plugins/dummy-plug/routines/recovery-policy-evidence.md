---
kind: routine
description: Recall the team's recovery policy and gather code evidence for each question with explore, side by side
tools: recall_memory, explore
inputs: {"type":"object","properties":{"query":{"type":"string"},"questions":{"type":"array"}},"required":["questions"]}
---
An example routine. Copy it to `.magi/skills/recovery-policy-evidence.md` and run it with
`run_routine{name="recovery-policy-evidence", args={questions={"...", "..."}}}`. It recalls the
recovery policy once, hands it to one explore per question (they run together), and prints each
answer under its question. It only reads.

```lua
plan({"recall policy", "gather evidence", "collect"})
stage("recall policy")
local policy = call_tool("recall_memory", {query = input.query or "recovery policy"}) or ""
local brief = policy:sub(1, 2000)
stage("gather evidence")
local calls = {}
for _, q in ipairs(input.questions) do
  calls[#calls + 1] = {"explore", {depth = "quick", question = "The recovery policy, as recalled:\n" .. brief ..
    "\n\nQuestion: " .. tostring(q) .. "\nAnswer with path:line evidence from the code, and say where the code disagrees with the policy."}}
end
local answers = call_all(calls)
stage("collect")
for i, q in ipairs(input.questions) do
  local a = answers[i]
  print("## " .. tostring(q))
  if not a.ok then
    print("(no answer: " .. tostring(a.error) .. ")")
  elseif a.text == "" then
    print("(no answer: empty response, not a finding)")
  else
    print(a.text)
  end
end
```
