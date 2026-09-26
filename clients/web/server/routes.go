package main

import "net/http"

// routes is every path this server answers, in one place.
//
// Wrapped where the table is built, not where the server is started: a guard applied at the call
// site is one a later route can be added beside, and a test that calls the wrapper directly passes
// either way — measured, by removing the wrapping and watching the check stay green.
//
// A list rather than a run of mux.HandleFunc calls because the page links to some of these, and a
// test checks that everything the page references is a path this binary serves — which is the real
// meaning of "self-contained", and cannot be checked against a list that exists only as statements.
func (s *server) routes() map[string]http.HandlerFunc {
	out := map[string]http.HandlerFunc{}
	for path, h := range s.handlers() {
		// Three wrappers, and the order is the argument. Audit outermost, because a cross-site
		// POST that never reaches a handler is the line in that record somebody would actually
		// want. Then the cross-site guard, which is about the BROWSER and applies to everybody
		// including the operator. Then may-do, which is about the person — asked last, so a
		// forged cross-site request is turned away before anybody's permissions are consulted.
		out[path] = s.audited(sameSiteOnly(s.claiming(s.mayDo(path, h))))
	}
	return out
}

// handlers is the table itself. routes() is what anything outside gets, and it is the wrapped one.
func (s *server) handlers() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/":                     s.page,
		"/fleet":                s.fleet,
		"/events":               s.events,
		"/submit":               s.submit,
		"/interrupt":            s.interrupt,
		"/resume":               s.resume,
		"/shell":                s.shell,
		"/cron":                 s.cron,
		"/search":               s.search,
		"/answer":               s.answer,
		"/manifest.webmanifest": s.manifest,
		"/icon.svg":             s.icon,
		"/icon-maskable.svg":    s.iconMaskable,
		"/font/":                s.font,
		// The page's own two subtrees. Missing, `import '/vendor/material.js'` answered 404, which
		// fails the whole ES module — so on a real console NOTHING ran: no components, no script, no
		// language beyond the seed inlined above. The demo hid it for as long as it existed, because
		// a static export writes these files to disk beside the page.
		"/vendor/": s.asset,
		"/i18n/":   s.asset,
		// The console itself: every compiled module, every stylesheet. See uiAsset for the cache
		// contract, which is GWT's.
		"/ui/":           s.uiAsset,
		"/skills":        s.skills,
		"/wiki":          s.wiki,
		"/forget":        s.forgetSkill,
		"/report-format": s.reportFormat,
		"/remember":      s.remember,
		"/context":       s.context,
		"/dispatch":      s.dispatch,
		"/mcp":           s.mcp,
		"/handoffs":      s.handoffs,
		"/subagents":     s.subagents,
		"/jobs":          s.jobs,
		"/tools":         s.tools,
		"/model":         s.models,
		"/loop":          s.loop,
		"/transcript":    s.transcript,
		"/council":       s.council,
		"/plan":          s.plan,
		"/compact":       s.compact,
		"/permission":    s.permission,
		"/console":       s.console,
		"/me":            s.me,
		"/access":        s.access,
		"/files":         s.files,
		"/file":          s.file,
		"/find":          s.find,
		"/save":          s.save,
		"/git":           s.git,
		"/look":          s.look,
		"/complete":      s.complete,
		"/open-file":     s.openFile,
		"/suggest":       s.suggest,
		"/autocomplete":  s.autocomplete,
		"/profiles":      s.profilesList,
		"/providers":     s.providers,
		"/update":        s.update,
		"/git-do":        s.gitDo,
		"/git-msg":       s.gitMsg,
		"/git-pr":        s.gitPR,
		"/pr":            s.prFacts,
		"/pr-msg":        s.prMsg,
		"/diff":          s.diff,
		"/file-do":       s.fileDo,
		"/history":       s.history,
		"/meet":          s.meet,
		"/meet-say":      s.meetSay,
		"/meet-close":    s.meetClose,
		"/meet-open":     s.meetOpen,
		"/meet-hand":     s.meetHand,
		"/push":          s.push,
		"/sw.js":         s.serviceWorker,
	}
}
