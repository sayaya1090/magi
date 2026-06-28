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

## Speeding up

Each task rebuilds magi from source (~30–60s of container setup). To avoid that,
publish a prebuilt `linux/amd64` + `linux/arm64` magi binary in a GitHub release and
change `magi-setup.sh.j2` to `curl` it instead of building.
