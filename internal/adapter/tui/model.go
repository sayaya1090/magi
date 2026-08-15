package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/report"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
	"github.com/sayaya1090/magi/internal/version"
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
// questReq is a pending ask_user question shown as a selection modal.
type questReq struct {
	callID   string
	question string
	options  []string
	sel      int
	// report is the grounds the agent wrote for whoever has to decide: what it tried, what each
	// way costs, which way it leans — the sections the decision-report skill asked for.
	//
	// It was dropped here. The tool refuses a report with a section missing, the event carries it,
	// the console draws it — and the terminal, where the person usually is, showed the question
	// with the working thrown away. A decision put to somebody who has been away is the one place
	// the grounds are worth the most, and it was the one place they did not arrive.
	report []report.Filled
	// index/total place it in the run its call is asking. A person answering the first of three
	// and being handed the next without warning is in a different situation from one answering
	// the only question there is.
	index, total int
}

type permReq struct {
	sid    session.SessionID // session that raised it — the MAIN turn or a SUBAGENT child (routes the reply)
	callID string
	name   string
	args   string
	reason string // policy reason the prompt fired (empty = routine confirmation)
	sel    int    // focused button index into permButtons (Tab/click navigation)
}

// Model is the Bubble Tea model for the interactive TUI.
// CommandSource supplies plugin-contributed slash commands (e.g. /login) to the
// TUI palette and dispatch. Satisfied by *pluginlua.Host; nil when no plugin
// host is wired, in which case the TUI has no plugin commands.
type CommandSource interface {
	PluginCommands() []port.PluginCommand
	DispatchCommand(name string, args []string) (bool, error)
	// TakeUIEffects drains UI effects a command queued while running (e.g. a
	// plugin /logout asking to clear the transcript back to the splash).
	TakeUIEffects() []string
}

type Model struct {
	ctx context.Context
	// app is the ENGINE, reached only through the interface next door. See engine.go for why the
	// boundary is written down rather than left to whatever the screen happens to reach for.
	app     Engine
	cmds    CommandSource // plugin slash commands (may be nil)
	sid     session.SessionID
	model   string
	userLbl string // display name for the user block (plugin set_user_label); "" = "you"
	workdir string

	forkOrigin session.SessionID // the session this one was forked from (for /loopdiff)
	// movedTo is where the companion went when it left THIS conversation, or empty.
	//
	// A viewer is not dragged after it. Somebody attached to a conversation is reading that
	// conversation, and having the screen swap under the cursor because a console elsewhere picked
	// another one is the same rudeness as a page that scrolls itself. So the fact is stated, the
	// offer is one key, and the decision stays with whoever is looking.
	movedTo session.SessionID

	history []string // submitted prompts (↑/↓ recall)
	histIdx int      // current position when browsing history
	// histDraft is what the user had typed before stepping up into the history. Browsing
	// REPLACES the input box, so without stashing it the half-written prompt is gone the
	// moment ↑ is pressed and coming back down leaves an empty box — the input silently
	// destroyed by a key that reads as navigation. readline-family prompts all restore it.
	histDraft string
	palSel    int // selected index in the command palette

	vp viewport.Model
	ta textarea.Model
	sp spinner.Model

	// Composer suggestion (see suggest.go): the model's guess at how the instruction being typed
	// ends, shown as a dim hint under the composer and taken with Tab. suggestBase is the text the
	// standing suggestion was fetched for (so an arrow key does not refetch); suggestTok drops the
	// answer to a keystroke that has been overtaken.
	suggestion  string
	suggestBase string
	suggestTok  int

	glam      *glamour.TermRenderer
	glamWidth int // transcript width the glam renderer was built for

	width, height int
	ready         bool
	running       bool
	// turnReqID is the reqID of the request whose turn is currently being processed; its
	// user block shows the in-flight spinner. awaitingTurnReqID marks the window between a
	// fresh submit and its prompt.submitted arriving, so only that prompt (not a queued
	// interjection landing mid-turn) claims turnReqID. Cleared when the turn finishes.
	turnReqID string
	// spinTool is set for the one render of a tool call that is running right now, so its glyph
	// can be the spinner instead of the still ⚙. A render flag rather than a field on the block:
	// which call is live is a property of the frame, not of the record, and writing it into the
	// block would leave a spinner frame in the cache for a call that has long since ended.
	spinTool          bool
	awaitingTurnReqID bool
	quitting          bool
	isDark            bool
	imageProto        string // "kitty" | "iterm2" | "" (half-block)

	blocks          []block
	pendingShell    []shellRun // `!`-run commands+output staged to prepend to the next prompt's context
	liveText        string
	liveProgress    string    // latest live progress note from a long-running tool (wait_for); cleared when its result lands
	liveThink       string    // streaming reasoning ("thinking") for the current turn
	showThink       bool      // expand ALL reasoning blocks (default collapsed); toggle ctrl+t
	liveThinkStart  int       // content line where the streaming "thinking" block begins (-1 = not shown); for click-to-toggle
	blockLineStart  []int     // content line where each cached block begins (for click mapping)
	lastThoughtAt   time.Time // last thought-toggle click time (to swallow a double-click's 2nd toggle)
	lastThoughtLine int       // content line of that last thought click
	perm            *permReq
	quest           *questReq // pending ask_user question modal (nil = none)
	searching       bool      // transcript search bar open (keys captured)
	searchQuery     string    // live query; matches highlighted while the bar is open
	searchHits      []int     // content-line indexes containing the query
	searchCur       int       // index into searchHits of the current match
	ctxPct          float64   // context window usage %
	ctxTokens       int       // current context size in tokens (used), for the footer gauge
	ctxWindow       int       // model's context window in tokens (0 = unknown), for the footer gauge
	plannerMode     string    // last planner decision (solo | parallel N) shown in the header

	turnStart      time.Time                           // wall-clock start of the current turn (§8.1 elapsed)
	turnSteps      int                                 // tool calls this turn (the step budget actually spent)
	turnFiles      map[string]bool                     // unique files touched by write/edit/multiedit this turn
	turnCouncil    int                                 // highest council round decided this turn
	turnDur        time.Duration                       // frozen elapsed of the last finished turn
	turnUnverified bool                                // the finished turn was labeled UNVERIFIED by the execution-evidence gate
	turnIn         int                                 // current input/context tokens (↑)
	turnOut        int                                 // cumulative output tokens this turn (↓)
	councilRound   int                                 // current council round (0 = no council active); header chip
	councilMember  string                              // member currently being polled (live); cleared when the turn ends
	councilPhase   string                              // phase of the open round ("plan" audit vs "" review/consensus); drives the footer waiting line
	turnReceipted  bool                                // this turn already printed its one-line receipt (a turn can end twice)
	drawnVerdicts  map[string]event.CouncilVerdictData // the open round's members already printed, by name
	reviewFoldNext bool                                // a review round voted continue → fold the pre-review report once its revision actually lands

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
	resumeList []session.SessionMeta // sessions shown by the last /resume, after the filter
	// resumeAll is every session in the workspace; resumeList is what survives resumeQuery. Kept
	// apart because a filter has to be able to widen again, and re-listing the store on every
	// keystroke would read every log in the directory per character typed.
	resumeAll   []session.SessionMeta
	resumeQuery string
	// resumeWhy is the best matching turn per session, so a row can say WHY it matched. A list of
	// titles answers "which of these is it" only when the words are in the titles, and the reason
	// somebody is searching is usually that they are not.
	resumeWhy map[session.SessionID]string
	resumeSel int  // selected row in the interactive resume picker
	resuming  bool // the resume picker modal is open

	routeList   []routeRow // rows shown by the models & routing editor
	subagenting bool       // the /subagents list is open
	subSel      int        // selected row in it
	subEditing  bool       // typing a model override for the selected subagent
	subBuf      string     // that model, while it is being typed
	// subSugSel is the highlighted row of the model suggest box, -1 meaning "whatever is typed".
	// The list is the one /route offers, so the two screens cannot disagree about what exists.
	subSugSel    int
	subagentList []subagentRow

	// The /cron screen. Its own fields rather than reusing the subagent ones: the two are open at
	// different times, but sharing a buffer between two editors is how a half-typed value from one
	// screen turns up in the other.
	cronning      bool // the /cron list is open
	cronSel       int
	cronList      []app.ScheduledJobInfo
	cronEditing   bool // the two-field editor is over the list
	cronFresh     bool // that editor is making a new job, so the name is typeable
	cronField     cronEditField
	cronName      string
	cronSchedBuf  string
	cronPromptBuf string
	cronConfirm   bool         // a delete is waiting for y
	cronNote      string       // what the engine said about the last change
	routeSel      int          // selected row
	routing       bool         // the editor modal is open
	routeEditing  bool         // typing a new value for the selected (session/agent) row
	routeBuf      string       // the value being typed
	routePickIdx  int          // profile index while cycling with ←/→ (-1 = free text)
	profileForm   *profileForm // non-nil while adding/editing an LLM profile

	// Session-model suggest box (the /route session row): a merged, de-duplicated
	// list of configured profile models + the gateway's live catalog, so a user
	// picks a real model instead of typing an ID that silently fails later.
	modelCatalog  []string // gateway /models result (fetched async, may be empty)
	catalogLoaded bool     // catalog prefetch finished/attempted (gates re-fetch + loading hint)
	modelSugSel   int      // selected suggestion index while editing (-1 = free text, use routeBuf)

	// Multi-agent live view (B): one pane per spawned subagent child session.
	panes     []*agentPane // active/finished subagent panes, in spawn order
	focusPane int          // index into panes of the focused pane (-1 = main transcript)
	zoom      bool         // focused pane expanded full-screen
	// zoomPane pins a FINISHED subagent's pane (one from doneRoster, no longer in
	// panes) for the zoom view, so clicking a completed entry in the status panel
	// still opens its detail. nil → the zoom view follows focusPane (a live pane).
	zoomPane *agentPane
	// doneRoster holds panes that finished and faded out of the inline transcript:
	// the right status panel keeps listing them (a persistent record of what ran) even
	// though their inline box is gone. Cleared on a new turn (closePanes).
	doneRoster []*agentPane

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
func New(ctx context.Context, a Engine, cmds CommandSource, sid session.SessionID, model, workdir string, isDark bool, imageProto string) Model {
	applyTheme(isDark)

	ta := textarea.New()
	ta.Placeholder = "Ask magi to do something...  (enter to send | alt+enter/ctrl+j newline)"
	ta.Prompt = "❯ "
	// One ❯ on the first display row only: the textarea repeats the prompt on every
	// soft-wrapped/extra row, which reads as separate entries on a multi-row prompt.
	// Continuation rows get two spaces so the text column stays aligned (the cursor
	// math measures promptView(0), so the offset is unchanged).
	ta.SetPromptFunc(2, func(pi textarea.PromptInfo) string {
		if pi.LineNumber == 0 {
			return "❯ "
		}
		return "  "
	})
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	// Size the box from the true visual line count (soft wraps included), not just
	// logical newlines, so a long wrapped prompt isn't clipped to its last row.
	// Capped at maxInputRows; refresh() no longer sets the height manually.
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = maxInputRows
	// Report a real OS cursor (not a drawn one) so IME pre-edit (e.g. Korean)
	// composes at the input position instead of the screen corner.
	ta.SetVirtualCursor(false)
	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := Model{
		ctx: ctx, app: a, cmds: cmds, sid: sid, model: model, workdir: workdir,
		isDark: isDark, imageProto: imageProto, ta: ta, sp: sp,
		focusPane: -1, roleColor: map[string]int{}, panelW: defaultPanelWidth,
	}
	// Seed the user label from any value a startup plugin already set (its transient
	// broadcast fires before this model subscribes, so read the stored value here).
	m.userLbl = a.UserLabel(sid)
	m.applyWidgetStyles() // theme-dependent textarea/spinner styling (re-applied on theme flip)
	return m
}

// applyWidgetStyles (re)applies the active theme's colors to the textarea and
// spinner. Called at construction and again on a live theme flip, so these widgets
// follow a light↔dark switch (their colors are otherwise captured by value once).
func (m *Model) applyWidgetStyles() {
	st := textarea.DefaultStyles(m.isDark)
	st.Focused.Prompt = lipgloss.NewStyle().Foreground(colPrimary)
	st.Blurred.Prompt = lipgloss.NewStyle().Foreground(colOutline)
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(colMuted)
	st.Blurred.Placeholder = lipgloss.NewStyle().Foreground(colMuted)
	// DefaultStyles paints the focused cursor line a near-white bg (ANSI 255 in light
	// themes), which clashes with the input box's own (unset) background — only the
	// text line filled, the box not. Drop it so the input box is uniform (flat).
	st.Focused.CursorLine = st.Focused.CursorLine.UnsetBackground()
	m.ta.SetStyles(st)
	m.sp.Style = lipgloss.NewStyle().Foreground(colPrimary)
}

// cmdInfo describes a slash command for the completion palette.
type cmdInfo struct{ name, desc string }

// slashCommands is the single source of truth for available commands.
var slashCommands = []cmdInfo{
	{"/help", "show help"},
	{"/route", "models & routing editor (alias: /model)"},
	{"/tools", "list available tools"},
	{"/subagents", "turn plugin subagents on and off"},
	{"/cron", "see and change what this workspace runs on a schedule"},
	{"/sessions", "list sessions in this directory"},
	{"/resume", "resume a session (/resume to list, /resume N to switch)"},
	{"/rewind", "roll back the last user turn(s) (/rewind [n])"},
	{"/image", "render an image file inline (/image <path>)"},
	{"/diff", "show the working-tree git diff"},
	{"/loop", "show the loop map (turns · steps · council)"},
	{"/context", "context window usage; /context <tokens> sets the model's window (e.g. 128k, unlimited)"},
	{"/cost", "tokens and cost: this session incl. its subagents, and the whole process (council + side calls included)"},
	{"/fork", "branch this session to explore an alternative (origin kept)"},
	{"/replay", "re-run the last turn on a branch (compare with /loopdiff)"},
	{"/loopdiff", "compare this branch with its fork origin"},
	{"/init", "analyze the project and write AGENTS.md"},
	{"/ultra", "ultra work mode: work it thoroughly and verify (/ultra <task>)"},
	{"/permission", "cycle permission mode"},
	{"/compact", "summarize & shrink the context"},
	{"/clear", "clear the transcript"},
	{"/quit", "exit magi (alias: /exit)"},
}

// debugFade shows the finished-pane fade state machine in the footer when
// MAGI_DEBUG_FADE is set, so a stuck fade can be diagnosed from the screen.
var debugFade = os.Getenv("MAGI_DEBUG_FADE") != ""

// fadeLogFile is a written trace of pane lifecycle + fade events (MAGI_DEBUG_FADE)
// at /tmp/magi-fade.log, so a stuck fade can be diagnosed without copy-pasting.
var fadeLogFile = func() *os.File {
	if !debugFade {
		return nil
	}
	f, _ := os.Create("/tmp/magi-fade.log")
	return f
}()

// fadeDbg appends a timestamped line to the fade trace file (no-op unless enabled).
func fadeDbg(format string, a ...any) {
	if fadeLogFile == nil {
		return
	}
	fmt.Fprintf(fadeLogFile, time.Now().Format("15:04:05.000")+" "+format+"\n", a...)
	_ = fadeLogFile.Sync()
}

// shortSID is the tail of a session id, for compact log lines.
func shortSID(s session.SessionID) string {
	if t := string(s); len(t) > 6 {
		return t[len(t)-6:]
	}
	return string(s)
}

// fadeDebug renders the fade machinery's live state for the footer (MAGI_DEBUG_FADE).
func (m *Model) fadeDebug() string {
	done := 0
	maxFade := 0.0
	for _, p := range m.panes {
		if p.done {
			done++
		}
		if p.fade > maxFade {
			maxFade = p.fade
		}
	}
	return fmt.Sprintf("  [%s · panes=%d done=%d maxfade=%.2f run=%v]",
		version.Commit, len(m.panes), done, maxFade, m.running)
}

// renderTickMsg drives throttled, coalesced repaints during streaming.
type renderTickMsg struct{}

// shellResultMsg carries the outcome of a `!`-inline-shell command back to the
// Update loop, off the goroutine that ran it (so the TUI never blocks on it).
type shellResultMsg struct {
	cmd  string
	out  string
	exit int
}

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

// bgPollMsg drives periodic re-querying of the terminal background color. Terminals
// rarely PUSH a theme-change notification, but they reliably answer the OSC 11 query,
// so polling lets magi follow a live OS light↔dark switch within a couple seconds.
type bgPollMsg struct{}

func bgPoll() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return bgPollMsg{} })
}

func (m Model) Init() tea.Cmd {
	// Clear the screen on startup so magi opens on a clean canvas rather than
	// visually continuing the terminal's prior scrollback.
	// Subscribe via a message so the mutation lands on the running model.
	return tea.Batch(tea.ClearScreen, textarea.Blink, renderTick(), tea.RequestBackgroundColor, bgPoll(), jobPoll(), func() tea.Msg {
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

	case jobPollMsg:
		// The strip follows the background jobs; a detached process writes to a file, so this is a
		// poll rather than a subscription. Re-arms itself unconditionally, so the strip keeps
		// following jobs started later in the session.
		if m.syncJobPanes() {
			m.dirty = true
		}
		return m, jobPoll()

	case bgPollMsg:
		// Re-query the terminal's background color, then reschedule — a BackgroundColorMsg
		// will follow and flip the theme below if it changed.
		return m, tea.Batch(tea.RequestBackgroundColor, bgPoll())

	case tea.BackgroundColorMsg:
		// The terminal reported its background color — at startup and again whenever
		// the OS/terminal theme changes. Re-theme live if dark/light flipped.
		if dark := msg.IsDark(); dark != m.isDark {
			m.isDark = dark
			applyTheme(dark)
			m.applyWidgetStyles()        // textarea/spinner colors follow the flip too
			m.glam, m.glamWidth = nil, 0 // force the markdown renderer to rebuild for the new theme
			m.cache = m.cache[:0]        // re-render cached blocks with the new colors
			m.refresh()
		}
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
		if msg.sid != m.sid {
			return m, nil // an event for a session this view is not showing
		}
		if msg.sub != m.mainSub {
			return m, nil // event from a switched-away primary session
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
		if m.advancePaneFade() { // drives the finished-pane fade-out off the heartbeat
			m.dirty = true
		}
		// The viewport's height is fixed at the last refresh; the chrome around it is measured
		// at every render. Anything that changes the chrome without marking the model dirty
		// therefore leaves a frame taller than the terminal, and it STAYS that way until some
		// unrelated event happens to mark it — the tick alone will not fix it.
		//
		// The pane block makes that gap wide. It is sized from what is left after the base
		// chrome and a minimum viewport, so it is not monotonic in the base: the input box
		// giving back one row lets the block grow from a one-row strip to a four-row box, and
		// the frame gains two rows nobody asked for. Observed at 15 rows in a 13-row terminal.
		//
		// Rather than chase every producer, the drift is measured here and answered: if the
		// frame would not fit, lay it out again. One pass converges, because refresh clamps the
		// viewport to what the chrome leaves.
		if m.ready && m.height > 0 && m.chromeHeight()+m.vp.Height() > m.height {
			m.dirty = true
		}
		if m.dirty {
			m.refresh()
			m.dirty = false
		}
		return m, renderTick()

	case shellResultMsg:
		return m, m.applyShellResult(msg)

	case modelCatalogMsg:
		m.modelCatalog = msg.models
		m.catalogLoaded = true
		if m.routing {
			m.refresh() // repaint the open editor with the freshly-loaded suggestions
		}
		return m, nil

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

	case suggestTickMsg:
		// The pause elapsed with no newer keystroke: fetch. A stale token (they kept typing) is
		// dropped, and a "/" that arrived since is a command, not an instruction.
		if msg.tok != m.suggestTok {
			return m, nil
		}
		if v := strings.TrimSpace(m.ta.Value()); v == "" || strings.HasPrefix(v, "/") {
			return m, nil
		}
		return m, m.fetchSuggest(m.ta.Value(), msg.tok)

	case suggestReadyMsg:
		// Kept only if it is still the current request and the composer still holds exactly the text
		// it was about — otherwise it is ghost text for something no longer on the screen.
		if msg.tok == m.suggestTok && msg.text != "" && m.ta.Value() == msg.prefix {
			m.suggestion = msg.text
			m.refresh()
		}
		return m, nil

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
		if sc := m.onComposerChanged(); sc != nil { // schedule a composer suggestion on a pause
			cmds = append(cmds, sc)
		}
		m.refresh() // re-flow for palette/queue height changes
	} else {
		m.ta, cmd = m.ta.Update(msg)
		cmds = append(cmds, cmd)
		m.vp, cmd = m.vp.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
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
		// The offer belonged to the conversation being left, and following it is what this is.
		m.movedTo = ""
	}
	m.sid = sid
	m.blocks = rebuildBlocks(msgs)
	m.history = userPrompts(msgs)               // seed ↑/↓ recall + tab completion from prior turns
	m.histIdx, m.histDraft = len(m.history), "" // a switched session carries no draft
	m.cache = m.cache[:0]
	m.liveText, m.liveThink, m.liveProgress, m.running, m.activeAgents = "", "", "", false, nil
	// Two more things belong to the session being LEFT. The council detail is a full-screen panel
	// carrying another session's verdict and its evidence, and it stayed up over the resumed
	// transcript with nothing marking it as foreign — the header said the new session, the panel
	// showed the old one's member and rationale. Reachable through the ordinary key path: the
	// input keeps taking keys while the panel is up, so `/resume` typed there switches underneath
	// it. The highlighted selection is the same thing one layer down — coordinates from a
	// transcript that is no longer on screen, painted onto the one that is.
	m.councilDetail, m.councilDetailEvidence = nil, ""
	m.selecting, m.selActive = false, false
	// …and so does the identity of the turn that was running. running is cleared just above, but
	// turnReqID kept pointing at a request in the transcript that was just replaced. The revive
	// path (running comes back from engine activity) only adopts a block's reqID when turnReqID is
	// EMPTY, so a stale one survives it: running is true, the spinner is looking for a block that
	// is not in this session, and the turn animates beside nothing. turnStart is the same story one
	// field over — revive only sets it when zero, so the resumed meter would count from the other
	// session's start. Found by a random session switching mid-turn, which is what /resume does.
	m.turnReqID, m.awaitingTurnReqID = "", false
	m.turnStart = time.Time{}
	// Subscribe from lastSeq so we stream only new events (transcript already shown).
	cmd := m.startSub(sid, lastSeq)
	m.refresh()
	// A turn that was cut off is in the log and nothing used to say so: the conversation came back
	// and the work was silently abandoned. Say it, and say what is at stake — but do NOT restart it.
	// Re-running forty steps of edits without being asked is a side effect the user did not choose,
	// and the abandoned turn may be exactly what they killed magi to stop.
	if u, ok := m.app.UnfinishedTurnOf(m.ctx, sid); ok {
		return tea.Batch(cmd, m.snack(fmt.Sprintf(
			"resumed %s — the last turn stopped after %d tool calls without finishing: %s",
			sid, u.Steps, oneLine(u.Text, 60))))
	}
	return tea.Batch(cmd, m.snack("resumed "+string(sid)))
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
// permHint says what a mode DOES, which is narrower than its name in every case.
//
// No mode reaches every tool. Only the danger set is gated — write, edit, multiedit, bash,
// wait_for, webfetch, websearch, port_owner — plus whatever a guardrail rule stops; reading,
// searching and planning run in all four. "confirm each action" and "block all tool actions" were
// therefore both untrue, and the second is the dangerous direction to be wrong in: somebody
// setting deny and walking away was told the agent could do nothing.
func permHint(mode string) string {
	switch mode {
	case "auto":
		return " — edits go through, commands and network ask"
	case "allow":
		return " — ask only when a guardrail insists"
	case "deny":
		return " — refuse writing, running and reaching out"
	default:
		return " — ask before it writes, runs or reaches out"
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
	// Stepping off the bottom for the first time: keep the draft so coming back restores it.
	// A multi-line draft never gets here (the guard above), which is what makes the one-line
	// case an omission rather than a choice — you do not protect one and discard the other.
	if m.histIdx >= len(m.history) {
		m.histDraft = m.ta.Value()
	}
	ni := m.histIdx + dir
	if ni < 0 {
		ni = 0
	}
	if ni >= len(m.history) {
		m.histIdx = len(m.history)
		m.ta.SetValue(m.histDraft)
		m.syncTaViewport()
		return true
	}
	m.histIdx = ni
	m.ta.SetValue(m.history[ni])
	m.syncTaViewport() // a long recalled prompt scrolls the box; CursorEnd alone clamps on stale content
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
	"  enter send · esc interrupt · ↑/↓ history · pgup/pgdn scroll · ctrl+l clear · shift+tab perm mode · ctrl+q quit"

const initPrompt = "Analyze this project and create an AGENTS.md file at the repo root using the write tool. " +
	"Include: a one-paragraph overview, the directory structure, key conventions, and the build/test/run commands. " +
	"Inspect the project first (list, read, glob, grep) before writing. Keep it concise and accurate."

// ultraPreamble asks for a thorough, self-driven pass. It used to describe an orchestrator handing
// work to named specialists — explore, librarian, coder, tester, reviewer — none of which exist:
// the roster and the task tool are gone, so every step of that recipe was an instruction the agent
// could not follow. What is left is the part that never depended on them.
const ultraPreamble = "Work autonomously and thoroughly on this:\n" +
	"1. Lay out the steps with the todowrite tool and keep their statuses current.\n" +
	"2. Read before you write: find the exact files and lines, don't guess.\n" +
	"3. Make the smallest change that does the job.\n" +
	"4. VERIFY by running it — the build, the tests, the program itself — and read the real output.\n" +
	"5. If it fails, fix and re-run until it passes; never leave the code broken.\n" +
	"6. Ask the council (the `council` tool) when you want another reading, and declare completion " +
	"with `council{complete: true}` when you believe it is done."
