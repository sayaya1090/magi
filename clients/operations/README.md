# Operations helper

Build with `go build -o /tmp/magi-operations ./clients/operations/cmd`. Configure Magi first and use a core supporting Docker/Kubernetes client roles. Start with `/tmp/magi-operations -runtime docker -context default -resources container-name` or `-runtime kubernetes -context test-cluster -namespace assigned -resources pod-name`. Kubernetes requires kubectl; Docker requires its CLI and a reachable compatible daemon.

The helper listens on loopback port 8738 and prints a bearer token. `MAGI_OPERATIONS_TOKEN` supplies a fixed token; `MAGI_BINARY` selects the core. Only one helper uses this port. Its target key is `runtime:context:namespace`; connect through the authenticated appbridge API with this exact target, then use that binding for chat/events/approvals. A dedicated browser UI is not implemented yet.

`status` and `logs` accept only `{resource}` from the startup allowlist. Docker status projects container State, excluding environment variables. Kubernetes status projects Pod name, phase and conditions. Logs request the last 10 minutes and 200 lines, with a 64 KiB output limit and 15-second deadline. Output is untrusted data and may contain application secrets; do not request logs unnecessarily. No exec, secrets, apply, delete or restart tool is registered.

Run `go test -race ./clients/operations/... ./clients/appbridge` and `go vet ./clients/operations/...`. Tests verify fixed context/namespace arguments, resource rejection, unknown arguments and output bounds. Real Docker inspection on this Mac failed because Podman's registry drop-in TOML is invalid; kubectl was absent. These failures are not live acceptance. Verify a disposable container/Pod's status and stdout/stderr logs before use. Changes and approval-based restart remain pending.
