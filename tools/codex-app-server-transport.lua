-- Inlined into the optional local Codex plugin by update-codex-transport.py.
-- Protocol: Codex app-server v2 (CLI 0.163.0). No MCP server is registered.
local appTurnSubmitted = false
local function appSend(method, params)
  mcp.rpc = mcp.rpc + 1
  local id = mcp.rpc
  local ok = mcp.ch:write('{"jsonrpc":"2.0","id":' .. id .. ',"method":' ..
    jstr(method) .. ',"params":' .. params .. '}')
  if not ok then return nil, 'write failed' end
  return id
end

local function appRead(cap)
  local line, err = readLine(cap)
  if not line then return nil, err or 'no reply' end
  local d = magi.json_decode(line)
  if type(d) ~= 'table' then return nil, 'invalid app-server JSON' end
  -- The shim borrows a model, not the CLI's tool execution. Never approve a
  -- server-initiated tool/permission request or wait forever for its reply.
  if d.method and d.id then
    mcp.ch:write('{"jsonrpc":"2.0","id":' .. jstr(d.id) ..
      ',"error":{"code":-32601,"message":"Magi executes its own tools"}}')
    return nil, 'unexpected server request: ' .. tostring(d.method)
  end
  return d
end

local function appReply(id, cap, capture)
  while true do
    local d, err = appRead(cap)
    if not d then return nil, err end
    if d.id == id then
      if d.error then return nil, tostring(d.error.message or 'app-server error') end
      if type(d.result) ~= 'table' then return nil, 'missing app-server result' end
      return d.result
    elseif capture and d.method then
      mcp.pending[#mcp.pending + 1] = d
    end
  end
end

local function liveOpen()
  local ch, err = magi.pipe('codex', { 'app-server' }, { neutral_dir = true, idle = '30m' })
  if not ch then
    magi.log('[codex] app-server unavailable: ' .. tostring(err))
    return false
  end
  mcp = { ch = ch, rpc = 0, totals = {}, pending = {} }
  local id, why = appSend('initialize', '{"clientInfo":{"name":"magi","version":"1"}}')
  local reply
  if id then reply, why = appReply(id, 1) end
  if not reply then liveDrop('app-server initialization: ' .. tostring(why)); return false end
  if not ch:write('{"jsonrpc":"2.0","method":"initialized"}') then
    liveDrop('app-server initialization write'); return false
  end
  magi.log('[codex] persistent app-server connected')
  return true
end

local function liveCall(name, argsJSON, silenceCap)
  appTurnSubmitted = false
  local a = magi.json_decode(argsJSON)
  if type(a) ~= 'table' then return nil, 'invalid live arguments' end
  local thread = a.threadId
  if name == 'codex' then
    local params = '{"sandbox":"read-only","approvalPolicy":"never","ephemeral":true' ..
      (a.model and (',"model":' .. jstr(a.model)) or '') .. '}'
    local id, err = appSend('thread/start', params)
    if not id then return nil, err end
    local res; res, err = appReply(id, silenceCap)
    if not res then return nil, err end
    thread = type(res.thread) == 'table' and res.thread.id or nil
    if type(thread) ~= 'string' then return nil, 'thread/start returned no thread id' end
  end
  if type(thread) ~= 'string' then return nil, 'missing thread id' end
  local baseline = mcp.totals[thread] or {}
  local params = '{"threadId":' .. jstr(thread) .. ',"input":[{"type":"text","text":' ..
    jstr(a.prompt or '') .. ',"text_elements":[]}]}'
  -- A failed write can still have reached the child. Fail closed after the
  -- attempt instead of paying for a second exec request with an unknown outcome.
  appTurnSubmitted = true
  local id, err = appSend('turn/start', params)
  if not id then return nil, err end
  local res; res, err = appReply(id, silenceCap, true)
  if not res then return nil, err end
  local turn = type(res.turn) == 'table' and res.turn.id or nil
  if type(turn) ~= 'string' then return nil, 'turn/start returned no turn id' end
  local chunks, usage, total = {}, nil, nil
  while true do
    local d = table.remove(mcp.pending, 1)
    if not d then d, err = appRead(silenceCap) end
    if not d then return nil, err end
    local p = d.params
    if type(p) == 'table' and p.threadId == thread and
      (p.turnId == turn or (type(p.turn) == 'table' and p.turn.id == turn)) then
      if d.method == 'item/completed' and type(p.item) == 'table' and
        p.item.type == 'agentMessage' and type(p.item.text) == 'string' then
        chunks[#chunks + 1] = p.item.text
      elseif d.method == 'thread/tokenUsage/updated' and type(p.tokenUsage) == 'table' then
        total = p.tokenUsage.total
        if type(total) == 'table' then
          -- Totals are cumulative for the thread. Repeated updates replace the
          -- snapshot; summing them would double-count both cached and output tokens.
          usage = {
            prompt = math.max(0, (tonumber(total.inputTokens) or 0) - (baseline.inputTokens or 0)),
            cached = math.max(0, (tonumber(total.cachedInputTokens) or 0) - (baseline.cachedInputTokens or 0)),
            completion = math.max(0, (tonumber(total.outputTokens) or 0) - (baseline.outputTokens or 0)),
          }
        end
      elseif d.method == 'turn/completed' then
        if p.turn.status ~= 'completed' then return nil, 'Codex turn ' .. tostring(p.turn.status) end
        if total then mcp.totals[thread] = total end
        if #chunks == 0 then return nil, 'completed turn carried no text' end
        return { text = table.concat(chunks, '\n'), thread = thread, usage = usage }
      end
    end
  end
end

