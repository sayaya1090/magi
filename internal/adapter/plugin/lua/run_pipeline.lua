local function quote(v)
  if type(v) ~= "string" or v:find("%z") then error("literal argument required") end
  return "'" .. v:gsub("'", "'\\''") .. "'"
end
local function argument(slot, v)
  if slot.word then return quote(slot.word) end
  if type(v) ~= "string" then error("missing pipeline operand") end
  if slot.numeric then
    if v:sub(1,#slot.numeric) ~= slot.numeric or not v:sub(#slot.numeric+1):match("^%d[%d%-]*$") then error("invalid numeric option") end
  elseif v:sub(1,1) == "-" then error("expected an operand, got an option") end
  return quote(v)
end
local function array(value, minimum, maximum)
  if type(value) ~= "table" or #value < minimum or #value > maximum then return false end
  for k in pairs(value) do
    if type(k) ~= "number" or k ~= math.floor(k) or k < 1 or k > #value then return false end
  end
  return true
end
function run_pipeline_v1(spec, input)
  local commands = {}
  for i,stage in ipairs(spec.core) do
    local words = {quote(stage[1])}
    for j=2,#stage do
      local slot = stage[j]
      if slot.items then
        if not array(input.items,1,32) then error("expected 1 to 32 operands after --") end
        for _,v in ipairs(input.items) do words[#words+1] = quote(v) end
      else
        words[#words+1] = argument(slot,input["s"..i.."_arg"..(j-1)])
      end
    end
    commands[#commands+1] = table.concat(words," ")
  end
  if input.tail and not array(input.tail,0,5) then error("expected an array of pipeline stages") end
  if input.tail and #input.tail > 0 then
    local found = false
    for _,form in ipairs(spec.tails) do
      local rendered, fits = {}, #form == #input.tail
      for i,stage in ipairs(form) do
        local argv = input.tail[i]
        if not array(argv,#stage,#stage) or argv[1] ~= stage[1] then fits=false; break end
        local words = {quote(stage[1])}
        for j=2,#stage do
          if stage[j].word and stage[j].word ~= argv[j] then fits=false; break end
          local ok, word = pcall(argument,stage[j],argv[j])
          if not ok then fits=false; break end
          words[#words+1] = word
        end
        rendered[i] = table.concat(words," ")
      end
      if fits then for _,v in ipairs(rendered) do commands[#commands+1]=v end; found=true; break end
    end
    if not found then error("output pipeline was not observed") end
  end
  local out,err=call_tool("bash",{command=table.concat(commands," | ")})
  if not out then error(tostring(err)) end
  return out
end
