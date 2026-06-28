package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// eventMsg carries a domain event from the app's bus into the Bubble Tea loop.
// sub tags the subscription generation so events from a switched-away session
// are ignored.
type eventMsg struct {
	ev  event.Event
	sid session.SessionID
	sub int
}

// subscribeMsg starts streaming a session's events (initial load / resume).
type subscribeMsg struct {
	sid     session.SessionID
	fromSeq int64
}

// subClosedMsg signals a subscription channel closed.
type subClosedMsg struct {
	sid session.SessionID
	sub int
}

// startSub cancels any current subscription and begins streaming sid from
// fromSeq, returning the command that delivers the first event.
func (m *Model) startSub(sid session.SessionID, fromSeq int64) tea.Cmd {
	if m.subCancel != nil {
		m.subCancel()
	}
	ch, cancel, err := m.app.Subscribe(m.ctx, sid, fromSeq)
	if err != nil {
		return nil
	}
	m.subCh = ch
	m.subCancel = cancel
	m.subID++
	m.mainSub = m.subID
	m.closePanes() // switching primary session retires any subagent panes
	return waitEvent(ch, sid, m.mainSub)
}

// waitEvent blocks for the next event on ch and tags it with the session id and
// subscription generation so the model can route it to the right pane.
func waitEvent(ch <-chan event.Event, sid session.SessionID, id int) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return subClosedMsg{sid: sid, sub: id}
		}
		return eventMsg{ev: e, sid: sid, sub: id}
	}
}

// permReq is a pending permission request shown as a modal.
type permReq struct {
	callID string
	name   string
	args   string
}

// Model is the Bubble Tea model for the interactive TUI.
type Model struct {
	ctx     context.Context
	app     *app.App
	sid     session.SessionID
	model   string
	workdir string

	forkOrigin session.SessionID // the session this one was forked from (for /loopdiff)

	history []string // submitted prompts (↑/↓ recall)
	histIdx int      // current position when browsing history
	palSel  int      // selected index in the command palette

	vp viewport.Model
	ta textarea.Model
	sp spinner.Model

	glam      *glamour.TermRenderer
	glamWidth int // transcript width the glam renderer was built for

	width, height int
	ready         bool
	running       bool
	quitting      bool
	isDark        bool
	imageProto    string // "kitty" | "iterm2" | "" (half-block)

	blocks          []block
	liveText        string
	liveThink       string    // streaming reasoning ("thinking") for the current turn
	showThink       bool      // expand ALL reasoning blocks (default collapsed); toggle ctrl+t
	blockLineStart  []int     // content line where each cached block begins (for click mapping)
	paneLineStart   []int     // content line where each focused-pane block begins, in zoom view (click mapping)
	lastThoughtAt   time.Time // last thought-toggle click time (to swallow a double-click's 2nd toggle)
	lastThoughtLine int       // content line of that last thought click
	perm            *permReq
	ctxPct          float64 // context window usage %
	plannerMode     string  // last planner decision (solo | parallel N) shown in the header

	turnStart     time.Time     // wall-clock start of the current turn (§8.1 elapsed)
	turnDur       time.Duration // frozen elapsed of the last finished turn
	turnIn        int           // current input/context tokens (↑)
	turnOut       int           // cumulative output tokens this turn (↓)
	councilRound  int           // current council round (0 = no council active); header chip
	councilMember string        // member currently being polled (live); cleared when the turn ends

	cache  []string // rendered finalized blocks (keyed by cacheW)
	cacheW int      // width the cache was rendered at
	dirty  bool     // content changed since last paint (throttled refresh)

	snackbar string // transient bottom notice (M3 snackbar)
	snackSeq int    // token so a stale auto-dismiss doesn't clear a newer notice

	pastes   map[int]string // collapsed paste blobs by id (chip in input → full on send/display)
	pasteSeq int            // monotonic paste id

	activeAgents []string // active subagent role names this turn (header badge)

	subCh      <-chan event.Event // current session subscription
	subCancel  func()
	subID      int                   // monotonic id assigned to every subscription (main + panes)
	mainSub    int                   // the primary session's subscription id (pane subs must not clobber it)
	resumeList []session.SessionMeta // sessions shown by the last /resume
	resumeSel  int                   // selected row in the interactive resume picker
	resuming   bool                  // the resume picker modal is open

	routeList    []routeRow   // rows shown by the models & routing editor
	routeSel     int          // selected row
	routing      bool         // the editor modal is open
	routeEditing bool         // typing a new value for the selected (session/agent) row
	routeBuf     string       // the value being typed
	routePickIdx int          // profile index while cycling with ←/→ (-1 = free text)
	profileForm  *profileForm // non-nil while adding/editing an LLM profile

	// Multi-agent live view (B): one pane per spawned subagent child session.
	panes     []*agentPane // active/finished subagent panes, in spawn order
	focusPane int          // index into panes of the focused pane (-1 = main transcript)
	zoom      bool         // focused pane expanded full-screen

	councilDetail          *event.CouncilVerdictData // open council-verdict detail (full-screen; nil = closed)
	councilDetailEvidence  string                    // the evidence shown alongside the open verdict
	paneScroll             int                       // scroll offset into the subagent pane list (clamped in renderPanes)
	pendingCouncilEvidence string                    // evidence from the latest convened round, attached to its verdicts
	roleColor              map[string]int            // role name -> agentPalette index (stable per session)

	panelW        int  // right status-panel width (drag its left edge to resize)
	resizingPanel bool // a panel-splitter drag is in progress

	// In-app mouse selection (so wheel-scroll AND drag-copy coexist with no mode
	// toggle): drag highlights content lines and copies on release. Selection is
	// anchored to CONTENT line indices (not screen rows) so scrolling mid-drag
	// keeps it aligned.
	selecting    bool     // a drag is in progress
	selActive    bool     // a finished selection is currently highlighted
	selAL, selAC int      // anchor: content line, display column where the drag started
	selHL, selHC int      // head: content line, display column at the current drag point
	selDragged   bool     // motion occurred (distinguishes drag from click)
	contentLines []string // current viewport content lines (styled)
	contentPlain []string // same lines, ANSI-stripped (for copy)
}

// New builds the TUI model for a session. isDark selects the color theme;
// imageProto is the detected inline-image protocol ("kitty"/"iterm2"/"").
func New(ctx context.Context, a *app.App, sid session.SessionID, model, workdir string, isDark bool, imageProto string) Model {
	applyTheme(isDark)

	ta := textarea.New()
	ta.Placeholder = "Ask magi to do something…  (enter to send)"
	ta.Prompt = "❯ "
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	// Report a real OS cursor (not a drawn one) so IME pre-edit (e.g. Korean)
	// composes at the input position instead of the screen corner.
	ta.SetVirtualCursor(false)
	st := textarea.DefaultStyles(isDark)
	st.Focused.Prompt = lipgloss.NewStyle().Foreground(colPrimary)
	st.Blurred.Prompt = lipgloss.NewStyle().Foreground(colOutline)
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(colMuted)
	st.Blurred.Placeholder = lipgloss.NewStyle().Foreground(colMuted)
	ta.SetStyles(st)
	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colPrimary)

	return Model{
		ctx: ctx, app: a, sid: sid, model: model, workdir: workdir,
		isDark: isDark, imageProto: imageProto, ta: ta, sp: sp,
		focusPane: -1, roleColor: map[string]int{}, panelW: defaultPanelWidth,
	}
}

// cmdInfo describes a slash command for the completion palette.
type cmdInfo struct{ name, desc string }

// slashCommands is the single source of truth for available commands.
var slashCommands = []cmdInfo{
	{"/help", "show help"},
	{"/route", "models & routing editor (aliases: /model, /agents)"},
	{"/tools", "list available tools"},
	{"/sessions", "list sessions in this directory"},
	{"/resume", "resume a session (/resume to list, /resume N to switch)"},
	{"/rewind", "roll back the last user turn(s) (/rewind [n])"},
	{"/image", "render an image file inline (/image <path>)"},
	{"/diff", "show the working-tree git diff"},
	{"/loop", "show the loop map (turns · steps · council)"},
	{"/context", "show what fills the context window (usage · compactions)"},
	{"/fork", "branch this session to explore an alternative (origin kept)"},
	{"/replay", "re-run the last turn on a branch (compare with /loopdiff)"},
	{"/loopdiff", "compare this branch with its fork origin"},
	{"/init", "analyze the project and write AGENTS.md"},
	{"/ultra", "ultra work mode: orchestrate specialists (/ultra <task>)"},
	{"/permission", "cycle permission mode"},
	{"/compact", "summarize & shrink the context"},
	{"/clear", "clear the transcript"},
	{"/quit", "exit magi (alias: /exit)"},
}

// renderTickMsg drives throttled, coalesced repaints during streaming.
type renderTickMsg struct{}

// snackClearMsg auto-dismisses a snackbar after its delay (seq guards staleness).
type snackClearMsg struct{ seq int }

// snack shows a transient bottom notice and returns the auto-dismiss timer cmd.
func (m *Model) snack(text string) tea.Cmd {
	m.snackbar = text
	m.snackSeq++
	seq := m.snackSeq
	m.refresh()
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return snackClearMsg{seq: seq} })
}

func renderTick() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg { return renderTickMsg{} })
}

func (m Model) Init() tea.Cmd {
	// Clear the screen on startup so magi opens on a clean canvas rather than
	// visually continuing the terminal's prior scrollback.
	// Subscribe via a message so the mutation lands on the running model.
	return tea.Batch(tea.ClearScreen, textarea.Blink, renderTick(), func() tea.Msg {
		return subscribeMsg{sid: m.sid, fromSeq: 0}
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		m.refresh()
		return m, nil

	case tea.KeyPressMsg:
		if cmd, handled := m.handleKey(msg); handled {
			return m, cmd
		}

	case subscribeMsg:
		return m, m.startSub(msg.sid, msg.fromSeq)

	case subClosedMsg:
		return m, nil // channel closed (cancelled/switched); stop reading

	case eventMsg:
		// Route to a subagent pane when the event belongs to a child session.
		if msg.sid != m.sid {
			if p := m.paneBySub(msg.sub); p != nil {
				m.applyPaneEvent(p, msg.ev)
				m.dirty = true
				cmds = append(cmds, waitEvent(p.ch, p.sid, p.sub))
			}
			return m, tea.Batch(cmds...)
		}
		if msg.sub != m.mainSub {
			return m, nil // event from a switched-away primary session
		}
		// A spawned subagent opens a live pane subscribed to its child session.
		if msg.ev.Type == event.TypeAgentSpawned {
			if cmd := m.openPane(msg.ev); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		m.applyEvent(msg.ev)
		// Coalesce repaints: mark dirty and let the render tick refresh, so a
		// burst of streaming tokens repaints once per frame, not per token.
		m.dirty = true
		cmds = append(cmds, waitEvent(m.subCh, m.sid, m.mainSub)) // read the next event
		if m.running {
			cmds = append(cmds, m.sp.Tick)
		}
		return m, tea.Batch(cmds...)

	case renderTickMsg:
		if m.dirty {
			m.refresh()
			m.dirty = false
		}
		return m, renderTick()

	case snackClearMsg:
		if msg.seq == m.snackSeq {
			m.snackbar = ""
			m.refresh()
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		if m.running {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg:
		return m, m.handleMouse(msg)

	case tea.PasteMsg:
		m.handlePaste(msg.Content)
		m.refresh()
		return m, nil
	}

	// Default routing. Key presses go ONLY to the textarea — never the viewport,
	// whose default keymap binds plain letters (space/f/b/u/d/j/k/h/l) and would
	// otherwise scroll the transcript as you type prose. Scrolling is driven by
	// the explicit keys in handleKey. Non-key messages (e.g. mouse) reach both.
	var cmd tea.Cmd
	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.ta, cmd = m.ta.Update(msg)
		cmds = append(cmds, cmd)
		m.refresh() // re-flow for palette/queue height changes
	} else {
		m.ta, cmd = m.ta.Update(msg)
		cmds = append(cmds, cmd)
		m.vp, cmd = m.vp.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// handleKey processes global keybindings and the permission modal.
// handleMouse processes mouse events: drag selects+copies in-app, plain click
// focuses a subagent pane, and the wheel scrolls (mid-drag too). One mode, no
// toggle — wheel and drag-select coexist because the app owns the selection.
// toggleThoughtAt flips the expand state of the reasoning block at content line
// `line` (a click target). Returns true if a reasoning block was toggled.
// openCouncilDetailAt opens the council detail modal if the clicked content line
// falls in a council-verdict block. Same block-lookup as toggleThoughtAt.
func (m *Model) openCouncilDetailAt(line int) bool {
	i := -1
	for j := len(m.blockLineStart) - 1; j >= 0; j-- {
		if line >= m.blockLineStart[j] {
			i = j
			break
		}
	}
	if i < 0 || i >= len(m.blocks) || m.blocks[i].kind != blockCouncilVerdict || m.blocks[i].councilVerdict == nil {
		return false
	}
	m.councilDetail = m.blocks[i].councilVerdict
	m.councilDetailEvidence = m.blocks[i].evidence
	return true
}

func (m *Model) toggleThoughtAt(line int) bool {
	i := -1
	for j := len(m.blockLineStart) - 1; j >= 0; j-- {
		if line >= m.blockLineStart[j] {
			i = j
			break
		}
	}
	if i < 0 || i >= len(m.blocks) || m.blocks[i].kind != blockReasoning {
		return false
	}
	if !m.thoughtClickSkip(line) {
		m.blocks[i].expanded = !m.blocks[i].expanded
		if len(m.cache) > i {
			m.cache = m.cache[:i] // re-render this block (and those after) at new height
		}
	}
	return true
}

// thoughtClickSkip reports whether this thought click is the 2nd half of a
// double-click on the same line (within a short window) and should therefore NOT
// re-toggle — so a double-click reliably EXPANDS instead of toggling twice back
// to collapsed. It always records the click for the next comparison.
func (m *Model) thoughtClickSkip(line int) bool {
	now := time.Now()
	skip := m.lastThoughtLine == line && now.Sub(m.lastThoughtAt) < 350*time.Millisecond
	m.lastThoughtAt = now
	m.lastThoughtLine = line
	return skip
}

// toggleThoughtAtZoom flips the expand state of the reasoning block at content
// line `line` in the focused subagent's zoom view. Returns true if a reasoning
// block was toggled. The zoom view rebuilds from p.blocks each frame (no cache),
// so toggling expanded and refreshing is enough to re-render at the new height.
func (m *Model) toggleThoughtAtZoom(line int) bool {
	if m.focusPane < 0 || m.focusPane >= len(m.panes) {
		return false
	}
	p := m.panes[m.focusPane]
	i := -1
	for j := len(m.paneLineStart) - 1; j >= 0; j-- {
		if line >= m.paneLineStart[j] {
			i = j
			break
		}
	}
	if i < 0 || i >= len(p.blocks) || p.blocks[i].kind != blockReasoning {
		return false
	}
	if !m.thoughtClickSkip(line) {
		p.blocks[i].expanded = !p.blocks[i].expanded
	}
	return true
}

func (m *Model) handleMouse(msg tea.Msg) tea.Cmd {
	switch e := msg.(type) {
	case tea.MouseClickMsg:
		// Clicking the breadcrumb header while zoomed (or in council detail) goes back.
		if e.Button == tea.MouseLeft && (m.zoom || m.councilDetail != nil) && e.Y <= 1 {
			m.zoom = false
			m.councilDetail = nil
			m.refresh()
			return nil
		}
		// Grabbing the panel's left edge starts a resize drag (not a selection).
		if e.Button == tea.MouseLeft && m.onPanelSplitter(e.X) {
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
	// Permission modal takes priority.
	if m.perm != nil {
		switch msg.String() {
		case "y", "Y":
			return m.respond("allow"), true
		case "a", "A":
			return m.respond("always"), true
		case "n", "N", "esc":
			return m.respond("deny"), true
		}
		return nil, true // swallow other keys while modal is open
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
		switch msg.String() {
		case "up":
			m.palSel = sel - 1
			if m.palSel < 0 {
				m.palSel = 0
			}
			return nil, true
		case "down":
			m.palSel = sel + 1
			if m.palSel >= len(matches) {
				m.palSel = len(matches) - 1
			}
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
				m.zoom = false // moving focus exits zoom; press ctrl+o to re-zoom
			}
			m.refresh()
			return nil, true
		}
	case "ctrl+o":
		// Zoom the focused subagent pane full-screen (and back). Entering zoom jumps
		// to the bottom so the latest output (e.g. the conclusion) is in view.
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
			m.zoom = false
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
	case "enter":
		text := strings.TrimSpace(m.ta.Value())
		if text == "" {
			return nil, true
		}
		if m.running {
			// A turn is in flight. Slash commands that are safe to run live are
			// allowed; the rest are rejected. Plain messages STEER the running
			// agent — they're injected into the current turn (picked up at its next
			// step), not parked in a queue.
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
		m.openRouteEditor() // session model + per-agent routing, interactively
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
		if cv, err := m.app.ContextView(m.ctx, m.sid); err != nil {
			out = m.snack("context: " + err.Error())
		} else {
			m.info(cv)
		}
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
		out = m.snack("unknown command: " + cmd + " — try /help")
	}
	m.refresh()
	return out, true
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

// sessionRouteRow is the editor's first row: the session's default model.
const sessionRouteRow = "(session)"

type routeRowKind int

const (
	rowSession routeRowKind = iota
	rowAgent
	rowProfile
	rowAddProfile
)

// routeRow is one line in the models & routing editor.
type routeRow struct {
	kind  routeRowKind
	name  string // "(session)", agent name, or profile name
	value string // display value
}

// profileForm is the multi-field sub-editor for an LLM profile definition.
type profileForm struct {
	isNew   bool
	name    string
	fields  []formField
	sel     int // == len(fields) selects the [save] action
	editing bool
	buf     string
}

type formField struct {
	label  string
	value  string
	secret bool // mask in display (api_key)
}

// openRouteEditor opens the models & routing editor: session model, per-agent
// routing, the defined profiles, and an "+ add profile" row.
func (m *Model) openRouteEditor() {
	m.profileForm = nil
	m.refreshRouteList()
	m.routeSel, m.routing, m.routeEditing, m.routeBuf = 0, true, false, ""
}

func (m *Model) refreshRouteList() {
	rows := []routeRow{{kind: rowSession, name: sessionRouteRow, value: m.model}}
	for _, r := range m.app.AgentRoutes() {
		v := r.Model
		if r.Provider != "" {
			v += "  @" + r.Provider
		}
		rows = append(rows, routeRow{kind: rowAgent, name: r.Name, value: v})
	}
	for _, p := range m.app.Profiles() {
		ep := p.BaseURL
		if ep == "" {
			ep = "(default endpoint)"
		}
		rows = append(rows, routeRow{kind: rowProfile, name: "profile:" + p.Name, value: ep + " · " + p.Model})
	}
	rows = append(rows, routeRow{kind: rowAddProfile, name: "+ add profile"})
	m.routeList = rows
}

// handleRouteKey drives the editor; delegates to the profile sub-form when open.
func (m *Model) handleRouteKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.profileForm != nil {
		return m.handleProfileForm(msg)
	}
	if len(m.routeList) == 0 {
		m.routing = false
		return nil, true
	}
	if m.routeEditing {
		switch msg.String() {
		case "enter":
			row := m.routeList[m.routeSel]
			val := strings.TrimSpace(m.routeBuf)
			if row.kind == rowSession {
				if val != "" {
					m.app.SetModel(m.sid, val)
					m.model = val
				}
			} else {
				m.app.SetAgentRoute(row.name, val)
			}
			m.refreshRouteList()
			m.routeEditing = false
			m.refresh()
			return nil, true
		case "esc":
			m.routeEditing = false
			m.refresh()
			return nil, true
		case "backspace":
			if n := len(m.routeBuf); n > 0 {
				m.routeBuf = m.routeBuf[:n-1]
			}
			m.routePickIdx = -1 // back to free text
			m.refresh()
			return nil, true
		case "left":
			if m.routeList[m.routeSel].kind == rowAgent {
				m.cycleProfilePick(-1)
			}
			m.refresh()
			return nil, true
		case "right":
			if m.routeList[m.routeSel].kind == rowAgent {
				m.cycleProfilePick(+1)
			}
			m.refresh()
			return nil, true
		}
		if t := msg.Key().Text; t != "" {
			m.routeBuf += t
			m.routePickIdx = -1 // typing overrides the profile picker
			m.refresh()
		}
		return nil, true
	}
	switch msg.String() {
	case "up", "ctrl+p":
		if m.routeSel > 0 {
			m.routeSel--
		}
	case "down", "ctrl+n":
		if m.routeSel < len(m.routeList)-1 {
			m.routeSel++
		}
	case "enter":
		switch row := m.routeList[m.routeSel]; row.kind {
		case rowSession, rowAgent:
			m.routeEditing = true
			m.routeBuf = ""
			m.routePickIdx = -1
		case rowProfile:
			m.openProfileForm(strings.TrimPrefix(row.name, "profile:"))
		case rowAddProfile:
			m.openProfileForm("")
		}
	case "esc", "ctrl+c":
		m.routing = false
	}
	m.refresh()
	return nil, true
}

// cycleProfilePick steps the agent-row edit buffer through the defined profiles
// (←/→), so a profile can be picked instead of typed. Wraps around.
func (m *Model) cycleProfilePick(dir int) {
	profs := m.app.Profiles()
	if len(profs) == 0 {
		return
	}
	m.routePickIdx += dir
	n := len(profs)
	if m.routePickIdx < 0 {
		m.routePickIdx = n - 1
	} else if m.routePickIdx >= n {
		m.routePickIdx = 0
	}
	m.routeBuf = profs[m.routePickIdx].Name
}

// openProfileForm opens the profile sub-editor for an existing profile (name set)
// or a new one (empty name).
func (m *Model) openProfileForm(name string) {
	f := &profileForm{isNew: name == "", name: name}
	var def app.ProfileDef
	for _, p := range m.app.Profiles() {
		if p.Name == name {
			def = p
		}
	}
	if f.isNew {
		f.fields = append(f.fields, formField{label: "name"})
	}
	hk, hv := firstHeader(def.Headers)
	f.fields = append(f.fields,
		formField{label: "base_url", value: def.BaseURL},
		formField{label: "api_key", value: def.APIKey, secret: true},
		formField{label: "model", value: def.Model},
		formField{label: "header_key", value: hk},
		formField{label: "header_value", value: hv},
	)
	m.profileForm = f
	m.refresh()
}

func (m *Model) handleProfileForm(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	f := m.profileForm
	if f.editing {
		switch msg.String() {
		case "enter":
			f.fields[f.sel].value = strings.TrimSpace(f.buf)
			f.editing = false
		case "esc":
			f.editing = false
		case "backspace":
			if n := len(f.buf); n > 0 {
				f.buf = f.buf[:n-1]
			}
		default:
			if t := msg.Key().Text; t != "" {
				f.buf += t
			}
		}
		m.refresh()
		return nil, true
	}
	switch msg.String() {
	case "tab":
		m.saveProfileForm() // quick-save from anywhere in the form
		return nil, true
	case "up", "ctrl+p":
		if f.sel > 0 {
			f.sel--
		}
	case "down", "ctrl+n":
		if f.sel < len(f.fields) { // last position == [save]
			f.sel++
		}
	case "enter":
		if f.sel == len(f.fields) {
			m.saveProfileForm()
			return nil, true
		}
		f.editing = true
		f.buf = f.fields[f.sel].value
	case "esc", "ctrl+c":
		m.profileForm = nil // discard, back to the list
	}
	m.refresh()
	return nil, true
}

// saveProfileForm builds a ProfileDef from the fields and applies+persists it.
func (m *Model) saveProfileForm() {
	f := m.profileForm
	get := func(label string) string {
		for _, fl := range f.fields {
			if fl.label == label {
				return strings.TrimSpace(fl.value)
			}
		}
		return ""
	}
	name := f.name
	if name == "" {
		name = get("name")
	}
	if name != "" {
		def := app.ProfileDef{Name: name, BaseURL: get("base_url"), APIKey: get("api_key"), Model: get("model")}
		if hk := get("header_key"); hk != "" {
			def.Headers = map[string]string{hk: get("header_value")}
		}
		m.app.SetProfile(def)
	}
	m.profileForm = nil
	m.refreshRouteList()
	m.refresh()
}

func firstHeader(h map[string]string) (string, string) {
	for k, v := range h {
		return k, v
	}
	return "", ""
}

// routeView renders the editor (or the profile sub-form when open).
func (m *Model) routeView() string {
	if m.profileForm != nil {
		return m.profileFormView()
	}
	var b strings.Builder
	hint := "↑/↓ select · enter edit/open · esc close"
	if m.routeEditing {
		hint = "type value · ←/→ pick profile · enter apply · empty clears · esc"
	}
	b.WriteString(stylePermTitle.Render("models & routing") + "  " + styleFooter.Render(hint) + "\n")
	sepDrawn := false
	for i, r := range m.routeList {
		// Set the profiles section (profile rows + add button) apart from the
		// session/agent rows with a blank line and a dim header.
		if !sepDrawn && (r.kind == rowProfile || r.kind == rowAddProfile) {
			b.WriteString("\n" + styleFooter.Render("backends (profiles)") + "\n")
			sepDrawn = true
		}
		if r.kind == rowAddProfile {
			btn := " + add profile "
			if i == m.routeSel {
				b.WriteString("  " + styleBtnSel.Render(btn) + "\n")
			} else {
				b.WriteString("  " + styleBtn.Render(btn) + "\n")
			}
			continue
		}
		val := r.value
		if i == m.routeSel && m.routeEditing {
			val = m.routeBuf + "▌"
		}
		line := fmt.Sprintf("%-16s %s", r.name, val)
		if i == m.routeSel {
			b.WriteString(stylePalSelRow.Render("› "+line) + "\n")
		} else {
			b.WriteString("  " + styleToolResult.Render(line) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// profileFormView renders the multi-field profile sub-editor.
func (m *Model) profileFormView() string {
	f := m.profileForm
	var b strings.Builder
	title := "edit profile: " + f.name
	if f.isNew {
		title = "new profile"
	}
	hint := "↑/↓ field · enter edit · esc cancel"
	if f.editing {
		hint = "type · enter ok · esc cancel"
	}
	b.WriteString(stylePermTitle.Render(title) + "  " + styleFooter.Render(hint) + "\n")
	for i, fl := range f.fields {
		val := fl.value
		if fl.secret && val != "" && !(f.editing && i == f.sel) {
			val = "••••"
		}
		if f.editing && i == f.sel {
			val = f.buf + "▌"
		}
		line := fmt.Sprintf("%-13s %s", fl.label, val)
		if i == f.sel {
			b.WriteString(stylePalSelRow.Render("› "+line) + "\n")
		} else {
			b.WriteString("  " + styleToolResult.Render(line) + "\n")
		}
	}
	b.WriteString("\n") // spacer: set the action apart from the fields
	if f.sel == len(f.fields) {
		b.WriteString("  " + styleBtnSel.Render(" Save "))
	} else {
		b.WriteString("  " + styleBtn.Render(" Save ") + styleFooter.Render("  (Tab)"))
	}
	return b.String()
}

// resumeRows caps how many sessions the picker shows at once.
const resumeRows = 12

// resumeView renders the interactive session picker (↑/↓ select, enter resume).
func (m *Model) resumeView() string {
	var b strings.Builder
	b.WriteString(stylePermTitle.Render("resume a session") + "  " +
		styleFooter.Render("↑/↓ select · enter resume · esc cancel") + "\n")
	start := 0
	if m.resumeSel >= resumeRows {
		start = m.resumeSel - resumeRows + 1
	}
	end := start + resumeRows
	if end > len(m.resumeList) {
		end = len(m.resumeList)
	}
	for i := start; i < end; i++ {
		s := m.resumeList[i]
		title := s.Title
		if title == "" {
			title = styleFooter.Render("(no messages)")
		}
		when := s.Created.Format("01-02 15:04")
		line := fmt.Sprintf("%s  %s", when, oneLine(title, max(20, m.width-24)))
		if i == m.resumeSel {
			b.WriteString(stylePalSelRow.Render("› "+line) + "\n")
		} else {
			b.WriteString("  " + styleToolResult.Render(line) + "\n")
		}
	}
	if len(m.resumeList) > resumeRows {
		b.WriteString(styleFooter.Render(fmt.Sprintf("  %d/%d", m.resumeSel+1, len(m.resumeList))))
	}
	return strings.TrimRight(b.String(), "\n")
}

// switchSession loads another session's transcript and re-subscribes to it.
func (m *Model) switchSession(sid session.SessionID) tea.Cmd {
	msgs, lastSeq, err := m.app.SessionState(m.ctx, sid)
	if err != nil {
		return m.snack("resume failed: " + err.Error())
	}
	// Switching to a different session drops any fork-origin link (a stale origin
	// would make /loopdiff compare unrelated sessions). /fork re-sets it after its
	// own switchSession call, so the fork flow is unaffected.
	if sid != m.sid {
		m.forkOrigin = ""
	}
	m.sid = sid
	m.blocks = rebuildBlocks(msgs)
	m.history = userPrompts(msgs) // seed ↑/↓ recall + tab completion from prior turns
	m.histIdx = len(m.history)
	m.cache = m.cache[:0]
	m.liveText, m.liveThink, m.running, m.activeAgents = "", "", false, nil
	m.restoreChildPanes(sid) // bring this session's subagents back as inspectable panes
	m.refresh()
	// Subscribe from lastSeq so we stream only new events (transcript already shown).
	return tea.Batch(m.startSub(sid, lastSeq), m.snack("resumed "+string(sid)))
}

// info appends an info block to the transcript.
func (m *Model) info(text string) {
	m.blocks = append(m.blocks, block{kind: blockInfo, text: text})
}

// cyclePermission rotates the tool-permission policy ask→auto→allow→deny→ask
// and shows the new mode in a snackbar. "auto" (accept-edits) auto-approves file
// edits but still confirms commands; "allow" auto-approves everything.
func (m *Model) cyclePermission() tea.Cmd {
	order := []string{"ask", "auto", "allow", "deny"}
	cur := m.app.Permission()
	next := order[0]
	for i, p := range order {
		if p == cur {
			next = order[(i+1)%len(order)]
		}
	}
	m.app.SetPermission(next)
	return m.snack("permission mode: " + next + permHint(next))
}

// permHint describes what a permission mode does, for the snackbar.
func permHint(mode string) string {
	switch mode {
	case "auto":
		return " — auto-accept edits, confirm commands"
	case "allow":
		return " — auto-approve everything"
	case "deny":
		return " — block all tool actions"
	default:
		return " — confirm each action"
	}
}

// permChip renders the permission mode as a color-coded chip: ask=amber (safe),
// auto=cyan (accept-edits), allow=yellow (caution), deny=red.
func permChip(mode string) string {
	c := colPrimary
	switch mode {
	case "auto":
		c = colAccent
	case "allow":
		c = colWarn
	case "deny":
		c = colError
	}
	return lipgloss.NewStyle().Foreground(colSurface).Background(c).Bold(true).Padding(0, 1).Render("perm " + mode)
}

// recallHistory replaces the input with a previously submitted prompt
// (↑ older, ↓ newer). Returns true when it handled the key.
func (m *Model) recallHistory(dir int) bool {
	if m.running || len(m.history) == 0 || m.ta.LineCount() > 1 {
		return false
	}
	ni := m.histIdx + dir
	if ni < 0 {
		ni = 0
	}
	if ni >= len(m.history) {
		m.histIdx = len(m.history)
		m.ta.SetValue("")
		return true
	}
	m.histIdx = ni
	m.ta.SetValue(m.history[ni])
	m.ta.CursorEnd()
	return true
}

// historyComplete returns the most recent prior prompt that starts with prefix
// (excluding an exact match). Repeated calls with the SAME completed value walk
// to the next-older match, so pressing tab cycles candidates.
func (m *Model) historyComplete(prefix string) string {
	// If the current value is itself a previous completion, continue from just
	// before it to reach an older match; otherwise scan from the newest.
	start := len(m.history) - 1
	for i := len(m.history) - 1; i >= 0; i-- {
		if m.history[i] == prefix {
			start = i - 1
			break
		}
	}
	for i := start; i >= 0; i-- {
		h := m.history[i]
		if h != prefix && strings.HasPrefix(h, prefix) {
			return h
		}
	}
	return ""
}

// sessionsList renders the sessions for the current workdir.
func (m *Model) sessionsList() string {
	metas, err := m.app.ListSessions(m.ctx, m.workdir)
	if err != nil || len(metas) == 0 {
		return "sessions: (none)"
	}
	var b strings.Builder
	b.WriteString("sessions in this directory:")
	for i, s := range metas {
		if i >= 10 {
			b.WriteString("\n  …")
			break
		}
		b.WriteString("\n  " + string(s.ID) + "  " + s.Created.Format("01-02 15:04"))
	}
	return b.String()
}

// orDefaultStr returns s, or def when s is empty.
func orDefaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// removeFirst removes the first occurrence of v from xs (used to retire a
// finished subagent from the active list).
func removeFirst(xs []string, v string) []string {
	for i, x := range xs {
		if x == v {
			return append(xs[:i], xs[i+1:]...)
		}
	}
	return xs
}

// agentSummary renders active subagent names compactly for the header badge,
// collapsing duplicates as "explore×2".
func agentSummary(names []string) string {
	order := make([]string, 0, len(names))
	count := map[string]int{}
	for _, n := range names {
		if count[n] == 0 {
			order = append(order, n)
		}
		count[n]++
	}
	parts := make([]string, 0, len(order))
	for _, n := range order {
		if count[n] > 1 {
			parts = append(parts, fmt.Sprintf("%s×%d", n, count[n]))
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, ", ")
}

func joinOr(xs []string, empty string) string {
	if len(xs) == 0 {
		return empty
	}
	return strings.Join(xs, ", ")
}

const helpText = "commands:\n" +
	"  /help         show this help\n" +
	"  /model        show the active model\n" +
	"  /agents       list subagents (delegate via the task tool)\n" +
	"  /tools        list available tools\n" +
	"  /sessions     list sessions in this directory\n" +
	"  /diff         show the working-tree git diff\n" +
	"  /init         analyze the project and write AGENTS.md\n" +
	"  /permission   cycle permission mode (ask/auto/allow/deny)\n" +
	"  /compact      summarize & shrink the conversation context\n" +
	"  /clear        clear the visible transcript\n" +
	"  /quit         exit magi\n" +
	"mentions:\n" +
	"  @path/to/file   include a file's contents in your message\n" +
	"subagents (when running in parallel):\n" +
	"  tab cycle focus · ctrl+o zoom focused pane · click a pane to focus · esc back\n" +
	"mouse:\n" +
	"  wheel scrolls · drag to select & copy · click a subagent pane to focus (no modes)\n" +
	"keys:\n" +
	"  enter send · esc interrupt · ↑/↓ history · pgup/pgdn scroll · ctrl+l clear · shift+tab perm mode · ctrl+c quit"

// submit sends a user prompt, expanding @file mentions and collapsed-paste
// placeholders into full content. The transcript shows the FULL pasted content
// (the input box is cramped, the main view isn't); history keeps the collapsed
// chip so ↑ recall doesn't dump the blob back into the input.
func (m *Model) submit(text string) tea.Cmd {
	m.history = append(m.history, text)
	m.histIdx = len(m.history)
	return m.submitAs(m.expandPastes(text), m.expandPastes(m.expandMentions(text)))
}

// submitAs displays one text in the transcript but sends another to the agent
// (used by @-mention expansion and /init). It clears the input box.
func (m *Model) submitAs(display, send string) tea.Cmd {
	m.ta.Reset()
	return m.sendPrompt(display, send)
}

// steer injects a message into the running turn (the agent picks it up at its
// next step) instead of queuing it. The message appears in the transcript
// immediately; the running spinner keeps going.
func (m *Model) steer(text string) tea.Cmd {
	m.history = append(m.history, text)
	m.histIdx = len(m.history)
	send := m.expandPastes(m.expandMentions(text))
	m.blocks = append(m.blocks, block{kind: blockUser, text: m.expandPastes(text)})
	m.ta.Reset()
	m.refresh()
	sid := m.sid
	note := "steered into the running turn"
	if m.anyPaneRunning() {
		note = "steered — the main agent will see this when the running subagents finish this step"
	}
	return tea.Batch(m.snack(note), func() tea.Msg {
		_ = m.app.Steer(m.ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: send}},
			Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
		})
		return nil
	})
}

// safeWhileRunning reports whether a slash command is safe to run during an
// in-flight turn (read-only / UI-only — does not mutate the running session).
func safeWhileRunning(cmd string) bool {
	switch cmd {
	case "/help", "/model", "/agents", "/route", "/tools", "/sessions", "/diff", "/loop", "/loopdiff", "/context", "/permission":
		return true
	}
	return false
}

// sendPrompt appends a user block and dispatches the prompt without touching the
// input box, so flushing the queue never clobbers what the user is now typing.
func (m *Model) sendPrompt(display, send string) tea.Cmd {
	m.closePanes() // a new turn retires the previous turn's subagent panes
	m.blocks = append(m.blocks, block{kind: blockUser, text: display})
	m.running = true
	m.turnStart = time.Now() // §8.1: start the elapsed/token meter
	m.turnIn, m.turnOut, m.turnDur = 0, 0, 0
	m.refresh()
	sid := m.sid
	return tea.Batch(m.sp.Tick, func() tea.Msg {
		_ = m.app.Submit(m.ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: send}},
			Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
		})
		return nil
	})
}

// mentionRE matches @-prefixed file paths.
var mentionRE = regexp.MustCompile(`@([^\s]+)`)

// expandMentions appends the contents of any @-mentioned files that exist in the
// workdir, so the agent has them in context (like a project's @ mentions).
func (m *Model) expandMentions(text string) string {
	matches := mentionRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	seen := map[string]bool{}
	for _, mt := range matches {
		rel := mt[1]
		if seen[rel] {
			continue
		}
		seen[rel] = true
		content, ok := m.readWorkdirFile(rel)
		if !ok {
			continue
		}
		b.WriteString("\n\n--- " + rel + " ---\n" + content)
	}
	return b.String()
}

// jailPath resolves a (possibly relative) path inside the workdir, rejecting
// escapes. Returns the absolute path.
func (m *Model) jailPath(rel string) (string, bool) {
	base := filepath.Clean(m.workdir)
	abs := filepath.Clean(filepath.Join(base, rel))
	if filepath.IsAbs(rel) {
		abs = filepath.Clean(rel)
	}
	if r, err := filepath.Rel(base, abs); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

// readWorkdirImagePath returns the jailed absolute path for an image file.
func (m *Model) readWorkdirImagePath(rel string) (string, bool) { return m.jailPath(rel) }

// readWorkdirFile reads a workdir-relative file (jailed), capped at 50KB.
func (m *Model) readWorkdirFile(rel string) (string, bool) {
	abs, ok := m.jailPath(rel)
	if !ok {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return "", false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", false
	}
	const cap = 50 * 1024
	if len(data) > cap {
		return string(data[:cap]) + "\n…(truncated)", true
	}
	return string(data), true
}

const initPrompt = "Analyze this project and create an AGENTS.md file at the repo root using the write tool. " +
	"Include: a one-paragraph overview, the directory structure, key conventions, and the build/test/run commands. " +
	"Inspect the project first (list, read, glob, grep) before writing. Keep it concise and accurate."

// ultraPreamble turns the agent into an orchestrator (orchestrator-style Ultra Work Mode):
// plan → delegate to specialists (in parallel) → implement → verify → self-correct.
const ultraPreamble = "You are operating in ULTRA WORK MODE as an orchestrator. Work autonomously and thoroughly:\n" +
	"1. Make a plan with the todowrite tool.\n" +
	"2. Gather context by delegating to subagents via the task tool — run independent investigations IN PARALLEL " +
	"(one task call with a tasks array): explore (map the area), librarian (find exact locations), oracle (hard reasoning).\n" +
	"3. Implement changes by delegating to the coder subagent.\n" +
	"4. Verify with the tester subagent; if it fails, self-correct (delegate fixes) and re-verify.\n" +
	"5. Have the reviewer subagent check the result.\n" +
	"6. Keep todo statuses updated and finish with a concise summary of what changed and how it was verified."

func (m *Model) respond(decision string) tea.Cmd {
	p := m.perm
	m.perm = nil
	sid := m.sid
	return func() tea.Msg {
		_ = m.app.RespondPermission(m.ctx, command.RespondPermission{
			SessionID: sid, CallID: p.callID, Decision: decision,
			Actor: event.Actor{Kind: event.ActorUser, ID: "tui"},
		})
		return nil
	}
}

// applyEvent folds a domain event into the transcript state.
func (m *Model) applyEvent(e event.Event) {
	switch e.Type {
	case event.TypePromptSubmitted:
		// A subagent result injected into the parent (actor=agent) shows in the
		// main transcript as a COMPACT one-liner — the full text lives in that
		// subagent's pane/detail view. Real user prompts are added locally (submit/
		// steer), so they're not handled here.
		if e.Actor.Kind == event.ActorAgent {
			name := strings.TrimPrefix(e.Actor.ID, "subagent:")
			m.blocks = append(m.blocks, block{kind: blockInfo, text: "↩ " + name + " reported (open its pane to read)"})
		}

	case event.TypePartDelta:
		var d event.PartDeltaData
		if json.Unmarshal(e.Data, &d) == nil {
			switch d.Kind {
			case session.PartText:
				m.liveText += d.Text
			case session.PartReasoning:
				m.liveThink += d.Text
			}
		}

	case event.TypePartAppended:
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			return
		}
		switch d.Part.Kind {
		case session.PartReasoning:
			m.liveThink = ""
			m.blocks = append(m.blocks, block{kind: blockReasoning, text: d.Part.Text})
		case session.PartText:
			m.liveText = ""
			m.blocks = append(m.blocks, block{kind: blockAssistant, text: d.Part.Text})
		case session.PartToolCall:
			if d.Part.ToolCall != nil {
				m.liveText = ""
				m.blocks = append(m.blocks, block{
					kind: blockToolCall,
					name: d.Part.ToolCall.Name,
					args: string(d.Part.ToolCall.Args),
				})
			}
		case session.PartToolResult:
			if d.Part.ToolResult != nil {
				m.foldToolResult(toolResultText(d.Part.ToolResult), !d.Part.ToolResult.IsError)
			}
		}

	case event.TypeAgentSpawned:
		var d event.AgentStatusData
		if json.Unmarshal(e.Data, &d) == nil {
			m.activeAgents = append(m.activeAgents, orDefaultStr(d.Role, d.AgentID))
		}

	case event.TypeAgentStatus:
		var d event.AgentStatusData
		if json.Unmarshal(e.Data, &d) == nil && d.State == "done" {
			m.activeAgents = removeFirst(m.activeAgents, orDefaultStr(d.Role, d.AgentID))
			if p := m.paneBySID(session.SessionID(d.AgentID)); p != nil {
				p.done = true
			}
		}

	case event.TypePermissionRequested:
		var d event.PermissionRequestedData
		if json.Unmarshal(e.Data, &d) == nil {
			m.perm = &permReq{callID: d.CallID, name: d.Name, args: string(d.Args)}
		}

	case event.TypeContextUsage:
		var d event.ContextUsageData
		if json.Unmarshal(e.Data, &d) == nil {
			m.ctxPct = d.Percent
			m.turnIn = d.Tokens     // ↑ current context (§8.1)
			m.turnOut = d.OutTokens // ↓ cumulative output so far
		}

	case event.TypeWorkflowPhase:
		var d event.WorkflowPhaseData
		if json.Unmarshal(e.Data, &d) == nil && d.Phase == "plan" {
			m.plannerMode = d.Status // header chip (compact: solo | parallel)
			// Also surface the planner's decision + reason as a visible line.
			info := "◈ planner: " + d.Status
			if d.Detail != "" {
				info += " — " + d.Detail
			}
			m.blocks = append(m.blocks, block{kind: blockInfo, text: info})
		}

	case event.TypeCouncilConvened:
		// A council round opened: record it as a transcript milestone and arm the
		// header chip. (D14 — the consensus termination gate.)
		var d event.CouncilConvenedData
		if json.Unmarshal(e.Data, &d) == nil {
			m.councilRound = d.Round
			m.pendingCouncilEvidence = formatCouncilEvidence(d) // shown in each verdict's detail view
			label, verb := "council", "deliberate"
			if d.Phase == "plan" {
				label, verb = "plan audit", "review the plan"
			}
			line := fmt.Sprintf("⚖ %s round %d — %s %s (%s)", label, d.Round, strings.Join(d.Members, ", "), verb, d.Rule)
			if len(d.Signals) > 0 {
				line += " · " + strings.Join(d.Signals, ", ")
			}
			// Plan audit: show the procedure being judged THIS round, so a revised plan
			// that gets rejected and replanned stays visible (you can see what changed
			// across rounds, not just the final one that ran).
			if d.Phase == "plan" {
				if plan := strings.TrimSpace(d.Plan); plan != "" {
					for _, pl := range strings.Split(plan, "\n") {
						line += "\n    " + pl
					}
				}
			}
			m.blocks = append(m.blocks, block{kind: blockInfo, text: line})
		}

	case event.TypeCouncilDeliberating:
		// Live: which member is being polled right now (header chip).
		var d event.CouncilDeliberatingData
		if json.Unmarshal(e.Data, &d) == nil {
			m.councilRound = d.Round
			m.councilMember = d.Member
		}

	case event.TypeCouncilVerdict:
		// One compact, member-colored line per vote (icon + name + decision); the
		// full reasoning (lens/rationale/feedback) is attached to the block and
		// shown in a detail modal when the line is clicked.
		var d event.CouncilVerdictData
		if json.Unmarshal(e.Data, &d) == nil {
			vd := d
			m.blocks = append(m.blocks, block{kind: blockCouncilVerdict, councilVerdict: &vd, evidence: m.pendingCouncilEvidence})
		}

	case event.TypeCouncilDecided:
		// The round's outcome + tally. A "continue" injects feedback and the loop
		// runs again; "done" (or a noted forced finish) lets the turn end. Clear the
		// chip: between rounds the agent is working, not deliberating, so the council
		// indicator should show only during an open round (convened→decided).
		m.councilRound = 0
		m.councilMember = ""
		var d event.CouncilDecidedData
		if json.Unmarshal(e.Data, &d) == nil {
			label := "council"
			if d.Phase == "plan" {
				label = "plan audit"
			}
			_, verdict := councilVerdictLabel(d.Phase, d.Decision) // termination continue → reject; plan → approve/revise
			if strings.Contains(d.Note, "finishing") || strings.Contains(d.Note, "proceeding") {
				// Any forced finish (round cap OR no-progress) — not a real approval/done.
				// Normal consensus decisions carry no note; error fallbacks read as-is.
				verdict = "finished (no consensus)"
				if d.Phase == "plan" {
					verdict = "proceed (no consensus)"
				}
			}
			doneLabel, contLabel := "done", "continue"
			if d.Phase == "plan" {
				doneLabel, contLabel = "approve", "revise" // plan audit votes are approve/revise, not done/continue
			}
			line := fmt.Sprintf("⚖ %s round %d: %s — %d %s / %d %s", label, d.Round, verdict, d.Tally.Done, doneLabel, d.Tally.Continue, contLabel)
			if d.Tally.Abstain > 0 {
				line += fmt.Sprintf(" / %d abstain", d.Tally.Abstain)
			}
			if d.Note != "" {
				line += " (" + d.Note + ")"
			} else if d.Feedback != "" {
				line += " → feedback injected"
			}
			m.blocks = append(m.blocks, block{kind: blockInfo, text: line})
		}

	case event.TypeTurnFinished:
		m.running = false
		m.liveText = ""
		m.liveThink = ""
		m.activeAgents = nil
		m.councilRound = 0
		m.councilMember = ""
		// Freeze the turn meter from the cumulative usage (§8.1).
		if !m.turnStart.IsZero() {
			m.turnDur = time.Since(m.turnStart)
		}
		var fd event.TurnFinishedData
		if json.Unmarshal(e.Data, &fd) == nil {
			if fd.Usage.In > 0 {
				m.turnIn = fd.Usage.In
			}
			if fd.Usage.Out > 0 {
				m.turnOut = fd.Usage.Out
			}
		}

	case event.TypeError:
		var d event.ErrorData
		_ = json.Unmarshal(e.Data, &d)
		m.running = false
		m.liveText = ""
		m.liveThink = ""
		m.activeAgents = nil
		m.councilRound = 0
		m.councilMember = ""
		if !m.turnStart.IsZero() { // freeze the meter too (mirror panes) (§8.1)
			m.turnDur = time.Since(m.turnStart)
		}
		m.blocks = append(m.blocks, block{kind: blockError, text: d.Message})
	}
}

func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	m.ta.SetWidth(w - 4)
	m.buildGlam(m.width - m.panelCols()) // wrap to the transcript width (panel-aware)

	if !m.ready {
		m.vp = viewport.New()
		m.ready = true
	}
	// Width/height/content (and follow-to-bottom) are handled by refresh(), which
	// the caller invokes right after resize.
}

// buildGlam (re)creates the markdown renderer to wrap at transcript width tw,
// matching the theme. No-op if tw is unchanged since the last build.
func (m *Model) buildGlam(tw int) {
	if tw == m.glamWidth && m.glam != nil {
		return
	}
	mdStyle := "light"
	if m.isDark {
		mdStyle = "dark"
	}
	if glam, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(mdStyle),
		glamour.WithWordWrap(max(20, tw-6)),
	); err == nil {
		m.glam = glam
		m.glamWidth = tw
	}
}

// maxInputRows caps how tall the input box grows for multi-line input.
const maxInputRows = 6

// chromeHeight is the number of rows used by header + input + footer (+ modal /
// command palette).
// minViewport is the smallest transcript viewport we keep visible; the pane block
// is capped so chrome + viewport never exceeds the screen (input stays on screen).
const minViewport = 3

// panesMaxNumer/panesMaxDenom bound the subagent pane block to a fraction of the
// screen (≈ 1/2) so a burst of agents can't swallow the transcript. Panes past the
// cap scroll (renderPanes window) instead of growing the block. This is a stricter
// limit than minViewport alone, which would let panes shrink the transcript to 3 rows.
const panesMaxNumer, panesMaxDenom = 1, 3

// baseChromeHeight is the fixed chrome (everything except the agent-pane block):
// header, bordered input, footer, and any active modal. It must NOT call
// panesBlockHeight (panesBlockHeight derives its cap from this — no recursion).
func (m *Model) baseChromeHeight() int {
	inputRows := m.ta.Height()
	if inputRows < 1 {
		inputRows = 1
	}
	h := 2 + (inputRows + 2) + 1 // header(2) + bordered input(rows+border) + footer
	if m.perm != nil {
		h += 5
	}
	if m.resuming {
		rows := len(m.resumeList)
		if rows > resumeRows {
			rows = resumeRows + 1 // +1 for the "n/N" position line
		}
		h += rows + 1 // header line of the picker
	}
	if m.routing {
		if m.profileForm != nil {
			h += len(m.profileForm.fields) + 3 // fields + spacer + Save + header
		} else {
			h += len(m.routeList) + 3 // rows + header + profiles separator (blank + label)
		}
	}
	if n := len(m.paletteMatches()); !m.running && n > 0 {
		h += n + 2 // palette rows + box border
	}
	// The snackbar is a floating toast overlay (overlayLine), so it reserves no row.
	return h
}

func (m *Model) chromeHeight() int { return m.baseChromeHeight() + m.panesBlockHeight() }

// overlayLine composites overlay onto a screen row of content at display column
// col (ANSI-aware), without changing the content's dimensions — used for toasts.
func overlayLine(content string, row, col int, overlay string) string {
	lines := strings.Split(content, "\n")
	if row < 0 || row >= len(lines) {
		return content
	}
	line := lines[row]
	w := ansi.StringWidth(line)
	ow := ansi.StringWidth(overlay)
	if col < 0 {
		col = 0
	}
	left := ansi.Cut(line, 0, col)
	if lw := ansi.StringWidth(left); lw < col {
		left += strings.Repeat(" ", col-lw) // pad if the line is shorter than col
	}
	right := ""
	if col+ow < w {
		right = ansi.Cut(line, col+ow, w)
	}
	lines[row] = left + overlay + right
	return strings.Join(lines, "\n")
}

// panesBlockHeight is the rows reserved for the tiled subagent overview. Zero
// when there are no panes or when zoomed (zoom takes over the whole viewport).
// paneLayout is the SINGLE source of truth for how the agent panes fit on screen,
// consumed by both panesBlockHeight (reserve) and renderPanes (render) so they can
// never drift. It caps the pane block to the space left after base chrome and a
// minimum viewport, and folds any overflow into a "+N more" summary row.
//
//	nShown  = panes actually rendered
//	perPane = rows each running pane gets (collapsed panes are 1 row)
//	more    = panes hidden behind the "+N more" line (0 = none)
//	total   = exact rendered height (== panesBlockHeight)
func (m *Model) paneLayout() (nShown, perPane, more, total int) {
	n := len(m.panes)
	if n == 0 || m.zoom {
		return 0, 0, 0, 0
	}
	capH := m.height - m.baseChromeHeight() - minViewport
	if capH < 0 {
		capH = 0
	}
	// Cap the block to ~half the screen so panes never crowd out the transcript;
	// the overflow scrolls instead of growing. (minViewport alone would allow the
	// block to fill everything down to a 3-row viewport.)
	if frac := m.height * panesMaxNumer / panesMaxDenom; capH > frac {
		capH = frac
	}
	if !m.running {
		// Collapsed: one row per finished pane.
		if n <= capH {
			return n, 1, 0, n
		}
		show := capH - 1 // reserve one row for "+N more"
		if show <= 0 {
			if capH >= 1 {
				return 0, 0, n, 1 // only the summary fits
			}
			return 0, 0, 0, 0
		}
		return show, 1, n - show, show + 1
	}
	// Running: a pane box is title + >=1 content + 2 border = >=4 rows (and renders
	// exactly perPane rows when perPane>=4). Size the COUNT so show*4 fits the budget
	// (capH minus a "+N more" row on overflow), so per-pane never floors past the cap.
	const minPane, maxPane = 4, 6
	maxShow := capH / minPane
	if n > maxShow {
		maxShow = (capH - 1) / minPane // overflow → one row goes to "+N more"
	}
	show := n
	if show > maxShow {
		show = maxShow
	}
	if show <= 0 {
		if capH >= 1 {
			return 0, 0, n, 1 // only the summary fits
		}
		return 0, 0, 0, 0
	}
	more = n - show
	budget := capH
	if more > 0 {
		budget-- // reserve the "+N more" row
	}
	perPane = budget / show // >= minPane by construction (show*minPane <= budget)
	if perPane > maxPane {
		perPane = maxPane // compact panes; a couple of agents shouldn't fill the screen
	}
	total = show * perPane
	if more > 0 {
		total++
	}
	return show, perPane, more, total
}

func (m *Model) panesBlockHeight() int {
	_, _, _, total := m.paneLayout()
	return total
}

// refresh recomputes the viewport content and pins it to the bottom.
func (m *Model) refresh() {
	if !m.ready {
		return
	}
	// Grow the input box to fit multi-line input (so it doesn't show only the last
	// line), capped at maxInputRows.
	if rows := clampInt(m.ta.LineCount(), 1, maxInputRows); rows != m.ta.Height() {
		m.ta.SetHeight(rows)
	}
	// Capture follow intent from the CURRENT state, before any height/content
	// change — a layout change (subagent pane added/removed, terminal resize) or
	// new content must not flip whether we were pinned to the bottom.
	follow := m.vp.AtBottom()
	vpHeight := m.height - m.chromeHeight()
	if vpHeight < minViewport {
		vpHeight = minViewport
	}
	// Reserve the right status panel's width; the transcript wraps to the rest.
	tw := m.width - m.panelCols()
	m.buildGlam(tw)
	m.vp.SetWidth(tw)
	m.vp.SetHeight(vpHeight)
	content := m.transcript()
	if m.councilDetail != nil {
		// Council verdict detail takes over the viewport (like a zoomed pane).
		content = m.renderCouncilDetail(tw)
	} else if m.zoom {
		// Zoomed: the viewport shows the focused subagent's full transcript.
		content = m.renderZoom(tw)
	}
	// Keep styled + ANSI-stripped copies for cell-precise selection + copy.
	m.contentLines = strings.Split(content, "\n")
	m.contentPlain = make([]string, len(m.contentLines))
	for i, l := range m.contentLines {
		m.contentPlain[i] = ansi.Strip(l)
	}
	if m.selecting || m.selActive {
		content = m.highlightSelection()
	}
	m.vp.SetContent(content)
	if follow {
		m.vp.GotoBottom()
	}
}

// selBounds returns the selection ordered so (sl,sc) <= (el,ec).
func (m *Model) selBounds() (sl, sc, el, ec int) {
	sl, sc, el, ec = m.selAL, m.selAC, m.selHL, m.selHC
	if sl > el || (sl == el && sc > ec) {
		sl, sc, el, ec = el, ec, sl, sc
	}
	return
}

// highlightSelection reverse-videos the selected cell range on each line,
// preserving the surrounding markdown styling (cells are cut ANSI-aware).
func (m *Model) highlightSelection() string {
	sl, sc, el, ec := m.selBounds()
	out := make([]string, len(m.contentLines))
	copy(out, m.contentLines)
	for i := sl; i <= el && i < len(out); i++ {
		if i < 0 {
			continue
		}
		styled := m.contentLines[i]
		w := ansi.StringWidth(styled)
		c0, c1 := 0, w
		if i == sl {
			c0 = sc
		}
		if i == el {
			c1 = ec
		}
		if c0 < 0 {
			c0 = 0
		}
		if c1 > w {
			c1 = w
		}
		if c0 >= c1 {
			continue
		}
		mid := ansi.Strip(ansi.Cut(styled, c0, c1))
		out[i] = ansi.Cut(styled, 0, c0) + styleSelection.Render(mid) + ansi.Cut(styled, c1, w)
	}
	return strings.Join(out, "\n")
}

// screenToContent maps a screen cell (x,y) to a (content line, display column),
// accounting for the header height and the current scroll offset.
func (m *Model) screenToContent(x, y int) (line, col int) {
	const vpTop = 2 // header: title + divider
	line = m.vp.YOffset() + (y - vpTop)
	if line < 0 {
		line = 0
	}
	if n := len(m.contentPlain); n > 0 && line >= n {
		line = n - 1
	}
	col = x
	if col < 0 {
		col = 0
	}
	if line >= 0 && line < len(m.contentPlain) {
		if w := ansi.StringWidth(m.contentPlain[line]); col > w {
			col = w
		}
	}
	return line, col
}

// selectedText returns the plain text of the current cell-precise selection.
func (m *Model) selectedText() string {
	if len(m.contentPlain) == 0 {
		return ""
	}
	sl, sc, el, ec := m.selBounds()
	if sl < 0 {
		sl = 0
	}
	if el >= len(m.contentPlain) {
		el = len(m.contentPlain) - 1
	}
	var rows []string
	for i := sl; i <= el; i++ {
		plain := m.contentPlain[i]
		w := ansi.StringWidth(plain)
		c0, c1 := 0, w
		if i == sl {
			c0 = sc
		}
		if i == el {
			c1 = ec
		}
		if c0 < 0 {
			c0 = 0
		}
		if c1 > w {
			c1 = w
		}
		if c0 > c1 {
			c0 = c1
		}
		rows = append(rows, strings.TrimRight(ansi.Cut(plain, c0, c1), " "))
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}

func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	// Mouse capture is always on: the wheel scrolls, click focuses a pane, and
	// drag selects+copies in-app — so there's no mode toggle for the user to learn.
	v.MouseMode = tea.MouseModeCellMotion
	if m.quitting {
		return v
	}
	if !m.ready {
		v.Content = "starting magi…"
		return v
	}

	var headLine string
	if m.councilDetail != nil {
		// Council detail view: a clickable breadcrumb back to the transcript.
		c := m.councilColor(m.councilDetail.Member)
		headLine = styleKeyLabel.Render("‹ back") + styleHeader.Render("   ") +
			styleBrand.Render("✦ magi") + styleHeader.Render(" › ") +
			lipgloss.NewStyle().Foreground(c).Bold(true).Render("⚖ "+m.councilDetail.Member+" verdict")
	} else if m.zoom && m.focusPane >= 0 && m.focusPane < len(m.panes) {
		// Zoom view: the header is a clickable breadcrumb back to the overview.
		p := m.panes[m.focusPane]
		c := m.paneColorOf(p)
		headLine = styleKeyLabel.Render("‹ back") + styleHeader.Render("   ") +
			styleBrand.Render("✦ magi") + styleHeader.Render(" › ") +
			lipgloss.NewStyle().Foreground(c).Bold(true).Render(p.desc(max(20, m.width-24))) + "  " + m.paneStatus(p)
	} else {
		headLine = styleBrand.Render("✦ magi") +
			styleHeader.Render("   model "+m.model+m.ctxMeter()+"   ") +
			permChip(m.app.Permission())
		if m.plannerMode != "" {
			headLine += "  " + styleKeyLabel.Render("◈ plan: "+m.plannerMode)
		}
		if m.councilRound > 0 {
			chip := fmt.Sprintf("⚖ council r%d", m.councilRound)
			if m.councilMember != "" {
				chip += ": " + m.councilMember
			}
			headLine += "  " + styleKeyLabel.Render(chip)
		}
		if len(m.activeAgents) > 0 {
			headLine += "  " + styleBadge.Render(fmt.Sprintf("⛐ %d: %s", len(m.activeAgents), agentSummary(m.activeAgents)))
		}
	}
	header := styleHeader.Render(headLine) +
		"\n" + styleDivider.Render(strings.Repeat("─", max(1, m.width)))

	var status string
	switch {
	case m.running:
		status = "  " + m.sp.View() + styleFooter.Render(" working… "+turnMeter(time.Since(m.turnStart), m.turnIn, m.turnOut)+"  ") + footerKeys("esc", "interrupt")
	case m.turnDur > 0:
		status = styleFooter.Render("  "+turnMeter(m.turnDur, m.turnIn, m.turnOut)) + "   " + footer()
	default:
		status = footer()
	}

	// The input stays live even while a turn runs — you can keep typing, and
	// pressing enter queues the prompt for when the turn finishes.
	inputStyle := styleInput
	if m.ta.Focused() {
		inputStyle = styleInputFocus
	}
	input := inputStyle.Width(m.width - 2).Render(m.ta.View())

	// Transcript area (viewport + tiled subagent panes) = left column; the status
	// panel, if any, sits to its right at a fixed width.
	tw := m.width - m.panelCols()
	// The viewport may render fewer rows than its height when the transcript is
	// short; place it in a full-height box (blank rows become spaces, which
	// JoinVertical keeps) so the panes/input below sit at the bottom of the screen
	// instead of floating with empty space beneath the input.
	vpContent := m.vp.View()
	if strings.TrimSpace(vpContent) == "" {
		vpContent = " " // empty/blank content isn't padded by lipgloss; give it a space
	}
	// Width+Height fills every row to tw columns (blank rows become spaces), so
	// JoinVertical keeps them and the panes/input below sit at the screen bottom.
	vpv := lipgloss.NewStyle().Width(tw).Height(m.vp.Height()).Render(vpContent)
	leftRows := []string{vpv}
	aboveInput := 2 + m.vp.Height() // header(2: title+divider) + viewport rows above input
	if pv := m.renderPanes(tw, aboveInput); pv != "" {
		leftRows = append(leftRows, pv)
		aboveInput += lipgloss.Height(pv)
	}
	left := lipgloss.JoinVertical(lipgloss.Left, leftRows...)
	mid := left
	if m.panelCols() > 0 {
		mid = lipgloss.JoinHorizontal(lipgloss.Top, left, " ", m.statusPanel(lipgloss.Height(header), lipgloss.Height(left)))
	}
	parts := []string{header, mid}
	if m.resuming {
		pv := m.resumeView()
		parts = append(parts, pv)
		aboveInput += lipgloss.Height(pv)
	} else if m.routing {
		pv := m.routeView()
		parts = append(parts, pv)
		aboveInput += lipgloss.Height(pv)
	} else if matches := m.paletteMatches(); !m.running && len(matches) > 0 {
		pv := m.paletteView(matches)
		parts = append(parts, pv)
		aboveInput += lipgloss.Height(pv)
	} else if m.perm != nil {
		pv := m.permView()
		parts = append(parts, pv)
		aboveInput += lipgloss.Height(pv)
	}
	parts = append(parts, input)
	parts = append(parts, status)
	v.Content = lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Toast: overlay a transient notice in the top-left (on the header divider)
	// without reserving a layout row, so it floats and doesn't shift the UI.
	if m.snackbar != "" {
		v.Content = overlayLine(v.Content, 1, 2, styleToast.Render(m.snackbar))
	}

	// Report the real cursor at the input position so IME composition (Korean,
	// etc.) appears inline rather than at the screen corner — including while a
	// turn runs, so queued typing composes correctly.
	if m.perm == nil && m.ta.Focused() {
		if c := m.ta.Cursor(); c != nil {
			// Offset by the input box (border+padding = 2 cols) and the rows above it
			// (+1 for the box's top border).
			c.Position.X += 2
			c.Position.Y += aboveInput + 1
			v.Cursor = c
		}
	}
	return v
}

// paletteView renders the slash-command completion popup.
func (m *Model) paletteView(matches []cmdInfo) string {
	sel := m.clampSel(len(matches))
	// Pad command names to a common width so descriptions align in a column.
	nameW := 0
	for _, c := range matches {
		if len(c.name) > nameW {
			nameW = len(c.name)
		}
	}
	var b strings.Builder
	for i, c := range matches {
		if i > 0 {
			b.WriteString("\n")
		}
		name := c.name + strings.Repeat(" ", nameW-len(c.name))
		if i == sel {
			b.WriteString(stylePalSelRow.Render("› " + name + "   " + c.desc))
		} else {
			b.WriteString("  " + stylePalName.Render(name) + "   " + styleFooter.Render(c.desc))
		}
	}
	return stylePalBox.Width(m.width - 2).Render(b.String())
}

// renderCouncilDetail is the FULL-SCREEN detail for one clicked council verdict —
// the same destination style as a zoomed subagent pane. It shows the member's vote
// (decision/lens/confidence/rationale/feedback) AND the evidence the members were
// given that round (task/plan/report/diff), so the user sees both what was judged
// and how. Returned as viewport content (scrolls); closed with esc or a click.
func (m *Model) renderCouncilDetail(width int) string {
	v := m.councilDetail
	if v == nil {
		return ""
	}
	hue := m.councilColor(v.Member)
	wrap := lipgloss.NewStyle().Width(max(8, width-2))
	var b strings.Builder
	// (The "‹ back" breadcrumb is the fixed header — see View.)
	icon, word := councilVerdictLabel(v.Phase, v.Decision)
	b.WriteString(lipgloss.NewStyle().Foreground(hue).Bold(true).Render("⚖ "+v.Member) + "  " + councilVerdictStyle(v.Phase, v.Decision).Render(icon+" "+word))
	if v.Lens != "" {
		b.WriteString(styleFooter.Render("  [" + v.Lens + "]"))
	}
	if v.Confidence > 0 {
		b.WriteString(styleFooter.Render(fmt.Sprintf("  · confidence %.0f%%", v.Confidence*100)))
	}
	b.WriteString("\n")
	// 기승전결: what the member SAW (evidence) first, then the verdict's reasoning.
	if ev := strings.TrimSpace(m.councilDetailEvidence); ev != "" {
		b.WriteString("\n" + styleFooter.Render("— evidence the council saw —") + "\n\n" + wrap.Render(ev) + "\n")
	}
	if v.Rationale != "" {
		b.WriteString("\n" + styleFooter.Render("rationale") + "\n" + wrap.Render(v.Rationale) + "\n")
	}
	if v.Feedback != "" {
		b.WriteString("\n" + styleFooter.Render("next step") + "\n" + wrap.Render(v.Feedback) + "\n")
	}
	return b.String()
}

// formatCouncilEvidence renders the round's evidence (what every member saw) for
// the detail view.
func formatCouncilEvidence(d event.CouncilConvenedData) string {
	var b strings.Builder
	add := func(title, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		b.WriteString("# " + title + "\n" + strings.TrimSpace(body) + "\n\n")
	}
	add("Task", d.Task)
	if d.Phase == "plan" {
		add("Proposed plan", d.Plan)
	} else {
		add("Plan / acceptance criteria", d.Plan)
		add("Agent report (the claim)", d.Report)
	}
	if len(d.Signals) > 0 {
		add("Signals", strings.Join(d.Signals, "\n"))
	}
	add("Diff", d.Diff)
	if d.NoChanges {
		b.WriteString("(no files changed — a read-only / answer turn)\n")
	}
	return strings.TrimSpace(b.String())
}

func (m *Model) permView() string {
	body := stylePermTitle.Render("permission required") + "\n" +
		fmt.Sprintf("run tool %s %s\n", styleToolName.Render(m.perm.name), styleToolArgs.Render(compactArgs(m.perm.args))) +
		styleFooter.Render("") +
		footerKeys("y", "allow") + footerKeys("a", "always") + footerKeys("n", "deny")
	return stylePermBox.Width(m.width - 4).Render(body)
}

// ctxMeter renders the context-window usage indicator (e.g. "   ctx 42%").
func (m *Model) ctxMeter() string {
	if m.ctxPct <= 0 {
		return ""
	}
	return fmt.Sprintf("   ctx %.0f%%", m.ctxPct)
}

// turnMeter renders elapsed + token usage, e.g. "3m49s · ↑28.1k ↓10.4k". Token
// parts are omitted when unknown (a backend that reports no usage). (§8.1)
func turnMeter(d time.Duration, in, out int) string {
	s := fmtDur(d)
	if in > 0 {
		s += " · ↑" + humanTokens(in)
	}
	if out > 0 {
		s += " ↓" + humanTokens(out)
	}
	return s
}

// fmtDur formats a duration compactly: "47s", "3m49s", "1h02m".
func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// humanTokens abbreviates token counts: 847, 10.4k, 1.2M.
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return strconv.Itoa(n)
	}
}

func footer() string {
	return styleFooter.Render("") +
		footerKeys("enter", "send") + footerKeys("esc", "interrupt") + footerKeys("ctrl+c", "quit")
}

func footerKeys(key, desc string) string {
	return "  " + styleKeyLabel.Render(key) + " " + styleFooter.Render(desc)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
