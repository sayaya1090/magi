package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sayaya1090/magi/internal/core/command"
)

func (m *Model) handleMouse(msg tea.Msg) tea.Cmd {
	// The permission modal is exclusive: while it's open a click on a button pill
	// activates it (like the hotkeys), a click on any other pill moves focus there,
	// and every other mouse event is swallowed so a click can't select/scroll the
	// transcript behind the modal.
	if m.perm != nil {
		switch e := msg.(type) {
		case tea.MouseClickMsg:
			if e.Button == tea.MouseLeft {
				if i, ok := m.permButtonAt(e.X, e.Y); ok {
					m.perm.sel = i
					m.refresh()
				}
			}
		case tea.MouseReleaseMsg:
			if e.Button == tea.MouseLeft {
				if i, ok := m.permButtonAt(e.X, e.Y); ok {
					return m.respond(permButtons()[i].decision)
				}
			}
		case tea.MouseWheelMsg:
			// The wheel still scrolls the transcript behind the modal so the context of
			// the decision stays reviewable while the prompt is open.
			m.wheelScrollTranscript(e.Button)
		}
		return nil
	}
	// The ask_user question modal is likewise exclusive: a click on an option row
	// focuses it (press) and picks it (release); every other mouse event is swallowed.
	if m.quest != nil {
		switch e := msg.(type) {
		case tea.MouseClickMsg:
			if e.Button == tea.MouseLeft {
				if i, ok := m.questOptionAt(e.Y); ok {
					m.quest.sel = i
					m.refresh()
				}
			}
		case tea.MouseReleaseMsg:
			if e.Button == tea.MouseLeft {
				if i, ok := m.questOptionAt(e.Y); ok {
					return m.answerQuestion(m.quest.options[i])
				}
			}
		case tea.MouseWheelMsg:
			m.wheelScrollTranscript(e.Button)
		}
		return nil
	}
	switch e := msg.(type) {
	case tea.MouseClickMsg:
		// Clicking the breadcrumb header while zoomed (or in council detail) goes back.
		if e.Button == tea.MouseLeft && (m.zoom || m.councilDetail != nil) && e.Y <= 1 {
			m.exitZoom()
			m.councilDetail = nil
			m.refresh()
			return nil
		}
		// Grabbing the panel's left edge starts a resize drag (not a selection).
		if e.Button == tea.MouseLeft && m.onPanelSplitter(e.X, e.Y) {
			m.resizingPanel = true
			return nil
		}
		if e.Button == tea.MouseLeft {
			// A click vs a drag is decided on release.
			m.selAL, m.selAC = m.screenToContent(e.X, e.Y)
			m.selHL, m.selHC = m.selAL, m.selAC
			m.selecting, m.selDragged, m.selActive = true, false, false
		}
	case tea.MouseMotionMsg:
		if m.resizingPanel {
			m.setPanelWidthForSplit(e.X)
			m.refresh()
			return nil
		}
		if m.selecting && e.Button == tea.MouseLeft {
			m.selHL, m.selHC = m.screenToContent(e.X, e.Y)
			m.selDragged = true
			m.refresh()
		}
	case tea.MouseReleaseMsg:
		if m.resizingPanel {
			m.resizingPanel = false
			return nil
		}
		if m.selecting {
			m.selecting = false
			if m.selDragged {
				m.selActive = true
				text := m.selectedText()
				m.refresh()
				if text != "" {
					copyToOSClipboard(text)
					return tea.Batch(tea.SetClipboard(text), m.snack(fmt.Sprintf("copied %d chars", len([]rune(text)))))
				}
				return nil
			}
			// A plain click while the council detail modal is open dismisses it.
			if m.councilDetail != nil {
				m.councilDetail = nil
				m.refresh()
				return nil
			}
			// Plain click in the right panel's subagent list → open that subagent's
			// detail view directly (same destination as clicking its pane).
			if m.handlePanelClick(e.X, e.Y) {
				m.refresh()
				return nil
			}
			// Plain click: expand/collapse a clicked reasoning ("thought") block,
			// else focus a subagent pane. In the zoom view the click targets the
			// focused subagent's blocks, not the main transcript's.
			if m.zoom {
				if m.toggleThoughtAtZoom(m.selAL) {
					m.refresh()
					return nil
				}
			} else if m.openCouncilDetailAt(m.selAL) {
				m.refresh()
				return nil
			} else if m.toggleThoughtAt(m.selAL) {
				m.refresh()
				return nil
			} else if m.toggleLiveThinkAt(m.selAL) {
				m.refresh()
				return nil
			}
			// Plain click → focus a pane; a click outside any pane releases focus so the
			// wheel/↑↓ drive the transcript again (the inverse of clicking to engage it).
			if !m.handlePaneClick(e.Y) {
				m.focusPane = -1
			}
			m.refresh()
		}
	case tea.MouseWheelMsg:
		// The wheel scrolls the subagent list when it's the active region; otherwise
		// the transcript. "Active" = a pane is focused (click engages it, like the
		// keyboard) OR the cursor is over the pane block. Focus-based routing keeps the
		// (small, capped) block scrollable without precise hovering, so the wheel stops
		// "competing" with the transcript under the cursor. paneTop is the row below it.
		paneTop := 2 + m.vp.Height()
		blockH := m.panesBlockHeight()
		overPanes := blockH > 0 && e.Y >= paneTop && e.Y < paneTop+blockH
		// Focus-based routing requires a visible block, so a stale focus on a screen
		// too short to render any pane can't capture the wheel from the transcript.
		if !m.zoom && len(m.panes) > 0 && (overPanes || (m.focusPane >= 0 && blockH > 0)) {
			switch e.Button {
			case tea.MouseWheelUp:
				m.paneScroll--
			case tea.MouseWheelDown:
				m.paneScroll++
			}
			m.refresh() // renderPanes clamps paneScroll
			return nil
		}
		// Scroll explicitly by direction; works mid-drag since selection is
		// anchored to content lines, not screen rows.
		switch e.Button {
		case tea.MouseWheelUp:
			m.vp.ScrollUp(3)
		case tea.MouseWheelDown:
			m.vp.ScrollDown(3)
		}
		if m.selecting {
			m.refresh()
		}
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// Any key clears a finished mouse selection's highlight.
	if m.selActive {
		m.selActive = false
		m.refresh()
	}
	// Question modal (ask_user): ↑/↓ or 1-9 select, enter answers, esc dismisses.
	if m.quest != nil {
		q := m.quest
		switch key := msg.String(); key {
		case "up", "k":
			if q.sel > 0 {
				q.sel--
			}
		case "down", "j":
			if q.sel < len(q.options)-1 {
				q.sel++
			}
		case "tab":
			q.sel = (q.sel + 1) % len(q.options)
		case "shift+tab":
			q.sel = (q.sel + len(q.options) - 1) % len(q.options)
		case "enter":
			return m.answerQuestion(q.options[q.sel]), true
		case "esc":
			return m.answerQuestion(""), true
		default:
			if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
				if i := int(key[0] - '1'); i < len(q.options) {
					q.sel = i
					return m.answerQuestion(q.options[i]), true
				}
			}
			// Transcript scroll keys stay live so the question's context is reviewable.
			m.scrollTranscriptKey(key)
		}
		m.refresh()
		return nil, true
	}

	// Permission modal takes priority.
	if m.perm != nil {
		switch msg.String() {
		case "y", "Y":
			return m.respond("allow"), true
		case "a", "A":
			return m.respond("always"), true
		case "p", "P":
			// Persist: allow for this project across sessions (writes an allow rule).
			return m.respond("persist"), true
		case "n", "N", "esc":
			return m.respond("deny"), true
		case "tab", "right":
			m.perm.sel = (m.perm.sel + 1) % len(permButtons())
			m.refresh()
			return nil, true
		case "shift+tab", "left":
			m.perm.sel = (m.perm.sel + len(permButtons()) - 1) % len(permButtons())
			m.refresh()
			return nil, true
		case "enter":
			return m.respond(permButtons()[m.perm.sel].decision), true
		}
		// Let the transcript scroll keys through so the user can page back to re-read
		// the context of the decision; everything else is swallowed by the modal.
		m.scrollTranscriptKey(msg.String())
		return nil, true
	}

	// Transcript search bar: capture keys while open (the textarea otherwise
	// swallows every printable key, so search runs as a modal like perm).
	if m.searching {
		switch msg.String() {
		case "esc", "ctrl+f":
			m.searching = false
			m.searchQuery, m.searchHits = "", nil
			m.refresh()
		case "enter", "down", "ctrl+n":
			m.searchStep(1)
		case "up", "ctrl+p":
			m.searchStep(-1)
		case "backspace":
			if q := []rune(m.searchQuery); len(q) > 0 {
				m.searchQuery = string(q[:len(q)-1])
			}
			m.updateSearch()
		default:
			if r := []rune(msg.String()); len(r) == 1 && r[0] >= ' ' {
				m.searchQuery += msg.String()
				m.updateSearch()
			}
		}
		return nil, true
	}

	// Interactive resume picker takes priority while open.
	if m.resuming {
		switch msg.String() {
		case "up", "ctrl+p":
			if m.resumeSel > 0 {
				m.resumeSel--
			}
			m.refresh()
			return nil, true
		case "down", "ctrl+n":
			if m.resumeSel < len(m.resumeList)-1 {
				m.resumeSel++
			}
			m.refresh()
			return nil, true
		case "enter":
			m.resuming = false
			sid := m.resumeList[m.resumeSel].ID
			m.refresh()
			return m.switchSession(sid), true
		case "esc", "ctrl+c":
			m.resuming = false
			m.refresh()
			return nil, true
		}
		return nil, true // swallow other keys while the picker is open
	}

	// Interactive /route editor takes priority while open.
	if m.routing {
		return m.handleRouteKey(msg)
	}

	// Slash-command palette navigation (open while typing "/foo", no space yet).
	// Suppressed while a turn runs — the palette is hidden then.
	if matches := m.paletteMatches(); !m.running && len(matches) > 0 {
		sel := m.clampSel(len(matches))
		n := len(matches)
		switch msg.String() {
		case "up":
			// Wrap around: stepping up past the top lands on the bottom entry.
			m.palSel = (sel - 1 + n) % n
			return nil, true
		case "down":
			// Wrap around: stepping down past the bottom lands on the top entry.
			m.palSel = (sel + 1) % n
			return nil, true
		case "tab":
			m.ta.SetValue(matches[sel].name + " ")
			m.ta.CursorEnd()
			m.palSel = 0
			return nil, true
		case "enter":
			name := matches[sel].name
			m.palSel = 0
			return m.handleSlash(name)
		case "esc":
			m.ta.Reset()
			m.palSel = 0
			return nil, true
		default:
			m.palSel = 0 // typing changes the filter; reset selection
		}
	}

	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return tea.Quit, true
	case "ctrl+l":
		m.blocks = nil
		m.cache = m.cache[:0]
		m.liveText = ""
		m.refresh()
		return nil, true
	case "ctrl+f":
		// Open transcript search: alt-screen means the terminal's own find can't
		// see the viewport, so this is the only way to search a long session.
		m.searching = true
		m.searchQuery, m.searchHits = "", nil
		m.refresh()
		return nil, true
	case "ctrl+t":
		m.showThink = !m.showThink
		m.cache = m.cache[:0] // reasoning blocks render differently now
		m.refresh()
		return nil, true
	case "shift+tab":
		return m.cyclePermission(), true
	case "pgup":
		m.vp.PageUp()
		return nil, true
	case "pgdown":
		m.vp.PageDown()
		return nil, true
	case "ctrl+u":
		m.vp.HalfPageUp()
		return nil, true
	case "ctrl+d":
		m.vp.HalfPageDown()
		return nil, true
	case "shift+up":
		m.vp.ScrollUp(1)
		return nil, true
	case "shift+down":
		m.vp.ScrollDown(1)
		return nil, true
	case "tab":
		// History autocomplete: complete the typed prefix to the most recent
		// matching prior prompt (repeat tab cycles older matches).
		if val := m.ta.Value(); val != "" && !strings.HasPrefix(val, "/") {
			if c := m.historyComplete(val); c != "" {
				m.ta.SetValue(c)
				m.ta.CursorEnd()
				m.refresh()
				return nil, true
			}
		}
		// Otherwise cycle focus across the main transcript and subagent panes.
		if len(m.panes) > 0 {
			m.cyclePaneFocus(1)
			if m.zoom {
				m.exitZoom() // moving focus exits zoom; press ctrl+o to re-zoom
			}
			m.refresh()
			return nil, true
		}
	case "ctrl+o":
		// Zoom the focused subagent pane full-screen (and back). Entering zoom jumps
		// to the bottom so the latest output (e.g. the conclusion) is in view.
		if m.zoom && m.zoomPane != nil {
			m.exitZoom() // collapse a pinned finished-pane zoom
			m.refresh()
			return nil, true
		}
		if m.focusPane >= 0 && m.focusPane < len(m.panes) {
			m.zoom = !m.zoom
			m.refresh()
			if m.zoom {
				m.vp.GotoBottom()
			}
			return nil, true
		}
		if len(m.panes) > 0 {
			m.focusPane = 0
			m.zoomPane = nil
			m.zoom = true
			m.refresh()
			m.vp.GotoBottom()
			return nil, true
		}
	case "up":
		// In a focused (but un-zoomed) subagent list, ↑/↓ move the selection and the
		// window follows it; otherwise recall input history.
		if m.focusPane >= 0 && !m.zoom && len(m.panes) > 0 {
			if m.focusPane > 0 {
				m.focusPane--
			}
			m.ensureFocusVisible()
			m.refresh()
			return nil, true
		}
		if m.recallHistory(-1) {
			return nil, true
		}
	case "down":
		if m.focusPane >= 0 && !m.zoom && len(m.panes) > 0 {
			if m.focusPane < len(m.panes)-1 {
				m.focusPane++
			}
			m.ensureFocusVisible()
			m.refresh()
			return nil, true
		}
		if m.recallHistory(1) {
			return nil, true
		}
	case "esc":
		// Leave the council verdict detail first if it's open (like exiting zoom).
		if m.councilDetail != nil {
			m.councilDetail = nil
			return nil, true
		}
		// Focused on a still-running subagent → interrupt just that one (stays
		// focused so you see it stop). Press esc again to leave the view.
		if m.focusPane >= 0 && m.focusPane < len(m.panes) && !m.panes[m.focusPane].done {
			p := m.panes[m.focusPane]
			_ = m.app.Interrupt(m.ctx, command.Interrupt{SessionID: p.sid})
			return m.snack("interrupting " + p.role), true
		}
		if m.zoom {
			m.exitZoom()
			m.refresh()
			return nil, true
		}
		if m.focusPane >= 0 {
			m.focusPane = -1
			m.refresh()
			return nil, true
		}
		if m.running {
			_ = m.app.Interrupt(m.ctx, command.Interrupt{SessionID: m.sid})
			return nil, true
		}
	case "alt+enter", "ctrl+j", "shift+enter":
		// enter is reserved for send/steer, so these insert a literal newline to
		// compose a multi-line message. ctrl+j (LF) works on every terminal;
		// alt+enter and shift+enter need the terminal to send a distinct code
		// (Kitty keyboard protocol) — where it doesn't, that key arrives as enter
		// and sends, which is fine since ctrl+j always covers the newline case.
		m.ta.InsertString("\n")
		m.refresh()
		return nil, true
	case "enter":
		text := strings.TrimSpace(m.ta.Value())
		if text == "" {
			return nil, true
		}
		// `!`-prefix runs the rest as a shell command in the workdir (Claude-style
		// inline shell). Its output is shown and folded into the next prompt's context.
		// Works idle (staged) or mid-turn (steered) — runShellBang branches internally.
		if strings.HasPrefix(text, "!") {
			if cmd := strings.TrimSpace(text[1:]); cmd != "" {
				return m.runShellBang(cmd), true
			}
			return nil, true // bare "!" — nothing to run
		}
		if m.running {
			// A turn is in flight. Slash commands that are safe to run live are
			// allowed; the rest are rejected. Plain messages are handed to the engine,
			// which QUEUES them by default to run as their own turn after the current
			// one finishes (the agent may route them in sooner) — see App.Steer.
			if strings.HasPrefix(text, "/") {
				if safeWhileRunning(strings.Fields(text)[0]) {
					return m.handleSlash(text)
				}
				return m.snack("can't run " + strings.Fields(text)[0] + " while working"), true
			}
			return m.steer(text), true
		}
		if strings.HasPrefix(text, "/") {
			return m.handleSlash(text)
		}
		return m.submit(text), true
	}
	return nil, false
}

// scrollTranscriptKey applies the viewport scroll keys and reports whether the key was
// one. Modal handlers (permission / question) call it before swallowing input so the
// user can page back through the transcript to re-read the context of a decision — the
// alt-screen hides the terminal's own scrollback, so this is the only way back.
func (m *Model) scrollTranscriptKey(key string) bool {
	switch key {
	case "pgup":
		m.vp.PageUp()
	case "pgdown":
		m.vp.PageDown()
	case "ctrl+u":
		m.vp.HalfPageUp()
	case "ctrl+d":
		m.vp.HalfPageDown()
	case "shift+up":
		m.vp.ScrollUp(1)
	case "shift+down":
		m.vp.ScrollDown(1)
	default:
		return false
	}
	return true
}

// wheelScrollTranscript scrolls the transcript by a mouse-wheel button, used by the
// modal mouse handlers so the wheel keeps working behind an open prompt.
func (m *Model) wheelScrollTranscript(b tea.MouseButton) {
	switch b {
	case tea.MouseWheelUp:
		m.vp.ScrollUp(3)
	case tea.MouseWheelDown:
		m.vp.ScrollDown(3)
	}
}

// handleSlash dispatches built-in slash commands. Content commands render into
// the transcript; brief confirmations use a snackbar (M3).
func (m *Model) handleSlash(text string) (tea.Cmd, bool) {
	m.ta.Reset()
	var out tea.Cmd
	fields := strings.Fields(text)
	cmd := fields[0]
	switch cmd {
	case "/resume":
		out = m.handleResume(fields[1:])
	case "/rewind":
		n := 1
		if len(fields) > 1 {
			if v, e := strconv.Atoi(fields[1]); e == nil && v > 0 {
				n = v
			}
		}
		if m.running {
			out = m.snack("cannot rewind while running")
			break
		}
		if _, err := m.app.Rewind(m.ctx, m.sid, n); err != nil {
			out = m.snack("rewind: " + err.Error())
			break
		}
		out = m.switchSession(m.sid) // reload truncated transcript + resubscribe
		out = tea.Batch(out, m.snack(fmt.Sprintf("rewound %d turn(s)", n)))
	case "/image":
		if len(fields) < 2 {
			out = m.snack("usage: /image <path>")
			break
		}
		path, ok := m.readWorkdirImagePath(fields[1])
		if !ok {
			out = m.snack("image: path outside workdir")
			break
		}
		img, err := renderImageFile(path, max(8, m.width-6), m.imageProto)
		if err != nil {
			out = m.snack("image: " + err.Error())
			break
		}
		m.blocks = append(m.blocks, block{kind: blockImage, text: img})
	case "/help", "/?":
		m.info(helpText)
	case "/quit", "/exit":
		m.quitting = true
		return tea.Quit, true
	case "/clear":
		m.blocks = nil
		m.cache = m.cache[:0]
		m.liveText = ""
		out = m.snack("cleared")
	case "/model", "/agents", "/route":
		out = m.openRouteEditor() // session model + per-agent routing, interactively
	case "/tools":
		m.info("tools:\n  " + joinOr(m.app.ToolNames(), "(none)"))
	case "/sessions":
		m.info(m.sessionsList())
	case "/permission":
		out = m.cyclePermission()
	case "/diff":
		res, err := m.app.GitDiff(m.ctx, m.workdir)
		switch {
		case err != nil:
			out = m.snack("diff: " + err.Error())
		case res == "":
			out = m.snack("diff: no changes")
		default:
			m.blocks = append(m.blocks, block{kind: blockDiff, text: res})
		}
	case "/loop":
		if mp, err := m.app.LoopMap(m.ctx, m.sid); err != nil {
			out = m.snack("loop: " + err.Error())
		} else {
			m.info(mp)
		}
	case "/context":
		out = m.cmdContext(fields)
	case "/fork":
		if m.running {
			out = m.snack("cannot fork while running")
			break
		}
		origin := m.sid
		fork, err := m.app.Fork(m.ctx, origin, 0)
		if err != nil {
			out = m.snack("fork: " + err.Error())
			break
		}
		out = m.switchSession(fork) // explore the branch; the origin is untouched
		m.forkOrigin = origin
		out = tea.Batch(out, m.snack("forked — exploring branch; /loopdiff to compare with origin"))
	case "/loopdiff":
		if m.forkOrigin == "" {
			out = m.snack("no fork origin — run /fork or /replay first")
			break
		}
		if d, err := m.app.SessionDiff(m.ctx, m.forkOrigin, m.sid); err != nil {
			out = m.snack("loopdiff: " + err.Error())
		} else {
			m.info(d)
		}
	case "/replay":
		if m.running {
			out = m.snack("cannot replay while running")
			break
		}
		origin := m.sid
		fork, replayPrompt, err := m.app.Replay(m.ctx, origin)
		if err != nil {
			out = m.snack("replay: " + err.Error())
			break
		}
		if replayPrompt == "" {
			out = m.snack("replay: nothing to replay")
			break
		}
		sw := m.switchSession(fork) // explore the re-run on a branch; origin kept
		m.forkOrigin = origin
		out = tea.Batch(sw, m.submitAs("↻ replay: "+replayPrompt, replayPrompt))
	case "/init":
		return m.submitAs("/init — analyzing project to write AGENTS.md…", initPrompt), true
	case "/ultra":
		task := strings.TrimSpace(strings.TrimPrefix(text, "/ultra"))
		if task == "" {
			out = m.snack("usage: /ultra <task>")
			break
		}
		if m.running {
			out = m.snack("already running")
			break
		}
		return m.submitAs("/ultra: "+task, ultraPreamble+"\n\nTask: "+task), true
	case "/compact":
		sid := m.sid
		out = m.snack("compacting context…")
		go func() { _ = m.app.Compact(m.ctx, command.Compact{SessionID: sid}) }()
	default:
		// Delegate to a plugin-registered command (e.g. /login) before giving up.
		if m.cmds != nil {
			if handled, err := m.cmds.DispatchCommand(cmd, fields[1:]); handled {
				if err != nil {
					out = m.snack(cmd + ": " + err.Error())
				} else {
					out = m.snack(cmd + " ✓")
				}
				m.applyPluginUIEffects()
				break
			}
		}
		out = m.snack("unknown command: " + cmd + " — try /help")
	}
	m.refresh()
	return out, true
}

// applyPluginUIEffects drains UI effects a just-dispatched plugin command queued
// and applies them to the view. Only cosmetic effects are honored; the on-disk
// session is never touched here.
func (m *Model) applyPluginUIEffects() {
	if m.cmds == nil {
		return
	}
	for _, e := range m.cmds.TakeUIEffects() {
		switch e {
		case "clear_transcript":
			m.blocks = nil
			m.cache = m.cache[:0]
			m.liveText = ""
		case "reload_config":
			// reload_config may have changed the session model; re-read it so the
			// header chip reflects the running session.
			if mdl := m.app.SessionModel(m.sid); mdl != "" {
				m.model = mdl
			}
		}
	}
}

// cmdContext handles the /context command. Bare "/context" lists usage + every
// model in use and its window; "/context <tokens>" sets the session model's
// window; "/context <model> <tokens>" sets a specific model's. tokens may be
// "128k"/"1m"/"unlimited". Returns the snack/refresh command (nil when it only
// prints the usage view).
func (m *Model) cmdContext(fields []string) tea.Cmd {
	switch {
	case len(fields) >= 3: // /context <model> <tokens>
		n, ok := parseTokenCount(fields[len(fields)-1])
		if !ok {
			return m.snack("usage: /context <model> <tokens|unlimited>")
		}
		model := strings.Join(fields[1:len(fields)-1], " ")
		if note, err := m.app.SetContextWindow(m.ctx, m.sid, model, n); err != nil {
			return m.snack("context: " + err.Error())
		} else {
			return m.snack(note)
		}
	case len(fields) == 2: // /context <tokens> → session model
		n, ok := parseTokenCount(fields[1])
		if !ok {
			return m.snack("usage: /context [<model>] <tokens|unlimited>")
		}
		if note, err := m.app.SetContextWindow(m.ctx, m.sid, "", n); err != nil {
			return m.snack("context: " + err.Error())
		} else {
			return m.snack(note)
		}
	default:
		if cv, err := m.app.ContextView(m.ctx, m.sid); err != nil {
			return m.snack("context: " + err.Error())
		} else {
			m.info(cv)
			return nil
		}
	}
}

// parseTokenCount parses a context-window size like "128000", "128k", "1m", or
// "unlimited"/"none"/"0" (→ 0). Returns (tokens, true) on success.
func parseTokenCount(s string) (int, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "unlimited", "none", "off", "0", "auto":
		return 0, true
	}
	mult := 1
	switch {
	case strings.HasSuffix(s, "k"):
		mult, s = 1000, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult, s = 1_000_000, strings.TrimSuffix(s, "m")
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0, false
	}
	return n * mult, true
}

// pluginCmdMatches returns plugin commands whose "/name" matches the in-progress
// slash input, so they appear in the palette alongside built-ins.
func (m *Model) pluginCmdMatches(v string) []cmdInfo {
	if m.cmds == nil {
		return nil
	}
	var out []cmdInfo
	for _, c := range m.cmds.PluginCommands() {
		name := "/" + c.Name()
		if strings.HasPrefix(name, v) {
			out = append(out, cmdInfo{name: name, desc: c.Description()})
		}
	}
	return out
}

// paletteMatches returns the commands matching the in-progress slash input
// (open only while typing "/foo" with no space yet).
// slashAliases map an alias to its canonical command. Hidden from the bare "/"
// list (canonicals only) but surfaced when their own prefix is typed (e.g. "/m"
// → /model), carrying the canonical's description.
var slashAliases = map[string]string{
	"/model":  "/route",
	"/agents": "/route",
	"/exit":   "/quit",
}

func (m *Model) paletteMatches() []cmdInfo {
	v := strings.TrimSpace(m.ta.Value())
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \t") {
		return nil
	}
	var out []cmdInfo
	for _, c := range slashCommands {
		if strings.HasPrefix(c.name, v) {
			out = append(out, c)
		}
	}
	// Once a letter is typed (more than just "/"), also surface matching aliases
	// with their canonical's description, so "/m" finds /model.
	if len(v) > 1 {
		for alias, canon := range slashAliases {
			if !strings.HasPrefix(alias, v) {
				continue
			}
			for _, c := range slashCommands {
				if c.name == canon {
					out = append(out, cmdInfo{name: alias, desc: c.desc})
					break
				}
			}
		}
	}
	out = append(out, m.pluginCmdMatches(v)...)
	return out
}

func (m *Model) clampSel(n int) int {
	s := m.palSel
	if s < 0 {
		s = 0
	}
	if s >= n {
		s = n - 1
	}
	return s
}

// handleResume opens an interactive picker (no arg) or switches directly by
// index (/resume N). The picker shows each session's time and a summary so the
// user can tell conversations apart.
func (m *Model) handleResume(args []string) tea.Cmd {
	metas, err := m.app.ListSessions(m.ctx, m.workdir)
	if err != nil || len(metas) == 0 {
		return m.snack("no sessions to resume")
	}
	m.resumeList = metas
	if len(args) == 0 {
		m.resuming = true
		m.resumeSel = 0
		m.refresh()
		return nil
	}
	n, perr := strconv.Atoi(args[0])
	if perr != nil || n < 1 || n > len(m.resumeList) {
		return m.snack("usage: /resume <n> (run /resume to pick)")
	}
	return m.switchSession(m.resumeList[n-1].ID)
}
