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
        # Terminal-Bench passes the bare/prefixed model via --model; magi wants the
        # tail (e.g. "openai/qwen3-coder:30b" -> "qwen3-coder:30b").
        self._model_name = model_name.split("/", 1)[-1]
        # git ref of magi to build (branch, tag, or sha); override with --version.
        self._ref = kwargs.get("version") or "main"
        # Fast path: if binary_url is given (-k binary_url=http://host.docker.internal:PORT),
        # download a prebuilt magi-<arch> from there instead of building from source —
        # turns ~minutes of per-container setup into seconds. See README "Speeding up".
        self._binary_url = kwargs.get("binary_url") or os.environ.get(
            "MAGI_BENCH_BINARY_URL"
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
        if self._binary_url:
            return self._get_templated_script_path("magi-local-setup.sh.j2")
        return self._get_templated_script_path("magi-setup.sh.j2")

    def _get_template_variables(self) -> dict[str, str]:
        if self._binary_url:
            return {"binary_url": self._binary_url.rstrip("/")}
        return {"go_version": GO_VERSION, "ref": self._ref}

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
