package main

import "github.com/sayaya1090/magi/internal/adapter/tool/builtin"

// registerOrchestrationTools adds the multi-agent orchestration tools (D9 bundled policy).
//
// Human-in-the-loop tools are omitted in a headless/bench run: with no one to answer a
// multiple-choice question (ask_user) or send a mid-turn interjection (route_interjection), they
// can never fire there and only add weight to the model's tool list — which a weak model pays for
// on every request. The orchestrator-internal tools (task, ask, report, resolveconcern,
// cancel_dispatch, replan) function without a human and are always registered.
func registerOrchestrationTools(reg *builtin.Registry, headless bool) {
	reg.Register(builtin.Task{})           // parent → subagent delegation
	reg.Register(builtin.Ask{})            // subagent → orchestrator escalation (input)
	reg.Register(builtin.Report{})         // subagent → orchestrator final result (output)
	reg.Register(builtin.ResolveConcern{}) // orchestrator-only: retire a handled ledger concern
	reg.Register(builtin.CancelDispatch{}) // orchestrator-only: cancel remaining parallel subagents
	reg.Register(builtin.Replan{})         // plan-eligible: declare the current plan unworkable and re-plan
	if !headless {
		reg.Register(builtin.AskUser{})           // multiple-choice question to the human user
		reg.Register(builtin.RouteInterjection{}) // route a mid-turn user interjection
	}
}
