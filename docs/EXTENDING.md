# magi extension guide — MCP servers & shared experience (RAG)

> Korean edition: [`EXTENDING.ko.md`](EXTENDING.ko.md).

How to attach **external tools (MCP)** and **team-shared memory/skills (the experience store, D13)**
to magi, step by step. For the concepts read [`ARCHITECTURE.md`](ARCHITECTURE.md) §11 (Extension
points) and §7; for the full usage read [`MANUAL.md`](MANUAL.md) §7 and §10. This document is the
practical procedure for someone attaching one for the first time.

> Related extension surfaces: **Lua plugins** (your own tools and hooks, hot-reloadable) → MANUAL §9,
> and **hooks** (shell lifecycle) → MANUAL §harness. *Transport* concerns such as auth and TLS belong
> at the Go `http.RoundTripper` seam (`openai.WithHTTPClient`), not in a plugin or an MCP server —
> ARCHITECTURE §11.

---

## 0. Config files and precedence (both features)

Both are turned on in `config.toml`. Load order (`cmd/magi/main.go`):

1. **Global**: `<config>/config.toml`
   - macOS: `~/Library/Application Support/magi/config.toml`
   - Linux: `~/.config/magi/config.toml`
2. **Project**: `<workdir>/.magi/config.toml` (commit it and the workflow follows the repo)

Merge rules:

| Key | How it merges |
|---|---|
| `hooks`, `allow`, `deny`, `allow_domains` | **append** (global + project) |
| scalars such as `experience_dir`, `profile`, `sandbox` | project **overrides** |
| the `[routing]` and `[mcp.*]` maps | **merged per key** — the project wins on a shared key |

> A missing file is not an error. With neither present, magi runs on defaults.

---

## 1. Adding an MCP server

An MCP server connects over **stdio or HTTP (Streamable HTTP)**, and after the handshake the tools it
reports are **registered automatically into the same registry as the built-ins**. The registered name
is **namespaced** — `mcp__<server label>__<remote tool name>` (so `read` from `[mcp.filesystem]`
becomes `mcp__filesystem__read`). The registry is keyed by name, so without namespacing a server's
`read`/`write`/`list` would **shadow the built-ins**, or two servers would overwrite each other; the
namespace is what prevents that. The call itself is forwarded under the remote's original name. If a
stdio server process dies or an HTTP connection drops, its tools are removed automatically
(`internal/adapter/mcp/`).

### 1.1 Declaring one

Add an `[mcp.<name>]` block to `config.toml`. `<name>` is both the management label and the
**namespace in the tool name** (`mcp__<name>__<remote tool>`), so keep it short and inside the tool
name character set (`[A-Za-z0-9_-]`; anything else is replaced with `_`).

**stdio transport** (spawns a local process):

```toml
# e.g. the filesystem MCP server
[mcp.filesystem]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "."]

# e.g. a server that needs environment variables (GitHub)
[mcp.github]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env = ["GITHUB_PERSONAL_ACCESS_TOKEN=ghp_xxx"]   # an array of "KEY=VALUE" strings
```

**HTTP transport** (a remote or local HTTP server):

```toml
# e.g. an MCP server already running over HTTP
[mcp.remote]
url = "http://localhost:3000/mcp"

# e.g. with custom headers and environment variables
[mcp.authenticated]
url = "${MCP_SERVER_URL}"  # read from the environment
[mcp.authenticated.headers]
Authorization = "Bearer ${MCP_API_TOKEN}"
X-Client-ID = "magi-client"
X-Environment = "${DEPLOY_ENV}"
```

Fields (`config.MCPServer`):

| Field | Type | Meaning |
|---|---|---|
| `url` | string | the HTTP endpoint (Streamable HTTP transport). When present, `command` is ignored. Supports `${VAR}` expansion |
| `headers` | map[string]string | custom HTTP headers (HTTP transport). Supports `${VAR}` expansion |
| `command` | string | the binary to run (found on PATH; stdio transport) |
| `args` | []string | its arguments (stdio transport) |
| `env` | []string | `"KEY=VALUE"` form. **Appended to** the process environment, so the existing one is kept (stdio transport) |

> **Choosing the transport**: a `url` field selects HTTP; without one, stdio is used.

> **Environment expansion**: in the HTTP `url` and in `headers` values, a `${ENV_VAR}` pattern is
> substituted at runtime. A missing or empty variable leaves the text as it was. This is how a secret
> is injected from the environment rather than hard-coded into the config.

> **HTTP vs HTTPS**: both work. `http://` is fine for test and development; prefer `https://` in
> production.

> ⚠️ **Secrets**: a token written directly into `env` is stored in plain text in `config.toml`. If the
> project's `.magi/config.toml` is committed, do not put a token in it — keep it in the global
> `config.toml`, or have a wrapper script read it from the OS keychain or a `MAGI_*` env var and pass
> it to the child.

### 1.2 Verifying it

1. **Run the server binary by hand first** to confirm it is installed and on PATH (e.g. `npx -y <pkg>`
   sitting and waiting on stdin means it works — Ctrl+C to stop).
2. Start magi. A registration failure goes to stderr:
   ```
   magi: mcp "github": <reason>
   ```
   (spawn failure, handshake failure, tools/list failure, …) — no such line means it registered.
3. In the TUI, **`/tools`** lists what registered. MCP tools appear as §1.1 says, under
   **`mcp__<server label>__<remote tool>`** — they used to register unprefixed, which let servers
   overwrite each other and quietly shadow the built-ins. Headless: `magi -p "list the available tools"`.

### 1.3 Behaviour and caveats

- Permissions: an MCP tool call goes through the same permission mode (`ask`/`auto`/`allow`/`deny`)
  and policy engine as any other tool. A dangerous external tool can be blocked with `deny` or a
  policy rule.
- **Name collisions**: because the server label is part of the name, the same tool name on two
  servers does not collide, and a server's `read`/`write`/`list` cannot hide the built-ins. The only
  collision left is **using the same label twice**, and `[mcp.<name>]` is a map, so config merging has
  already folded those into one.
- If a server dies mid-session, only its tools leave the registry; the session continues.

### 1.4 Troubleshooting

| Symptom | Cause / what to do |
|---|---|
| `mcp "x": exec: "cmd": not found` | `command` is not on PATH → give an absolute path, or install it |
| It registered but nothing is in `/tools` | the server returned an empty `tools/list` → check its config and arguments |
| An auth error on call | a missing or mistyped token in `env` → check the `"KEY=VALUE"` form from §1.1 |
| Nothing happens at all | the `[mcp.*]` block is in the wrong file → re-read the paths and precedence in §0 |

---

## 2. Bootstrapping the shared experience store (RAG)

At session start, **memories and skills from a directory are retrieved by keyword and injected into
the system prompt** (D13). The `remember` tool contributes new learnings to a review queue, and making
the directory a git repo is how a team shares it
(`internal/adapter/experience/git/store.go`).

> ⚠️ **An honest limit**: the "RAG" here is **term-overlap scoring, not embedding vectors or semantic
> search**. Promotion is **a manual review**, not automatic. If you need semantic search, attach it as
> a separate ContextProvider or MCP server.

### 2.1 Creating the directory

The default location is `<config>/experience`. To share it with a team, make a separate git repo and
point `experience_dir` at it.

```bash
mkdir -p /path/to/team-experience/{memories,skills,pending}
cd /path/to/team-experience && git init   # optional: in a git repo, contributions are committed
```

```toml
# config.toml
experience_dir = "/path/to/team-experience"   # omitted → <config>/experience
```

Layout:

```
<dir>/
  memories/*.md   # approved memories — the whole file is the retrievable text
  skills/*.md     # approved skills — first line = description, the rest = body
  pending/*.md    # the review queue `remember` writes into (never retrieved)
```

### 2.2 Memory and skill file formats

- **A memory** (`memories/<anything>.md`): **the whole file text** is the unit of retrieval. No
  frontmatter needed. For tags, put a `tags: a, b` line in the body and those words join the match.

  ```markdown
  The integration tests in this repo need the MAGI_E2E_* env vars.
  Without them they t.Skip, so a green CI does not mean they passed.

  tags: testing, e2e, ci
  ```

- **A skill** (`skills/<name>.md`): **the first line is the description**, the rest is the body. The
  filename (without the extension) is the skill's name.

  ```markdown
  Cutting a release
  1. update the CHANGELOG 2. tag vX.Y.Z 3. goreleaser builds it in CI…
  ```

### 2.3 How retrieval behaves

- At each session start the user's prompt is the query, and the **top 5 memories and top 3 skills** by
  term-overlap score are injected (`Retrieve`). A score of zero (no shared words) is excluded.
- Adding more files does not inject more — only the top few. So memories are best kept **short and to
  a single fact**, which is what makes retrieval accurate.

### 2.4 Contributing and reviewing (`remember`)

- When the agent calls `remember` (on its own or because you asked it to), the note is written to
  `pending/` as `mem-<timestamp>-<n>.md` and, in a git repo, committed best-effort.
  **It is not retrieved yet** — it is only in the queue.
- **Promotion (review)**: a person reads what is in `pending/` and moves the good ones into
  `memories/` (or `skills/`). That is what makes them retrievable.

  ```bash
  cd "$EXPDIR"
  mv pending/mem-20260622-120000-0.md memories/   # after reading it
  git add -A && git commit -m "experience: approve memory"   # when sharing with a team
  ```

- 🔒 **`remember` must not store secrets** — the tool's description says so, and a contribution lands
  as plain-text .md that gets committed. Never put a token, key, or password in one.

### 2.5 Sharing with a team

Make `experience_dir` a git repo and have the team **pull it, review, and push**. magi only does a
best-effort `git commit` when contributing — it never pushes or pulls on its own, which is left to the
team's workflow.

### 2.6 Troubleshooting

| Symptom | Cause / what to do |
|---|---|
| A memory is never injected | it is still in `pending/` (needs promoting) / no words shared with the query / the file is empty |
| `remember` says "unavailable" | `experience_dir` is unset and the default path does not exist → create it per §2.1 |
| Nothing is committed | the directory is not a git repo → `git init` (the file is still written without one) |

---

## 3. Registering MCP servers and context providers from a plugin (Lua)

Besides declaring them in `config.toml`, a **Lua plugin** can register an MCP server or a context
provider (RAG) at runtime. This is only active when the plugin host was given the MCP manager, the
context registry and the runtime info (`cmd/magi/main.go`).

### 3.1 `magi.register_mcp` — register an HTTP MCP server

```lua
-- static headers
magi.register_mcp{
  name = "svc",
  url = "http://localhost:3000/mcp",
  headers = { Authorization = "Bearer abc" },
}

-- dynamic headers: the function is re-evaluated on every request (the value at request time,
-- not frozen at registration)
magi.register_mcp{
  name = "svc",
  url = "http://localhost:3000/mcp",
  headers = function()
    return {
      ["X-Model"]     = magi.model(),     -- the current model
      ["X-Platform"]  = magi.platform(),  -- darwin/linux/windows
      ["X-Timestamp"] = magi.time(),      -- the time of the request (RFC3339)
    }
  end,
}
```

> **Static vs dynamic**: a table fixes the headers (`AddHTTP`); a function is **called per request**
> (`AddHTTPDynamic`). The function runs serially under the plugin's Lua lock, so it is concurrency-safe.
> Use a function for anything that changes per request — a clock, a model, a token.

Runtime info API: `magi.model()`, `magi.platform()`, `magi.time()`, `magi.workdir()`.

> 🔐 **`magi.nonce(nbytes?)`** returns `nbytes` (default 16) of cryptographic randomness as a hex
> string (`crypto/rand`). The sandbox's `math.random` is **deterministically seeded** (with `os`
> removed there is no clock to seed from), so for **security values** — an OAuth/PKCE `state`, a CSRF
> token, a request id — **never use `math.random`; use `magi.nonce`.**

### 3.2 `magi.register_context_provider` — inject RAG context

A registered provider is **called on every step of the top-level agent**, and the chunks it returns
are injected into the system prompt's `# Retrieved context` section (5s timeout per provider, capped
at a combined 8KB budget; a provider that fails is ignored rather than blocking the turn).

```lua
magi.register_context_provider{
  name = "project-rag",
  provide = function(q)
    -- q.session_id, q.workdir, q.prompt are provided
    local hits = my_search(q.prompt)            -- any search logic
    local chunks = {}
    for _, h in ipairs(hits) do
      table.insert(chunks, { source = h.path, text = h.snippet })
    end
    return chunks                                -- an array of {source=, text=}
  end,
}
```

### 3.3 `magi.register_command` — register a TUI slash command

A plugin can register its own slash commands such as `/login` and `/logout` (capability `"command"`).
When the TUI receives a slash it does not know, it delegates to the plugin commands, and they appear
in the palette and in completion. `name` is given without the slash (`"login"` → `/login`), and
`execute` receives the tokens after the command. **Returning a non-empty string is treated as an error
message**; `nil` means success (a `✓` in the snackbar).

```lua
magi.register_command{
  name        = "login",
  description = "Re-authenticate with the corporate SSO",  -- shown in /help and the palette
  execute     = function(args)
    -- args = the whitespace-separated tokens after "/login"
    local ok = do_sso_login(args[1])
    if not ok then return "SSO login failed" end     -- an error: shown in the snackbar
    -- success: return nil
  end,
}
```

### 3.4 `magi.set_llm_headers` — custom headers for the LLM backend

For an internal gateway (LiteLLM and the like) that requires a header such as `X-CLIENT-API-KEY`, or
when a token issued by a browser SSO has to serve as the credential. A table is static; a function is
**re-evaluated per request**.

```lua
-- static
magi.set_llm_headers({ ["X-CLIENT-API-KEY"] = "abc" })

-- dynamic: pick up a rotating token on every request (e.g. an SSO token refreshed into a file)
magi.set_llm_headers(function()
  local tok = magi.read_file(".magi/adsso.token") or ""
  return { Authorization = "Bearer " .. tok }
end)
```

> If all you need is a static key, `config.toml` does it **without a plugin**:
> ```toml
> [llm.headers]
> X-CLIENT-API-KEY = "${LITELLM_CLIENT_KEY}"   # ${ENV} expansion supported
> ```
> Both paths (static in config, dynamic from a plugin) apply together, with the dynamic headers
> written last.

### 3.5 Gated capabilities: `exec` · `open_url` · `http`

For a plugin to **run an external process, open a browser, or make an HTTP call**, it must say so in
`permissions` in `plugin.toml`. Undeclared, the bridge refuses (`permission denied: …`). This is what
fetching RAG over HTTP, or driving an SSO login flow from a plugin, needs.

| API | Permission | Notes |
|---|---|---|
| `magi.exec(cmd, {args})` | `exec:<cmd>` | direct exec, no shell (so no injection), relative to the workdir, 60s timeout. Returns `{stdout,stderr,code}` |
| `magi.open_url(url)` | `exec:open-url` | opens the OS default browser. **http/https only** |
| `magi.http{url,method,headers,body}` | `net:<host>` | http/https only, 30s timeout, 5MB response cap. Returns `{status,body}` |
| `magi.serve{port,handler}` | `net:listen` | a **long-lived in-process HTTP server** on `127.0.0.1` (no external runtime → one binary, same on every OS). `port=0` takes a free port. Returns `{port, stop()}` |
| `magi.set_base_url(url)` | `net:<host>` | **changes the agent's LLM backend base URL at runtime** (to a loopback proxy, or to a gateway discovered at login). An empty string restores it; unloading restores it automatically. http/https only. ⚠️ The agent sends **the real API key and every prompt** to the target, so granting `net:<host>` is granting permission to redirect LLM traffic there — **grant the host explicitly and narrowly** |
| `magi.set_model(model)` | `config:write:model` | **changes the active model of the current session at runtime** (and persists it to config, like the `/route` editor). Applies from the next loop iteration. Empty string refused; `true` on success, `(nil, err)` on failure. Useful for an SSO plugin that learns which backends are available after login. `magi.model()` reads back the new value immediately |
| `magi.set_context_window(tokens[, model])` | `config:write:model` | **overrides a model's context window (in tokens) at runtime** — for an internal model API that the built-in probes (vLLM `/v1/models`, LiteLLM, Ollama) cannot reach, so the footer gauge and the ratio-based auto-compaction use the true value. `tokens<=0` means unlimited/unknown. Omitting `model` (or passing an empty string) targets **the current session model**, which is the usual case. It also locks the value so a later lazy probe cannot overwrite it. It is a runtime value and is not persisted, so re-apply it from `on("session_start")`. `true` on success, `(nil, err)` on failure |
| `magi.reload_config()` | `config:write:model` | **re-reads config.toml from disk and applies it at runtime** — currently the session model. On a parse failure it returns `(nil, err)` and the running session keeps its existing settings, so a bad edit cannot silently blank the model. Routing, the base URL and plugin reloads still need a restart. Useful after changing the model with `set_config_key` |
| `magi.clear_transcript()` | (none — UI only) | **resets the on-screen transcript to the splash** (the session on disk is untouched). For a plugin's `/logout` to return to a clean start screen. Returns `true` |
| `magi.get_config_key(key, default?)` | `config:read:<key>` | reads a dotted key (`routing.model`, `plugins.<name>.token`) from the user's **config.toml**. A plugin's own section (`plugins.<name>.*`) needs no permission. **A missing key → `default`; a parse failure → `(nil, err)`** — the two are distinguished, so check the error to avoid the loop of overwriting a broken config |
| `magi.set_config_key(key, value)` | `config:write:<key>` | writes a dotted key into config.toml (**comments preserved**, `config.SetKey`). The value is a string; an empty string deletes the key. A plugin's own section needs no permission. A top-level key updates the existing active line and leaves commented-out template defaults alone (so no duplicate key is created) |

> 🔑 **store_get/store_set vs get/set_config_key**: the first pair is the plugin's **own isolated JSON
> store** (no `config:` permission needed). The second pair reaches into the **user's config.toml** and
> is **permission-gated**: `config:read:<key>` / `config:write:<key>`, with a trailing `*` for a prefix
> wildcard (`config:write:routing.*`, `config:write:*`). A plugin's own `plugins.<name>.*` section is
> implicitly allowed. Keys accept only `[A-Za-z0-9_-]` dotted segments (to prevent injection). A
> **fixed deny-list** is blocked even with permission: `mcp`, `hooks`, `allow`, `deny`, `permission`,
> `sandbox`, `profile`, `allow_domains` — the keys that change command execution and the security
> posture.

**Example: SSO login → the token as the LLM auth header (the plugin drives the whole flow)**

```toml
# plugin.toml
name = "adsso"
permissions = ["exec:open-url", "net:sso.corp.example", "fs:write:.magi/"]
```

```lua
-- init.lua: log in through the browser at startup, cache the exchanged token, inject it per request
local token = ""
local function login()
  magi.open_url("https://sso.corp.example/authorize?...")   -- open the browser
  -- (after receiving the code via callback or polling) exchange it:
  local r = magi.http{ url = "https://sso.corp.example/token",
                         method = "POST", body = "grant_type=..." }
  if r and r.status == 200 then token = r.body end
end
login()
magi.set_llm_headers(function() return { Authorization = "Bearer " .. token } end)
```

> ⚠️ `exec` and `http` widen the sandbox considerably. Grant them only to plugins you trust, narrowed
> to the smallest host or command. (For a static key alone, §3.4's `config.toml [llm].headers` is
> enough.)

### 3.6 Lifecycle hooks, user prompts and callbacks (SSO and the like)

The general-purpose way for a plugin to **interact with the user at startup** (to authenticate, say).

- **`magi.on(event, fn)`** — register a handler the host calls at a defined moment.
  Events: `startup` (after plugins load, before the first turn, with the UI ready), `session_start`
  (after a session is created), `shutdown` (on exit).
  Handlers run **synchronously** and may block (e.g. waiting for authentication to finish at startup).
- **`magi.ask{title, fields}`** — an interactive form. Field `type`: `text`, `password`, `number`,
  `multiline`, `select`, `multiselect`, `confirm`, `note`. Returns the answers as a table.
  **Without a TTY (headless) it errors** — handle the fallback.
  Fields: `{ name=, type=, label=, options={}, default= }`. (Tab submits, Esc cancels.)
- **`magi.serve`** — a loopback HTTP server on `127.0.0.1`. Two modes, both needing `net:listen`:
  - **with a handler (long-lived)**: `magi.serve{port, handler=function(req) … end}` routes every
    request to `handler(req)`, and the returned table is the response. `port=0` takes a free port.
    Returns `{port, stop()}`. Shut down automatically on unload or reload.
  - **without a handler (one-shot, blocking — for an OAuth/PKCE redirect)**:
    `magi.serve{port, path, timeout}` blocks until the first matching request, returns
    `{query={...}, path=}`, and stops.

  The request is `{ method, path, query={k=v}, headers={k=v}, body }`, and the response is
  `{ status=200, headers={k=v}, body }` (or just a string, which becomes a 200 body).
  Being **in-process**, it needs no external runtime and works inside the single static binary —
  identically on every OS.

**Example: SSO — a "log in with the browser / paste a token" menu at startup (pure plugin, no core change)**

```toml
# plugin.toml
name = "adsso"
permissions = ["exec:open-url", "net:listen", "net:sso.corp.example", "fs:write:.magi/"]
```

```lua
-- init.lua
magi.on("startup", function()
  if magi.store_get("adsso.token") then return end            -- already have one
  local a = magi.ask{ title = "SSO sign-in", fields = {
    { name = "how", type = "select", options = { "Log in with the browser", "Paste a token" } },
  }}
  if not a then return end                                        -- headless etc. → fall back
  local token
  if a.how == "Log in with the browser" then
    magi.open_url("https://sso.corp.example/authorize?redirect_uri=http://127.0.0.1:8765/cb&...")
    local cb = magi.serve{ port = 8765, path = "/cb", timeout = 120 } -- one-shot (no handler)
    local r = magi.http{ url = "https://sso.corp.example/token", method = "POST",
                           body = "grant_type=authorization_code&code=" .. cb.query.code }
    token = parse_token(r.body)
  else
    token = magi.ask{ fields = {{ name = "t", type = "password", label = "Token" }} }.t
  end
  magi.store_set("adsso.token", token)
end)

-- inject the token into every LLM request (read from the store, so it survives a restart)
magi.set_llm_headers(function()
  return { Authorization = "Bearer " .. (magi.store_get("adsso.token") or "") }
end)
```

→ There is no trace of this SSO in the core. All the core provides is the general capability: a plugin
asks the user something at a lifecycle moment and interacts with its environment.

- **`magi.set_user_label(name)`** — set the display name for the user in the transcript (the fallback
  when unset is `you`). Use it to show the logged-in username after SSO. An empty or whitespace-only
  string is ignored, keeping the fallback. Requires the `ui` permission.

  **Encoding contract — pass a raw UTF-8 Lua string.** The core preserves the label as lossless UTF-8
  through storage, broadcast and rendering (its internal `json.Marshal` does not escape non-ASCII to
  `\uXXXX`, and the contract is pinned by round-trip unit tests in `internal/app` and
  `internal/adapter/tui`). So if a **literal escape sequence** appears on screen, it is not the core —
  it is **the plugin passing an already-escaped string**, typically a hand-rolled parser reading an SSO
  response's JSON without decoding `\uXXXX`. The `body` and `query` from `magi.http` and `magi.serve`
  are already UTF-8, so when taking a name out of a JSON body, **decode the JSON properly** and pass
  that value rather than the escaped text.

### 3.7 `serve` + `set_base_url` — a loopback LLM proxy (no core change)

A plugin can raise an **in-process HTTP server** with `magi.serve` and point the agent's LLM traffic at
it with `magi.set_base_url`. Prompt/response logging, request rewriting, mocking, a rate gate — all
from a plugin, **without an external process** (so: one binary, identical on every OS). The server is
shut down automatically on unload.

```toml
# plugin.toml
name = "llm-proxy"
# net:listen to host the server, net:127.0.0.1 to point the agent at loopback,
# net:localhost to forward upstream
permissions = ["net:listen", "net:127.0.0.1", "net:localhost"]
```

```lua
-- init.lua: intercept every LLM request, log it, and forward to the real backend
local upstream = "http://localhost:11434/v1"   -- the original backend (needs net: for this host)
local s = magi.serve{ port = 0, handler = function(req)
  magi.log("LLM " .. req.method .. " " .. req.path .. " (" .. #req.body .. " bytes)")
  local r = magi.http{ url = upstream .. req.path, method = req.method,
                       headers = req.headers, body = req.body }
  return { status = r.status, body = r.body }
end }
magi.set_base_url("http://127.0.0.1:" .. s.port .. "/v1")   -- point the agent at the proxy (loopback)
```

> 🔐 **`set_base_url` security**: the agent sends **every prompt and response to `base()` with the real
> API key attached.** Granting `net:<host>` is therefore an explicit approval to redirect the agent's
> credentialed traffic to that host — **grant the target host explicitly and narrowly** (a broad `net:`
> granted for RAG can end up opening redirection too). For a loopback proxy that is `net:127.0.0.1`;
> for a gateway, declare that host. On plugin unload or reload the override is **restored
> automatically**, so the LLM is never left pointing at a dead target.

> ⚠️ **Limits**: (1) the `serve` handler's response is the **complete body** returned by `magi.http`,
> so it is **not token-by-token SSE streaming** (it arrives at once, after upstream finishes). The 30s
> and 5MB caps apply too, which makes this proxy suitable for **logging, mocking and short
> completions** and unsuitable for long streaming pass-through. (2) A `serve` plugin bound to a fixed
> port (`port>0`) can fail to bind on hot reload while the previous instance still holds it, so
> **`port=0` (automatic) is recommended**.

---

## See also

- Add your own **tools and hooks** without writing Go → Lua plugins (MANUAL §9, `plugins/examples/wordcount`)
- Shell **lifecycle hooks** (test/format gates) → MANUAL §harness, `[[hooks]]`
- Implement a new backend through the **ports and adapters** structure → ARCHITECTURE §3, §11
