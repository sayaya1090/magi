# Infrastructure and operations agent design

The business kind is operations. Docker and Kubernetes are client/tool environments, not separate business kinds. Ownership roles follow assigned runtime scopes: a service set in one Docker context, or a service set in one Kubernetes context and namespace.

## Implementation status

AgentKind, client identities, dedicated prompts, tool namespace isolation and console type registration are added. Read-only Docker/Kubernetes adapters and a fixed-target browser UI are implemented; human-confirmed restart proposals and post-change checks are implemented. Selecting the role does not install tools automatically.

## Tool contract and implementation order

1. Require Docker context or Kubernetes context/namespace at helper startup. Requests cannot switch environments. Bind a resource/service allowlist and keep credentials in the helper.
2. Read tools provide status, resource descriptions, recent events and bounded logs. Bound lines, bytes and duration; disclose truncation. Do not expose secrets, exec, port-forward or arbitrary CLI arguments.
3. Separate mutations from reads. Start with explicit container restart or Deployment rollout restart only. Approval must show target, impact and recovery; inspect health afterwards. Deletion, prune, apply and scale require separate design and tests.
4. Do not enable generic bash. Execute validated argument arrays through exec.Command, without shell strings. Define read-only annotations independently of prompt wording.
5. Test foreign context/namespace/resource rejection, log bounds, timeouts, denial without mutation and post-restart inspection. Distinguish real Docker/disposable Kubernetes checks from mocks.

Coding agents own application codebases; operations agents own runtime environments. Dockerfile/deployment-manifest edits belong to explicit codebase ownership and cannot gain source-editing rights through operations permissions.
