r"""Terminal-Bench installed-agent adapter for magi.

Runs magi's headless mode (`magi -p`) inside each Terminal-Bench task container.
The harness installs magi via magi-setup.sh.j2 (builds the single Go binary from
source), then invokes it once per task with the task instruction.

Usage (from the repo root, with Terminal-Bench installed and Docker running):

    export MAGI_BASE_URL=https://your-openai-compatible-endpoint/v1
    export MAGI_API_KEY=sk-...                       # omit for keyless backends
    tb run \
      --agent-import-path bench.terminalbench.magi_agent:MagiAgent \
      --model qwen3-coder:30b \                       # whatever MAGI_BASE_URL serves
      --dataset terminal-bench-core

Note: the cloud default (gpt-oss:120b-cloud) needs `ollama signin`, which an
ephemeral task container can't do — point MAGI_BASE_URL at a reachable
OpenAI-compatible endpoint (hosted API, or an Ollama you control) for benching.
"""

import os
import shlex
from pathlib import Path

from terminal_bench.agents.installed_agents.abstract_installed_agent import (
    AbstractInstalledAgent,
)
from terminal_bench.terminal.models import TerminalCommand

# Go toolchain used to build magi in-container (must satisfy go.mod's `go 1.26`).
GO_VERSION = "1.26.4"


class MagiAgent(AbstractInstalledAgent):
    """magi as a Terminal-Bench installed agent (builds from source, runs headless)."""

    @staticmethod
    def name() -> str:
        return "magi"

    def __init__(self, model_name: str = "qwen3-coder:30b", *args, **kwargs):
        super().__init__(*args, **kwargs)
        # Pass the model id to magi verbatim — it goes straight to the OpenAI-compatible
        # endpoint's `model` field, so it must keep any provider prefix the backend wants
        # (e.g. Ollama "qwen3-coder:30b", OpenRouter "qwen/qwen3-coder"). Set --model to
        # exactly what MAGI_BASE_URL expects.
        self._model_name = model_name
        # git ref of magi to build (branch, tag, or sha); override with --version.
        self._ref = kwargs.get("version") or "main"
        # Fast path: if binary_url is given (-k binary_url=http://host.docker.internal:PORT),
        # download a prebuilt magi-<arch> from there instead of building from source —
        # turns ~minutes of per-container setup into seconds. See README "Speeding up".
        self._binary_url = kwargs.get("binary_url") or os.environ.get(
            "MAGI_BENCH_BINARY_URL"
        )
        # Network-free path: if binary_path names a host directory holding prebuilt
        # magi-arm64/magi-amd64 (-k binary_path=/tmp/magi-serve), the binaries are
        # docker-cp'd into the container and installed without any in-container
        # network use. Some tasks sabotage network tooling from inside the container
        # (cron-broken-network overwrites /usr/bin/curl on a 1s loop), which fails
        # the binary_url download and scores agent_installation_failed before the
        # agent runs a single step — the copy path is immune. Takes precedence over
        # binary_url. Works identically under Podman: the copy rides the same
        # docker-compatible put_archive API the install script already uses.
        self._binary_path = kwargs.get("binary_path") or os.environ.get(
            "MAGI_BENCH_BINARY_PATH"
        )

    @property
    def _env(self) -> dict[str, str]:
        # Pass through only what's set, so keyless backends (local Ollama) work too.
        env: dict[str, str] = {}
        for key in ("MAGI_BASE_URL", "MAGI_API_KEY"):
            val = os.environ.get(key)
            if val:
                env[key] = val
        return env

    @property
    def _install_agent_script_path(self) -> Path:
        if self._binary_path:
            return self._get_templated_script_path("magi-copy-setup.sh.j2")
        if self._binary_url:
            return self._get_templated_script_path("magi-local-setup.sh.j2")
        return self._get_templated_script_path("magi-setup.sh.j2")

    def _get_template_variables(self) -> dict[str, str]:
        if self._binary_path:
            return {}
        if self._binary_url:
            return {"binary_url": self._binary_url.rstrip("/")}
        return {"go_version": GO_VERSION, "ref": self._ref}

    def perform_task(self, instruction, session, logging_dir=None):
        # Pre-seed the prebuilt binaries next to the install script via docker cp
        # (mkdir -p + put_archive — no in-container network, no curl/wget), so the
        # copy-setup script only has to pick the arch and `install` it. Both arches
        # are shipped when present; the script selects by `uname -m`.
        if self._binary_path:
            base = Path(self._binary_path)
            binaries = [
                base / f"magi-{suffix}"
                for suffix in ("arm64", "amd64")
                if (base / f"magi-{suffix}").is_file()
            ]
            if not binaries:
                raise FileNotFoundError(
                    f"binary_path {base} holds no magi-arm64/magi-amd64 binaries"
                )
            session.copy_to_container(binaries, container_dir="/installed-agent")
        return super().perform_task(instruction, session, logging_dir)

    def _run_agent_commands(self, instruction: str) -> list[TerminalCommand]:
        return [
            TerminalCommand(
                command=(
                    "magi -p "
                    f"{shlex.quote(instruction)} "
                    "--permission allow "  # non-interactive: no approval prompts
                    f"--model {shlex.quote(self._model_name)}"
                ),
                min_timeout_sec=0.0,
                max_timeout_sec=float("inf"),
                block=True,
                append_enter=True,
            )
        ]
