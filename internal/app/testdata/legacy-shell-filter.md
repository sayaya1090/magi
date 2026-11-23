---
kind: routine
status: learned-write
description: Shell pipeline: git {s1_arg1} {s1_arg2} {s1_arg3} -- {s1_arg5} | grep -E {s2_arg2} | grep -v {s3_arg2}
tools: bash
inputs: {"properties":{"s1_arg1":{"description":"Current argument 1 for git in stage 1","type":"string"},"s1_arg2":{"description":"Current argument 2 for git in stage 1","type":"string"},"s1_arg3":{"description":"Current argument 3 for git in stage 1","type":"string"},"s1_arg5":{"description":"Current argument 5 for git in stage 1","type":"string"},"s2_arg2":{"description":"Current argument 2 for grep in stage 2","type":"string"},"s3_arg2":{"description":"Current argument 2 for grep in stage 3","type":"string"}},"required":["s1_arg1","s1_arg2","s1_arg3","s1_arg5","s2_arg2","s3_arg2"],"type":"object"}
source: repeated shell graph eed9fced9161e5784de4a6425c4af8d3fde1c8a857acaa19c4f56e5c4c500dbe
dry_check: passed 1/1 (synthetic arguments)
---
Supply arguments from the current request. Each tool call follows the conversation's permission policy.

```lua
local function quote(value)
  if type(value) ~= "string" or value:find("%z") then error("expected a literal shell argument") end
  return "'" .. value:gsub("'", "'\\''") .. "'"
end
local command = table.concat({"git", quote(input.s1_arg1), quote(input.s1_arg2), quote(input.s1_arg3), "--", quote(input.s1_arg5)}, ' ') .. " | " .. table.concat({"grep", "-E", quote(input.s2_arg2)}, ' ') .. " | " .. table.concat({"grep", "-v", quote(input.s3_arg2)}, ' ')
local out, err = call_tool("bash", {command=command})
if out == nil then error(tostring(err)) end
print(out)
```
