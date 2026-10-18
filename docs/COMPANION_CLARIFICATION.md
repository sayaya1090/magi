# Clarifying delegated requests

Status: design. This is not an implemented automatic reply route.

## Decision criteria

Ask the requester first when request inputs, facts or requirements are missing and the requester can supply them. Ask the user for execution approval, account secrets and user preferences. A requester's reply cannot replace user approval or tool permissions. Do not ask again for facts already provided. If the requester cannot answer, show the evidence and remaining uncertainty in the user's decision report.

## A route bound to the original request

When accepting delegated work, store requester companion ID, requester session, original receipt and reply route separately from its display name. Do not infer a destination from that name or submit an unrelated task. Validate the route within the original request's authentication boundary; the model cannot replace its host or session.

Expose `clarify_request` only in sessions holding an original delegated request. Inputs are missing information, the question, why it matters, evidence already checked and the required response format. The harness supplies destination and original receipt. Return a clarification ID immediately and deliver the attributed, verbatim answer only to that delegated session. Execution or modification requests are not clarification questions.

## Requester processing

A clarification does not interrupt the requester's active task or start another development task. Use a read-only path that first checks the original request and facts already supplied. Return a reason when evidence is insufficient or a user decision is needed. Replies grant no permissions. The model and maximum context, time and cost budgets for this read-only path must be specified before implementation.

## State and fault tolerance

Record original request and clarification IDs together. Distinguish waiting, answered, requester unavailable, timeout, cancellation and permission refusal. Repeated submission of the same question uses one ID, and replies are injected once. Bound round trips, elapsed time, question length and reply size. Use original request ancestry to prevent circular clarification chains. Cancelling original work cancels its remaining clarifications. After restart, refuse replies for completed originals or unrelated sessions.

## User interface

The main review area shows when the requester is being consulted. An absent answer is not user rejection. If the user must decide, present the original request, clarification exchange and remaining decision in that companion's ordered `decision-report` sections. Additional facts and execution approval remain separate actions.

## Acceptance conditions

Test duplicate display names, requester restart, two identical questions, out-of-order and duplicate replies, original cancellation, lost replies and timeout, circular questions and attempts to bypass user approval. An integration scenario must obtain additional facts without directly reading or editing the requester's workspace. Specify protocol, storage and per-client acceptance conditions before converting this design into implementation.
