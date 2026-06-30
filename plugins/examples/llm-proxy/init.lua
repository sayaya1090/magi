-- llm-proxy: an example magi plugin that puts an in-process HTTP proxy in front
-- of the agent's LLM backend, with no external runtime — it works inside the single
-- static binary on every platform.
--
-- It demonstrates two host capabilities:
--   magi.serve{port, handler}  — a resident loopback HTTP server (net:listen)
--   magi.set_base_url(url)      — redirect the agent's LLM traffic (net:<host>)
--
-- Every request the agent makes to the model is logged here, then forwarded to the
-- real backend unchanged. Swap the handler body to mock, rewrite, or rate-limit.

local upstream = magi.store_get("upstream") or "http://localhost:11434/v1"

-- port = 0 picks a free port; read the actual one back from the returned table.
local server = magi.serve{
  port = 0,
  handler = function(req)
    magi.log(("LLM %s %s (%d bytes)"):format(req.method, req.path, #(req.body or "")))
    local r, err = magi.http{
      url = upstream .. req.path,
      method = req.method,
      headers = req.headers,
      body = req.body,
    }
    if r == nil then
      return { status = 502, body = "proxy upstream error: " .. tostring(err) }
    end
    return { status = r.status, body = r.body }
  end,
}

-- Point the agent at the loopback proxy. Pass "" to magi.set_base_url to restore
-- the configured backend (e.g. from a shutdown handler).
magi.set_base_url(("http://127.0.0.1:%d/v1"):format(server.port))
magi.log("llm-proxy listening on 127.0.0.1:" .. server.port .. " → " .. upstream)
