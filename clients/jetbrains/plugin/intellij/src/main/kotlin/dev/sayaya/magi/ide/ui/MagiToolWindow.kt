package dev.sayaya.magi.ide.ui

import com.intellij.openapi.Disposable
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Disposer
import com.intellij.openapi.wm.ToolWindow
import com.intellij.openapi.wm.ToolWindowFactory
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.components.JBTextArea
import com.intellij.ui.content.ContentFactory
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.model.Ask
import dev.sayaya.magi.ide.model.Subject
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.Waiting
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.transport.HandServer
import dev.sayaya.magi.ide.transport.Published
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.Markup
import dev.sayaya.magi.ide.model.FileRef
import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.model.SessionRow
import dev.sayaya.magi.ide.usecase.Assist
import dev.sayaya.magi.ide.usecase.End
import dev.sayaya.magi.ide.usecase.Authorship
import dev.sayaya.magi.ide.usecase.Hand
import dev.sayaya.magi.ide.usecase.Level
import dev.sayaya.magi.ide.usecase.Problems
import dev.sayaya.magi.ide.usecase.Row
import dev.sayaya.magi.ide.usecase.Rows
import dev.sayaya.magi.ide.usecase.Who
import dev.sayaya.magi.ide.usecase.Transcript
import java.awt.BorderLayout
import java.awt.Color
import java.awt.FlowLayout
import javax.swing.BorderFactory
import javax.swing.JButton
import javax.swing.JTextPane
import javax.swing.SwingUtilities
import javax.swing.text.SimpleAttributeSet
import javax.swing.text.StyleConstants

/**
 * 대화 — 컴패니언에게 말을 걸고, 그가 묻는 것에 답하는 창. **하단 독**에 산다.
 *
 * 콘솔에서는 대화가 가운데다(`docs/UI.md` §2.2). IDE 에서 가운데는 **고치는 것**의 자리라 그대로
 * 옮기면 §5 의 첫 규칙("IDE 와 겹치는 것은 만들지 않는다")을 배치로 어긴다. 그리고 계속 흘러내리는
 * 글을 IntelliJ 가 두는 자리는 아래다 — Run, Terminal, Build 가 전부 거기 있다. 사실 판은 설정 화면 안으로 접혔다([MagiConfigurable] — 사용자 결정 2026-08-29, 상시 수준은 상태 표시줄이 잇는다).
 *
 * 전사는 데몬의 `transcript` 문에서 이벤트로 오고, 셰이퍼([Rows])가 행으로 편다 — 무엇이
 * 행이 되는지는 `docs/TRANSCRIPT.ko.md` 의 표가 정하고, 이 창은 그 행을 붓질만 한다
 * ([renderRow]). 한동안 이 창은 `#seq type (actor)` 만 적었고 사람이 친 글도 답도 화면에
 * 없었다 — 살아 있는 샌드박스에서 실측한 구멍이라, 몸통이 행에 서는 것부터 골든이 붙든다.
 *
 * 소켓 입출력은 전부 풀 스레드에서 돈다. EDT 에서 소켓을 잡으면 데몬이 느린 동안 IDE 가 선다.
 */
class MagiToolWindow : ToolWindowFactory {

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val view = View(project)
        // 수준이 서는 자리. 라벨이었을 때는 거의 안 변하는 큰 한 줄이 전사 위 판 하나를 먹었다
        // (사용자 실측: "왜 이렇게 커? 변하는 데이터도 없네") — 창이 무엇인지 말하는 자리는
        // 제목표시줄이고, 항상 보여야 하는 쪽은 상태 표시줄이 이미 한다(docs/UI.ko.md §3.1).
        view.title = { t -> SwingUtilities.invokeLater { toolWindow.setTitle(t) } }
        // 행동의 동사들은 기어 메뉴로(설정 화면은 남는 상태의 자리다 — docs/UI.ko.md §5). 결과는
        // 전사로 보고된다: 사건은 라벨이 아니라 전사라는 그 규칙이 여기도 그대로다.
        // 세우기는 제목줄로 — 도는 턴을 세우는 손은 늘 보이되 앞자리를 안 먹는다(TUI 의 esc 와
        // 같은 급). 보내기 옆에 쌍둥이로 서 있던 동안 매 턴의 동사처럼 읽혔다(사용자 실측).
        toolWindow.setTitleActions(listOf(object : com.intellij.openapi.actionSystem.AnAction(
            "세우기", "도는 턴을 세운다", com.intellij.icons.AllIcons.Actions.Suspend) {
            override fun actionPerformed(e: com.intellij.openapi.actionSystem.AnActionEvent) {
                MagiWindows.of(project)?.interruptFromTitle()
            }
        }))
        toolWindow.setAdditionalGearActions(com.intellij.openapi.actionSystem.DefaultActionGroup(
            object : com.intellij.openapi.actionSystem.AnAction("대화 탭 열기…") {
                override fun actionPerformed(e: com.intellij.openapi.actionSystem.AnActionEvent) {
                    // 목록은 sessions 문(최근 활동 순 — 차례는 데몬 것), 고르면 고정 탭이 선다.
                    // 탭 전환은 보기다 — resume 을 부르지 않는다(§4.2b).
                    fun balloon(t: String) = com.intellij.notification.NotificationGroupManager
                        .getInstance().getNotificationGroup("magi")
                        .createNotification(t, com.intellij.notification.NotificationType.WARNING)
                        .notify(project)
                    Workspace(project).onDaemon({ balloon("대화 목록을 못 받았다 — $it") }) { comp ->
                        // 침묵 금지(§0.5-7): 문이 없거나 목록이 비면 그 사실이 풍선으로 선다 —
                        // 눌렀는데 아무 일도 안 나는 메뉴는 없는 메뉴보다 나쁘다.
                        val sr = comp.sessions()
                        val rows = if (sr.ok) sr.sessions.orEmpty() else null
                        if (rows == null) {
                            balloon("이 데몬엔 sessions 문이 없다" +
                                (sr.error?.let { " — " + it.lineSequence().first().take(80) } ?: ""))
                            return@onDaemon
                        }
                        if (rows.isEmpty()) { balloon("열 대화가 없다"); return@onDaemon }
                        SwingUtilities.invokeLater {
                            // 라벨-역찾기(indexOf)는 같은 라벨 둘에서 오결합한다 — 행을 든 채 고른다.
                            class Pick(val row: SessionRow) {
                                override fun toString() =
                                    (row.title?.take(40)?.ifBlank { null } ?: "(제목 없음)") + "  ·" + row.id.takeLast(6)
                            }
                            com.intellij.openapi.ui.popup.JBPopupFactory.getInstance()
                                .createPopupChooserBuilder(rows.map { Pick(it) })
                                .setTitle("어느 대화를 탭으로?")
                                .setItemChosenCallback { picked ->
                                    val sid = picked.row.id
                                    val tab = View(project, pinned = sid)
                                    val content = ContentFactory.getInstance()
                                        .createContent(tab.root, "·" + sid.takeLast(6), false)
                                    content.isCloseable = true
                                    // 탭이 닫히면 스트림도 닫힌다 — 고아 스트림 금지.
                                    com.intellij.openapi.util.Disposer.register(content, tab)
                                    toolWindow.contentManager.addContent(content)
                                    toolWindow.contentManager.setSelectedContent(content)
                                    tab.refresh()
                                }
                                .createPopup()
                                .showInFocusCenter()
                        }
                    }
                }
            },
            view.verb("대화 요약해 접기 (compact)") { it.compact() },
            view.verb("마지막 턴 되감기 (rewind)") { it.rewind(1) },
        ))
        MagiWindows.put(project, view)
        // 창의 수명에 건다. 이걸 안 걸면 창이 닫혀도 스트림·손·등록이 그대로 남는다.
        Disposer.register(toolWindow.disposable, view)
        // 두 판은 좌우가 아니라 **탭**이다. 분할이던 동안 문제 판이 거의 빈 채로 폭의 35%를
        // 먹었고(사용자 실측), 한 창에 이름 다른 글 두 벌을 두는 IDE 의 어휘가 탭이다 — Run 창이
        // 프로세스마다 탭이지 분할이 아니다(§0-5).
        val make = ContentFactory.getInstance()
        toolWindow.contentManager.addContent(make.createContent(view.root, "대화", false))
        toolWindow.contentManager.addContent(make.createContent(view.problemsView, "문제", false))
        view.refresh()
    }

    /**
     * [pinned] 가 있으면 이 판은 **그 대화에 고정**된다(세션 탭 — docs/UI.ko.md §4.2b): 공표를
     * 안 따르고, session.moved 에도 안 움직이며, 입력은 그 대화로 간다(계약: submit/steer 는
     * 이름 댄 세션에 턴을 연다). null 이면 공표를 따르는 주 판이다.
     */
    internal class View(private val project: Project, private val pinned: String? = null) : Disposable {
        private val workspace = Workspace(project)
        val root = JBPanel<JBPanel<*>>(BorderLayout())
        /** 수준을 제목표시줄에 쓰는 손. 창을 만든 쪽이 채운다 — 여기서는 IDE 를 모른다. */
        var title: (String) -> Unit = {}
        private val prompt = JBLabel(" ").apply { border = Look.quiet }
        private val buttons = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.LEFT, 8, 4))
            .apply { border = JBUI.Borders.empty(0, 8, 6, 8) }
        private val input = JBTextArea(1, 40).apply { border = JBUI.Borders.empty(8, 10) }
        private val hint = JBLabel(" ").apply {
            foreground = Look.faint
            border = JBUI.Borders.empty(2, 12, 6, 12)
        }
        /** 마지막으로 받은 제안. 탭으로 받아들인다. */
        private var suggestion: String? = null
        private val debounce = javax.swing.Timer(400) { askSuggestion() }.apply { isRepeats = false }

        /**
         * 전사. **연결을 단독으로 소유한다** — 스트림은 락스텝이 아니라 연결을 통째로 넘겨받으므로
         * 다른 교환과 겸할 수 없다(설계 문서 §3 「스트리밍」).
         */
        private val column = Look.column()
        private val scroll = JBScrollPane(column).apply {
            border = JBUI.Borders.empty()
            horizontalScrollBarPolicy = javax.swing.ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER
        }

        /** 전사 셰이퍼. 이벤트는 워커 스레드에서 먹고, 그리는 것은 EDT 가 스냅샷으로 한다. */
        private val shaper = Rows()

        /**
         * 전사에 한 번이라도 붙었나 — 계획 판의 「모른다/없다」 갈림이 여기 걸린다.
         *
         * **`init` 보다 위에 선다**([dirty] 와 같은 함정, 리뷰가 실측): `follow` 는 워커 기동
         * 전에 `began` 을 동기로 부르므로 init 중에 true 가 쓰이는데, 선언이 init 뒤면
         * 초기화자가 그 값을 도로 false 로 덮는다 — 계획 판이 보통 경로에서 영영 "모른다"였다.
         */
        @Volatile private var everBegan = false

        /**
         * 지금 따르는 대화. `session.moved` 의 낡음 판정이 여기 걸린다 — 그 사실은 떠난 세션
         * 로그에 영속이라 재생마다 다시 오고, 공표된 현재와 이 값이 같으면 그것은 역사다.
         */
        @Volatile private var followedSid: String? = null

        /** [follow] 와 moved-재접속이 같은 자물쇠를 잡는다 — 진 쪽 스트림이 안 닫힌 채 남으면 안 된다. */
        private val followLock = Any()

        /**
         * 연결의 점. "— 전사에 붙었다" 같은 문장 행이 대화 사이에 끼는 대신(사용자 실측: 읽기를
         * 끊는다) 색 하나로 선다 — 웹이 그렇게 한다. 사유는 툴팁에.
         */
        // 초기값도 두-지표 규칙 안에 있다(리뷰: ●-muted 는 붙음-success 와 색만 달랐다) — 안
        // 붙은 상태의 글리프는 ◌ 다.
        private val link = JBLabel("◌").apply { foreground = Look.muted; toolTipText = "아직 안 붙었다" }

        // 상태는 **두 지표**로 말한다(M3 상호작용 규칙 — 웹 감사가 "연결 점 세 상태가 색만"으로
        // 정확히 이 자리를 잡았었다): 색 + 글리프. ● 붙음 · ↻ 다시 붙는 중 · ✕ 끊김 · ◌ 끊었다.
        // 그리고 스트림의 수준과 손(hand)의 수준은 **딴 사실**이라 딴 필드에 산다 — 한 칸에
        // 실으면 다음 스트림 이벤트가 손 정보를 지운다(결함 모양 「한 변수가 두 사실」).
        @Volatile private var linkColour: Color = Look.muted
        @Volatile private var linkGlyph: String = "◌"
        @Volatile private var streamWhy: String = "아직 안 붙었다"
        @Volatile private var handWhy: String? = null
        private fun mood(colour: Color, glyph: String, why: String) {
            linkColour = colour; linkGlyph = glyph; streamWhy = why
            paintLink()
        }
        private fun handSaid(t: String?) { handWhy = t; paintLink() }
        private fun paintLink() = SwingUtilities.invokeLater {
            link.text = linkGlyph
            link.foreground = linkColour
            link.toolTipText = streamWhy + (handWhy?.let { " · $it" } ?: "")
        }

        /**
         * 동사의 **실패만** 적는 한 줄. 성공은 안 적는다 — 보낸 말이 행으로 서는 것 자체가
         * 증거다(같은 사용자 결정). 다음 성공이 지운다.
         */
        private val notice = JBLabel(" ").apply {
            foreground = Look.error
            border = JBUI.Borders.empty(2, 12, 0, 12)
        }

        /**
         * 마지막으로 받은 사실의 seq — 재접속의 커서다. 컴팩션이 seq 를 보존하므로 커서는 믿어도
         * 된다(docs/CLIENTS 명문화). 대화가 바뀌면 0 으로 — 옛 커서를 새 대화로 들고 가면 앞을
         * 못 본다. 거절은 데몬이 이벤트보다 먼저 말한다([Transcript.Sink.note]).
         */
        @Volatile private var lastSeq = 0L

        /** 펼쳐 둔 행들. 행 판은 리드로우마다 다시 서므로 상태는 밖에 산다. */
        private val opened = java.util.Collections.synchronizedSet(mutableSetOf<String>())
        private fun foldKey(r: Row) = "${r.msgId}:${r.who}:${r.callId}:${r.text.hashCode()}"

        /**
         * 그릴 일이 밀려 있는가. 워커가 프레임 백 개를 밀어도 EDT 에는 스냅샷 한 번이다.
         *
         * **`init` 보다 위에 선다.** 코틀린은 선언 순서대로 초기화하고, `init` 의 못-붙음 보고가
         * [append]→[redrawLog] 로 이 값을 두드린다 — 아래 있던 동안 그 첫 보고가 NPE 로 죽어
         * **툴윈도 내용이 통째로 안 만들어졌다**(샌드박스 실측: 입력칸까지 같이 사라진다).
         */
        private val dirty = java.util.concurrent.atomic.AtomicBoolean(false)

        /**
         * 문제 판. IntelliJ 자신의 Problems 뷰에 넣지 않았다 — 거기는 IDE 가 자기 인스펙션으로
         * 채우는 자리이고, 컴패니언이 자기 실행에서 본 것을 섞으면 **누가 언제 말한 것인지**가
         * 사라진다. §5-4 가 요구하는 것이 정확히 그 두 가지라 따로 세운다.
         */
        private val problems = Look.pane()
        val problemsView: JBScrollPane by lazy { JBScrollPane(problems).apply { border = JBUI.Borders.empty() } }

        /**
         * 어느 턴이 무엇을 건드렸나. 전사에서 같이 쌓는다 — 두 번째 스트림을 열지 않는다.
         *
         * 이 창이 사는 동안만 안다. 데몬이 재생해 준 만큼이 전부이고, 그전 것은 모른다 —
         * 모르는 것을 아는 척하지 않는 것이 §5-5 의 규칙이다.
         */
        val authors = Authorship()
        private var following: java.io.Closeable? = null

        /** 창이 닫히는 중인가. 서면 재접속이 멈춘다 — 닫은 창이 스스로 되살아나면 안 된다. */
        private val closing = java.util.concurrent.atomic.AtomicBoolean(false)

        /**
         * 손. 창이 서면 같이 서고, 창이 살아 있는 동안만 산다.
         *
         * **창에 매단 이유**가 있다. 손은 IDE 의 편집기를 움직이는데, 편집기가 없는 IDE(웰컴 화면)
         * 에서 서 있으면 데몬은 붙었다고 믿고 에이전트는 매번 거절을 받는다. 창이 있다는 것이
         * 곧 프로젝트가 열려 있다는 것이라 그 자리에 맨다.
         */
        private var hand: HandServer? = null

        /**
         * 전사를 화면으로 옮기는 자리. **연결에 안 매인다** — 다시 붙을 때마다 새로 만들면
         * 같은 규칙이 매번 다시 쓰이고, 그중 한 벌만 고치는 날이 온다.
         */
        private val sink = object : Transcript.Sink {
                /**
                 * 붙었다. [ended] 가 말을 하므로 이쪽도 한다 — 다만 문장 행이 아니라 **점**이다
                 * (혼잣말이 대화 사이에 끼는 것이 읽기를 끊는다는 사용자 실측).
                 *
                 * 비움은 여기 없다. 커서([lastSeq])가 서면서 재생이 증분이 됐고, 「전량이
                 * 온다(=비워야 한다)」를 아는 것은 since==null 을 판정하는 [follow] 뿐이다 —
                 * 여기서 비우면 증분 재접속이 이미 그린 대화를 지운다(`SourceTextTest` 가
                 * began 의 비움을 금지로 붙든다).
                 */
                override fun began() {
                    // 비움은 여기가 아니라 [follow] 다 — 커서가 있으면 재생이 증분이라 비울 것이
                    // 없고, 그 판정(since==null)을 아는 것은 follow 뿐이다. 문장 행 대신 점.
                    everBegan = true
                    mood(Look.success, "●", "전사에 붙어 있다")
                }

                override fun frame(e: LogEvent) {
                    // 죽어 가는 스트림의 마지막 프레임 가드(리뷰): close 의 stopped 는 다음
                    // 콜백에서야 검사되므로, 갈아탄 직후 옛 세션 프레임 하나가 착지할 수 있다 —
                    // 갓 비운 판에 옛 행이 서고 lastSeq 가 옛 seq 로 오염되면 다음 재접속이
                    // 그 사이를 조용히 건너뛴다.
                    if (e.session.isNotBlank() && e.session != followedSid) return
                    // 대화가 옮겨 갔다(우측 판의 갈아타기·새 대화, 혹은 다른 화면에서). 이 스트림은
                    // 옛 대화의 것이라 여기 남으면 화면과 컨트롤이 서로 다른 대화를 믿는다 — 끊고
                    // 새 공표를 따라 다시 붙는다. close 가 ByUs 로 접히므로 재접속은 직접 건다.
                    if (e.type == "session.moved") {
                        // 고정 탭은 안 움직인다 — 이 탭의 존재 이유가 「그 대화를 계속 보기」다.
                        if (pinned != null) return
                        // 낡음 가드(리뷰 실측): 이 사실은 재생마다 온다. 공표된 현재가 지금 따르는
                        // 대화와 같으면 움직일 일이 없다 — 없으면 끊고-붙기 무한 루프다(`to` 비교로는
                        // 못 막는다: 낡은 사실의 to 는 영원히 현재와 다르다).
                        ApplicationManager.getApplication().executeOnPooledThread {
                            if (closing.get()) return@executeOnPooledThread // 닫힌 창을 되살리지 않는다
                            val now = socket()?.let { p -> runCatching { Published.of(p)?.session }.getOrNull() }
                            if (now != null && now != followedSid && follow() != Attach.Ok) reattach()
                        }
                        return
                    }
                    // 판을 비우는 것은 여기가 아니라 [began] 이다. 사유는 그쪽에 적었다.
                    // 조각에는 줄을 안 준다. 같은 말이 `part.appended` 사실로 뒤따르고, 재생에는
                    // 그 사실만 실린다 — 안 가리면 붙어 있던 창과 나중에 다시 붙은 창이 같은
                    // 대화를 다르게 그린다(사유는 `Transcript.echoesFact`).
                    if (!Transcript.echoesFact(e) && shaper.feed(e)) redrawLog()
                    refreshDisk() // 컴패니언이 고친 디스크를 IDE 가 다시 보게(사유는 Rows.drainDisk)
                    if (e.seq > lastSeq) lastSeq = e.seq // 사실만 커서가 된다(전이는 seq==0)
                    // 문제는 전사에서 갈라 나온다. 두 번째 스트림을 열지 않는 이유는 §3 의 "창 하나에
                    // 스트림 하나" 그대로다 — 같은 프레임을 두 번 파싱하게 된다.
                    authors.feed(e)
                    Problems.of(e)?.let { note(it) }
                    // 물음이 움직였으면 다시 묻는다. 물음 자체(`*.requested`)는 전이라 로그에 안
                    // 실려서, 이 신호가 없으면 창을 연 뒤에 올라온 물음은 단추가 영영 안 생긴다 —
                    // 로그에 줄 하나 뜨고 끝이었다(사유는 `Transcript.movesPrompt`).
                    //
                    // **여기서 `e` 를 읽지 않는다.** 넷이 다 전이인 것은 아니다 —
                    // `permission.decided` 는 사실이라 저장되고, 다시 붙을 때마다 재생으로 또 온다
                    // (실측도 그쪽에 적었다). 신호로만 쓰고 그릴 값은 데몬에게 새로 물으니 옛
                    // 프레임이 불러도 지금 값이 그려진다. 이 줄이 `e` 를 보기 시작하면 재생이
                    // 지나간 물음을 지금 것으로 그린다.
                    if (Transcript.movesPrompt(e)) refresh()
                    Problems.dissentOf(e)?.let { dissent(it) }
                }
                // 데몬이 이벤트보다 **먼저** 보내는 말이다. 이미 그린 것을 지워야 한다는 뜻이라
                // 눈에 띄게 적는다 — 조용히 흘리면 화면이 거짓말을 한 채로 남는다.
                override fun note(why: String) {
                    // 커서를 못 믿겠다는 통보 — 이벤트보다 먼저 오는 것이 계약이라, 이미 그린 것을
                    // 지우고 전량 재생을 새로 받는 자세로 돌아간다.
                    lastSeq = 0
                    authors.forget()
                    shaper.clear()
                    SwingUtilities.invokeLater { problems.text = "" }
                    // ↻ 는 「다시 붙는 중」의 글리프다 — 여기는 붙어 **있는** 채 커서만 거절된
                    // 자리라, 영구 ↻ 는 거짓이 된다(리뷰). 붙음 글리프에 경고색+사유로.
                    mood(Look.warn, "●", why)
                    redrawLog()
                }
                /**
                 * **누가 끝냈는지로 갈린다.** 사람이 닫았으면 그걸로 끝이고, 데몬이 닫았거나
                 * 끊겼으면 다시 붙는다 — 안 그러면 창은 살아 보이는데 아무것도 안 오고, 물음을
                 * 다시 그리던 신호(`Transcript.movesPrompt`)가 그 스트림을 타고 오므로 **답할
                 * 단추가 같이 죽는다.**
                 */
                override fun ended(end: End) = when (end) {
                    End.ByUs -> mood(Look.muted, "◌", "전사를 끊었다")
                    // 손의 소식도 여기서 거둔다(리뷰): 손은 저 데몬에 붙었던 것이라 스트림이 죽으면
                    // 그 사실도 죽는다 — 안 거두면 새 데몬이 모르는 "손: …"을 툴팁이 영구 주장한다.
                    End.ByDaemon -> { handSaid(null); mood(Look.warn, "↻", "전사가 끝났다(데몬이 닫았다) — 다시 붙는 중"); reattach() }
                    is End.Broken -> { handSaid(null); mood(Look.error, "✕", "전사가 끊겼다: ${end.why} — 다시 붙는 중"); reattach() }
                }
            }

        /** 대기 프롬프트가 서는 윗판. **물음이 없으면 통째로 숨는다** — 빈 라벨과 빈 단추 줄이
         *  여백으로 남아 탭과 전사 사이에 죽은 띠를 만들었다(사용자 실측). */
        private lateinit var head: JBPanel<JBPanel<*>>

        /**
         * 보낼 첨부들 — 본문이 아니라 **참조**다(경로+줄범위). 발췌는 코어가 렌더·영속하므로
         * (docs/CLIENTS §2) 여기는 이름표 칩만 세운다. [say] 가 싣고 비운다.
         */
        private val refs = java.util.Collections.synchronizedList(mutableListOf<FileRef>())
        private val chips = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.LEFT, 6, 2)).apply {
            isOpaque = false
            isVisible = false
        }

        /**
         * 입력창에 물음의 시작을 앉힌다 — Alt+Enter 인텐션이 부른다. **비어 있을 때만**: 사람이
         * 치던 글 위에 얹으면 그 글이 사라진 것처럼 보인다(사라지는 입력 없음). 커서는 끝으로.
         */
        fun prefill(text: String) = SwingUtilities.invokeLater {
            if (input.text.isBlank()) {
                input.text = text
                input.caretPosition = input.text.length
            }
            input.requestFocusInWindow()
        }

        /** 첨부 하나를 세운다 — 에디터·프로젝트 뷰 액션이 부른다. 같은 참조는 두 번 안 선다. */
        fun attach(ref: FileRef) = SwingUtilities.invokeLater {
            if (refs.contains(ref)) return@invokeLater
            refs.add(ref)
            drawChips()
        }

        private fun drawChips() {
            chips.removeAll()
            val snap = synchronized(refs) { refs.toList() }
            chips.isVisible = snap.isNotEmpty()
            snap.forEach { ref ->
                val label = ref.path.substringAfterLast('/') + (ref.lines?.let { ":$it" } ?: "")
                chips.add(JButton("$label ✕").apply {
                    margin = java.awt.Insets(0, 6, 0, 6)
                    toolTipText = ref.path
                    addActionListener { refs.remove(ref); drawChips() }
                })
            }
            chips.revalidate(); chips.repaint()
        }

        init {
            val top = JBPanel<JBPanel<*>>(BorderLayout())
            top.add(prompt, BorderLayout.CENTER) // diff 판이 빠지며 한 장짜리가 된 래퍼는 접었다(리뷰)
            top.add(buttons, BorderLayout.SOUTH)
            // 윗단과 전사를 실선으로 가른다. **지금 상태**와 **지나간 것**은 다른 종류의 글이라
            // 눈이 한 번은 걸려야 한다(§3.1a 의 도랑이 하는 일을 좁은 판에서 선 하나가 한다).
            head = JBPanel<JBPanel<*>>(BorderLayout()).apply {
                add(top, BorderLayout.CENTER)
                add(Look.rule(), BorderLayout.SOUTH)
                isVisible = false // 물음이 올 때 [drawPrompt] 가 편다
            }

            val send = JButton("보내기").apply { addActionListener { say() } }
            val acts = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.RIGHT, 8, 8)).apply { add(link); add(send) }
            val writing = JBPanel<JBPanel<*>>(BorderLayout()).apply {
                border = JBUI.Borders.empty(8, 12, 0, 8)
                add(JBScrollPane(input), BorderLayout.CENTER)
                add(acts, BorderLayout.EAST)
            }
            val bottom = JBPanel<JBPanel<*>>(BorderLayout())
            bottom.add(JBPanel<JBPanel<*>>(BorderLayout()).apply {
                isOpaque = false
                add(Look.rule(), BorderLayout.NORTH)
                add(chips, BorderLayout.CENTER) // 첨부 칩 — 없으면 숨어 띠가 안 생긴다
            }, BorderLayout.NORTH)
            bottom.add(writing, BorderLayout.CENTER)
            bottom.add(JBPanel<JBPanel<*>>().apply {
                layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.Y_AXIS)
                isOpaque = false
                add(notice)
                add(hint)
            }, BorderLayout.SOUTH)

            // 치는 동안 제안을 묻는다. 매 글자마다가 아니라 멈추면 — 모델 호출이라 값이 있다.
            //
            // **다시 묻는 것과 지금 답을 거두는 것은 한 사건이다.** 전에는 치면 타이머만 다시 돌고
            // 화면의 제안은 그대로 서 있었다. 그 제안은 **한 글자 전의 앞머리로 만든 것**인데 라벨은
            // `Tab` 이라고 적혀 있으니, 시킨 대로 누르면 지금 안 맞는 글자가 붙는다("git" 의 제안
            // " status" 가 "gith" 뒤에 붙어 "gith status"). 낡은 **답을 안 붙이는** 문지기는 아래
            // `askSuggestion` 에 있었지만 그건 늦게 온 답을 막을 뿐, **이미 화면에 선 제안**은 아무도
            // 안 거뒀다. 물음을 다시 여는 자리에서 같이 거둔다.
            input.document.addDocumentListener(object : javax.swing.event.DocumentListener {
                override fun insertUpdate(e: javax.swing.event.DocumentEvent) = retract()
                override fun removeUpdate(e: javax.swing.event.DocumentEvent) = retract()
                override fun changedUpdate(e: javax.swing.event.DocumentEvent) {}
                private fun retract() {
                    dropSuggestion(); debounce.restart()
                    // `@` 멘션(SURVEY 채택 ③): 마지막 낱말이 @이름 꼴이면 디바운스가 제안 대신
                    // 파일 찾기로 간다 — 목록은 데몬의 읽기 전용 glob(감옥은 코어 규칙).
                    // 치는 만큼 자란다(1..5줄). 고정 3줄은 빈 대화에서 벽이었고, 무한정 자라면
                    // 입력이 전사를 밀어낸다.
                    val want = input.text.count { ch -> ch == '\n' }.plus(1).coerceIn(1, 5)
                    if (input.rows != want) { input.rows = want; input.revalidate() }
                }
            })
            // 탭으로 받아들인다. 제안이 없으면 탭은 원래 하던 일을 한다.
            input.registerKeyboardAction({ acceptSuggestion() },
                javax.swing.KeyStroke.getKeyStroke("TAB"), javax.swing.JComponent.WHEN_FOCUSED)
            // Enter 는 보낸다 — 웹도 터미널도 그렇다. 줄바꿈은 Shift+Enter 로 남긴다.
            // registerKeyboardAction 이 아니라 inputMap 인 이유: JTextArea 의 insert-break 가
            // ENTER 에 앉아 있어서, 그 자리를 바꿔 앉혀야 눌림과 줄바꿈이 같이 안 난다.
            input.getInputMap(javax.swing.JComponent.WHEN_FOCUSED)
                .put(javax.swing.KeyStroke.getKeyStroke("ENTER"), "magi.send")
            input.getInputMap(javax.swing.JComponent.WHEN_FOCUSED)
                .put(javax.swing.KeyStroke.getKeyStroke("shift ENTER"), "insert-break")
            input.actionMap.put("magi.send", object : javax.swing.AbstractAction() {
                override fun actionPerformed(e: java.awt.event.ActionEvent) = say()
            })

            root.add(head, BorderLayout.NORTH)
            // 대화만 이 판이다. 문제는 출처가 다른 글이라(왼쪽은 전부, 저쪽은 사람이 손댈 것만)
            // 같은 창의 **다른 탭**으로 간다 — 이름은 탭이 단다.
            root.add(scroll, BorderLayout.CENTER)
            root.add(bottom, BorderLayout.SOUTH)
            // 못 붙으면 **말하고 다시 붙어 본다.** 바로 아래 [offerHand] 는 못 세운 것을
            // 그대로 말하는데 이 줄은 안 했다 — 같은 init 의 두 줄이 실패를 다르게 다뤘다.
            //
            // 안 하면 이렇게 된다. IDE 를 먼저 열고 터미널에서 데몬을 나중에 띄우는 것이
            // **보통의 차례**인데, 그때 `.session` 이 아직 없어 [Published.of] 가 null 을
            // 주고 [follow] 는 false 로 돌아온다. 그 false 를 아무도 안 읽었다: 화면에
            // 한 줄도 안 나가고, [reattach] 는 [ended] 에서만 불리는데 스트림이 선 적이
            // 없으므로 [ended] 도 영영 안 온다. **창은 그대로 죽어 있고 데몬을 띄워도 안
            // 살아난다** — 사람이 툴윈도를 닫았다 여는 수밖에 없다.
            //
            // 빈 전사에 붙은 것과 못 붙은 것이 **똑같이 빈 화면**이었다. `began` 이 그
            // 둘을 갈랐고(위), 이 줄이 못 붙은 쪽에 말과 재시도를 준다.
            //
            // **사유를 댄다.** 예전엔 안 댔고 그게 맞기도 했다 — [follow] 가 셋을 불리언 하나로
            // 접어 돌려줬으니 여기서 「데몬이 없다」고 쓰면 모르는 것을 아는 척하는 것이었다.
            // 접힌 사유를 화면에서 펴지 않는 것은 지금도 규칙이고, 그래서 편 것이 아니라
            // **안 접게 했다**([Attach]). 셋은 사람이 할 일이 서로 다르다: 자리가 없는 것은
            // 프로젝트 문제고, `.session` 이 없는 것은 데몬을 띄우면 되고, 던진 것은 데몬이
            // 있는데 말이 안 통하는 것이다. 가장 흔한 차례(IDE 를 먼저 열고 데몬을 나중에)가
            // 하필 가운데다.
            //
            // `else` 를 안 쓴다. 넷째 갈래가 생기는 날 컴파일러가 울어야 한다 — 안 그러면 새
            // 사유가 옛 문장 뒤에 조용히 숨는다.
            when (val a = follow()) {
                Attach.Ok -> {}
                Attach.NoWorkspace -> lost("이 프로젝트에 붙일 자리가 없다(작업공간 경로를 못 찾았다)")
                Attach.NoSession -> lost("데몬이 아직 없다")
                is Attach.Failed -> lost("데몬에 말을 못 걸었다: ${a.why}")
            }
            // 손은 프로젝트당 하나 — 탭마다 세우면 루프백 포트가 탭 수만큼 열리고, 붙이기는
            // 고정 이름 충돌("jetbrains is already attached")로 탭마다 거절 공지가 선다(리뷰).
            if (pinned == null) offerHand()
        }

        /**
         * 창이 닫히면 내놓은 것을 도로 거둔다.
         *
         * 이 자리가 통째로 비어 있었다. 창은 전사 스트림 하나와 루프백 서버 하나를 세우고
         * **아무것도 안 거뒀다.** 창을 닫아도 스트림 스레드가 계속 돌고, 손 포트가 계속 열려
         * 있고, 무엇보다 **데몬은 손이 붙어 있다고 계속 믿는다** — 컴패니언이 편집을 죽은 창으로
         * 보낸다.
         *
         * 그 계약은 이미 두 곳에 적혀 있었다 — [hand] 필드 주석이 창이 사는 동안만 손이 산다고 하고,
         * `Companion.kt` 는 "창이 닫히거나 IDE 가 나갈 때 — 안 떼면 데몬이 죽은 주소를 계속 들고 있는다"
         * 고 적어 뒀다. 둘 다 적어 두기만 하고 **부르는 자리를 안 만들었다.** 주석이 약속한 것을 코드가
         * 안 지키면 다음 사람은 지켜지는 줄 알고 그 위에 쌓는다.
         *
         * **문을 먼저 닫고 그다음에 뗀다.** 떼는 것은 소켓 왕복이라 늦을 수 있고 그동안에도 편집이
         * 들어오면 안 된다. 못 떼도 포트는 이미 닫혔으니 죽은 창을 고치는 일은 없다 — 데몬이 죽은
         * 주소를 잠깐 들고 있을 뿐이고, 그건 다음 `mcp-attach` 가 정리한다.
         */
        override fun dispose() {
            // 먼저 세운다. 아래에서 스트림을 닫으면 `ended` 가 도는데, 그때 이미 서 있어야 안 되살아난다.
            closing.set(true)
            // 주 판만 거둔다(리뷰 F1·F2): 등록과 손은 주 판의 것이라, 고정 탭의 dispose 가
            // 이것들을 만지면 탭 하나 닫는 행위가 상태 표시줄·계획판·액션 전부와 **주 판의
            // 손**을 부순다 — 데몬에 붙어 있는 mcp 이름은 하나뿐이다.
            if (pinned == null) MagiWindows.remove(project)
            debounce.stop()
            runCatching { following?.close() }
            following = null
            val server = hand ?: return
            hand = null
            runCatching { server.close() }
            // 떼는 것은 best-effort 다. 사유를 화면에 안 싣는다 — 그 화면이 지금 사라지는 중이다.
            if (pinned == null) runCatching { workspace.onDaemon({ }, { it.detachHand() }) }
        }

        /**
         * 손을 세우고 컴패니언에게 준다.
         *
         * 거절을 **그대로 보인다.** 같은 워크스페이스를 IDE 둘로 열면 먼저 붙은 쪽만 손이 되고
         * 둘째는 거절을 받는데, 그때 조용하면 둘째 IDE 의 사람은 자기 편집 도구가 왜 안 쓰이는지
         * 알 길이 없다 — §7 의 다섯째 시나리오가 그것이다. 손이 아닌 것과 고장난 것은 다른 사건이다.
         */
        private fun offerHand() {
            val server = runCatching { HandServer.start(Hand(IdeHand(project))) }.getOrNull()
                ?: return report("손을 못 세웠다 — 루프백 포트를 못 열었다.")
            hand = server
            onDaemon { comp ->
                val r = comp.attachHand(server.url, mapOf("X-Magi-Hand" to server.token))
                // 성공은 침묵 — 손이 붙었는지는 링크 점 툴팁이 안다. 거절은 그대로 보인다(§7 다섯째).
                if (r.ok) {
                    clearNotice()
                    handSaid("손: " + (r.tools?.joinToString(", ") ?: "붙음"))
                } else report("손을 못 붙였다 — " + (r.error ?: "사유 없음"))
            }
        }

        /**
         * 전사에 붙는다. 재생 먼저 그다음 라이브 — 그리고 이제 **커서를 준다**: 마지막 사실의
         * seq([lastSeq])를 since 로 실어 증분만 받는다. 컴팩션이 seq 를 보존하므로 커서는 믿어도
         * 된다(docs/CLIENTS §2). 대화가 바뀌면 0 으로, 거절은 [Transcript.Sink.note] 가 먼저 온다.
         */
        /**
         * 전사에 붙는 시도의 결과. **불리언이 아니다.**
         *
         * 예전엔 셋을 `false` 하나로 접어 돌려줬다. 그러면 화면은 둘 중 하나만 할 수 있다 —
         * 아무 사유도 안 대거나(사람은 왜 안 되는지 모른 채 창을 닫았다 연다), 아니면 「데몬이
         * 없다」고 **지어내거나**. 둘 다 접은 자리가 만든 것이지 화면의 잘못이 아니다.
         *
         * 셋인 이유는 사람이 할 일이 셋이라서다. 붙일 자리가 없는 것은 프로젝트 쪽 문제고,
         * `.session` 이 없는 것은 데몬을 띄우면 되고, 던진 것은 데몬이 있는데 말이 안 통하는
         * 것이다. `End` 의 갈래 셋과 같은 사유다 — **받는 쪽이 할 일이 다르면 갈래다.**
         *
         * `when` 에 `else` 를 안 쓴다. 넷째가 생기면 컴파일러가 울어야 한다 — 안 그러면 새
         * 사유가 옛 문장 뒤에 조용히 숨고, 그건 접어 뒀던 때와 같은 상태다.
         */
        private sealed interface Attach {
            data object Ok : Attach

            /** 붙일 자리가 없다. 작업공간 경로를 못 찾았다 — 데몬 유무와 무관하다. */
            data object NoWorkspace : Attach

            /** 자리는 아는데 `.session` 이 없다. **데몬이 아직 안 떴다** — 가장 흔한 차례다. */
            data object NoSession : Attach

            /** 열다 실패했다. 데몬이 있는데 말이 안 통한다. [Failed.why] 는 던진 것이 한 말 그대로다. */
            data class Failed(val why: String) : Attach
        }

        private fun follow(): Attach = synchronized(followLock) {
            // 자물쇠 하나(리뷰 4): 호출자가 init·reattach 백오프·moved 프레임 셋으로 늘었다.
            // 잠그지 않으면 동시 follow 둘이 각자 스트림을 열고 진 쪽이 안 닫힌 채 같은 sink 에
            // 계속 먹인다 — clear 와 재생이 뒤섞여 행이 두 벌 선다.
            val sock = socket() ?: return Attach.NoWorkspace
            val sid = pinned
                ?: (runCatching { Published.of(sock)?.session }.getOrNull() ?: return Attach.NoSession)
            following?.let { runCatching { it.close() } }
            // 던진 것을 **그대로** 싣는다. `getOrNull` 로 버리고 여기서 문장을 지으면 「데몬이
            // 이렇게 말했다」 자리에 내가 만든 낱말이 앉는다 — 접어 두던 때와 같은 거짓이고,
            // 사유가 하나뿐이라 더 그럴듯해서 더 나쁘다.
            if (sid != followedSid) lastSeq = 0 // 옛 커서를 새 대화로 들고 가면 앞을 못 본다
            val since = lastSeq.takeIf { it > 0 }
            if (since == null) {
                // 전량 재생이 온다 — 두 벌이 안 쌓이게 비우고 받는다. 커서가 서면 증분이라 비울
                // 것이 없고, 이 갈림을 아는 자리는 여기뿐이다(비움이 began 에 있던 사유는 그때의
                // 「커서를 안 보낸다」였다).
                authors.forget()
                shaper.clear()
                SwingUtilities.invokeLater { problems.text = "" }
            }
            // 스트림보다 **먼저** 적는다 — 워커의 첫 프레임이 대입보다 빨리 오면 위의 세션
            // 가드가 제 프레임을 남의 것으로 버린다. 실패해도 남는 값은 다음 시도의 lastSeq
            // 리셋 판정을 안 바꾼다(같은 sid 재시도).
            followedSid = sid
            val started = runCatching {
                Transcript({ DaemonClient.connect(sock) }, sid).follow(sink, since)
            }.getOrElse { return Attach.Failed(it.message ?: it.toString()) }
            following = started
            return Attach.Ok
        }

        /**
         * 못 붙었다고 화면에 한 번 적고 다시 붙어 본다. **되풀이되는 실패는 안 적는다** —
         * 같은 줄을 무한히 쌓으면 사람이 읽던 전사가 밀려난다([reattach] 의 규칙).
         */
        private fun lost(why: String) {
            mood(Look.error, "✕", "전사에 못 붙었다: $why — 다시 붙는 중")
            reattach()
        }

        /**
         * 끊긴 전사에 다시 붙는다. **창이 닫혔으면 안 붙는다.**
         *
         * 스트림만 되살리는 것으로는 모자란다. 끊겨 있는 동안 올라온 물음은 이 창이 **못 본
         * 이벤트로 지나갔으므로**, 붙자마자 지금 무엇을 묻고 있는지 다시 물어야 한다. 창을 열 때
         * 한 번 묻는 것과 같은 사유가 재접속마다 있다 — 닿음이 돌아온 것 자체가 사건이다.
         *
         * 물러서며 기다린다(1초에서 30초까지 배로). 데몬이 오래 없으면 30초마다 유닉스 소켓에
         * 한 번 붙어 보는 값이고, 실패는 화면에 안 적는다 — 같은 줄을 무한히 쌓으면 사람이
         * 읽던 전사가 밀려난다.
         */
        private fun reattach() {
            if (closing.get()) return
            runCatching {
                ApplicationManager.getApplication().executeOnPooledThread {
                    var wait = 1_000L
                    while (!closing.get()) {
                        try { Thread.sleep(wait) } catch (e: InterruptedException) { return@executeOnPooledThread }
                        if (closing.get()) return@executeOnPooledThread
                        if (follow() == Attach.Ok) return@executeOnPooledThread refresh()
                        wait = (wait * 2).coerceAtMost(30_000L)
                    }
                }
            }
        }

        /**
         * 전사를 통째로 다시 그린다. **덧붙이기가 아니라 재생이다** — 셰이퍼의 변이에 재배치가
         * 있어서(재부상, 인라인 답) 붙이기만 하는 판은 순서를 잃는다. 지우는 사건은 없으므로
         * (docs/TRANSCRIPT.ko.md §4) 이 재생은 언제나 같은 것을 더 그릴 뿐이다.
         *
         * 행이 글자가 아니라 **컴포넌트**인 이유: 텍스트 판 하나에 다 밀어 넣던 동안 전사가
         * 여백 없는 로그 덤프로 읽혔다(사용자 실측). 행마다 판을 주면 사이 여백·대기 막대·접힌
         * 인자 펼치기가 전부 스윙의 보통 물건이 된다.
         */
        // 디스크 새로고침의 코얼레스 창(리뷰 F1). `turn.finished` 는 사실이라 재생에도 실린다
        // — 창을 열면 과거 턴 전부가 프레임 단위로 흘러 들어오고, 프레임마다 refresh 를 치면
        // 워크스페이스 통째 훑기가 "bash 있던 턴 수"만큼 돈다. 그래서 드레인은 모으기만 하고,
        // 반 초 뒤 한 번에 민다 — broad 가 섰으면 개별 경로는 생략(통째가 덮는다).
        private val diskPaths = java.util.concurrent.ConcurrentHashMap.newKeySet<String>()
        private val diskBroad = java.util.concurrent.atomic.AtomicBoolean(false)
        private val diskArmed = java.util.concurrent.atomic.AtomicBoolean(false)

        /**
         * 셰이퍼의 디스크 대장([Rows.drainDisk])을 가져다 IDE 의 VFS 를 깨운다 — 데몬(외부
         * 프로세스)이 고친 파일을 에디터가 포커스 전환 없이도 보게. 판정(어느 파일, 언제
         * 통째)은 core 가 했고 여기는 호출만 한다. 실행은 풀드 스레드다(리뷰 F4):
         * refreshAndFindFileByPath 는 캐시에 없는 경로면 **그 스레드에서 동기 IO** 를 하는
         * API 라 EDT 에 올리면 안 되고, markDirtyAndRefresh(async=true)는 스레드 무관이라
         * EDT 를 고를 이유가 없다.
         */
        private fun refreshDisk() {
            val d = shaper.drainDisk()
            if (d.paths.isEmpty() && !d.broad) return
            if (d.broad) diskBroad.set(true) else diskPaths.addAll(d.paths)
            if (!diskArmed.compareAndSet(false, true)) return
            ApplicationManager.getApplication().executeOnPooledThread {
                Thread.sleep(500)
                diskArmed.set(false) // 스냅숏 전에 내린다 — 이후 도착분은 새 플러시를 무장한다
                val broad = diskBroad.getAndSet(false)
                val paths = ArrayList(diskPaths).also { diskPaths.clear() }
                val base = project.basePath ?: return@executeOnPooledThread
                val lfs = com.intellij.openapi.vfs.LocalFileSystem.getInstance()
                if (broad) {
                    lfs.refreshAndFindFileByPath(base)?.let {
                        com.intellij.openapi.vfs.VfsUtil.markDirtyAndRefresh(true, true, false, it)
                    }
                    return@executeOnPooledThread
                }
                for (rel in paths) {
                    // 모델이 보낸 원문이라 윈도우즈 백슬래시가 실릴 수 있다 — VFS 는 '/' 만
                    // 알아듣고, 틀린 구분자는 에러가 아니라 조용한 no-op 이다(리뷰 F5).
                    val slashed = rel.replace('\\', '/')
                    val abs = if (java.nio.file.Paths.get(slashed).isAbsolute) slashed else "$base/$slashed"
                    lfs.refreshAndFindFileByPath(abs)?.let {
                        com.intellij.openapi.vfs.VfsUtil.markDirtyAndRefresh(true, false, false, it)
                    }
                }
            }
        }

        private fun redrawLog() {
            if (!dirty.compareAndSet(false, true)) return
            SwingUtilities.invokeLater {
                dirty.set(false)
                // 바닥 고정은 **바닥에 있던 사람에게만**. 무조건 고정이던 동안, 턴이 도는 중에
                // 위로 스크롤해 과거를 읽으면 새 이벤트마다 바닥으로 낚아채였다(라이브 실측 —
                // 지나간 편집을 찾아 올라가는 손이 매번 튕겼다). 떠나 있던 사람의 자리는
                // 그대로 두고, 꼬리를 따르던 사람만 계속 따르게 한다.
                val bar = scroll.verticalScrollBar
                val atBottom = bar.value + bar.visibleAmount >= bar.maximum - 48
                column.removeAll()
                shaper.list().forEach { column.add(rowPanel(it)) }
                column.revalidate()
                column.repaint()
                if (atBottom) SwingUtilities.invokeLater {
                    scroll.verticalScrollBar.value = scroll.verticalScrollBar.maximum
                    // 줄바꿈 판의 2-패스 레이아웃이 pin 직후 높이를 더 키우면, 한 번의 미착지로
                    // 꼬리 추적이 영구 이탈한다(리뷰 F4 — 종전 무조건 고정은 다음 이벤트가
                    // 자가 치유했었다). 다음 프레임에 한 번 더 주장한다. 슬랙 48px 은 한 줄
                    // 행 하나 미만이라 "바닥에 있던 사람"만 문다.
                    SwingUtilities.invokeLater {
                        scroll.verticalScrollBar.value = scroll.verticalScrollBar.maximum
                    }
                }
            }
        }

        /**
         * 행 하나를 붓질한다. **무엇을 적을지는 셰이퍼가 정했고 여기는 자리와 색만 안다** —
         * 반대로 하면 행 규칙이 판 수만큼 생긴다(`docs/TRANSCRIPT.ko.md` §0).
         */
        private fun renderRow(r: Row): JBPanel<JBPanel<*>> = rowPanel(r)

        private fun rowPanel(r: Row): JBPanel<JBPanel<*>> {
            val p = JBPanel<JBPanel<*>>(BorderLayout(0, 2))
            p.border = if (r.pending) Look.pendingRow() else Look.row()
            p.isOpaque = false
            when (r.who) {
                Who.User, Who.Agent -> {
                    val marks = buildList {
                        if (r.queued) add("⌛ 대기" to Look.faint)
                        if (r.abandoned) add("✕ 버려짐" to Look.muted)
                        if (r.pending) add("… 처리 중" to Look.faint)
                    }
                    val name = if (r.who == Who.User) "사람" else "magi"
                    val hue = if (r.who == Who.User) Look.primary else Look.accent
                    p.add(Look.rowHead(name, hue, marks, clock(r.at)), BorderLayout.NORTH)
                    p.add(Look.prose(r.text), BorderLayout.CENTER)
                }
                // 생각은 기본 접힘 — 웹이 그렇다. 클릭이 펴고, 펼침은 리드로우를 살아남는다([opened]).
                Who.Thinking -> {
                    val long = r.text.contains('\n') || r.text.length > 120
                    val open = foldKey(r) in opened
                    if (open) {
                        p.add(Look.aside("(생각) ⌃"), BorderLayout.NORTH)
                        p.add(Look.prose(r.text), BorderLayout.CENTER)
                    } else {
                        val head = r.text.lineSequence().firstOrNull().orEmpty().take(120)
                        p.add(Look.aside("(생각) $head" + if (long) "  ⌄" else ""), BorderLayout.CENTER)
                    }
                    if (long) foldable(p, r)
                }
                Who.Tool -> {
                    val (glyph, hue) = when {
                        r.ok == null -> "…" to Look.faint
                        r.note -> "✓ 읽을 것 있음" to Look.warn
                        r.ok == true -> "✓" to Look.success
                        else -> "✗" to Look.error
                    }
                    val open = foldKey(r) in opened
                    p.add(Look.toolHead(r.tool.orEmpty(), glyph, hue,
                        if (open) "⌃" else oneLine(r.args.orEmpty(), 100) + "  ⌄", clock(r.at)),
                        BorderLayout.NORTH)
                    if (open) {
                        // 펼침: 인자 원문과 결과 원문 — 옮겨 적을 것이라 고정폭이다.
                        val body = JBPanel<JBPanel<*>>().apply {
                            layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.Y_AXIS)
                            isOpaque = false
                            r.args?.let { add(Look.code(it)) }
                            r.out?.let { add(Look.code(it, Look.error)) }
                            // 지나간 편집도 같은 규칙으로 나란히-보기 — 인자의 old/new 원문 두 면,
                            // 앵커·replaceAll 은 제외(승인 diff 와 같은 「인자가 전체 진실」 집합).
                            toolDiffSides(r)?.let { (path2, old2, new2) ->
                                add(JButton("diff 뷰어로").apply {
                                    addActionListener {
                                        val f = com.intellij.diff.DiffContentFactory.getInstance()
                                        com.intellij.diff.DiffManager.getInstance().showDiff(
                                            project,
                                            com.intellij.diff.requests.SimpleDiffRequest(
                                                "magi 편집 — $path2",
                                                f.create(project, old2), f.create(project, new2),
                                                "이전", "이후",
                                            ),
                                        )
                                    }
                                })
                            }
                        }
                        p.add(body, BorderLayout.CENTER)
                    } else {
                        // 접힘: 실패의 첫 줄만. 전문과 파일:줄 앵커는 문제 탭의 몫이다.
                        r.out?.let { p.add(Look.code("↳ " + it.lineSequence().firstOrNull().orEmpty(), Look.error),
                            BorderLayout.CENTER) }
                    }
                    foldable(p, r)
                }
                Who.Council -> {
                    val name = r.member ?: "합의"
                    val marks = buildList {
                        r.decision?.let {
                            add(it to when (it) { "done" -> Look.success; "continue" -> Look.warn; else -> Look.faint })
                        }
                    }
                    p.add(Look.rowHead("⚖ $name", Look.seat(name) ?: Look.body, marks, clock(r.at)),
                        BorderLayout.NORTH)
                    val body = JBPanel<JBPanel<*>>().apply {
                        layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.Y_AXIS)
                        isOpaque = false
                        if (r.text.isNotBlank()) add(Look.prose(r.text))
                        r.keep?.takeIf { it.isNotBlank() }?.let { add(Look.aside("keep: $it")) }
                        r.why?.takeIf { it.isNotBlank() }?.let { add(Look.aside(it)) }
                    }
                    p.add(body, BorderLayout.CENTER)
                }
                Who.Info -> p.add(Look.aside(r.text), BorderLayout.CENTER)
            }
            return p
        }

        /**
         * 접었다 폈다 — 클릭 하나. 판은 리드로우마다 새로 서므로 상태는 [opened] 가 든다.
         *
         * **자식까지 같은 리스너를 단다**(리뷰 실측): 본문이 JTextArea 라 그 위 클릭은 텍스트
         * 컴포넌트가 소비하고 판까지 안 올라온다 — 스윙은 버블링이 없다. 접힌 생각 행의 보이는
         * 전부가 그 텍스트였으니, 안 달면 글자를 눌러도 안 펴진다. 드래그 선택은 mouseClicked
         * 가 안 울리므로(누른 자리=뗀 자리일 때만) 복사와 안 싸운다.
         */
        private fun foldable(p: JBPanel<JBPanel<*>>, r: Row) {
            val flip = object : java.awt.event.MouseAdapter() {
                override fun mouseClicked(e: java.awt.event.MouseEvent) {
                    val k = foldKey(r)
                    if (!opened.remove(k)) opened.add(k)
                    redrawLog()
                }
            }
            fun hook(c: java.awt.Component) {
                // 단추 서브트리는 접기 그물 밖이다(리뷰 F1): 같은 클릭이 diff 를 열면서 행을
                // 접으면, 연 것이 눈앞에서 사라진다 — 단추는 제 일 하나만 한다.
                if (c is javax.swing.AbstractButton) return
                c.addMouseListener(flip)
                c.cursor = java.awt.Cursor.getPredefinedCursor(java.awt.Cursor.HAND_CURSOR)
                if (c is java.awt.Container) c.components.forEach { hook(it) }
            }
            hook(p)
        }

        /**
         * 도구 행의 나란히-보기 재료 — 판정은 core 의 한 벌([Rows.EditSides], 골든 있음)에
         * 위임한다. 여기 남는 것 하나: **적용된 행만**(ok==true) — ✗ 행에 「이전/이후」를 세우면
         * 일어나지 않은 이후를 주장한다(승인 제목을 "물음 시점/제안"으로 바꾼 그 사유).
         */
        private fun toolDiffSides(r: Row): Triple<String, String, String>? {
            if (r.ok != true) return null
            return Rows.EditSides.of(r.tool, r.args)
        }

        /** 물음 id → 이미 연 가상 파일. 클릭마다 새 인스턴스면 같은 이름의 탭이 쌓인다(리뷰). */
        private val diffTabs = java.util.concurrent.ConcurrentHashMap<String, com.intellij.testFramework.LightVirtualFile>()

        /** 승인의 변화를 IDE 답게 연다 — 나란히(원문 두 면) 또는 패치 파일(코어 diff 원문). */
        private fun openApprovalDiff(w: Waiting) {
            val o = w.args as? kotlinx.serialization.json.JsonObject
            fun str(k: String) = (o?.get(k) as? kotlinx.serialization.json.JsonPrimitive)
                ?.takeIf { it.isString }?.content
            val old = str("old")
            val new = str("new")
            val path = str("path") ?: "변경"
            // 판정은 core 의 한 벌에 위임한다 — 두 벌로 적힌 동안 FlexBool 모양("yes"·1)에서
            // 갈라졌었다(리뷰). 여기 것과 전사 것이 같은 함수를 부르므로 갈라질 자리가 없다.
            val sides = Rows.EditSides.of(w.what, o?.toString())
            if (sides != null) {
                val f = com.intellij.diff.DiffContentFactory.getInstance()
                com.intellij.diff.DiffManager.getInstance().showDiff(
                    project,
                    com.intellij.diff.requests.SimpleDiffRequest(
                        "magi 승인 — ${sides.first}",
                        f.create(project, sides.second), f.create(project, sides.third),
                        // 이 창은 물음 순간의 스냅샷이다 — 답이 끝난 뒤에도 "지금"을 주장하면
                        // 거짓이 된다(비대칭-통지의 그 원칙).
                        "물음 시점", "제안",
                    ),
                )
                return
            }
            val vf = diffTabs.computeIfAbsent(w.id) {
                // 파일 타입을 plain text 로 못박는다(라이브 실측): 이름이 .diff 면 IntelliJ 의
                // 패치 에디터가 잡는데, 코어의 write 승인 diff 는 헤더(---/+++/@@) 없는 헝크라
                // "Invalid patch file" 판이 선다 — 원문 diff 를 그대로 보여 주는 것이 계약이고
                // (재계산 금지), 항상 읽히는 쪽이 색입힘보다 먼저다.
                com.intellij.testFramework.LightVirtualFile(
                    "magi-승인-${path.substringAfterLast('/')}-${w.id.takeLast(6)}.diff",
                    com.intellij.openapi.fileTypes.PlainTextFileType.INSTANCE, w.diff.orEmpty(),
                ).apply { isWritable = false }
            }
            com.intellij.openapi.fileEditor.FileEditorManager.getInstance(project).openFile(vf, true)
        }

        /** 이벤트 ts 를 이 자리의 시각으로. 못 읽으면 빈칸 — 지어내지 않는다. */
        private fun clock(at: String?): String = at?.let {
            runCatching {
                java.time.Instant.parse(it).atZone(java.time.ZoneId.systemDefault())
                    .toLocalTime().withNano(0).toString()
            }.getOrNull()
        }.orEmpty()

        /** 한 줄로 줄인다. 인자가 길면 전사가 그 인자만으로 화면을 다 먹는다. */
        private fun oneLine(s: String, max: Int): String {
            val one = s.lineSequence().joinToString(" ")
            return if (one.length <= max) one else one.take(max) + "…"
        }

        /**
         * 한 건을 적는다. **언제·어느 호출인지가 같이 간다** — 낡은 문제 목록은 없느니만 못하다는
         * 것이 §5-4 의 요구이고, 그 답이 목록을 지우는 것이 아니라 **출처를 적는 것**이다.
         *
         * `where` 가 없으면 그대로 둔다. 못 읽은 앵커를 지어내면 엉뚱한 줄을 가리키고, 그건 항목이
         * 안 눌리는 것보다 나쁘다.
         */
        private fun note(p: Problems.Problem) = SwingUtilities.invokeLater {
            val head = if (p.advisory) "· 했음(읽을 것 있음)" else "· 실패"
            push(problems, head, if (p.advisory) Look.warn else Look.error, bold = true)
            push(problems, " ${p.tool.orEmpty()}", Look.body)
            push(problems, "  #${p.seq}  ${p.at.orEmpty()}", Look.muted)
            p.where?.let { push(problems, "  ${it.path}:${it.line}", Look.accent) }
            push(problems, "\n    " + p.text.trim().lines().firstOrNull().orEmpty().take(160) + "\n",
                Look.faint)
            problems.caretPosition = problems.document.length
        }

        /**
         * 카운슬의 반대. 문제 판에 같이 서지만 **[Problems.of] 가 고른 것과 섞이지 않게** 자리
         * 색으로 누가 말했는지를 적는다 — 실패는 붉고, 반대는 그 자리의 색이다. 콘솔이 긋는 선과
         * 같다: 판정은 판정의 색이고 이름은 누구인지의 색이다(`console.css`).
         */
        private fun dissent(d: Problems.Dissent) = SwingUtilities.invokeLater {
            push(problems, "· 카운슬 ", Look.faint)
            push(problems, d.member, Look.seat(d.member) ?: Look.faint, bold = true)
            push(problems, " 반대", Look.body)
            push(problems, "  #${d.seq}  ${d.at.orEmpty()}", Look.muted)
            push(problems, "\n    ${d.why}\n", Look.faint)
            problems.caretPosition = problems.document.length
        }


        /**
         * 판에 글자 한 토막을 얹는다. **여기가 색이 붙는 유일한 자리다.**
         *
         * 글자를 정하지 않는다는 것이 규칙이다 — 무엇을 적을지는 부르는 쪽이 이미 정했고 여기는
         * 그것을 어떻게 보이게 할지만 안다. 반대로 하면(여기서 줄을 조립하면) 같은 서식이 두 군데
         * 생기고, 그중 한쪽만 고치는 날이 온다.
         *
         * 전부 EDT 에서 부른다. 예전 `problems.append` 는 스트림 스레드에서 문서를 고치고 캐럿만
         * EDT 로 옮겼는데, 그건 Swing 이 금지하는 것을 두 줄 중 한 줄만 지킨 것이다.
         */
        private fun push(
            pane: JTextPane, text: String, colour: Color?,
            italic: Boolean = false, bold: Boolean = false,
        ) {
            val a = SimpleAttributeSet()
            colour?.let { StyleConstants.setForeground(a, it) }
            if (italic) StyleConstants.setItalic(a, true)
            if (bold) StyleConstants.setBold(a, true)
            pane.styledDocument.insertString(pane.styledDocument.length, text, a)
        }

        /**
         * 동사의 **실패만** 알린다 — 입력줄 위의 붉은 한 줄([notice]). 성공은 안 적는다: 보낸
         * 말이 전사 행으로 서는 것 자체가 증거고, "— 보냈다" 류 혼잣말이 대화 사이에 끼는 것이
         * 읽기를 끊었다(사용자 실측). 다음 성공([clearNotice])이 지운다 — 사건 라벨이 사건을
         * 덮는 무늬는 남지만, 여기 서는 것은 실패뿐이라 성공이 실패를 지우는 방향만 있다.
         */
        private fun report(text: String) = SwingUtilities.invokeLater { notice.text = text }
        private fun clearNotice() = SwingUtilities.invokeLater { notice.text = " " }

        /** 거들기는 연결을 따로 판다 — 모델 호출이 락스텝 연결을 물면 그동안 다른 교환이 선다. */
        private fun assist() = socket()?.let { s -> Assist({ DaemonClient.connect(s) }) }

        /** 입력 꼬리의 @토큰 — "@셰이" 의 "셰이". 없으면 null. 공백이 끊는다. */
        private fun atToken(): String? {
            val t = input.text
            val at = t.lastIndexOf('@')
            if (at < 0) return null
            // 낱말 시작의 @ 만이다 — 아니면 "user@host" 를 치는 내내 팝업이 뜬다(리뷰).
            if (at > 0 && !t[at - 1].isWhitespace()) return null
            val tail = t.substring(at + 1)
            if (tail.any { it.isWhitespace() } || tail.length < 2) return null
            return tail
        }

        /** ESC 로 닫은 토큰 — 같은 토큰으로는 다시 안 띄운다(디바운스가 타이핑마다 도니까). */
        @Volatile private var dismissedToken: String? = null

        private fun askFiles(token: String) {
            if (token == dismissedToken) return
            ApplicationManager.getApplication().executeOnPooledThread {
                val sock2 = socket() ?: return@executeOnPooledThread
                // 토큰의 글롭 메타문자를 이스케이프한다(웹 globQuote 와 같은 넷) — 안 하면
                // "@page[1" 이 패턴 오류로 조용히 무반응이다.
                val safe = buildString {
                    token.forEach { c ->
                        if (c in "*?[]\\") append('\\')
                        append(c)
                    }
                }
                val files = runCatching {
                    // 세션은 안 실린다 — tool 문은 워크스페이스의 것, workdir 는 데몬이 박는다.
                    DaemonClient.connect(sock2).use { Companion(it, "").globFiles("**/*$safe*") }
                }.getOrDefault(emptyList())
                val cut = files.take(20)
                SwingUtilities.invokeLater {
                    if (atToken() != token) return@invokeLater // 그새 더 쳤다 — 낡은 목록 금지
                    if (cut.isEmpty()) return@invokeLater
                    com.intellij.openapi.ui.popup.JBPopupFactory.getInstance()
                        .createPopupChooserBuilder(cut)
                        // 컷은 알파벳순 앞 20(glob 이 정렬한다) — 잘렸으면 제목이 말한다.
                        .setTitle("@$token — 파일 첨부" +
                            if (files.size > cut.size) " (앞 ${cut.size} — 더 좁혀라)" else "")
                        .setItemChosenCallback { picked ->
                            dismissedToken = null
                            // 토큰을 걷고 칩을 세운다 — 본문이 아니라 참조가 실린다(§4.2c).
                            val t = input.text
                            val at = t.lastIndexOf('@')
                            if (at >= 0) input.text = t.substring(0, at)
                            attach(FileRef(picked))
                            input.requestFocusInWindow()
                        }
                        .createPopup()
                        .apply {
                            addListener(object : com.intellij.openapi.ui.popup.JBPopupListener {
                                override fun onClosed(e: com.intellij.openapi.ui.popup.LightweightWindowEvent) {
                                    // 고르지 않고 닫혔으면(ESC) 같은 토큰의 재팝업을 막는다 —
                                    // 다음 글자가 토큰을 바꾸면 자연히 풀린다.
                                    if (!e.isOk) dismissedToken = token
                                }
                            })
                            showUnderneathOf(input)
                        }
                }
            }
        }

        private fun askSuggestion() {
            atToken()?.let { askFiles(it); return }
            val prefix = input.text
            val a = assist() ?: return
            ApplicationManager.getApplication().executeOnPooledThread {
                val said = a.suggest(prefix)
                SwingUtilities.invokeLater {
                    // 그새 사람이 더 쳤으면 낡은 제안이다. 붙이지 않는다.
                    if (input.text != prefix) return@invokeLater
                    suggestion = said?.takeIf { it.isNotBlank() }
                    // 보이는 것과 **Tab 이 붙이는 것**이 같아야 한다. 여긴 모델이 지은 글자라
                    // 코드가 섞여 오고, 안 거르면 `<T>` 같은 조각이 태그로 먹혀 사라진다 —
                    // 사람은 짧아진 제안을 보고 Tab 을 누르고, 입력창에는 안 보이던 것이 들어간다.
                    hint.text = suggestion?.let { "<html><i>제안: ${Markup.text(it)} &nbsp;<b>Tab</b></i></html>" } ?: " "
                }
            }
        }

        private fun acceptSuggestion() {
            val s = suggestion ?: return
            input.text = input.text + s
            dropSuggestion()
        }

        /** 선 제안을 거둔다. 값과 그 값을 광고하는 줄이 **같이** 없어져야 한다. */
        private fun dropSuggestion() {
            suggestion = null
            hint.text = " "
        }

        private fun socket() = workspace.socket()

        /**
         * 상태 표시줄이 묻는 두 사실 — 턴이 언제 열렸고 카운슬이 몇 라운드인가. 원천은 전사
         * 셰이퍼라 이 창이 살아 있는 동안만 답이 있다. 셰이퍼 자체를 내주지 않는 것은 문을
         * 좁게 두는 것이다 — 넓은 문은 언젠가 딴것도 지나간다.
         */
        fun turnOpenedAt(): String? = if (shaper.open) shaper.openedAt else null
        fun councilRound(): Int? = shaper.councilRound

        /**
         * 계획 판이 묻는다. null = **모른다**(전사에 한 번도 못 붙었다), 빈 목록 = 계획이 없다 —
         * 둘을 한 값으로 뭉치면 화면이 모르는 것을 아는 척한다(§0.5-7).
         */
        fun plan(): List<Rows.Todo>? = if (everBegan) shaper.todos else null
        fun modelNow(): String? = shaper.model
        fun contextNow(): Rows.Ctx? = shaper.context

        /**
         * 기어 메뉴의 동사 하나. 답을 버리지 않는 것은 [add] 와 같은 규칙이고, 보고가 전사로
         * 가는 것은 [report] 의 규칙이다 — 단추마다 한 벌씩 다시 적지 않으려고 여기로 접는다.
         */
        fun verb(label: String, act: (Companion) -> Response): com.intellij.openapi.actionSystem.AnAction =
            object : com.intellij.openapi.actionSystem.AnAction(label) {
                override fun actionPerformed(e: com.intellij.openapi.actionSystem.AnActionEvent) =
                    onDaemon { comp ->
                        val r = act(comp)
                        if (r.ok) clearNotice() else report("$label — 안 갔다: ${r.error ?: "사유 없음"}")
                    }
            }

        /** 데몬에 한 번 붙어 무언가 하고 끊는다. 배선은 [Workspace] 가 갖는다 — 창 둘이 같이 쓴다. */
        private fun onDaemon(work: (Companion) -> Unit) =
            workspace.onDaemon(pinned, { say(Level.Unreachable(it)) }, work)

        fun refresh() = onDaemon { redraw(it) }

        /**
         * 프롬프트를 다시 묻고 다시 그린다. **이 라벨에는 수준만 쓴다.**
         *
         * 예전에는 보고를 여기로 받았다(`note` 인자). 단추가 답을 보낸 뒤 [refresh] 를 따로
         * 불렀고, 그러면 리프레시가 세우는 "사람을 기다리는 중이다"가 방금 받은 거절을 덮었기
         * 때문이다 — 그것도 연결을 새로 하나 더 열어 가면서, 어느 쪽이 먼저 EDT 에 닿을지는
         * 운이었다. 그래서 한 왕복 안으로 넣고 이기는 쪽을 코드에 박았다.
         *
         * 그건 **단추 경로만** 막았다. [interrupt] 와 [say] 가 세운 "안 갔다: 사유"는, 전사에
         * `movesPrompt` 프레임이 하나 들어와 [refresh] 가 돌면 그대로 덮였다 — 단추가 만든
         * 창은 닫았는데 **이벤트가 만드는 창은 그대로였다.** 순서를 더 박아도 안 닫힌다: 그
         * 프레임이 언제 올지는 이 창이 안 정한다.
         *
         * 지금은 [report] 가 사건을 전사로 내보내고 여기는 수준만 쓴다. 덮일 것이 없으니 이기는
         * 쪽을 정할 일도 없어서 `note` 가 없어졌다.
         *
         * 이 라벨에 남은 나머지 한 자리는 [onDaemon] 의 못 붙은 사유인데, **그것도 수준이다** —
         * 방금 일어난 일이 아니라 지금 데몬이 없다는 말이다.
         *
         * 「수준만 쓴다」는 이제 주석이 아니라 **타입**이다. 라벨은 [Level] 만 받으므로 사건은
         * 여기 들어올 이름이 없다 — 세는 것으로 붙들던 때는 자리 수가 그대로인 채 하나가 조용히
         * 사건으로 바뀔 수 있었다.
         */
        private fun redraw(comp: Companion) {
            val w = comp.waiting()
            SwingUtilities.invokeLater { drawPrompt(w) }
            say(if (w == null) Level.Attached else Level.Waiting)
        }

        /**
         * 돌고 있는 턴을 세운다.
         *
         * 이 단추가 없었다. 동사는 `Companion.interrupt` 로 있었는데 **부르는 자리가 없어서**,
         * 파일을 고치는 플러그인에 도는 턴을 멈출 방법이 하나도 없었다. 안 쓰는 동사라고 지우면
         * 지우는 것 자체가 결정이 된다 — 「멈출 수 없다」는 안전 속성이지 코드 정리 대상이 아니다.
         *
         * **"세웠다"고 안 한다.** 코어의 `app.go` 의 `Interrupt` 는 도는 턴이 없어도 `nil` 을
         * 돌려주므로, `ok` 는 요청이 닿았다는 뜻이지 무엇을 세웠다는 뜻이 아니다. 화면이 와이어가
         * 뒷받침 안 하는 말을 하면 사람은 안 멈춘 것을 멈춘 줄 안다. 실제로 무엇이 멈췄는지는
         * 전사에 나온다.
         *
         * 답은 안 버린다 — 같은 파일의 [add] 가 삼키다 고친 그 규칙이다.
         */
        /** 제목줄 액션의 손잡이. 보고 규칙은 [interrupt] 그대로다. */
        fun interruptFromTitle() = interrupt()

        private fun interrupt() = onDaemon { comp ->
            val r = comp.interrupt()
            if (r.ok) clearNotice() else report("세우기 — 안 갔다: ${r.error ?: "사유 없음"}")
        }

        private fun say() {
            val text = input.text.trim()
            if (text.isEmpty()) return
            val carry = synchronized(refs) { refs.toList() }
            onDaemon { comp ->
                // 고정 탭의 게이트(계약의 절반): 한 워크스페이스의 동시 턴은 아무것도 조정하지
                // 않는다 — 다른 대화의 턴이 도는 중이면 조용한 건너뛰기 대신 **묻는다**. 파일
                // 충돌은 사용자가 이름 댄 고통이다(docs/UI.ko.md §4.2b).
                run {
                    // 양방향 게이트(리뷰): 고정 탭의 턴이 도는 동안 주 판에서 보내도 동시 턴이다.
                    // 「내 데몬」은 이름이 아니라 **소켓**으로 고른다 — roster 는 머신 문이라 첫
                    // live 행이 옆 프로젝트의 데몬일 수 있다(오탐과 미탐이 동시에).
                    val sockStr = socket()?.toString()
                    val target = pinned ?: comp.facts().session
                    val mine = comp.roster().roster
                        ?.firstOrNull { it.live && !it.sighting && it.socket == sockStr }
                    if (mine?.state == "working" && mine.session != target) {
                        var go = false
                        SwingUtilities.invokeAndWait {
                            go = com.intellij.openapi.ui.Messages.showYesNoDialog(
                                project,
                                "다른 대화(${mine.session?.takeLast(6)})의 턴이 도는 중이다.\n" +
                                    "동시 턴은 파일 조작을 조정하지 않는다 — 그래도 이 대화에 보낼까?",
                                "동시 턴 경고", "보낸다", "만다", null,
                            ) == com.intellij.openapi.ui.Messages.YES
                        }
                        if (!go) return@onDaemon
                    }
                }
                val r = comp.say(text, carry)
                if (r.ok) {
                    clearNotice()
                    SwingUtilities.invokeLater {
                        input.text = ""
                        dropSuggestion()
                        // 보낸 것만 지운다(리뷰 실측): 왕복이 도는 동안 사람이 더 세운 칩을
                        // 전량 clear 가 소리 없이 지웠다 — 코어가 지키는 "사라지는 첨부 없음"을
                        // 클라이언트가 어기는 자리였다. attach 가 중복을 막으므로 removeAll 은 안전.
                        synchronized(refs) { refs.removeAll(carry) }
                        drawChips()
                        // 보낸 사람은 바닥으로 — 위에서 과거를 읽다 보냈어도 자기 메시지가
                        // 그려질 자리를 본다. 무조건 바닥 고정이 이를 우연히 보장하던 것을
                        // 조건부로 바꾸며 열린 구멍(리뷰 F3: 이 diff 가 처음 연 회귀).
                        scroll.verticalScrollBar.value = scroll.verticalScrollBar.maximum
                    }
                } else report("안 갔다: ${r.error ?: "사유 없음"}")
            }
        }

        /**
         * 대기 중인 프롬프트를 그린다. **무엇을 그릴지는 [Waiting.ask] 가 정하고 여기는 그리기만 한다.**
         *
         * 갈래가 왜 셋인지는 그 주석에 있다. 여기서 지키는 것은 하나다 — **못 그릴 때 침묵하지 않는다.**
         * 단추 없는 물음만 떠 있으면 사람은 창이 고장 난 줄 모르고, 컴패니언은 답을 기다리며 막혀 있다.
         * 그 침묵이 바로 이전 판의 `else` 가 하던 일이었다.
         */
        private fun drawPrompt(w: Waiting?) {
            buttons.removeAll()
            head.isVisible = w != null // 없는 물음의 자리를 비워 두지 않는다 — 죽은 띠가 된다
            // 물음이 서 있는 동안은 그 자리에 막대를 하나 세운다. 콘솔이 답 없는 물음에 긋는 것과
            // 같은 선이고(`.row.pending .txt`), 터미널도 같은 자리에 긋는다. 색으로만 말하지
            // 않는다 — 글자는 그대로 있고 막대는 **어디를 보라**는 표시다.
            prompt.border = if (w == null) Look.quiet else Look.pending()
            if (w == null) {
                prompt.text = " "
            } else {
                val at = if (w.total > 1) " (${w.index}/${w.total})" else ""
                val ask = w.ask
                val why = (ask as? Ask.Undrawable)?.why?.let { "<br/><i>${Markup.text(it)}</i>" }.orEmpty()
                // **무엇을 정하는지를 보인다.** 도구 이름은 요청의 설명이지 요청이 아니다 —
                // 판정과 사유는 `Waiting.subject` 에 있다.
                val subject = when (val sub = w.subject) {
                    is Subject.Stated -> listOfNotNull(
                        sub.args?.let { "<tt>${Markup.text(it)}</tt>" },
                        sub.reason?.let { Markup.text(it) },
                    ).joinToString("<br/>")
                    // 못 받은 것을 못 받았다고 적는다. 이 줄이 없으면 사람은 아는 것(도구 이름)만
                    // 보고 누르고, 창이 무엇을 덜 받았는지는 영영 안 나온다.
                    Subject.Unstated -> "<i>이 물음에 정해지는 것이 안 실려 왔다 — " +
                        "무엇을 허가하는지 이 창은 모른다.</i>"
                }
                prompt.text = "<html><b>${Markup.text(w.what)}</b>$at<br/>$subject$why</html>"
                when (ask) {
                    is Ask.Permission -> {
                        add("허용") { it.allow(w.id) }
                        add("거부") { it.deny(w.id) }
                        add("항상") { it.always(w.id) }
                    }
                    is Ask.Choose -> ask.options.forEach { opt -> add(opt) { it.answer(w.id, opt) } }
                    // 사유는 위 문구에 실었다. 단추는 안 만든다 — 지어낸 단추는 틀린 답을 보낸다.
                    is Ask.Undrawable -> Unit
                }
                // 변화 그 자체가 실려 온 승인(코어 계약상 치환·write — 앵커·replaceAll·multiedit 는 부재)은
                // 단추 하나로 **IDE 편집창**에서 본다: 치환 편집이면 인자의 old/new 원문
                // 나란히-보기, 그 밖은 코어의 diff 원문 탭. 독 안에 diff 를 구겨 넣던 인라인
                // 판(± 변경 보기)은 사용자 실측으로 걷었다 — "왜 플러그인 안에서 조이노? 그
                // 쪼끄만데서 다 보이겠나". diff 가 안 실린 승인은 위의 args 뷰가 그대로 선다.
                if (!w.diff.isNullOrBlank()) {
                    buttons.add(JButton("변경 보기").apply {
                        addActionListener { openApprovalDiff(w) }
                    })
                }
            }
            buttons.revalidate(); buttons.repaint()
        }

        /**
         * 프롬프트 단추 하나. [act] 의 답을 **버리지 않는다.**
         *
         * 버리고 있었다. `(Companion) -> Unit` 이라 `allow`·`deny`·`always`·`answer` 가 돌려주는
         * [Response] 가 통째로 사라졌고, 데몬이 거절해도 화면은 다시 그리고 말았다 — 사람이 누른
         * 것이 갔는지 안 갔는지 알 방법이 없는, **눌러도 아무 일도 안 나는 창**이었다. 같은 파일의
         * `say()` 는 처음부터 "안 갔다"를 보고했다. 한 창이 한 동사는 보고하고 나머지 넷은 삼켰다.
         *
         * 이게 지금 더 중요한 이유. 코어의 거절 문구가 **없음의 사유를 못 가른다** — 종류가 어긋난
         * 답도 "이미 답했거나 만료됐다"로 온다(`app.go` 의 `RespondQuestion`). 그 문장을 고치는 일이
         * 논의 중인데, 받는 쪽이 버리고 있으면 고쳐 봐야 아무 데도 안 닿는다.
         */
        private fun add(label: String, act: (Companion) -> Response) {
            buttons.add(JButton(label).apply {
                addActionListener {
                    onDaemon { c ->
                        val r = act(c)
                        // 성공이 지운다 — say 하나에만 걸면 만료 프롬프트의 "안 갔다"가 다음
                        // 성공 뒤에도 지금 것처럼 서 있는다(이 유닛이 없애려던 그 무늬).
                        if (r.ok) clearNotice() else report("안 갔다: ${r.error ?: "사유 없음"}")
                        redraw(c)
                    }
                }
            })
        }

        /**
         * 수준을 쓰는 **하나뿐인 문**. 자리가 라벨에서 제목표시줄로 갔고([title]), 문이 하나에
         * [Level] 만 받는 규칙은 그대로다 — 문이 여럿이면 타입은 그중 하나만 지킨다
         * (`SourceTextTest`). 라벨 시절의 수준 색은 여기서 끝났다: 제목은 IDE 의 글자라 색을
         * 못 받는데, 색은 원래 글자를 대신하지 않는 보조였으니 잃는 것은 보조뿐이다.
         */
        private fun say(l: Level) {
            if (pinned != null) {
                // 고정 탭의 title 은 허공이다 — 제목표시줄은 주 판의 것. 사람이 할 일이 생기는
                // 못-닿음만 점으로 승격한다(§0.5-7: 무통보 무동작 금지). 나머지 수준은 점과
                // 프롬프트 판이 이미 말한다.
                if (l is Level.Unreachable) mood(Look.error, "✕", l.text)
                return
            }
            title(l.text)
        }
    }
}

/**
 * 살아 있는 창을 찾는 길. 액션이 전사에서 쌓인 것을 물어야 하는데, 그 자료는 창이 들고 있고
 * 창은 게으르게 만들어진다.
 *
 * **`companion object` 가 아니라 이름 있는 객체다.** 처음엔 `MagiToolWindow` 안에 companion 으로
 * 뒀는데, 그러면 코틀린이 만드는 `MagiToolWindow.Companion` 이 **usecase 의 `Companion` 클래스를
 * 가린다** — 같은 파일이 그 클래스를 쓰고 있어서 그 자리들이 통째로 컴파일을 못 했다. 도메인에
 * `Companion` 이라는 이름이 있는 한 이 파일에 companion object 를 두면 안 된다.
 *
 * `WeakHashMap` **만으로는 안 놓아준다.** 값인 [MagiToolWindow.View] 가 자기 키인 `Project` 를
 * 필드로 들고 있어서 키가 값에서 강하게 닿고, 그러면 엔트리가 영영 안 걷힌다 — WeakHashMap 의
 * 고전적인 오용이다. 여기 주석은 한때 그 반대를 적어 두고 있었다. 실제로 놓아주는 것은
 * [MagiToolWindow.View.dispose] 가 부르는 [remove] 이고, 약한 키는 그것이 못 돌았을 때를 위한
 * 둘째 줄이다.
 *
 * 그리고 **없으면 null 을 준다** — 액션이 "창이 아직 안 열렸다"고 말할 수 있어야 하고, 빈 답을
 * 내면 "이 파일은 아무도 안 건드렸다"와 구분이 안 된다.
 */
internal object MagiWindows {
    private val live = java.util.WeakHashMap<Project, MagiToolWindow.View>()
    fun put(project: Project, view: MagiToolWindow.View) = synchronized(live) { live[project] = view }
    fun of(project: Project): MagiToolWindow.View? = synchronized(live) { live[project] }
    fun remove(project: Project) = synchronized(live) { live.remove(project) }
}
