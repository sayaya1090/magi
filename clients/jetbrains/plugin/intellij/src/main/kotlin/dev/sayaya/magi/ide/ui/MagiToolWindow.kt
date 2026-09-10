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
import dev.sayaya.magi.ide.usecase.RowText
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
 * 대화형 인터페이스 — magi 컴패니언과 상호작용하고 질문 및 승인 요청에 응답하는 창입니다. **하단 도구 창(Bottom Dock)**에 배치됩니다.
 *
 * 콘솔 환경에서는 대화 뷰가 중앙에 위치하지만(`docs/UI.md` §2.2), IDE 환경에서 중앙은 코드 편집의 영역이므로
 * §5의 첫 번째 원칙("IDE와 겹치는 기능은 만들지 않는다")에 따라 하단 독에 배치합니다.
 * IntelliJ에서 지속적인 출력 스트림을 다루는 Run, Terminal, Build 창과 일관된 배치입니다.
 * 런타임 상세 상태는 설정 화면([MagiConfigurable] — 2026-08-29 사용자 결정) 안으로 분리되었으며, 상시 상태는 상태 표시줄이 담당합니다.
 *
 * 트랜스크립트는 데몬의 `transcript` 문에서 이벤트 스트림으로 수신되며, 셰이퍼([Rows])가 화면 행 단위로 가공합니다.
 * 행 변환 규칙은 `docs/TRANSCRIPT.ko.md`의 명세를 따르며, 이 창은 해당 행을 렌더링([renderRow])합니다.
 * 초기 구현에서는 `#seq type (actor)` 메타데이터만 표기되어 사용자가 입력한 메시지와 답변 본문이 표시되지 않던 누락이 실측되어,
 * 행 본문이 정상 구성되는지 골든 테스트로 검증하고 있습니다.
 *
 * 소켓 입출력은 모두 백그라운드 스레드 풀에서 실행됩니다. EDT(Event Dispatch Thread)에서 소켓을 점유하면 데몬 응답 지연 시 IDE 전체가 블로킹됩니다.
 */
class MagiToolWindow : ToolWindowFactory {
    /**
     * 이 프로젝트에 해당 도구 창이 유효한지 판정합니다(UI Guidelines · Tool window:
     * "don't display the button when the window doesn't apply to the project setup").
     *
     * **가벼운 검사만 수행합니다.** 「데몬 생존 여부」로 판정하면 데몬을 나중에 기동하는 일반적인 사용 흐름에서
     * 버튼이 영구적으로 나타나지 않아 기동 액션에 접근할 수 없게 됩니다. 워크스페이스가 될 수 있는 디렉터리 경로가
     * 존재하는지만 확인하며, 웰컴 화면이나 프로젝트 경로가 없는 임시 창에서만 비활성화됩니다.
     */
    override fun shouldBeAvailable(project: Project) = project.basePath != null

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val view = View(project)
        // 상태 표시 위치 설계:
        // 과거 라벨 컴포넌트 시절에는 변경 빈도가 낮은 한 줄 텍스트가 전사 위쪽 영역을 크게 점유했습니다
        // (사용자 실측 피드백: "변하는 데이터도 없는데 너무 넓은 공간을 차지함").
        // 도구 창의 정체성은 제목 표시줄에서 나타내고, 상시 가시성이 필요한 상태는 상태 표시줄이 담당합니다(`docs/UI.ko.md` §3.1).
        // 제어 액션들은 기어 메뉴로 이동하고(설정 창은 영속 구성의 공간임 — `docs/UI.ko.md` §5), 처리 결과는 전사 로그로 보고됩니다.
        // 실행 중지(Stop) 버튼은 제목 표시줄 타이틀 액션으로 배치하여, 실행 중인 턴을 언제든 중지할 수 있으면서도 전송 버튼 옆 공간을 낭비하지 않도록 했습니다(TUI의 Esc 키와 동급).
        // 정보 카드는 **제목 표시줄** 아이콘으로 접어 둡니다(사용자 요구: Office 리본처럼 축소하여 빠른 접근 지원).
        // 기어 메뉴 대신 팝업 아이콘을 사용하는 이유는 단순 액션 목록이 아닌 상태 정보(상태 인디케이터 및 버전 번호)를 표시하는 전용 UI이기 때문입니다.
        toolWindow.setTitleActions(listOf(view.infoAction(), object : com.intellij.openapi.actionSystem.AnAction(
            MagiBundle.msg("chat.stop"), MagiBundle.msg("chat.stop.tip"), com.intellij.icons.AllIcons.Actions.Suspend) {
            override fun actionPerformed(e: com.intellij.openapi.actionSystem.AnActionEvent) {
                MagiWindows.of(project)?.interruptFromTitle()
            }
        }))
        toolWindow.setAdditionalGearActions(com.intellij.openapi.actionSystem.DefaultActionGroup(
            object : com.intellij.openapi.actionSystem.AnAction(MagiBundle.msg("chat.menu.tabs")) {
                override fun actionPerformed(e: com.intellij.openapi.actionSystem.AnActionEvent) {
                    // 목록은 sessions 문(최근 활동 순 — 차례는 데몬 것), 고르면 고정 탭이 선다.
                    // 탭 전환은 보기다 — resume 을 부르지 않는다(§4.2b).
                    fun balloon(t: String) = com.intellij.notification.NotificationGroupManager
                        .getInstance().getNotificationGroup("magi")
                        .createNotification(t, com.intellij.notification.NotificationType.WARNING)
                        .notify(project)
                    Workspace(project).onDaemonWithoutChat({ balloon(MagiBundle.msg("chat.sessions.failed", it)) }) { comp ->
                        // 침묵 금지(§0.5-7): 문이 없거나 목록이 비면 그 사실이 풍선으로 선다 —
                        // 눌렀는데 아무 일도 안 나는 메뉴는 없는 메뉴보다 나쁘다.
                        val sr = comp.sessions()
                        val rows = if (sr.ok) sr.sessions.orEmpty() else null
                        if (rows == null) {
                            balloon(MagiBundle.msg("chat.sessions.nodoor") +
                                (sr.error?.let { " — " + it.lineSequence().first().take(80) } ?: ""))
                            return@onDaemonWithoutChat
                        }
                        if (rows.isEmpty()) { balloon(MagiBundle.msg("chat.sessions.none")); return@onDaemonWithoutChat }
                        SwingUtilities.invokeLater {
                            // 라벨 기반 역조회(indexOf)는 동일 라벨 중복 시 오매핑 위험이 있으므로 SessionRow 객체를 직접 보유하여 선택합니다.
                            class Pick(val row: SessionRow) {
                                // 마지막 활동 시각 표시:
                                // 실측(2026-09-10) 기준 대화 목록이 241개에 달했고, 제목과 6자리 ID만으로는
                                // 제목이 없는 51개 세션을 포함해 원하는 대화를 탐색하기 어려웠습니다.
                                // 코어 데몬이 항상 전송하는 `lastActivity` 필드를 활용하여,
                                // VS Code 클라이언트와의 정합성을 맞춰 당일 활동은 시:분, 이전 활동은 날짜를 접두어로 표시합니다.
                                override fun toString() =
                                    (row.title?.take(40)?.ifBlank { null } ?: MagiBundle.msg("chat.untitled")) +
                                        "  ·" + row.id.takeLast(6) +
                                        RowText.asked(row.lastActivity).let { if (it.isEmpty()) "" else "  ·$it" }
                            }
                            com.intellij.openapi.ui.popup.JBPopupFactory.getInstance()
                                .createPopupChooserBuilder(rows.map { Pick(it) })
                                .setTitle(MagiBundle.msg("chat.pick.title"))
                                .setItemChosenCallback { picked ->
                                    MagiTabs.open(project, picked.row.id, "·" + picked.row.id.takeLast(6))
                                }
                                .createPopup()
                                .showInFocusCenter()
                        }
                    }
                }
            },
            view.verb(MagiBundle.msg("chat.menu.compact")) { it.compact() },
            view.verb(MagiBundle.msg("chat.menu.rewind")) { it.rewind(1) },
        ))
        MagiWindows.put(project, view)
        // 도구 창의 생명주기에 바인딩합니다. 등록 해제 누락 시 창이 닫힌 후에도 소켓 스트림 및 리소스가 잔류하게 됩니다.
        Disposer.register(toolWindow.disposable, view)
        // 채팅 뷰와 지적 사항(Problems) 뷰는 분할(Split) 레이아웃 대신 탭(Tab)으로 분리합니다.
        // 분할 레이아웃 사용 시 빈 문제 패널이 가로 폭의 35%를 상시 점유하던 문제를 개선하고,
        // 프로세스별 탭 구조를 따르는 Run 창 등 IDE 표준 레이아웃 관례(`docs/UI.ko.md` §0-5)를 준수합니다.
        val make = ContentFactory.getInstance()
        // 기본 탭 닫기 방지:
        // Content의 `isCloseable` 기본값은 true이며, 도구 창 선언의 `canCloseContents="true"`는 창 전체의 탭 닫기를 허용합니다.
        // 기본 탭이 닫힐 경우 `createToolWindowContent`는 재호출되지 않아 UI 복구가 불가능해지고,
        // View는 Content가 아닌 ToolWindow Disposable에 바인딩되어 있어 UI만 닫히고 스트림은 살아 있는 리소스 누수가 발생합니다(리뷰 R1).
        toolWindow.contentManager.addContent(
            make.createContent(view.root, MagiBundle.msg("chat.tab.chat"), false).apply { isCloseable = false },
        )
        toolWindow.contentManager.addContent(
            make.createContent(view.problemsView, MagiBundle.msg("chat.tab.problems"), false)
                .apply { isCloseable = false },
        )
        view.refresh()
    }

    /**
     * [pinned] 파라미터가 지정되면 해당 대화 세션에 고정된 독립 탭으로 동작합니다(`docs/UI.ko.md` §4.2b):
     * 전역 공표 상태나 `session.moved` 이벤트를 따르지 않으며, 입력 메시지를 지정된 세션 ID로만 라우팅합니다(계약: submit/steer는 대상 세션의 턴을 시작함).
     * null인 경우 전역 활성 세션을 추종하는 메인 패널로 동작합니다.
     */
    internal class View(private val project: Project, private val pinned: String? = null) : Disposable {
        private val workspace = Workspace(project)
        val root = JBPanel<JBPanel<*>>(BorderLayout())
        /** 도구 창 제목 표시줄에 상태 텍스트를 업데이트하는 핸들러. ToolWindow 구성부에서 주입합니다. */
        var title: (String) -> Unit = {}
        private val prompt = JBLabel(" ").apply { border = Look.quiet }
        private val buttons = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.LEFT, 8, 4))
            .apply { border = JBUI.Borders.empty(0, 8, 6, 8) }
        private val input = JBTextArea(1, 40).apply { border = JBUI.Borders.empty(8, 10) }
        private val hint = JBLabel(" ").apply {
            // 빈 공간 점유 방지(사용자 실측 피드백: 입력란 하단 과도한 여백 방지):
            // 알림 및 힌트가 존재할 때만 가시화하여 상시 레이아웃 낭비를 최소화합니다.
            isVisible = false
            foreground = Look.faint
            border = JBUI.Borders.empty(2, 12, 6, 12)
        }
        /** 마지막으로 수신된 입력 자동완성 제안. Tab 키 입력 시 적용됩니다. */
        private var suggestion: String? = null
        private val debounce = javax.swing.Timer(400) { askSuggestion() }.apply { isRepeats = false }

        /**
         * 트랜스크립트 뷰 컴포넌트.
         * 스트리밍 프로토콜은 단일 연결을 전용 점유하므로 일반 요청/응답 채널과 공유하지 않습니다(설계 문서 §3 「스트리밍」).
         */
        private val column = Look.column()

        /** 트랜스크립트 복사 액션 핸들러(단일 말풍선 복사 및 범위 선택 복사 지원). */
        private val copying = Copying().also { c ->
            c.rows { shaper.list() }
            c.popup(column) { shaper.list() }
        }
        private val scroll = JBScrollPane(column).apply {
            border = JBUI.Borders.empty()
            horizontalScrollBarPolicy = javax.swing.ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER
        }

        /** 트랜스크립트 셰이퍼. 이벤트 파싱은 워커 스레드에서 수행하며, 렌더링은 EDT에서 스냅샷 기반으로 실행합니다. */
        private val shaper = Rows()

        /**
         * 트랜스크립트 연결 수신 이력 플래그. 계획(Plan) 뷰의 "알 수 없음/없음" 상태 분기 판정에 사용됩니다.
         *
         * **클래스 초기화 순서 주의 (`init` 블록보다 먼저 선언 필요)**:
         * `follow` 호출 시 워커 스레드 기동 전 `began` 콜백이 동기 호출되어 true가 기록되는데,
         * 필드 선언이 `init` 블록 뒤에 위치하면 생성자 초기화 코드에 의해 false로 덮어써지는 버그가 실측되었습니다.
         */
        @Volatile private var everBegan = false

        /**
         * 현재 추종 중인 세션 ID.
         * `session.moved` 이벤트의 만료 여부를 판정하는 기준이 됩니다(이전 세션 로그에 영속화되어 재생 시 재수신될 수 있음).
         */
        @Volatile private var followedSid: String? = null

        /** [follow]와 moved 이벤트 처리 간의 상호 배제 동기화 락. 스트림 누수를 방지합니다. */
        private val followLock = Any()

        /**
         * 연결 상태 인디케이터.
         * "전사 연결됨" 등의 인라인 텍스트 행이 대화 흐름을 방해하지 않도록 색상 및 글리프로 간결하게 표현합니다. 세부 사유는 툴팁에 표시됩니다.
         */
        // 초기 상태 글리프 설정: 비연결 상태는 ◌ 글리프를 사용합니다(Material Design 접근성 가이드라인 준수).
        private val link = JBLabel("◌").apply { foreground = Look.muted; toolTipText = MagiBundle.msg("chat.link.none") }

        // 상태 표현 설계 (Material Design 3 접근성 지침 준수):
        // 색상뿐만 아니라 글리프를 함께 제공하여 구분합니다(● 연결됨, ↻ 재연결 중, ✕ 끊김, ◌ 비연결).
        // 스트림 연결 상태와 도구 어댑터(Hand) 상태는 생명주기가 상이하므로 별도 상태로 분리하여 관리합니다.
        /**
         * 연결 상태 불변 데이터 모델 — 색상, 글리프, 상태 메시지를 원자적 단위로 묶어 관리합니다.
         * 개별 필드를 분리하여 관리할 경우 재연결 루프와 스트림 싱크 간의 경쟁 상태로 인해
         * "녹색 점에 연결 끊김 문구"와 같은 UI 상태 불일치가 발생하는 문제를 방지합니다(리뷰 R6).
         */
        private data class Mood(val colour: Color, val glyph: String, val why: String)

        @Volatile private var mood: Mood = Mood(Look.muted, "◌", MagiBundle.msg("chat.link.none"))
        @Volatile private var handWhy: String? = null
        private fun mood(colour: Color, glyph: String, why: String) {
            mood = Mood(colour, glyph, why)
            paintLink()
        }
        private fun handSaid(t: String?) { handWhy = t; paintLink() }
        private fun paintLink() = SwingUtilities.invokeLater {
            val m = mood // 상태 불일치 방지를 위해 로컬 스냅샷을 1회만 참조합니다
            link.text = m.glyph
            link.foreground = m.colour
            link.toolTipText = m.why + (handWhy?.let { " · $it" } ?: "")
        }

        /**
         * 동사의 **실패만** 적는 한 줄. 성공은 안 적는다 — 보낸 말이 행으로 서는 것 자체가
         * 증거다(같은 사용자 결정). 다음 성공이 지운다.
         */
        private val notice = JBLabel(" ").apply {
            isVisible = false // 사유는 hint 쪽 주석 — 빈 줄이 자리를 먹지 않는다
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
                    mood(Look.success, "●", MagiBundle.msg("chat.link.connected"))
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
                        // **먼저 적고 나서 움직인다.** 여기서 곧장 돌아가면 셰이퍼가 이 사실을 못 보고,
                        // 못 따라가는 자리(고정 탭·나중에 다시 연 대화)에서는 전사가 아무 말 없이
                        // 끝난다 — 데몬이 죽은 것과 구별되지 않는 그 읽기다.
                        if (shaper.feed(e)) redrawLog()
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
                    // 화면 초기화는 이벤트 수신부가 아닌 [began] 콜백에서 수행합니다.
                    // 스트리밍 델타(part.delta)와 사실(part.appended) 이벤트 처리:
                    // 재생(Replay) 시에는 영속화된 fact 이벤트만 전달되므로, 실시간 스트림과 재생 스트림이 동일한 대화 전사를 그리도록 정합성을 맞춥니다(사유: `Transcript.echoesFact`).
                    // 스트리밍 델타(`part.delta`)는 새 행을 추가하지 않고 동일 행의 초안을 갱신합니다(설계 문서 §8 타자기 방식).
                    // 토큰마다 전체 뷰를 다시 그리는 부하를 방지하기 위해 초안 갱신은 120ms 디바운스(`redrawSoon`)로 배치 처리합니다.
                    if (e.type == "part.delta") {
                        if (shaper.feed(e)) redrawSoon()
                    } else if (!Transcript.echoesFact(e) && shaper.feed(e)) redrawLog()
                    refreshDisk() // 컴패니언이 수정한 디스크 변경사항을 IDE VFS에 동기화(사유: Rows.drainDisk)
                    if (e.seq > lastSeq) lastSeq = e.seq // 영속 사실 이벤트만 커서로 추적(과도기 전이 이벤트는 seq == 0)
                    // 지적 사항(Problems)은 트랜스크립트 스트림에서 파생 추출합니다.
                    // 설계 문서 §3 원칙("도구 창당 단일 스트림 점유")에 따라 별도 스트림을 개설하지 않고 동일 프레임 중복 파싱을 방지합니다.
                    authors.feed(e)
                    Problems.of(e)?.let { note(it) }
                    // 프롬프트 및 승인 상태 갱신 신호 처리:
                    // 질문 요청(`*.requested`)은 과도기적 전이 이벤트이므로 로그에 영속화되지 않습니다.
                    // 이 신호를 수신할 때 프롬프트를 갱신하지 않으면 창 오픈 후 유입된 질문의 승인 버튼이 생성되지 않습니다(사유: `Transcript.movesPrompt`).
                    //
                    // **이벤트 페이로드(`e`)를 직접 뷰에 투영하지 않습니다.**
                    // `permission.decided`와 같은 영속 사실 이벤트는 재생(Replay) 시에도 재수신됩니다.
                    // 이벤트를 갱신 트리거 신호로만 사용하고 실제 표시 데이터는 데몬에 새로 질의(refresh)하여, 재생 시 과거 질문이 최신 질문으로 잘못 렌더링되는 문제를 방지합니다.
                    if (Transcript.movesPrompt(e)) refresh()
                    Problems.dissentOf(e)?.let { dissent(it) }
                }
                // 데몬 측의 커서 재동기화 통보(이벤트 스트림보다 먼저 전달되는 사전 알림):
                // 이미 렌더링된 내용을 초기화해야 하므로 명시적으로 처리합니다. 무시할 경우 UI에 불일치한 상태가 잔류하게 됩니다.
                override fun note(why: String) {
                    // 데몬이 클라이언트의 커서(lastSeq)를 신뢰할 수 없을 때 발생하는 통보입니다.
                    // 계약에 따라 기존 렌더링 버퍼를 클리어하고 전체 로그를 재생받는 상태로 전환합니다.
                    lastSeq = 0
                    authors.forget()
                    shaper.clear()
                    SwingUtilities.invokeLater { problems.text = "" }
                    // ↻ 글리프는 '재연결 중' 상태를 의미하므로, 소켓은 연결되어 있으나 커서만 거절된 현재 상태에서는
                    // 연결 상태(●)에 경고 색상(Look.warn)과 사유를 표기하여 의미적 정확성을 유지합니다(코드 리뷰 지적 사항 반영).
                    mood(Look.warn, "●", why)
                    redrawLog()
                }
                /**
                 * 스트림 종료 원인별 분기 처리.
                 * 클라이언트가 의도적으로 종료한 경우(ByUs)를 제외하고, 데몬 측 종료나 연결 끊김 시에는
                 * 자동 재연결(`reattach()`)을 수행하여 프롬프트 신호 및 승인 버튼이 비활성화되는 현상을 방지합니다.
                 */
                override fun ended(end: End) = when (end) {
                    End.ByUs -> mood(Look.muted, "◌", MagiBundle.msg("chat.link.closed"))
                    // 도구 어댑터(Hand) 상태 정리(코드 리뷰 지적 사항):
                    // 스트림이 끊어지면 데몬에 등록되었던 어댑터 정보도 무효화되므로 툴팁 정보를 초기화합니다.
                    End.ByDaemon -> { handSaid(null); mood(Look.warn, "↻", MagiBundle.msg("chat.link.lost")); reattach() }
                    is End.Broken -> { handSaid(null); mood(Look.error, "✕", MagiBundle.msg("chat.link.broken", end.why)); reattach() }
                }
            }

        /** 대기 프롬프트 및 승인 요청 패널. 요청이 없을 때는 숨김 처리하여 불필요한 빈 여백을 제거합니다(사용자 실측 피드백 반영). */
        private lateinit var head: JBPanel<JBPanel<*>>

        /**
         * 첨부 참조 목록 — 파일 본문이 아닌 참조 정보(경로 + 라인 범위)입니다.
         * 본문 발췌 및 영속화는 코어 데몬이 담당하므로(`docs/CLIENTS.ko.md` §2), 클라이언트 UI는 칩(Chip) 형태의 라벨만 표시합니다.
         */
        private val refs = java.util.Collections.synchronizedList(mutableListOf<FileRef>())
        private val chips = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.LEFT, 6, 2)).apply {
            isOpaque = false
            isVisible = false
        }

        /**
         * 에디터 인텐션(Alt+Enter) 등에서 입력창에 초기 프롬프트를 주입합니다.
         * 사용자 작성 내용 유실을 방지하기 위해 입력창이 비어 있을 때만 주입하며 커서를 맨 끝으로 배치합니다.
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
                    // 경로와 **언제 나가는지** 둘 다 — 칩은 「지금 보낸 것」이 아니라 「다음
                    // 메시지에 실릴 것」이라, 그 사실이 어디에도 안 적혀 있었다.
                    toolTipText = ref.path + " — " + MagiBundle.msg("chat.attach.tip")
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

            val send = JButton(MagiBundle.msg("chat.send")).apply { addActionListener { say() } }
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
            // 초기 연결 분기 및 상태 고지:
            // 트랜스크립트 스트림이 연결되지 않았을 때 [ended] 콜백이 수신되지 않아 UI가 멈춘 채 잔류하는 현상을 방지합니다.
            // 빈 전사에 정상 연결된 상태와 아예 연결되지 못한 상태를 구분하기 위해 [Attach] sealed 인터페이스로 구체적 사유를 분류합니다.
            //
            // 분기별 대응 조치:
            // 1. Attach.NoWorkspace: 프로젝트 디렉터리 경로 오류 (프로젝트 설정 확인 필요)
            // 2. Attach.NoSession: 데몬 미기동 상태 (IDE를 먼저 띄우고 데몬을 나중에 띄우는 일반적인 흐름)
            // 3. Attach.Failed: 데몬 프로세스는 존재하나 소켓 통신 오류 발생
            //
            // 컴파일 타임 전수 검사(Exhaustive check)를 보장하기 위해 `else` 절을 사용하지 않고 모든 분기를 명시합니다.
            when (val a = follow()) {
                Attach.Ok -> {}
                Attach.NoWorkspace -> lost(MagiBundle.msg("chat.noworkspace"))
                Attach.NoSession -> lost(MagiBundle.msg("chat.nodaemon"))
                is Attach.Failed -> lost(MagiBundle.msg("chat.unreachable", a.why))
            }
            // MCP 도구 어댑터(Hand)는 프로젝트 단위로 1개만 등록합니다.
            // 세션 탭마다 개별 서버를 기동할 경우 루프백 포트 낭비 및 고정 식별자 충돌("jetbrains is already attached")로 거절 알림이 발생합니다(코드 리뷰 반영).
            if (pinned == null) offerHand()
        }

        /**
         * 도구 창 종료 시 등록된 리소스 및 스트림을 해제합니다.
         *
         * 해제 누락 방지 계약:
         * 트랜스크립트 스트림 스레드, HandServer 루프백 포트, 데몬 측의 어댑터 등록을 순서대로 정리합니다.
         * 이를 정리하지 않으면 창이 닫힌 후에도 백그라운드 스레드가 유지되고, 데몬이 종료된 IDE 창을 유효한 도구 어댑터로 인식하여
         * 파일 편집 요청을 닫힌 창으로 라우팅하는 문제가 발생합니다.
         *
         * 해제 순서:
         * 1. 로컬 루프백 소켓을 먼저 닫아 인바운드 편집 요청을 즉시 차단합니다.
         * 2. 이후 데몬으로 `detachHand` 요청을 비동기 전송합니다. 소켓 지연 등으로 전송에 실패하더라도 로컬 포트가 이미 닫혀 있으므로 오작동을 방지할 수 있습니다.
         */
        override fun dispose() {
            // 마크다운 브라우저 및 에디터 변경 마커는 메인 패널(pinned == null)의 전유 리소스입니다.
            // 고정 세션 탭이 닫힐 때 메인 패널의 리소스가 함께 해제되는 것을 방지합니다(리뷰 F5).
            //
            // **dispose 시점의 클래스 지연 로딩(Lazy Loading) 방지**:
            // IDE 종료 시점에 dispose가 호출될 때 플러그인 클래스로더가 이미 닫혀 있을 수 있습니다.
            // 런타임에 한 번도 참조되지 않은 클래스에 접근할 경우 NoClassDefFoundError가 발생하여 아래의 핵심 정리 로직이 누락되는 문제가 있었습니다:
            //   SEVERE ObjectTree — NoClassDefFoundError: …/RichAnswer
            //     at MagiToolWindow$View.dispose
            // 이에 따라 각 리소스 해제 호출부를 `runCatching`으로 개별 격리하여 단일 실패가 전체 정리를 중단시키지 않도록 방어합니다.
            if (pinned == null) {
                runCatching { RichAnswer.forget() }
                runCatching { EditMarkers.release() }
            }

            // 스트림 닫기 전 closing 플래그를 먼저 설정하여 ended 콜백에서의 자동 재연결 트리거를 차단합니다.
            closing.set(true)
            // 메인 패널만 전역 레지스트리와 도구 어댑터를 해제합니다(리뷰 F1·F2):
            // 고정 세션 탭에서 이를 해제하면 메인 패널, 상태 표시줄, 계획 뷰 전체의 도구 어댑터 연결이 파괴됩니다.
            if (pinned == null) runCatching { MagiWindows.remove(project) }
            debounce.stop()
            runCatching { following?.close() }
            following = null
            val server = hand ?: return
            hand = null
            runCatching { server.close() }
            // 데몬 측 어댑터 해제 요청은 Best-effort 방식으로 처리합니다.
            if (pinned == null) runCatching { workspace.onDaemon({ }, { it.detachHand() }) }
        }

        /**
         * 도구 어댑터(HandServer)를 기동하고 magi 데몬에 등록합니다.
         *
         * 등록 거절 사유 가시화:
         * 동일 워크스페이스를 여러 IDE 인스턴스로 열 경우 첫 번째 인스턴스만 등록에 성공하고 두 번째는 거절됩니다.
         * 거절 사유를 명시하지 않으면 사용자가 편집 도구 미작동 원인을 파악할 수 없으므로 상태 알림을 명확히 표시합니다(`docs/UI.ko.md` §7 시나리오 5).
         */
        private fun offerHand() {
            val server = runCatching { HandServer.start(Hand(IdeHand(project))) }.getOrNull()
                ?: return report(MagiBundle.msg("hand.noport"))
            hand = server
            onDaemon { comp ->
                val r = comp.attachHand(server.url, mapOf("X-Magi-Hand" to server.token))
                // 성공 시 별도 알림 없이 인디케이터 툴팁에 표시하며, 거절 시 사유를 사용자에게 보고합니다.
                if (r.ok) {
                    clearNotice()
                    handSaid(MagiBundle.msg("hand.tools", r.tools?.joinToString(", ") ?: MagiBundle.msg("hand.attached")))
                } else report(MagiBundle.msg("hand.failed", r.error ?: MagiBundle.msg("common.noreason")))
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
            mood(Look.error, "✕", MagiBundle.msg("chat.link.broken", why))
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
                        // 시도하는 중이라고 말한다. 백오프가 30초까지 벌어지므로, 이 말이
                        // 없으면 마지막 실패 사유가 30초 동안 「지금 상태」인 척 서 있는다.
                        mood(Look.faint, "↻", MagiBundle.msg("chat.link.connecting"))
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

        /**
         * 이 대화에서 컴패니언이 만진 파일들 — 우측 판 「변경」 구역의 원천.
         * **전사에 안 붙었으면 null**(모름)이다: 빈 목록으로 답하면 「없다」를 지어낸다.
         */
        fun touchedFiles(): List<String>? =
            if (everBegan) shaper.touchedThisTurn() else null

        /**
         * 렌더 패널의 열쇠. `msgId` 는 non-null 이라 엘비스가 죽은 가드였고(컴파일러가 이미
         * 울었다), 빈 문자열이 오는 이벤트들이 **한 열쇠를 공유해** 한 브라우저가 남의 답을
         * 그릴 자리였다(리뷰 F4).
         */

        /**
         * 흐르는 동안의 다시 그리기 — 120ms 에 한 번으로 묶되 **꼬리를 남긴다.** 그냥 버리면
         * 마지막 120ms 어치 글자는 다음 사실이 「바뀐 것 없음」을 돌려주는 턴에서 영영 안 뜬다
         * (리뷰 F5). 시계는 nanoTime — 벽시계는 뒤로 밀릴 수 있고 그러면 타자기가 언다.
         */
        private val nextDraw = java.util.concurrent.atomic.AtomicLong(0)
        private val tail = javax.swing.Timer(130) { redrawLog() }.apply { isRepeats = false }

        private fun redrawSoon() {
            val now = System.nanoTime()
            val due = nextDraw.get()
            if (now < due) { SwingUtilities.invokeLater { tail.restart() }; return }
            if (!nextDraw.compareAndSet(due, now + 120_000_000L)) {
                SwingUtilities.invokeLater { tail.restart() }
                return
            }
            redrawLog()
        }

        private fun redrawLog() {
            if (!dirty.compareAndSet(false, true)) return
            SwingUtilities.invokeLater {
                dirty.set(false)
                // 무거운 렌더는 **최근 답 몇 장**에만. 리스트 앞에서부터 캡을 먹으면 방금 온
                // 답이 부분집합으로 떨어진다(실측) — 살릴 열쇠를 먼저 정하고 행을 짓는다.
                val rows0 = shaper.list()
                // 고정 탭은 무거운 렌더를 안 세운다: 상태가 전역이라 두 판이 서로의 브라우저를
                // 갈아엎는다(리뷰 F5 — 같은 함정을 hand/등록에서 이미 한 번 겪었다).
                if (pinned == null) RichAnswer.keepOnly(
                    rows0.filter { it.who == Who.Agent && RichAnswer.needsRich(it.text) }
                        .takeLast(RichAnswer.KEEP).map { RowText.richKey(it) },
                )
                // 바닥 고정은 **바닥에 있던 사람에게만**. 무조건 고정이던 동안, 턴이 도는 중에
                // 위로 스크롤해 과거를 읽으면 새 이벤트마다 바닥으로 낚아채였다(라이브 실측 —
                // 지나간 편집을 찾아 올라가는 손이 매번 튕겼다). 떠나 있던 사람의 자리는
                // 그대로 두고, 꼬리를 따르던 사람만 계속 따르게 한다.
                val bar = scroll.verticalScrollBar
                val atBottom = bar.value + bar.visibleAmount >= bar.maximum - 48
                column.removeAll()
                // 다시 그릴 때마다 선택의 손도 새 패널을 잡는다 — 지나간 패널을 들고 있으면
                // 안 보이는 것을 칠한다. 고른 것 자체는 행 열쇠로 들고 있어 살아남는다.
                copying.beginBuild()
                shaper.list().forEach { r ->
                    val panel = rowPanel(r)
                    copying.install(panel, r) { shaper.list() }
                    column.add(panel)
                }
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
         * 단일 행 렌더링.
         * 텍스트 구성 및 데이터 모델링은 셰이퍼([Rows])가 담당하며, 이 뷰 컴포넌트는 레이아웃과 스타일링만 수행합니다(`docs/TRANSCRIPT.ko.md` §0 단일 책임 원칙).
         */
        private fun renderRow(r: Row): JBPanel<JBPanel<*>> = rowPanel(r)

        private fun rowPanel(r: Row): JBPanel<JBPanel<*>> {
            val p = JBPanel<JBPanel<*>>(BorderLayout(0, 2))
            p.border = if (r.pending) Look.pendingRow() else Look.row()
            p.isOpaque = false
            // 개별 말풍선 텍스트 복사 버튼:
            // 시각적 스타일(발화자, 실행 상태 등)은 `RowText.plain`을 통해 표준 텍스트 서식으로 직렬화하여 클립보드에 전달합니다.
            p.add(Look.copyButton(MagiBundle.msg("chat.copy.one")) { copying.copyOne(r) }, BorderLayout.EAST)
            when (r.who) {
                Who.User, Who.Agent -> {
                    val marks = buildList {
                        if (r.queued) add(MagiBundle.msg("chat.mark.queued") to Look.faint)
                        if (r.abandoned) add(MagiBundle.msg("chat.mark.dropped") to Look.muted)
                        if (r.pending) add(MagiBundle.msg("chat.mark.working") to Look.faint)
                    }
                    // 사용자 표시명 결정:
                    // SSO 플러그인 등이 `magi.set_user_label`로 등록한 값이 데몬 `status` 응답에 존재하면 우선 사용하고, 없으면 기본 라벨을 적용합니다.
                    val name = if (r.who == Who.User) (youName ?: MagiBundle.msg("chat.who.you"))
                    else MagiBundle.msg("chat.who.magi")
                    val hue = if (r.who == Who.User) Look.primary else Look.accent
                    p.add(Look.rowHead(name, hue, marks, RowText.clock(r.at)), BorderLayout.NORTH)
                    if (r.who == Who.Agent) {
                        // 에이전트 마크다운 답변 렌더링:
                        // 코드 블록, 테이블, 링크, 다이어그램 등 리치 서식이 필요한 경우에만 IDE 임베디드 브라우저 엔진([RichAnswer])을 활성화하고,
                        // 일반 텍스트는 경량 Swing 컴포넌트로 렌더링하여 프로세스 메모리를 절약합니다.
                        val rich = if (pinned == null && RichAnswer.needsRich(r.text)) {
                            RichAnswer.panel(project, r.text, RowText.richKey(r), this@View)
                        } else null
                        // 스트리밍 중인 초안은 커서 글리프(" ▌")를 붙여 완료된 텍스트와 구별합니다(리뷰 F11).
                        p.add(rich ?: Look.rich(r.text + if (r.draft) " ▌" else ""), BorderLayout.CENTER)
                    } else {
                        p.add(Look.prose(r.text), BorderLayout.CENTER)
                    }
                }
                // 추론 과정(Thinking)은 기본 접힘 상태로 렌더링하며 클릭 시 토글됩니다. 펼침 상태는 재렌더링 시에도 [opened] 집합으로 유지됩니다.
                Who.Thinking -> {
                    val long = r.text.contains('\n') || r.text.length > 120
                    val open = RowText.foldKey(r) in opened
                    if (open) {
                        p.add(Look.aside(MagiBundle.msg("chat.think") + " ⌃"), BorderLayout.NORTH)
                        p.add(Look.prose(r.text), BorderLayout.CENTER)
                    } else {
                        val head = r.text.lineSequence().firstOrNull().orEmpty().take(120)
                        p.add(Look.aside(MagiBundle.msg("chat.think") + " $head" + if (long) "  ⌄" else ""), BorderLayout.CENTER)
                    }
                    if (long) foldable(p, r)
                }
                Who.Tool -> {
                    val (glyph, hue) = when {
                        r.ok == null -> "…" to Look.faint
                        r.note -> MagiBundle.msg("chat.mark.readme") to Look.warn
                        r.ok == true -> "✓" to Look.success
                        else -> "✗" to Look.error
                    }
                    val open = RowText.foldKey(r) in opened
                    p.add(Look.toolHead(r.tool.orEmpty(), glyph, hue,
                        if (open) "⌃" else RowText.oneLine(r.args.orEmpty(), 100) + "  ⌄", RowText.clock(r.at)),
                        BorderLayout.NORTH)
                    if (open) {
                        // 펼침 상태: 도구 호출 인자 및 실행 결과 원문을 모노스페이스 폰트로 표시합니다.
                        val body = JBPanel<JBPanel<*>>().apply {
                            layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.Y_AXIS)
                            isOpaque = false
                            r.args?.let { add(Look.code(it)) }
                            r.out?.let { add(Look.code(it, Look.error)) }
                            // 파일 수정 도구 호출의 경우 이전/이후 변경 내역을 IDE Diff 뷰어로 확인할 수 있는 버튼을 제공합니다.
                            RowText.diffSides(r)?.let { (path2, old2, new2) ->
                                add(JButton(MagiBundle.msg("chat.diff.view")).apply {
                                    addActionListener {
                                        val f = com.intellij.diff.DiffContentFactory.getInstance()
                                        com.intellij.diff.DiffManager.getInstance().showDiff(
                                            project,
                                            com.intellij.diff.requests.SimpleDiffRequest(
                                                MagiBundle.msg("chat.diff.title.edit", path2),
                                                f.create(project, old2), f.create(project, new2),
                                                MagiBundle.msg("chat.diff.before"), MagiBundle.msg("chat.diff.after"),
                                            ),
                                        )
                                    }
                                })
                            }
                        }
                        p.add(body, BorderLayout.CENTER)
                    } else {
                        // 접힘 상태: 에러 발생 시 첫 줄 메시지만 요약 표기합니다.
                        r.out?.let { p.add(Look.code("↳ " + it.lineSequence().firstOrNull().orEmpty(), Look.error),
                            BorderLayout.CENTER) }
                    }
                    foldable(p, r)
                }
                Who.Council -> if (r.opened) {
                    // 카운슬 세션 라운드 개시 헤더: 개별 멤버 판정과 시각적으로 구별되도록 렌더링합니다.
                    val has = !r.evidence.isNullOrBlank()
                    val open = has && RowText.foldKey(r) in opened
                    val head = MagiBundle.msg("chat.council.round", r.round)
                    p.add(Look.rowHead("⚖ $head", Look.body,
                        if (has) listOf((if (open) "⌃" else MagiBundle.msg("chat.council.saw") + "  ⌄") to Look.faint)
                        else emptyList(),
                        RowText.clock(r.at)), BorderLayout.NORTH)
                    val body = JBPanel<JBPanel<*>>().apply {
                        layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.Y_AXIS)
                        isOpaque = false
                        if (r.text.isNotBlank()) add(Look.prose(r.text))
                        r.rule?.takeIf { it.isNotBlank() }?.let { add(Look.aside(it)) }
                        // 증거 자료(Evidence)는 대화 흐름을 가리지 않도록 기본 접힘 처리하며, 펼침 시 모노스페이스로 렌더링합니다.
                        if (open) add(Look.code(r.evidence.orEmpty()))
                    }
                    p.add(body, BorderLayout.CENTER)
                    if (has) foldable(p, r)
                } else {
                    val name = r.member ?: MagiBundle.msg("chat.who.council")
                    // 카운슬 멤버의 평가 관점([lens])을 표시하여 3인의 심의 기준을 구별합니다.
                    val lens = r.lens?.takeIf { it.isNotBlank() }?.let { " [$it]" }.orEmpty()
                    val marks = buildList {
                        // 판정 결과 텍스트는 `RowText.verdict`의 공통 어휘를 사용하며, 접근성을 위해 아이콘과 텍스트를 함께 표기합니다.
                        RowText.verdict(r.decision)?.let { v ->
                            val word = if (v.key.isBlank()) v.word else MagiBundle.msg(v.key)
                            add("${v.icon} $word" to when (r.decision) {
                                "done" -> Look.success; "continue" -> Look.warn; else -> Look.faint
                            })
                        }
                        // 멤버의 의견 미제시 상태는 무응답 마크로 명시합니다.
                        if (r.silent) add(MagiBundle.msg("chat.mark.noanswer") to Look.faint)
                    }
                    p.add(Look.rowHead("⚖ $name$lens", Look.seat(name) ?: Look.body, marks, RowText.clock(r.at)),
                        BorderLayout.NORTH)
                    val body = JBPanel<JBPanel<*>>().apply {
                        layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.Y_AXIS)
                        isOpaque = false
                        if (r.text.isNotBlank()) add(Look.prose(r.text))
                        // 심의 판정의 근거 인용(`cite`), 준수 사항(`keep`), 상세 이유(`why`)를 표시합니다:
                        // 증거 없이 승인된 판정과 근거가 명시된 판정을 시각적으로 검증할 수 있도록 지원합니다.
                        r.cite?.takeIf { it.isNotBlank() }?.let {
                            add(Look.aside(MagiBundle.msg("chat.verdict.on", it)))
                        }
                        r.keep?.takeIf { it.isNotBlank() }?.let { add(Look.aside(MagiBundle.msg("chat.verdict.keep", it))) }
                        r.why?.takeIf { it.isNotBlank() }?.let { add(Look.aside(it)) }
                    }
                    p.add(body, BorderLayout.CENTER)
                }
                Who.Info -> p.add(Look.aside(r.text), BorderLayout.CENTER)
            }
            return p
        }

        /**
         * 패널 접기/펼치기 클릭 이벤트 핸들러를 등록합니다. 상태는 [opened] 세트에 영속화됩니다.
         *
         * Swing 이벤트 전파 처리:
         * JTextArea 등 텍스트 컴포넌트가 마우스 클릭을 소비하므로, 자식 컴포넌트 전체에 재귀적으로 리스너를 바인딩합니다.
         * 마우스 드래그를 통한 텍스트 선택 동작과 충돌하지 않도록 순수 클릭(누른 위치==뗀 위치)만 처리합니다.
         */
        private fun foldable(p: JBPanel<JBPanel<*>>, r: Row) {
            val flip = object : java.awt.event.MouseAdapter() {
                override fun mouseClicked(e: java.awt.event.MouseEvent) {
                    val k = RowText.foldKey(r)
                    if (!opened.remove(k)) opened.add(k)
                    redrawLog()
                }
            }
            fun hook(c: java.awt.Component) {
                // 내부 버튼 컴포넌트(예: Diff 보기)는 접기 이벤트 대상에서 제외합니다(리뷰 F1).
                if (c is javax.swing.AbstractButton) return
                c.addMouseListener(flip)
                c.cursor = java.awt.Cursor.getPredefinedCursor(java.awt.Cursor.HAND_CURSOR)
                if (c is java.awt.Container) c.components.forEach { hook(it) }
            }
            hook(p)
            // **키보드로도 닿는다.** 접기가 마우스 전용이던 것은 설계 문서가 잔여로 적어 둔
            // 자리다 — 글리프와 커서 두 지표는 있었지만 손이 마우스를 못 쓰면 펼 길이 없었다.
            // 행에 포커스를 주고 Space·Enter 로 뒤집는다: 탭 순회로 행을 지나가며, 포커스가
            // 선 행은 테두리로 보인다(어디 있는지 안 보이는 포커스는 없는 것과 같다).
            p.isFocusable = true
            p.addKeyListener(object : java.awt.event.KeyAdapter() {
                override fun keyPressed(e: java.awt.event.KeyEvent) {
                    if (e.keyCode != java.awt.event.KeyEvent.VK_SPACE &&
                        e.keyCode != java.awt.event.KeyEvent.VK_ENTER
                    ) return
                    e.consume()
                    val k = RowText.foldKey(r)
                    if (!opened.remove(k)) opened.add(k)
                    redrawLog()
                }
            })
            val plain = p.border
            p.addFocusListener(object : java.awt.event.FocusAdapter() {
                override fun focusGained(e: java.awt.event.FocusEvent) {
                    p.border = javax.swing.BorderFactory.createCompoundBorder(
                        javax.swing.BorderFactory.createLineBorder(Look.primary, 1), plain,
                    )
                }
                override fun focusLost(e: java.awt.event.FocusEvent) { p.border = plain }
            })
        }

        /**
         * 도구 행의 나란히-보기 재료 — 판정은 core 의 한 벌([Rows.EditSides], 골든 있음)에
         * 위임한다. 여기 남는 것 하나: **적용된 행만**(ok==true) — ✗ 행에 「이전/이후」를 세우면
         * 일어나지 않은 이후를 주장한다(승인 제목을 "물음 시점/제안"으로 바꾼 그 사유).
         */

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
                        MagiBundle.msg("chat.diff.title.ok", sides.first),
                        f.create(project, sides.second), f.create(project, sides.third),
                        // 이 창은 물음 순간의 스냅샷이다 — 답이 끝난 뒤에도 "지금"을 주장하면
                        // 거짓이 된다(비대칭-통지의 그 원칙).
                        MagiBundle.msg("chat.diff.asked"), MagiBundle.msg("chat.diff.proposed"),
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


        /**
         * 한 건을 적는다. **언제·어느 호출인지가 같이 간다** — 낡은 문제 목록은 없느니만 못하다는
         * 것이 §5-4 의 요구이고, 그 답이 목록을 지우는 것이 아니라 **출처를 적는 것**이다.
         *
         * `where` 가 없으면 그대로 둔다. 못 읽은 앵커를 지어내면 엉뚱한 줄을 가리키고, 그건 항목이
         * 안 눌리는 것보다 나쁘다.
         */
        private fun note(p: Problems.Problem) = SwingUtilities.invokeLater {
            val head = MagiBundle.msg(if (p.advisory) "problems.did" else "problems.failed")
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
            push(problems, MagiBundle.msg("problems.council"), Look.faint)
            push(problems, d.member, Look.seat(d.member) ?: Look.faint, bold = true)
            push(problems, MagiBundle.msg("problems.against"), Look.body)
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
        private fun report(text: String) = SwingUtilities.invokeLater {
            notice.text = text
            notice.isVisible = true
            notice.parent?.revalidate()
        }
        private fun clearNotice() = SwingUtilities.invokeLater {
            notice.text = " "
            notice.isVisible = false
            notice.parent?.revalidate()
        }

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
                        .setTitle(MagiBundle.msg("chat.mention.title", token) +
                            if (files.size > cut.size) MagiBundle.msg("chat.mention.more", cut.size) else "")
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
            atToken()?.let { askFiles(it); return } // @멘션은 제안 스위치와 무관하다(파일 찾기다)
            if (!LocalPrefs.suggest(project)) return
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
                    hint.isVisible = suggestion != null
                    hint.parent?.revalidate()
                    hint.text = suggestion?.let { MagiBundle.msg("chat.suggest", Markup.text(it)) } ?: " "
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
            hint.isVisible = false
            hint.parent?.revalidate()
        }

        private fun socket() = workspace.socket()

        /**
         * 상태 표시줄이 묻는 두 사실 — 턴이 언제 열렸고 카운슬이 몇 라운드인가. 원천은 전사
         * 셰이퍼라 이 창이 살아 있는 동안만 답이 있다. 셰이퍼 자체를 내주지 않는 것은 문을
         * 좁게 두는 것이다 — 넓은 문은 언젠가 딴것도 지나간다.
         */
        fun turnOpenedAt(): String? = if (shaper.open) shaper.openedAt else null
        fun councilRound(): Int? = shaper.councilRound

        /** 지금 누구에게 묻는 중인가 — 라운드 옆에 서면 「멈춘 것」과 「기다리는 것」이 갈린다. */
        fun councilAsking(): String? = shaper.councilAsking

        /**
         * 계획 판이 묻는다. null = **모른다**(전사에 한 번도 못 붙었다), 빈 목록 = 계획이 없다 —
         * 둘을 한 값으로 뭉치면 화면이 모르는 것을 아는 척한다(§0.5-7).
         */
        fun plan(): List<Rows.Todo>? = if (everBegan) shaper.todos else null
        fun modelNow(): String? = shaper.model
        fun contextNow(): Rows.Ctx? = shaper.context

        /**
         * 컴패니언 런타임 환경 조회 및 전환 액션.
         * 웹 콘솔의 컴패니언 정보 카드와 동일한 기능을 도구 창 타이틀 바에서 바로 접근할 수 있도록 제공합니다.
         *
         * 공간 최적화 설계:
         * 과거 전사 패널 상단에 상시 배치되던 고정 라벨이 화면 공간을 불필요하게 점유하던 문제를 개선하여,
         * 상시 표시는 상태 표시줄 및 연결 인디케이터로 최소화하고 상세 정보와 제어 기능은 타이틀 바 팝업 액션으로 접어 두었습니다.
         *
         * 카드 표시 및 제어 항목:
         * - 읽기: 연결 상태(인디케이터 색상 및 글리프), 버전(`about`의 `version` 필드, 2026-09-09 실측 반영), 모델, 백엔드.
         * - 쓰기: 모델, 백엔드, 승인 모드 전환(데몬이 반환한 유효 목록 기반 선택).
         * - 파괴적 액션 보호: `restart`, `update`, `compact` 등 진행 중인 턴을 중단시킬 수 있는 명령은 실행 전 확인 다이얼로그를 거칩니다.
         */
        fun infoAction(): com.intellij.openapi.actionSystem.AnAction =
            object : com.intellij.openapi.actionSystem.AnAction(
                MagiBundle.msg("chat.info"), MagiBundle.msg("chat.info.tip"),
                com.intellij.icons.AllIcons.Actions.Properties) {
                override fun actionPerformed(e: com.intellij.openapi.actionSystem.AnActionEvent) {
                    val seat = e.inputEvent?.component
                    // 필요한 4개 상태(facts, about, models, profiles)를 단일 비동기 블록에서 일괄 조회합니다.
                    // UI 오픈 후 비동기로 개별 수신할 경우 콤보박스가 깜빡이며 사용자의 선택을 덮어쓰는 레이스 컨디션을 방지합니다.
                    onDaemon { comp ->
                        val f = comp.facts()
                        val ver = comp.about().version
                        // 모델 목록 타임아웃 처리:
                        // 코어 백엔드가 5초 내 응답하지 못할 경우 `ok=true`와 함께 빈 목록 및 `why` 사유가 반환됩니다("no menu is a better answer than a stuck one").
                        // 단순 `ok` 여부만 확인할 경우 실패 사유가 유실되므로 `why` 메시지를 추출하여 콤보박스 자리에 표시합니다.
                        val models = comp.models().let {
                            if (it.ok && it.why == null) it.models.orEmpty() to null
                            else emptyList<String>() to (it.why ?: it.error)
                        }
                        val backs = comp.profiles().let {
                            if (it.ok) it.profiles.orEmpty().mapNotNull { p -> p.name } to null
                            else emptyList<String>() to it.error
                        }
                        SwingUtilities.invokeLater { showInfo(seat, f, ver, models, backs) }
                    }
                }
            }

        /** 정보 팝업 카드를 생성하고 표시합니다. 조회된 데이터를 기반으로 EDT에서 순수 렌더링만 수행합니다. */
        private fun showInfo(
            under: java.awt.Component?,
            f: Companion.Facts,
            version: String?,
            models: Pair<List<String>, String?>,
            backends: Pair<List<String>, String?>,
        ) {
            val m = mood
            val card = JBPanel<JBPanel<*>>(java.awt.GridBagLayout()).apply { border = Look.quiet }
            val c = java.awt.GridBagConstraints().apply {
                gridx = 0; gridy = 0; anchor = java.awt.GridBagConstraints.WEST
                insets = com.intellij.util.ui.JBUI.insets(3, 0, 3, 8)
            }
            fun row(label: String, right: javax.swing.JComponent) {
                c.gridx = 0; c.weightx = 0.0; c.fill = java.awt.GridBagConstraints.NONE
                card.add(JBLabel(label).apply { foreground = Look.faint }, c)
                c.gridx = 1; c.weightx = 1.0; c.fill = java.awt.GridBagConstraints.HORIZONTAL
                card.add(right, c)
                c.gridy++
            }
            // 연결 상태 표시 — 도구 창 인디케이터와 동일한 Mood 스냅샷을 사용하여 일관성을 유지합니다.
            row(MagiBundle.msg("chat.info.state"), JBLabel(m.glyph + "  " + m.why).apply { foreground = m.colour })
            // 버전 표시 — 데몬 응답이 누락된 경우 임의 추정 대신 미수신 안내 문구를 표시합니다(`docs/UI.ko.md` §0.5-7).
            row(MagiBundle.msg("chat.info.version"), JBLabel(version?.ifBlank { null } ?: MagiBundle.msg("set.unsaid")))

            /** 선택 가능한 목록이 있을 때 콤보박스를 구성하고, 비어 있을 때는 사유 라벨을 표시합니다. */
            fun picker(
                now: String?,
                listed: Pair<List<String>, String?>,
                draw: (String?) -> String = { it.orEmpty() },
                send: (Companion, String) -> Response,
            ): javax.swing.JComponent {
                val (choices, whyNot) = listed
                // 선택 가능한 목록이 없는 경우 비활성 콤보박스 대신 사유를 명시합니다.
                if (choices.isEmpty()) return JBLabel(
                    (now?.let(draw)?.ifBlank { null } ?: MagiBundle.msg("set.unsaid")) +
                        (whyNot?.lineSequence()?.first()?.take(80)?.ifBlank { null }
                            ?.let { "  — " + MagiBundle.msg("chat.info.nolist", it) } ?: "")
                ).apply { if (whyNot != null) foreground = Look.warn }
                val box = Look.narrowCombo<String>()
                // 모델 식별자 토큰을 데이터로 보유하고 표시는 렌더러를 통해 사용자 친화 텍스트로 변환합니다.
                box.renderer = com.intellij.ui.SimpleListCellRenderer.create<String> { label, value, _ ->
                    label.text = draw(value)
                }
                // 현재 설정된 값이 목록에 없는 경우 임시 항목으로 추가하여,
                // Swing ComboBox가 첫 번째 항목으로 강제 재설정되며 의도치 않은 변경 요청을 전송하는 결함을 방지합니다(리뷰 R6).
                (choices + listOfNotNull(now?.takeIf { it.isNotBlank() && it !in choices })).forEach { box.addItem(it) }
                box.selectedItem = now
                var painting = true
                box.addActionListener {
                    if (painting) return@addActionListener
                    val pick = box.selectedItem as? String ?: return@addActionListener
                    onDaemon { comp ->
                        val r = send(comp, pick)
                        if (r.ok) clearNotice() else report(MagiBundle.msg("chat.notsent", pick, r.error ?: MagiBundle.msg("common.noreason")))
                    }
                }
                painting = false
                return box
            }
            row(MagiBundle.msg("chat.info.model"), picker(f.model, models) { comp, v -> comp.setModel(v) })
            row(MagiBundle.msg("chat.info.backend"), picker(f.backend, backends) { comp, v -> comp.useBackend(v) })
            row(MagiBundle.msg("chat.info.permission"),
                // 승인 모드 목록은 프로토콜 상수이므로 데몬 질의 없이 정적 정의를 사용합니다.
                picker(f.permission, Perms.TOKENS to null, Perms::label) { comp, v -> comp.setPermission(v) })

            val acts = JBPanel<JBPanel<*>>(java.awt.FlowLayout(java.awt.FlowLayout.LEFT, 6, 0))
            var popup: com.intellij.openapi.ui.popup.JBPopup? = null
            /** 카드 액션 버튼. [confirm] 메시지가 정의된 경우 실행 전 확인 다이얼로그를 띄웁니다. */
            fun act(label: String, confirm: String?, door: (Companion) -> Response) {
                acts.add(JButton(label).apply {
                    addActionListener {
                        if (confirm != null && !com.intellij.openapi.ui.MessageDialogBuilder
                                .yesNo(label, confirm).ask(project)) return@addActionListener
                        popup?.cancel()
                        onDaemon { comp ->
                            val r = door(comp)
                            // 처리 결과 메시지는 데몬 응답 본문을 반영합니다.
                            if (r.ok) report(r.out?.lineSequence()?.first()?.take(120)?.ifBlank { null } ?: label)
                            else report(MagiBundle.msg("chat.notsent", label, r.error ?: MagiBundle.msg("common.noreason")))
                        }
                    }
                })
            }
            act(MagiBundle.msg("chat.menu.compact"), MagiBundle.msg("chat.info.compact.ask")) { it.compact() }
            act(MagiBundle.msg("chat.info.restart"), MagiBundle.msg("chat.info.restart.ask")) { it.restart() }
            act(MagiBundle.msg("chat.info.update"), MagiBundle.msg("chat.info.update.ask")) { it.update() }
            c.gridx = 0; c.gridwidth = 2; c.weightx = 1.0
            c.fill = java.awt.GridBagConstraints.HORIZONTAL
            card.add(acts, c)

            popup = com.intellij.openapi.ui.popup.JBPopupFactory.getInstance()
                .createComponentPopupBuilder(card, null)
                .setTitle(MagiBundle.msg("chat.info"))
                .setRequestFocus(true)
                .createPopup()
            if (under != null && under.isShowing) popup.showUnderneathOf(under) else popup.showInFocusCenter()
        }

        /**
         * 기어 메뉴의 동사 하나. 답을 버리지 않는 것은 [add] 와 같은 규칙이고, 보고가 전사로
         * 가는 것은 [report] 의 규칙이다 — 단추마다 한 벌씩 다시 적지 않으려고 여기로 접는다.
         */
        fun verb(label: String, act: (Companion) -> Response): com.intellij.openapi.actionSystem.AnAction =
            object : com.intellij.openapi.actionSystem.AnAction(label) {
                override fun actionPerformed(e: com.intellij.openapi.actionSystem.AnActionEvent) =
                    onDaemon { comp ->
                        val r = act(comp)
                        if (r.ok) clearNotice() else report(MagiBundle.msg("chat.notsent", label, r.error ?: MagiBundle.msg("common.noreason")))
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
        /** 데몬이 말해 준 사람 이름. 아무도 안 바꿨으면 null 이고, 그때는 낙하 낱말을 쓴다. */
        @Volatile private var youName: String? = null

        private fun redraw(comp: Companion) {
            youName = comp.facts().user
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
         * **"세웠다"고 안 한다.** 코어의 `internal/app/app.go` 의 `Interrupt` 는 도는 턴이 없어도 `nil` 을
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
            if (r.ok) clearNotice() else report(MagiBundle.msg("chat.notsent", MagiBundle.msg("chat.stop"), r.error ?: MagiBundle.msg("common.noreason")))
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
                                MagiBundle.msg("chat.busy.body", mine.session?.takeLast(6) ?: ""),
                                MagiBundle.msg("chat.busy.title"), MagiBundle.msg("chat.busy.yes"), MagiBundle.msg("chat.busy.no"), null,
                            ) == com.intellij.openapi.ui.Messages.YES
                        }
                        if (!go) return@onDaemon
                    }
                }
                // 턴이 열려 있나는 **이 창이 아는 사실**이다 — 전사를 흘려보며 답 없는
                // prompt.submitted 가 서 있는지 세고 있다(`Rows.open`). 데몬에게 묻는
                // 탐침은 도는 턴 대부분을 놓치므로(Companion.turnIsOpen 주석) 여기서 준다.
                val r = comp.say(text, carry, shaper.open)
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
                } else report(MagiBundle.msg("common.notsent", r.error ?: MagiBundle.msg("common.noreason")))
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
                // **언제 선 물음인가.** 코어가 `Waiting.since` 로 늘 보내는 값이고(그 구조체에서
                // omitempty 가 없는 유일한 칸), 안 읽고 있었다. 아무도 없을 때 선 물음과 방금 선
                // 물음이 똑같이 보이면, 사람은 자리를 비운 사이 턴이 멈춰 서 있었다는 것을 모른다.
                val asked = RowText.asked(w.since).let { if (it.isEmpty()) "" else " " + MagiBundle.msg("chat.perm.asked", it) }
                // **무엇을 근거로 묻는지 같이 보인다.** 결정보고 스킬이 모은 것이고, 근거가 뒤에
                // 남은 프롬프트를 막으려고 전선을 타는 값이다(코어 `Waiting.Report` 주석).
                val grounds = w.report.orEmpty()
                    .filter { it.text.isNotBlank() }
                    .joinToString("") { "<br/><b>${Markup.text(it.key)}:</b> ${Markup.text(it.text)}" }
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
                    Subject.Unstated -> "<i>" + Markup.text(MagiBundle.msg("chat.perm.unknown")) + "</i>"
                }
                prompt.text = "<html><b>${Markup.text(w.what)}</b>$at<span>${Markup.text(asked)}</span><br/>$subject$grounds$why</html>"
                when (ask) {
                    is Ask.Permission -> {
                        add(MagiBundle.msg("chat.perm.allow")) { it.allow(w.id) }
                        add(MagiBundle.msg("chat.perm.deny")) { it.deny(w.id) }
                        add(MagiBundle.msg("chat.perm.always")) { it.always(w.id) }
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
                    buttons.add(JButton(MagiBundle.msg("chat.change.view")).apply {
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
         * 답도 MagiBundle.msg("chat.gone")로 온다(`internal/app/app.go` 의 `RespondQuestion`). 그 문장을 고치는 일이
         * 논의 중인데, 받는 쪽이 버리고 있으면 고쳐 봐야 아무 데도 안 닿는다.
         */
        private fun add(label: String, act: (Companion) -> Response) {
            buttons.add(JButton(label).apply {
                addActionListener {
                    onDaemon { c ->
                        val r = act(c)
                        // 성공이 지운다 — say 하나에만 걸면 만료 프롬프트의 "안 갔다"가 다음
                        // 성공 뒤에도 지금 것처럼 서 있는다(이 유닛이 없애려던 그 무늬).
                        if (r.ok) clearNotice() else report(MagiBundle.msg("common.notsent", r.error ?: MagiBundle.msg("common.noreason")))
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
            // 사람이 할 일이 생기는 못-닿음만 점으로 올린다(§0.5-7: 무통보 무동작 금지).
            // 나머지 수준은 상태 표시줄과 링크 점이 이미 말한다.
            if (l is Level.Unreachable) mood(Look.error, "✕", l.why)
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
/**
 * 대화 하나를 하단 독의 **고정 탭**으로 연다.
 *
 * 기어 메뉴의 세션 피커가 하던 일을 여기로 꺼냈다. 부르는 데가 둘이 됐기 때문이다 — 피커와,
 * 「계획」 판의 서브에이전트 줄. 두 벌로 두면 한쪽만 고쳐지고, 그 한쪽이 스트림을 안 닫는
 * 쪽이면 새는 것은 화면이 아니라 소켓이다(이 창이 이미 한 번 겪은 결함).
 *
 * 이미 그 대화의 탭이 열려 있으면 **새로 만들지 않고 그것을 고른다.** 탭을 세션 id 로 알아보는
 * 것이지 이름으로 알아보지 않는다: 이름은 id 의 꼬리 여섯 자라 서로 다른 두 대화가 같은 이름을
 * 달 수 있고, 그때 이름으로 찾으면 남의 탭을 고른다.
 */
internal object MagiTabs {
    private val SID = com.intellij.openapi.util.Key.create<String>("magi.tab.session")

    fun open(project: Project, sid: String, label: String) {
        val tw = com.intellij.openapi.wm.ToolWindowManager.getInstance(project)
            .getToolWindow("magi") ?: return
        tw.activate({
            val cm = tw.contentManager
            cm.contents.firstOrNull { it.getUserData(SID) == sid }?.let {
                cm.setSelectedContent(it)
                return@activate
            }
            val tab = MagiToolWindow.View(project, pinned = sid)
            val content = ContentFactory.getInstance().createContent(tab.root, label, false)
            content.isCloseable = true
            content.putUserData(SID, sid)
            // 탭이 닫히면 스트림도 닫힌다 — 고아 스트림 금지.
            com.intellij.openapi.util.Disposer.register(content, tab)
            cm.addContent(content)
            cm.setSelectedContent(content)
            tab.refresh()
        }, true)
    }
}

internal object MagiWindows {
    private val live = java.util.WeakHashMap<Project, MagiToolWindow.View>()
    fun put(project: Project, view: MagiToolWindow.View) = synchronized(live) { live[project] = view }
    fun of(project: Project): MagiToolWindow.View? = synchronized(live) { live[project] }
    fun remove(project: Project) = synchronized(live) { live.remove(project) }
}
