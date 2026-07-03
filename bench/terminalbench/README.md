# Benchmarking magi on Terminal-Bench

[Terminal-Bench](https://www.tbench.ai) runs an agent against real terminal tasks
in Docker containers and scores how many it completes. This directory is a
[custom installed-agent](https://github.com/laude-institute/terminal-bench) adapter
that builds magi from source inside each task container and runs it headless.

## Prerequisites

- **Docker** running locally (Terminal-Bench launches one container per task).
- **Terminal-Bench** CLI (`tb`):
  ```sh
  uv tool install terminal-bench      # or: pipx install terminal-bench
  tb --version
  ```
- **An LLM backend reachable from inside a container.** The cloud default
  (`gpt-oss:120b-cloud`) needs `ollama signin`, which an ephemeral task container
  can't do — so for benchmarking point magi at an OpenAI-compatible endpoint via env:
  ```sh
  export MAGI_BASE_URL=https://your-endpoint/v1   # hosted API, vLLM, or an Ollama you control
  export MAGI_API_KEY=sk-...                       # omit for keyless backends (e.g. local Ollama)
  ```
  > A local `http://localhost:11434/v1` usually isn't reachable from the task
  > container — use a host-routable URL (e.g. `http://host.docker.internal:11434/v1`)
  > or a hosted endpoint.

  **Free hosted models (recommended for benchmarking)** — OpenAI-compatible, reachable
  straight from the container (no host routing), no credit card. Pass the model id
  exactly as the backend expects (it's sent verbatim to `--model`):
  ```sh
  # OpenRouter — has a free qwen3-coder (480B) and ~28 other free models, one key:
  export MAGI_BASE_URL=https://openrouter.ai/api/v1
  export MAGI_API_KEY=sk-or-...
  #   --model qwen/qwen3-coder         (or any "<vendor>/<model>" free id)
  # Google Gemini (OpenAI-compatible): MAGI_BASE_URL=https://generativelanguage.googleapis.com/v1beta/openai
  # Groq:                              MAGI_BASE_URL=https://api.groq.com/openai/v1
  ```
  > Note: an OpenAI API key gives no *free* model — OpenAI's API is paid per token.

## Run

From the **repo root** (so the dotted import path resolves):

```sh
tb run \
  --agent-import-path bench.terminalbench.magi_agent:MagiAgent \
  --model qwen3-coder:30b \              # whatever MAGI_BASE_URL serves; passed to `magi --model`
  --dataset terminal-bench-core         # or --task-id <id> to smoke-test one task
```

Smoke-test a single task first (fast, cheap) before a full run:

```sh
tb run --agent-import-path bench.terminalbench.magi_agent:MagiAgent \
  --model qwen3-coder:30b --task-id hello-world
```

## How it works

- `magi_agent.py` — `MagiAgent(AbstractInstalledAgent)`. `_install_agent_script_path`
  renders `magi-setup.sh.j2`; `_run_agent_commands` runs
  `magi -p "<instruction>" --permission allow --model <model>` (non-interactive).
- `magi-setup.sh.j2` — installs the Go toolchain, `git clone`s magi at `{{ ref }}`
  (default `main`), and builds the static binary to `/usr/local/bin/magi`.
- `_env` passes through `MAGI_BASE_URL` / `MAGI_API_KEY` only when set.

## Options

- `--version <ref>` selects the magi git ref to build (branch / tag / sha).
- Bump `GO_VERSION` in `magi_agent.py` if `go.mod` raises its `go` directive.

## Speeding up (prebuilt binary)

By default each task builds magi from source in-container (Go download + build,
~minutes). For a multi-task run, serve a prebuilt binary from the host and pass
`-k binary_url=…` so containers just download it (seconds):

```sh
# 1. Cross-compile both arches the task containers might use:
mkdir -p /tmp/magi-serve
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/magi-serve/magi-arm64 ./cmd/magi
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/magi-serve/magi-amd64 ./cmd/magi

# 2. Serve them (reachable from containers as host.docker.internal):
( cd /tmp/magi-serve && python3 -m http.server 8077 )

# 3. Run with the fast path.
#    REQUIRED for local Ollama: the container's localhost is NOT the host, so point
#    MAGI_BASE_URL at a host-routable URL or the in-container magi hangs on every call
#    (looks like a 360s agent_timeout with no file produced).
MAGI_BASE_URL=http://host.docker.internal:11434/v1 \
tb run --agent-import-path bench.terminalbench.magi_agent:MagiAgent \
  -m qwen3-coder:30b -k binary_url=http://host.docker.internal:8077 \
  --dataset terminal-bench-core==0.1.1 --task-id hello-world
```

`MagiAgent` picks `magi-local-setup.sh.j2` (download) over `magi-setup.sh.j2` (build)
whenever `binary_url` (or the `MAGI_BENCH_BINARY_URL` env var) is set; otherwise it
builds from source, which needs no host-side server.

### Network-free install (`binary_path`) — preferred

The download path still runs `curl` *inside* the task container, and some tasks
sabotage that from within (cron-broken-network rewrites `/usr/bin/curl` on a 1s
loop), failing the install as `agent_installation_failed` before the agent runs a
single step. Pass `-k binary_path=/tmp/magi-serve` (or `MAGI_BENCH_BINARY_PATH`)
instead: the adapter docker-cp's the prebuilt `magi-arm64`/`magi-amd64` into the
container and `magi-copy-setup.sh.j2` just `install`s the right arch — no
in-container network use at all, and no `:8077` server to run. Works the same
under Podman (the copy rides the docker-compatible `put_archive` API the install
script already uses). Takes precedence over `binary_url`.

## Notes

- Pin the dataset version (`terminal-bench-core==0.1.1`); the floating `head` build
  has a broken `tasks/` layout in the current registry.
- `--n-tasks N` selects the N **longest** tasks first — use `--task-id` to target
  specific (e.g. quick) tasks while validating.
- Local Ollama: set `MAGI_BASE_URL=http://host.docker.internal:11434/v1` and a dummy
  `MAGI_API_KEY` (Ollama ignores it); `--n-concurrent 1` avoids overloading one model.
