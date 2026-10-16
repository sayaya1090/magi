# Codex backend token efficiency

The 2026-10-17 gwt investigation found 5,974,516 input tokens across four completed turns. Their final requests held between 32,838 and 105,932 input tokens. Those are different measurements: the turn total includes every model request, council call and side call; the final request describes one context. Reported cached input was 328,704 tokens, about 5.5%.

The same session issued 33 `bash_output` calls and seven `wait_for` calls. Three launches used the same build command. Some waiting was necessary, but repeatedly asking the model to inspect unchanged output resends its context. Tool-contract failures also caused retries, including Gradle snapshot limits and attempts to apply a Git task through the copy-only apply operation. The coding workspace changes address those contracts separately.

## Waiting inside tools

`bash_output` now waits up to 30 seconds when a running job has no unread output. New output or process exit ends that wait. Buffered output and completed jobs return immediately, preserving pagination. The optional `wait` value accepts 0–60 seconds; `wait:0` preserves immediate inspection for interactive jobs. Cancellation ends the read without stopping the job.

For builds, use `wait_for` with a job ID; its default wait is 300 seconds. The background launch response points there first. Reading output does not mean a build finished, and a wait timeout does not justify starting another build. These changes reduce empty polls; they do not eliminate model requests for meaningful new output.

## Optional local Codex plugin

This machine's optional Codex plugin was using `codex mcp-server` for a persistent conversation. Installed Codex CLI 0.163.0 does not expose that command, so the plugin fell back to a fresh `codex exec` for each request. A successful one-shot response hid the loss of conversation continuity.

The replacement uses the supported `codex app-server` protocol: `initialize`, `thread/start`, and `turn/start`. Continuations keep the thread ID. The adapter requires a read-only sandbox and never approves server-initiated tool requests. Magi remains responsible for its own tool calls. Cumulative usage notifications are converted to per-call deltas; repeated notifications are not summed. Reasoning tokens are already a subset of output tokens and are not added again.

The plugin is installed locally and is not bundled into the repository's binary. Its transport source and migration tool are retained here:

```sh
python3 tools/update-codex-transport.py '/path/to/plugins/codex/init.lua'
```

The migration checks the expected plugin layout, retains a digest-named backup, and replaces the file atomically. Running it again makes no change. It preserves the plugin's settings and prompt rendering. It requires the existing persistent-thread plugin layout; an older or different plugin is rejected without being changed. A running daemon must reload the plugin or restart before using the new transport.

Before a model turn is submitted, initialization failure may still use the existing exec fallback. After submission is attempted, an uncertain or failed result returns HTTP 409 instead of replaying through exec. This also prevents the HTTP client's 5xx retry policy from silently running the same request again. The user can retry explicitly.

## Evidence and limits

The deterministic protocol test uses a local peer and checks thread reuse, separate threads, duplicate usage notifications, notifications arriving before the start response, and failed completion. Tool tests cover quiet waiting, output, exit, buffered reads, cancellation, timeout and immediate inspection.

An opt-in real test made four short requests with Codex CLI 0.163.0 and `gpt-6.1-sol`. All four used the same thread. Input/cached counts were 14,077/5,632; 16,360/0; 14,119/13,952; and 14,140/13,952. The warmed requests reached about 99% cache reuse. The initial requests did not. This is protocol and cache evidence from a small test, not a replay of the gwt development task or a guarantee of the same rate under every provider, account or prompt change.

```sh
go test -race ./internal/adapter/tool/builtin ./internal/adapter/plugin/lua
MAGI_CODEX_REAL_TRANSPORT=1 go test ./internal/adapter/plugin/lua -run '^TestCodexAppServerRealTransport$' -v -count=1
```

The second command uses the operator's Codex login and spends tokens. Normal tests never run it automatically.
