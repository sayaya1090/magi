# Operations helper

Build with `go build -o /tmp/magi-operations ./clients/operations/cmd`. Configure Magi first and use a core supporting Docker/Kubernetes client roles. Start with `/tmp/magi-operations -runtime docker -context default -resources container-name` or `-runtime kubernetes -context test-cluster -namespace assigned -resources pod-name`. Kubernetes requires kubectl; Docker requires its CLI and a reachable compatible daemon.

The helper listens on loopback port 8738 and prints a bearer token. `MAGI_OPERATIONS_TOKEN` supplies a fixed token; `MAGI_BINARY` selects the core. Only one helper uses this port. Its target key is `runtime:context:namespace`; connect through the authenticated appbridge API with this exact target, then use that binding for chat/events/approvals. Open http://127.0.0.1:8738 and enter the local token. The page binds the fixed server target and provides chat, interruption, approval and question controls. Run `node clients/operations/test/ui-playwright.mjs` for mocked browser verification.

`status` and `logs` accept only `{resource}` from the startup allowlist. Docker status projects container State, excluding environment variables. Kubernetes status projects Pod name, phase and conditions. Logs request the last 10 minutes and 200 lines, with a 64 KiB output limit and 15-second deadline. Output is untrusted data and may contain application secrets; do not request logs unnecessarily. No exec, secrets, apply or delete tool is registered. Restart uses the separate approval flow below.

Run `go test -race ./clients/operations/... ./clients/appbridge` and `go vet ./clients/operations/...`. Tests verify fixed context/namespace arguments, resource rejection, unknown arguments and output bounds. Context names may contain slash, colon and @, as commonly used by Kubernetes. They remain separate command arguments; namespace and resource validation is unchanged. See the live verification record below for tested environments.

## Human-confirmed restart

Restart defaults off. Enable proposals with `-enable-restart`. Docker restarts only containers in resources; Kubernetes restarts only Deployments named by `-deployments app-name`. Read resources remain a Pod allowlist.

propose_restart creates a five-minute proposal without executing. The UI shows environment, resource, interruption impact and recovery limits. The approval button and confirmation dialog submit a one-use ID. Denial, expiry and reuse never execute. Core permission=allow cannot bypass this helper confirmation.

Approval runs Docker restart then State inspection, or Kubernetes rollout restart then rollout status (60 seconds), with a 90-second overall limit. Docker running/health or Kubernetes rollout completion is checked; service-level response tests are separate. Mutation success and verification failure are distinct. Failed/timed-out commands report uncertain completion, with no automatic retry or rollback. Pending approvals exist only in process memory.

## Live Docker verification (2026-10-12)

After backing up and repairing malformed Podman registry configuration, Docker API 5.5.2 responded. `MAGI_OPERATIONS_LIVE_DOCKER=1 go test ./clients/operations -run TestLiveDocker -count=1 -v` verified status, stdout/stderr logs, approved restart and post-restart inspection on an isolated container with no network or volumes (PASS, 20.31 seconds). Fixture removal was confirmed; existing workloads were unchanged. Kubernetes live acceptance is still pending.

## Local installation and Kubernetes checks (2026-10-12)

Installed `magi`, `magi-web`, `magi-operations`, `magi-figma`, `magi-mail`, `magi-slack` and `magi-github` in `~/.local/bin`; existing executables were backed up with `.before-clients`. The rebuilt console served `/ui/application/application.nocache.js` with HTTP 200. This checks embedded assets, not real-account client acceptance.

Four IDE-owned companions were restarted with the updated core. Empty sessions without stored events can receive a new ID on restart; no stored conversation was lost in these checks. Real-account Figma, mail and Slack acceptance remains pending.

Installed kubectl v1.37.1. A read-only Pod listing using context `handbook-dev/api-sayaya-cloud:6443/sayaya` and namespace `handbook-dev` failed because the API at `api.sayaya.cloud:6443` refused the connection. No Kubernetes mutation ran. Once connectivity is restored, verify status and bounded logs on an explicitly allowed test Pod; test approved restart only on a disposable Deployment. Record the context, namespace, commands and results without credentials.
