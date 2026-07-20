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
        # MAGI_MAX_PLAN_DEPTH is the recursion A/B knob: 2 = full recursive planning,
        # 1 = top-level plan + single-level delegate but no child re-planning / failure
        # recursion. MAGI_REFINE=0 is the orthogonal refine A/B knob: it downgrades the
        # hierarchical refine strategy to solo (the pre-refine baseline) while leaving
        # delegate untouched. MAGI_STEP_CONTEXT=0 is a third orthogonal knob: it turns off
        # the compact context brief injected into delegate hand-offs and read-only fan-out,
        # restoring the context-free baseline. MAGI_ADAPT=0 is a fourth: it disables the
        # REACTIVE (as-needed) failure re-decomposition — delegate/refine failures backtrack
        # after one shot, leaving only planned decomposition + the stall safety net.
        # MAGI_REFINE_SHARED=0 is a fifth: it makes a plan's dependent refine phases each get
        # their OWN spawn-time clone (the legacy baseline) instead of sharing ONE child session
        # in which later phases see their predecessors' actual work (the default).
        # MAGI_SPEC_FIDELITY=0 is a sixth: it turns off the spec-fidelity defenses (planner
        # literal-preservation rule + plan-time note + verbatim delegate SPEC anchor), so a deep
        # plan may paraphrase away exact identifiers a grader checks — the paraphrase-only
        # baseline. MAGI_PLAN_CONVERGE=0 is a seventh: it turns off the plan-audit convergence
        # judgment (does a re-plan actually address the council's concern), restoring the
        # round-count-only bound — the A/B knob for whether an unproductive re-plan stops early
        # instead of burning the round cap before execution starts. MAGI_STALL_CONVERGE=0 is an
        # eighth: it turns off the stalled-nudge convergence (collapse the no-progress re-arm when
        # a redirect produced no forward motion), restoring the fixed maxStallNudges re-arm — the
        # A/B knob for whether an ignored stall lands the honest force-stop sooner. MAGI_STEP_VERIFY=0
        # is a ninth: it turns off the per-step deliverable contract (default on), restoring the
        # baseline that leaves the step gate and its council skip inert. MAGI_RECOVERY_RUNCAP=on is a
        # tenth: it restores the one-executor-per-run-tree recovery cap (default OFF), collapsing the
        # per-depth re-decomposition to a single recovery executor — the A/B knob for whether capping
        # the recovery cascade beats the default of multiple recovery executors per run tree (each
        # stuck level re-runs the main orchestrator on a fresh re-plan). MAGI_ORIENT=off is an eleventh: it turns off the explore-first grounding pass
        # (default ON), which lands the workspace's build/verify anchors and layout in the main
        # context before planning — the A/B knob for whether that grounding beats the un-grounded
        # baseline. MAGI_IMPLICIT_ACCEPT=off is a twelfth: it tells the planner a task's real
        # acceptance conditions are usually stricter than the prose (exact output, unstated standard
        # semantics, unlisted edge cases), so it should plan for them (default ON). MAGI_CHECKPOINT_FIRST=off
        # is a thirteenth: it turns off test-first ordering (default ON) — when a task states HOW its
        # output is checked, reproduce that check as a runnable checkpoint before implementing — the
        # A/B knob for whether that beats reasoning about a verifiable artifact symbolically.
        # MAGI_SOLO_AUDIT=off is a fourteenth: it turns off the single-step plan audit (default ON),
        # restoring the >=2-step-only audit so a 1-step plan authors no per-step deliverable
        # criteria/checks — the A/B knob for whether auditing a lone step closes the completion-gate
        # gap it otherwise leaves open. MAGI_WAIT_GUARD=off is a fifteenth: it turns off the
        # environment-wait recovery suppression (default ON) — when a stall is dominated by
        # waiting/polling (sleep/ping/nc/readiness loops) the stuck-recovery coder spawn is skipped,
        # since a coder cannot speed an external wait and under delegate-off the recovery cascades
        # coder→coder whose timeout is misreported as the run's own deadline — the A/B knob for
        # whether suppressing that futile recovery beats the unconditional respawn. All are
        # forwarded so the arms share one prebuilt binary.
        for key in (
            "MAGI_BASE_URL",
            "MAGI_API_KEY",
            "MAGI_MAX_PLAN_DEPTH",
            "MAGI_REFINE",
            "MAGI_STEP_CONTEXT",
            "MAGI_ADAPT",
            "MAGI_REFINE_SHARED",
            "MAGI_SPEC_FIDELITY",
            "MAGI_PLAN_CONVERGE",
            "MAGI_STALL_CONVERGE",
            "MAGI_STEP_VERIFY",
            "MAGI_RECOVERY_RUNCAP",
            "MAGI_ORIENT",
            "MAGI_IMPLICIT_ACCEPT",
            "MAGI_CHECKPOINT_FIRST",
            "MAGI_SOLO_AUDIT",
            "MAGI_WAIT_GUARD",
            "MAGI_STREAM_DIAG",
            "MAGI_REASONING_EFFORT",
            "MAGI_CRITERIA_CONTEXT",
            "MAGI_SPEC_MINE",
            "MAGI_EXEC_EVIDENCE",
            "MAGI_COUNCIL_DEBATE",
            "MAGI_DIVERGE",
            "MAGI_STALL_NOVELTY",
            "MAGI_EXITCODE_BODYSCAN",
        ):
            val = os.environ.get(key)
            if val:
                env[key] = val

        # gemma is a "thinking" model: it reasons with no internal cap, and magi
        # sends neither a reasoning_effort nor a max_tokens bound. On a large agentic
        # step a single generation can spend tens of thousands of reasoning tokens
        # (~38 tok/s measured) and burn the whole 900s harbor budget before ever
        # emitting a tool call — the "silent hang" seen on pytorch-model-recovery.
        # magi's own planner/council supplies the deliberation, so disable the model's
        # internal thinking for the bench unless the caller overrode it. Measured on
        # gemma4:26b via Ollama: reasoning_effort=none keeps native tool calls intact
        # and cuts per-step latency ~5-7x (1s vs 7s actionable, 13s vs 58s heavy),
        # whereas "low" barely moves it. Scoped to gemma so reasoning models that
        # benefit from thinking (e.g. gpt-oss) are untouched.
        if "MAGI_REASONING_EFFORT" not in env and model.startswith("gemma"):
            env["MAGI_REASONING_EFFORT"] = "none"

        # Stream magi's stdout to disk line-by-line instead of writing it once at exec
        # completion. Harbor caps the agent phase with a trial-level asyncio.wait_for;
        # on the hard-kill (default 900s) the exec coroutine is cancelled and the
        # buffered exec path DISCARDS everything collected so far — so a task that runs
        # to the wall loses its whole trace (magi-stdout.txt never written). Registering
        # an output callback makes the docker env take its STREAMED exec path (stderr is
        # merged into stdout there), flushing each line to the file as it arrives, so
        # whatever ran before the kill is preserved for post-hoc analysis.
        command = (
            "magi -p "
            f"{shlex.quote(instruction)} "
            "--permission allow "
            f"--model {shlex.quote(model)}"
        )
        stdout_path = self.logs_dir / "magi-stdout.txt"
        result = None
        with stdout_path.open("w") as f:

            async def _on_output(text: str, _stream: object) -> None:
                f.write(text)
                f.flush()

            try:
                with environment.scoped_output_callback(_on_output):
                    result = await environment.exec(command=command, env=env or None)
            finally:
                f.flush()

        # Fallback for environments that don't stream (buffered exec leaves the file
        # empty): persist the buffered stdout once on a clean return.
        if result is None:
            return
        if result.stdout and stdout_path.stat().st_size == 0:
            stdout_path.write_text(result.stdout)
        if result.stderr:
            (self.logs_dir / "magi-stderr.txt").write_text(result.stderr)
        if result.return_code != 0:
            raise self._classify_exec_error("magi -p …", result)
