r"""Harbor installed-agent adapter for magi (Terminal-Bench 2.x).

Runs magi's headless mode (`magi -p`) inside each Harbor task environment.
Successor to bench/terminalbench (tb 0.2.x / terminal-bench-core 0.1.1) for the
harbor-framework datasets (terminal-bench-2-1 and later).

Install is network-free by design: the prebuilt linux binaries are pushed into
the environment with `environment.upload_file` (docker cp under the hood — works
identically under Podman) and installed by `uname -m`. Nothing downloads inside
the container, so tasks that sabotage in-container network tooling (the 0.1.1
cron-broken-network rewrote /usr/bin/curl every second; 2.1 keeps similar
reward-hacking hardening) cannot break the agent install.

Usage (from the repo root, with harbor installed and Docker/Podman running):

    # 1. Cross-compile both arches:
    mkdir -p /tmp/magi-serve
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/magi-serve/magi-arm64 ./cmd/magi
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/magi-serve/magi-amd64 ./cmd/magi

    # 2. Run (MAGI_BASE_URL must be host-routable from containers).
    #    PYTHONPATH pins the repo root so harbor's own venv (installed via
    #    `uv tool`/pipx, which does NOT put cwd on sys.path) can import this
    #    module; without it harbor dies with "No module named 'bench'".
    PYTHONPATH=$PWD \
    MAGI_BASE_URL=http://host.docker.internal:11434/v1 \
    MAGI_BENCH_BINARY_PATH=/tmp/magi-serve \
    harbor run --agent bench.harbor.magi_agent:MagiAgent \
      --model qwen3-coder:30b \
      --dataset terminal-bench/terminal-bench-2-1 --n-concurrent 1

    # magi's step ceiling defaults to 240 (sized for TB 2.x); no flag needed.

The model id is passed to magi verbatim after stripping an optional leading
"openai/" — harbor conventionally namespaces models as provider/name, while
magi hands the id straight to the OpenAI-compatible endpoint.
"""

import os
import shlex
from pathlib import Path

from harbor.agents.installed.base import BaseInstalledAgent
from harbor.environments.base import BaseEnvironment
from harbor.models.agent.context import AgentContext


class MagiAgent(BaseInstalledAgent):
    """magi as a Harbor installed agent (prebuilt binary, network-free install)."""

    @staticmethod
    def name() -> str:
        return "magi"

    def __init__(self, *args, binary_path: str | None = None, **kwargs):
        super().__init__(*args, **kwargs)
        self._binary_path = binary_path or os.environ.get("MAGI_BENCH_BINARY_PATH")
        if not self._binary_path:
            raise ValueError(
                "MagiAgent needs prebuilt binaries: pass binary_path or set "
                "MAGI_BENCH_BINARY_PATH to a dir holding magi-arm64/magi-amd64"
            )

    def get_version_command(self) -> str | None:
        return "magi --version"

    async def install(self, environment: BaseEnvironment) -> None:
        base = Path(self._binary_path)
        uploaded = []
        for suffix in ("arm64", "amd64"):
            src = base / f"magi-{suffix}"
            if src.is_file():
                await environment.upload_file(src, f"/installed-agent/magi-{suffix}")
                uploaded.append(suffix)
        if not uploaded:
            raise FileNotFoundError(
                f"binary_path {base} holds no magi-arm64/magi-amd64 binaries"
            )
        # Pick the environment's arch and install. No network, no curl/wget: the
        # binaries are already inside, so in-container sabotage of network tooling
        # cannot fail the install.
        await environment.exec(
            command=(
                'case "$(uname -m)" in '
                "aarch64|arm64) suffix=arm64 ;; "
                "x86_64|amd64) suffix=amd64 ;; "
                '*) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;; '
                "esac && "
                'install -m 0755 "/installed-agent/magi-${suffix}" /usr/local/bin/magi && '
                "magi --version"
            ),
            user="root",
        )

    async def run(
        self,
        instruction: str,
        environment: BaseEnvironment,
        context: AgentContext,
    ) -> None:
        model = self.model_name or ""
        # harbor namespaces models as provider/name; magi wants the raw id for the
        # OpenAI-compatible endpoint. Strip only the generic "openai/" namespace —
        # anything else (e.g. Ollama's own slashed ids) passes through verbatim.
        if model.startswith("openai/"):
            model = model[len("openai/") :]

        env: dict[str, str] = {}
        for key in ("MAGI_BASE_URL", "MAGI_API_KEY"):
            val = os.environ.get(key)
            if val:
                env[key] = val

        result = await environment.exec(
            command=(
                "magi -p "
                f"{shlex.quote(instruction)} "
                "--permission allow "
                f"--model {shlex.quote(model)}"
            ),
            env=env or None,
        )
        (self.logs_dir / "magi-stdout.txt").write_text(result.stdout or "")
        if result.stderr:
            (self.logs_dir / "magi-stderr.txt").write_text(result.stderr)
        if result.return_code != 0:
            raise self._classify_exec_error("magi -p …", result)
