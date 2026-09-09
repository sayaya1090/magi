package dev.sayaya.magi.ide.ui

import dev.sayaya.magi.ide.usecase.RowText
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Disposer
import com.intellij.openapi.wm.ToolWindow
import com.intellij.openapi.wm.ToolWindowFactory
import com.intellij.openapi.wm.ex.ToolWindowManagerListener
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.content.ContentFactory
import com.intellij.util.ui.JBUI
import com.intellij.openapi.application.ApplicationManager
import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.CronRow
import dev.sayaya.magi.ide.model.RosterRow
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.model.SessionRow
import java.awt.BorderLayout
import javax.swing.BoxLayout
import javax.swing.JButton
import javax.swing.JComboBox
import javax.swing.SwingUtilities
import javax.swing.Timer

/**
 * 우측 도구 창 — 작업 계획 및 상태 모니터링 패널입니다.
 * UI 배치 기준(2026-08-29 결정): 설정 창보다는 자주 접근하지만 메인 대화 창보다는 빈도가 낮은 보조 제어 컴포넌트들을 배치합니다.
 * 상단부터 순서대로 작업 계획(Plan), 활성 작업(Tasks), 변경된 파일(Changes), 플릿(Fleet), 타 컴패니언 요청(Requests),
 * 예약 실행(Schedule), 컨텍스트 사용량(Usage), 세션/모델 제어(Controls)를 표시합니다.
 *
 * 데이터 소스는 이원화되어 있습니다:
 * - 트랜스크립트 스트림([Rows]): 계획, 모델, 컨텍스트 사용량 정보(과도기 전이 이벤트는 재연결 시 0%로 추정하지 않고 대기 상태로 유지).
 * - 데몬 HTTP 엔드포인트(`jobs`, `roster`, `sessions`, `session-new`, `set-model`, `compact`): 외부 작업 및 세션 제어.
 * - 폴링 정책: 도구 창 가시화 중 3초 간격 주기적 폴링 + 패널 확장 시 즉시 갱신을 수행합니다.
 */
class PlanToolWindow : ToolWindowFactory {
    /**
     * 프로젝트별 도구 창 유효성을 검사합니다(UI Guidelines · Tool window:
     * "don't display the button when the window doesn't apply to the project setup").
     *
     * 가벼운 검사만 수행합니다:
     * 데몬 실행 여부를 기준으로 삼으면 IDE 기동 후 데몬을 실행하는 일반적인 흐름에서 도구 창 버튼이 노출되지 않는 문제가 발생합니다.
     * 유효한 워크스페이스 디렉터리 경로가 존재하는지만 확인합니다.
     */
    override fun shouldBeAvailable(project: Project) = project.basePath != null

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val workspace = Workspace(project)
        // 수직 박스 레이아웃 패널 — 자식 컴포넌트의 좌측 정렬(alignmentX = 0f)을 강제합니다.
        // BoxLayout(Y_AXIS)의 기본 alignmentX가 0.5(중앙 정렬)여서 폭이 고정된 컴포넌트(JBLabel 등)가 중앙으로 밀려
        // 좌측에 불필요한 공백 마진이 발생하던 문제를 방지하기 위해 `addImpl`에서 좌측 정렬을 일괄 강제합니다(사용자 실측 피드백 및 리뷰 반영).
        fun stack(top: Int, side: Int): JBPanel<JBPanel<*>> = object : JBPanel<JBPanel<*>>() {
            init {
                layout = BoxLayout(this, BoxLayout.Y_AXIS)
                if (side > 0 || top > 0) border = JBUI.Borders.empty(top, side)
            }
            override fun addImpl(comp: java.awt.Component, constraints: Any?, index: Int) {
                (comp as? javax.swing.JComponent)?.alignmentX = 0f
                super.addImpl(comp, constraints, index)
            }
        }
        val plan = stack(8, 12)
        val work = stack(0, 12)
        val changes = stack(0, 12)
        val fleet = stack(0, 12)
        val cronPane = stack(0, 12)
        val askedPane = stack(0, 12)
        val ctx = JBLabel(" ").apply { foreground = Look.faint; border = JBUI.Borders.empty(2, 12) }
        /**
         * 컨텍스트 토큰 구성 요소 렌더링.
         * JBLabel은 줄바꿈 문자(\n)를 처리하지 않으므로, 다중 행 텍스트 유실을 방지하기 위해 [Look.flow] 패널로 감싸 개별 라벨로 표시합니다.
         */
        val ctxParts = Look.flow().apply { border = JBUI.Borders.empty(0, 12, 2, 12) }
        // 긴 세션 제목으로 인해 도구 창 가로 폭이 비정상적으로 확장되지 않도록 제한된 너비 콤보박스를 사용합니다.
        val talk = Look.narrowCombo<String>()
        val model = Look.narrowCombo<String>(16)
        // 비동기 작업 알림 라벨.
        val said = JBLabel(" ").apply { foreground = Look.faint; border = JBUI.Borders.empty(2, 12) }
        // 만료된 데이터 경고 라벨: 폴링 실패 시 기존 캐시된 데이터를 완전히 지우지 않고 유지하되, 데이터가 최신이 아님을 사용자에게 명시합니다.
        val stale = JBLabel(MagiBundle.msg("plan.stale")).apply {
            foreground = Look.warn
            border = JBUI.Borders.empty(2, 12)
            isVisible = false
        }
        fun tell(t: String) = SwingUtilities.invokeLater { said.text = t }

        // UI 렌더링 중 발생하는 콤보박스 선택 변경 이벤트가 백엔드 API를 재호출하지 않도록 차단하는 가드 플래그입니다.
        var painting = false
        // 폴링 응답 정합성 시퀀스 번호: 네트워크 지연으로 인해 지연 수신된 이전 폴링 응답이 최신 폴링 결과를 덮어쓰지 않도록 검증합니다(리뷰 반영).
        val pollSeq = java.util.concurrent.atomic.AtomicLong()

        /**
         * 외부 컴패니언에 위임한 요청 및 접수증 관리 모델.
         * 제어 요청은 대상 컴패니언의 유닉스 도메인 소켓으로 직접 전달됩니다(`docs/CLIENTS.ko.md` §2).
         */
        class Asked(val socket: String, val who: String, val receipt: String, val ask: String) {
            /** 마지막으로 수신된 상태 메시지 (종료 후에도 캐시된 값 표시). */
            @Volatile var line: String? = null
            /** 추가 상태 질의 중단 플래그 (작업 완료 또는 접수증 만료 시 true). */
            @Volatile var over = false
            /** 응답 수신 완료 여부 플래그 (텍스트 파싱 대신 명시적 불리언 필드로 상태를 추적하여 다국어 변경 시 오작동 방지). */
            @Volatile var answered = false
        }
        val asked = java.util.Collections.synchronizedList(mutableListOf<Asked>())
        // 내부 함수 순환 참조 연결 핸들러.
        var refreshTalks: () -> Unit = {}
        // 인텔리제이 플랫폼의 `ToolWindowFactory`는 애플리케이션 싱글톤이므로,
        // 상태 변수를 클래스 필드로 선언하면 멀티 프로젝트 환경에서 데이터 간섭 및 Project 리소스 누수가 발생합니다(리뷰 F2). 따라서 로컬 변수로 스코프를 제한합니다.
        var askOf: (RosterRow) -> Unit = {}
        var paintAsked: (Long) -> Unit = {}
        /** 세션 표시 문자열("제목 (s_…id6)")에서 실제 세션 ID를 매핑하는 역조회 테이블. */
        var talkIds: Map<String, String> = emptyMap()

        model.addActionListener {
            if (painting) return@addActionListener
            val pick = model.selectedItem as? String ?: return@addActionListener
            workspace.onDaemon({ tell(MagiBundle.msg("common.failed", it)) }) { comp ->
                val r = comp.setModel(pick)
                tell(if (r.ok) MagiBundle.msg("plan.model.changed", pick) else MagiBundle.msg("common.notsent", r.error ?: MagiBundle.msg("common.noreason")))
            }
        }
        talk.addActionListener {
            if (painting) return@addActionListener
            val label = talk.selectedItem as? String ?: return@addActionListener
            val id = talkIds[label] ?: return@addActionListener
            workspace.onDaemonWithoutChat({ tell(MagiBundle.msg("common.failed", it)) }) { comp ->
                // 「이미 그 대화다」는 지금 대화를 알 때만 답이 있다. 모르면 그냥 갈아탄다 —
                // 모르는 것을 아는 척하지 않는다(대화가 없으면 「이미」도 없다).
                if (comp.facts().session == id) return@onDaemonWithoutChat
                val r = comp.resume(id)
                // 갈아타기의 나머지 절반은 대화 창이 한다 — session.moved 를 보고 새 대화에 붙는다.
                tell(if (r.ok) MagiBundle.msg("plan.session.moved", id.takeLast(6)) else MagiBundle.msg("common.notsent", r.error ?: MagiBundle.msg("common.noreason")))
            }
        }
        val fresh = JButton(MagiBundle.msg("plan.new")).apply {
            addActionListener {
                workspace.onDaemonWithoutChat({ tell(MagiBundle.msg("common.failed", it)) }) { comp ->
                    val r = comp.newSession()
                    // 턴이 도는 중이면 데몬이 거부한다 — 인터럽트 먼저라는 계약을 그대로 보인다.
                    tell(if (r.ok) MagiBundle.msg("plan.session.new", r.session?.takeLast(6) ?: "") else MagiBundle.msg("common.notsent", r.error ?: MagiBundle.msg("common.noreason")))
                    if (r.ok) refreshTalks() // 동사 뒤엔 목록이 낡았다 — 새 대화가 콤보에 서야 한다
                }
            }
        }
        val compact = JButton(MagiBundle.msg("plan.compact")).apply {
            addActionListener {
                workspace.onDaemon({ tell(MagiBundle.msg("common.failed", it)) }) { comp ->
                    val r = comp.compact()
                    tell(if (r.ok) MagiBundle.msg("plan.compact.sent") else MagiBundle.msg("common.notsent", r.error ?: MagiBundle.msg("common.noreason")))
                }
            }
        }

        val controls = stack(0, 0).apply {
            add(stale)
            add(Look.gutter(MagiBundle.msg("plan.tasks")))
            add(work)
            add(Look.gutter(MagiBundle.msg("plan.changes")))
            add(changes)
            add(Look.gutter(MagiBundle.msg("plan.companions")))
            add(fleet)
            add(Look.gutter(MagiBundle.msg("plan.requests")))
            add(askedPane)
            add(Look.gutter(MagiBundle.msg("plan.schedule")))
            add(cronPane)
            add(Look.gutter(MagiBundle.msg("plan.usage")))
            add(ctx)
            add(ctxParts)
            add(Look.gutter(MagiBundle.msg("plan.controls")))
            add(JBPanel<JBPanel<*>>(BorderLayout(8, 0)).apply {
                border = JBUI.Borders.empty(2, 12)
                add(talk, BorderLayout.CENTER)
                add(fresh, BorderLayout.EAST)
            })
            add(JBPanel<JBPanel<*>>(BorderLayout()).apply {
                border = JBUI.Borders.empty(2, 12)
                add(model, BorderLayout.CENTER)
            })
            add(JBPanel<JBPanel<*>>(BorderLayout()).apply {
                border = JBUI.Borders.empty(4, 12)
                add(compact, BorderLayout.WEST)
            })
            add(said)
        }
        val root = JBPanel<JBPanel<*>>(BorderLayout()).apply {
            add(plan, BorderLayout.CENTER)
            add(controls, BorderLayout.SOUTH)
        }
        toolWindow.contentManager.addContent(
            ContentFactory.getInstance().createContent(JBScrollPane(root), null, false)
        )

        // 스트림이 준 것(계획·컨텍스트·모델)을 그린다 — 데몬 왕복 없음.
        fun refresh() = SwingUtilities.invokeLater {
            // 「변경」 — 이 대화에서 컴패니언이 만진 파일들(사용자 요구: 파일별 diff 리뷰).
            // 목록은 셰이퍼의 변경 대장, diff 는 IDE 의 VCS 가 그린다 — 클릭이 문이다.
            changes.removeAll()
            val touched = MagiWindows.of(project)?.touchedFiles()
            if (touched == null) {
                // 모름과 없음을 가른다 — 옆의 「계획」이 같은 자리에서 그렇게 한다.
                changes.add(Look.aside(MagiBundle.msg("plan.changes.wait")))
            } else if (touched.isEmpty()) {
                changes.add(Look.aside(MagiBundle.msg("plan.changes.none")))
            } else touched.forEach { rel ->
                changes.add(JBLabel(rel).apply {
                    border = JBUI.Borders.empty(1, 2)
                    cursor = java.awt.Cursor.getPredefinedCursor(java.awt.Cursor.HAND_CURSOR)
                    toolTipText = MagiBundle.msg("plan.changes.tip")
                    addMouseListener(object : java.awt.event.MouseAdapter() {
                        override fun mouseClicked(e: java.awt.event.MouseEvent) {
                            // diff 는 우리가 안 그린다: 파일을 열고 IDE 라인 트래커를 세우면
                            // 거터 막대와 인라인 diff 팝업(되돌리기 포함)이 IDE 것으로 선다
                            // (사용자 교정: 「편집 인터페이스에 바로 그릴 수 있잖아」).
                            // 실패는 show() 안에서 말한다 — 여기 폴백은 풀드 스레드
                            // 안의 예외를 못 봐 죽은 코드였다(리뷰 F11).
                            EditMarkers.show(project, rel)
                        }
                    })
                })
            }
            changes.revalidate(); changes.repaint()

            plan.removeAll()
            val v = MagiWindows.of(project)
            val steps = v?.plan()
            when {
                steps == null -> plan.add(JBLabel(MagiBundle.msg("plan.plan.wait")).apply {
                    foreground = Look.faint
                })
                steps.isEmpty() -> plan.add(JBLabel(MagiBundle.msg("plan.plan.none")).apply {
                    foreground = Look.faint
                })
                else -> {
                    val done = steps.count { it.status == "completed" }
                    plan.add(Look.gutter(MagiBundle.msg("plan.plan.count", done, steps.size)))
                    steps.forEach { t ->
                        val glyph = when (t.status) {
                            "completed" -> "✓"
                            "in_progress" -> "◐"
                            else -> "○"
                        }
                        plan.add(JBLabel("$glyph  ${t.content}").apply {
                            foreground = if (t.status == "completed") Look.faint else Look.body
                            border = JBUI.Borders.empty(2, 0)
                        })
                    }
                }
            }
            // **문을 먼저 묻는다.** 같은 사실이 스트림의 `context.usage` 로도 오지만 그것은
            // transient 라 재생이 없다 — 도는 대화에 붙은 창은 턴이 한 번 돌기 전까지 아무것도
            // 못 봤다. 문은 지금 답한다. 스트림은 낙하이고, 문 없는 데몬에서는 그것이 유일한
            // 원천이다. (모름을 0% 로 그리지 않는다는 규칙은 그대로다 — 둘 다 없으면 안 적는다.)
            val seen = ctxFromDoor ?: v?.contextNow()
            ctx.text = seen?.let {
                MagiBundle.msg("plan.usage.ctx", "%.0f%%  (%s/%s)".format(it.percent, k(it.tokens), k(it.window)))
            } ?: MagiBundle.msg("plan.usage.none")
            // 컨텍스트 구성비([makeup]) 및 요약 압축 이력 표시:
            // 단순 압축 횟수뿐만 아니라 보존된 핵심 토픽 목록을 함께 표시하여,
            // 대화가 압축되었을 때 유지되고 있는 맥락을 사용자가 명확히 인지할 수 있도록 지원합니다.
            ctxParts.text = listOf(
                makeup(seen?.parts),
                // 요약 압축 횟수 및 보존된 토픽 정보:
                // 다회 압축된 세션의 경우 초기 판단 컨텍스트가 생략되었을 수 있으므로 압축 사실을 먼저 명시합니다.
                seen?.compactions?.takeIf { it > 0 }
                    ?.let { MagiBundle.msg("plan.usage.folded", it) }.orEmpty(),
                seen?.topics?.takeIf { it.isNotEmpty() }
                    ?.let { MagiBundle.msg("plan.usage.kept", it.joinToString(", ")) }.orEmpty(),
            ).filter { it.isNotBlank() }.joinToString("\n")
            ctxParts.isVisible = ctxParts.text.isNotBlank()
            v?.modelNow()?.let { now ->
                painting = true
                if ((0 until model.itemCount).none { model.getItemAt(it) == now }) model.addItem(now)
                model.selectedItem = now
                painting = false
            }
            plan.revalidate(); plan.repaint()
        }

        // 데몬 HTTP 폴링 루틴 — 스레드 풀에서 비동기 조회하고 EDT에서 렌더링합니다.
        // 연결 장애 시 반복적인 팝업 알림으로 인한 방해를 방지하기 위해 폴링 실패는 상시 상태 표시줄에 위임하고 도구 창에는 경고 라벨만 노출합니다.
        fun poll() {
            val my = pollSeq.incrementAndGet()
            // 3초 주기 폴링: 장기 블로킹 방지를 위해 짧은 타임아웃을 적용하여 비응답 데몬으로 인한 스레드 적체를 방지합니다.
            workspace.onDaemonPolling({ if (my == pollSeq.get()) SwingUtilities.invokeLater { stale.isVisible = true } }) { comp ->
            val jr = comp.jobs()
            val j = jr.jobs
            val r = comp.roster()
            val cr = comp.cron()
            // 기능 지원 여부(Capability)가 확인된 경우에만 해당 엔드포인트를 호출합니다.
            if (canAskContext) {
                val asked = runCatching { comp.context() }.getOrNull()?.takeIf { it.ok }?.context
                // 윈도우 크기가 0인 경우는 미측정 상태이므로 0%로 추정 렌더링하지 않습니다.
                ctxFromDoor = asked?.takeIf { it.window > 0 }
                    ?.let {
                        dev.sayaya.magi.ide.usecase.Rows.Ctx(
                            it.used, it.window, it.used * 100.0 / it.window, it.parts, it.topics,
                            it.compactions,
                        )
                    }
            }
            // 데몬 런타임 기능(Capabilities)은 1회만 조회하여 캐시합니다.
            if (!capsRead) {
                val caps = comp.about().caps.orEmpty()
                capsRead = true
                canEditCron = caps.contains("cron-set")
                canAskContext = caps.contains("context")
            }
            // 완료된 서브에이전트 목록 조회 (하위 호환성: 해당 엔드포인트를 미지원하는 구버전 데몬에서는 실행 중인 작업만 표시).
            val past = comp.children().children.orEmpty()
            SwingUtilities.invokeLater {
                if (my != pollSeq.get()) return@invokeLater // 이전 폴링 주기의 지연 응답은 폐기합니다
                stale.isVisible = false
                work.removeAll()
                val queued = j?.queued.orEmpty()
                val bgRunning = j?.background.orEmpty().filter { it.running }
                // 비정상 종료된 백그라운드 명령 필터링:
                // 성공한 명령은 공간 절약을 위해 생략하고, 강제 종료되었거나 0이 아닌 종료 코드로 끝난 실패 작업(`killed || exit != 0`)만 원인과 함께 표시합니다.
                val bgBad = j?.background.orEmpty()
                    .filter { !it.running && (it.killed || it.exit != 0) }.take(pastKids)
                val kids = j?.children.orEmpty().filter { it.running }
                // 구버전 데몬 버전 스큐(Version skew) 분기:
                // jobs 엔드포인트를 지원하지 않는 데몬(j == null)과 지원하나 실행 중인 작업이 없는 빈 상태를 구별하여 렌더링합니다.
                if (j == null) {
                    work.add(JBLabel(MagiBundle.msg("plan.tasks.nodoor") +
                        (jr.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")).apply {
                        foreground = Look.faint
                    })
                } else if (queued.isEmpty() && bgRunning.isEmpty() && bgBad.isEmpty() &&
                    kids.isEmpty() && past.isEmpty()) {
                    work.add(JBLabel(MagiBundle.msg("plan.tasks.none")).apply { foreground = Look.faint })
                }
                queued.forEach { q ->
                    // 사용자 요청 우선, 이후 위임된 작업(handover) 순으로 렌더링합니다.
                    val head = if (q.kind == "handover") "↤ ${q.from ?: MagiBundle.msg("plan.someone")}: " else "· "
                    work.add(JBLabel(head + (q.text?.lineSequence()?.firstOrNull() ?: "")).apply {
                        foreground = if (q.kind == "handover") Look.accent else Look.body
                    })
                }
                bgBad.forEach { b ->
                    val how = if (b.killed) MagiBundle.msg("plan.bg.killed")
                    else MagiBundle.msg("plan.bg.exit", b.exit)
                    work.add(JBLabel("⚙ ${b.command?.take(48) ?: b.id} — $how").apply {
                        foreground = Look.error
                        border = JBUI.Borders.empty(1, 0)
                    })
                }
                bgRunning.forEach { b ->
                    // 실행 중인 백그라운드 작업 중단 버튼 (job-kill 엔드포인트 연동).
                    work.add(JBPanel<JBPanel<*>>(BorderLayout(6, 0)).apply {
                        isOpaque = false
                        add(JBLabel("⚙ ${b.command?.take(56) ?: b.id}").apply { foreground = Look.faint },
                            BorderLayout.CENTER)
                        add(JButton("✕").apply {
                            margin = java.awt.Insets(0, 4, 0, 4)
                            toolTipText = MagiBundle.msg("plan.kill.tip")
                            addActionListener {
                                workspace.onDaemon({ tell(MagiBundle.msg("common.failed", it)) }) { c2 ->
                                    val kr = c2.killJob(b.id)
                                    // 중단 요청 결과 처리(`answerJobKill` 계약: 중복 클릭 시 에러가 아닌 "already gone"으로 응답):
                                    // 정상 종료 시 다음 폴링에서 항목이 제거되며, 이미 완료된 작업이었을 경우에만 안내 메시지를 표시합니다.
                                    when {
                                        !kr.ok -> tell(MagiBundle.msg("common.notsent", kr.error ?: MagiBundle.msg("common.noreason")))
                                        !kr.removed -> tell(MagiBundle.msg("plan.kill.gone", b.id))
                                    }
                                }
                            }
                        }, BorderLayout.EAST)
                    })
                }
                // 서브에이전트 목록 표시: 실행 중인 작업 우선, 이후 최근 종료된 작업 표시.
                // 실행 중인 세부 상태는 `jobs` 등록부를 참조하고, 종료된 작업의 제목과 메타데이터는 `children` 엔드포인트를 참조합니다.
                // 중복 항목은 실행 중인 상태를 우선하여 병합합니다.
                // 각 서브에이전트 행은 클릭 시 해당 서브에이전트의 전사 탭을 바로 열 수 있는 링크 컴포넌트로 구성됩니다.
                val running = kids.map { it.id }.toSet()
                kids.forEach { c ->
                    work.add(kidRow(project, "⛐ " + (c.task?.take(60) ?: c.id), c.id))
                }
                // 종료된 서브에이전트의 실패 사유 가시화:
                // `subagent_jobs.go`의 `finish()` 계약에 따라 실패 시 `Err` 필드가 기록되어 유지되므로,
                // 종료된 자식 작업 중 실패한 항목은 에러 메시지를 함께 렌더링합니다.
                val why = j?.children.orEmpty().filter { !it.running }.associate { it.id to it.err }
                past.filter { it.id !in running }.take(pastKids).forEach { c ->
                    val what = c.title?.take(52)?.ifBlank { null } ?: c.id.takeLast(6)
                    // 생성 출처(origin)를 사용자 친화적인 용어로 변환(회의/회의록 등 구분).
                    val who = c.origin?.ifBlank { null }?.let { Look.originWord(it) + " · " } ?: ""
                    val failed = why[c.id]?.takeIf { it.isNotBlank() }
                        ?.let { " — " + MagiBundle.msg("plan.kid.failed", it.lineSequence().first().take(60)) }
                        .orEmpty()
                    work.add(kidRow(project, "⛒ $who$what$failed", c.id))
                }
                fleet.removeAll()
                val rows = r.roster
                when {
                    rows == null -> fleet.add(JBLabel(MagiBundle.msg("plan.companions.nodoor") +
                        (r.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")).apply {
                        foreground = Look.faint
                    })
                    rows.isEmpty() -> fleet.add(JBLabel(MagiBundle.msg("plan.companions.none")).apply { foreground = Look.faint })
                    else -> {
                        // 동거 경고의 재료: 같은 workdir 에 둘 이상이 살면 그 사실이 행에 선다 —
                        // 동시 작업의 파일 충돌은 사용자가 이름 댄 고통이다.
                        // 「살면」의 셈: 산 로컬 행만, (host, workdir) 로 — 목격담의 같은 경로
                        // 문자열이나 시체가 동거로 서면 배지가 거짓말한다(리뷰 F5).
                        val crowd = rows.filter { it.live && !it.sighting && !it.workdir.isNullOrEmpty() }
                            .groupingBy { (it.host ?: "") to it.workdir!! }.eachCount()
                        rows.sortedBy { it.sighting }.forEach { row ->
                            val crowded = row.live && !row.sighting && !row.workdir.isNullOrEmpty() &&
                                (crowd[(row.host ?: "") to row.workdir!!] ?: 0) > 1
                            val label = fleetRow(row, crowded)
                            if (row.live && !row.sighting) {
                                label.cursor = java.awt.Cursor.getPredefinedCursor(java.awt.Cursor.HAND_CURSOR)
                                label.addMouseListener(object : java.awt.event.MouseAdapter() {
                                    // 자기 자신 행도 산다 — 자기에게 건네면 자기 큐 뒤에 선다(의도:
                                    // 막지 않는다, 지금 턴 뒤로 미루는 정당한 쓰임이 있다).
                                    override fun mouseClicked(e: java.awt.event.MouseEvent) {
                                        if (SwingUtilities.isLeftMouseButton(e)) askOf(row)
                                    }
                                })
                            }
                            fleet.add(label)
                        }
                    }
                }
                work.revalidate(); work.repaint(); fleet.revalidate(); fleet.repaint()
                cronPane.removeAll()
                // 모름≠없음의 갈림은 cron 필드가 아니라 ok 다(리뷰 실측): 빈 목록은 omitempty 로
                // 통째 생략돼 null 로 오므로, null 을 「문 없음」으로 읽으면 예약 없는 보통 데몬이
                // 영영 버전 스큐 문구를 뒤집어쓴다. 문이 없으면 ok=false + error 가 온다.
                val crons = if (cr.ok) cr.cron.orEmpty() else null
                when {
                    crons == null -> cronPane.add(JBLabel(MagiBundle.msg("plan.schedule.nodoor") +
                        (cr.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")).apply {
                        foreground = Look.faint
                    })
                    crons.isEmpty() -> cronPane.add(JBLabel(MagiBundle.msg("plan.schedule.none")).apply { foreground = Look.faint })
                    else -> crons.forEach { j ->
                        // 고장 행이 이 판이 표시해야 하는 행이다 — 다른 어떤 화면도 다시 언급 안 한다.
                        val line = when {
                            !j.problem.isNullOrBlank() -> "✗ ${j.name} — ${j.problem}"
                            !j.enabled -> MagiBundle.msg("plan.schedule.off", j.name)
                            else -> "◷ ${j.name}  ${j.next?.replace("T", " ")?.take(16) ?: ""}"
                        }
                        cronPane.add(JBLabel(line).apply {
                            foreground = if (!j.problem.isNullOrBlank()) Look.error else Look.faint
                            border = JBUI.Borders.empty(1, 0)
                            // 잡이 **무엇을 묻는지**는 툴팁에 — 한 줄에 다 적으면 이 좁은 판에서
                            // 다른 절을 밀어낸다. 고치는 화면은 누르면 열린다.
                            // 무엇을 하는 잡인지 — 도는 잡은 `$` 로 시작해 한눈에 갈린다.
                            (j.command?.takeIf { it.isNotBlank() }?.let { "$ $it" }
                                ?: j.prompt?.takeIf { it.isNotBlank() })
                                ?.let { toolTipText = it.take(300) }
                            if (canEditCron) {
                                cursor = java.awt.Cursor.getPredefinedCursor(java.awt.Cursor.HAND_CURSOR)
                                addMouseListener(object : java.awt.event.MouseAdapter() {
                                    override fun mouseClicked(e: java.awt.event.MouseEvent) =
                                        editCron(project, j, workspace, { tell(it) }, { poll() })
                                })
                            }
                        })
                    }
                }
                // 「추가」는 **문이 있을 때만** 선다. 없는 문을 부르고 거부를 읽는 방식으로는 낡은
                // 빌드와 거부하는 엔진을 못 가르고, 화면은 그 둘에 다른 말을 해야 한다.
                if (canEditCron) {
                    cronPane.add(JBLabel(MagiBundle.msg("plan.schedule.add")).apply {
                        foreground = Look.accent
                        cursor = java.awt.Cursor.getPredefinedCursor(java.awt.Cursor.HAND_CURSOR)
                        border = JBUI.Borders.empty(2, 0)
                        addMouseListener(object : java.awt.event.MouseAdapter() {
                            override fun mouseClicked(e: java.awt.event.MouseEvent) =
                                editCron(project, null, workspace, { tell(it) }, { poll() })
                        })
                    })
                }
                cronPane.revalidate(); cronPane.repaint()
            }
            paintAsked(my) // 풀 스레드 — 원격 왕복은 EDT 밖(리뷰 F1)
        }
        }

        // 대화 목록은 매 틱이 아니라 펴는 순간과 동사 뒤에만 — 스토어 훑기를 3초마다 시키지 않는다.
        fun loadTalks() = workspace.onDaemonWithoutChat({}) { comp ->
            val sr = comp.sessions()
            // 모름≠없음의 갈림은 ok 다 — 빈 목록은 omitempty 로 통째 생략돼 null 로 온다
            // (cron 이 판 그 함정의 sessions 판). 문이 없을 때만 ok=false 다.
            val list = if (sr.ok) sr.sessions.orEmpty() else null
            if (list == null) {
                // 눌리게 그려놓고 아무 일도 안 나는 콤보를 두지 않는다(M3: 불가능한 동작은 비활성).
                SwingUtilities.invokeLater {
                    talk.isEnabled = false
                    talk.toolTipText = MagiBundle.msg("plan.models.nodoor") +
                        (sr.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")
                }
                return@onDaemonWithoutChat
            }
            val now = comp.facts().session
            SwingUtilities.invokeLater {
                talk.isEnabled = true
                talk.toolTipText = null
                painting = true
                talkIds = list.associate { row ->
                    val label = (row.title?.take(40)?.ifBlank { null } ?: MagiBundle.msg("plan.untitled")) + "  ·" + row.id.takeLast(6)
                    label to row.id
                }
                talk.removeAllItems()
                talkIds.keys.forEach { talk.addItem(it) }
                talkIds.entries.firstOrNull { it.value == now }?.let { talk.selectedItem = it.key }
                painting = false
            }
        }
        fun loadModels() = workspace.onDaemon({}) { comp ->
            val mr = comp.models()
            // 같은 함정의 models 판: 빈 목록도 ok=true 로 오되 필드는 생략된다. why 는 백엔드가
            // 잠깐 죽었다는 말이라 그때도 목록은 못 믿는다 — 비활성+사유가 정직하다.
            val m = if (mr.ok && mr.why == null) mr.models.orEmpty() else null
            if (m.isNullOrEmpty()) {
                SwingUtilities.invokeLater {
                    model.isEnabled = false
                    model.toolTipText = when {
                        !mr.ok -> MagiBundle.msg("plan.models.nodoor") +
                            (mr.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")
                        mr.why != null -> MagiBundle.msg("plan.models.failed", mr.why!!.lineSequence().first().take(80))
                        else -> MagiBundle.msg("plan.models.empty")
                    }
                }
                return@onDaemon
            }
            SwingUtilities.invokeLater {
                model.isEnabled = true
                model.toolTipText = null
                painting = true
                val keep = model.selectedItem
                model.removeAllItems()
                m.forEach { model.addItem(it) }
                keep?.let { model.selectedItem = it }
                painting = false
            }
        }

        fun paintAskedNow() = SwingUtilities.invokeLater {
            askedPane.removeAll()
            val snap = synchronized(asked) { asked.toList() }
            // 빈 구역도 말을 한다 — 옆 구역들이 전부 그렇게 한다(빈 상태 규칙).
            if (snap.isEmpty()) askedPane.add(Look.aside(MagiBundle.msg("plan.requests.none")))
            snap.forEach { a ->
                askedPane.add(JBLabel("→ ${a.who}: ${a.ask.lineSequence().first().take(48)} — …").apply {
                    foreground = Look.faint
                    border = JBUI.Borders.empty(1, 0)
                })
            }
            askedPane.revalidate(); askedPane.repaint()
        }
        paintAsked = { my ->
            // **풀 스레드에서 돈다**(poll 의 pooled 구간이 부른다) — 접수증당 원격 연결 하나라
            // EDT 에 올리면 웨지된 상대가 IDE 를 세운다. 웨지는 DaemonClient 의 워치독이 시한에
            // 끊어 DaemonGone 으로 돌아온다 — 이 폴 스레드가 그 시한만큼 서는 것이 상한이다.
            val snap = synchronized(asked) { asked.toList() }
            snap.forEach { a ->
                if (a.over) return@forEach // 종결된 건은 더 안 묻는다 — 헛폴이 창 수명만큼 갔었다
                val r = runCatching {
                    DaemonClient.connect(java.nio.file.Paths.get(a.socket)).use {
                        it.exchange(Request(method = "hand-state", name = a.receipt))
                    }
                }.getOrNull()
                val h = r?.handover
                when {
                    r == null -> a.line = MagiBundle.msg("plan.empty.link")
                    !r.ok -> {
                        // 거절은 「대기를 끝내라」다(Taker.Handed 계약: 재시작·만료) — 연결
                        // 실패와 접으면 죽은 접수증을 영영 폴한다(리뷰 F4).
                        a.over = true; a.line = MagiBundle.msg("plan.done", r.error ?: MagiBundle.msg("plan.requests.norecord"))
                    }
                    h == null -> a.line = MagiBundle.msg("plan.empty.answer")
                    h.over -> { a.over = true; a.line = MagiBundle.msg("plan.done", h.news ?: MagiBundle.msg("common.noreason")) }
                    h.done -> { a.over = true; a.answered = true; a.line = MagiBundle.msg("plan.requests.answer", h.answer?.lineSequence()?.firstOrNull() ?: "") }
                    else -> a.line = MagiBundle.msg("plan.requests.working")
                }
            }
            SwingUtilities.invokeLater {
                if (my != pollSeq.get()) return@invokeLater // 늦은 완료가 새 그림을 덮지 않게(F6)
                askedPane.removeAll()
                if (snap.isEmpty()) askedPane.add(Look.aside(MagiBundle.msg("plan.requests.none")))
                snap.forEach { a ->
                    val t = "→ ${a.who}: ${a.ask.lineSequence().first().take(40)} — ${a.line ?: "…"}"
                    askedPane.add(JBLabel(t).apply {
                        foreground = if (a.over && !a.answered) Look.warn else Look.faint
                        border = JBUI.Borders.empty(1, 0)
                    })
                }
                askedPane.revalidate(); askedPane.repaint()
            }
        }
        askOf = { row ->
            val name = row.name?.takeIf { it.isNotBlank() } ?: row.socket.substringAfterLast('/')
            val q = com.intellij.openapi.ui.Messages.showInputDialog(
                project, MagiBundle.msg("plan.ask.body"),
                MagiBundle.msg("plan.ask.title", name), null,
            )
            if (!q.isNullOrBlank()) {
                val looking = com.intellij.openapi.ui.Messages.showYesNoDialog(
                    project, MagiBundle.msg("plan.ask.body"),
                    MagiBundle.msg("plan.ask.kind"), MagiBundle.msg("plan.ask.question"), MagiBundle.msg("plan.ask.request"), null,
                ) == com.intellij.openapi.ui.Messages.YES
                ApplicationManager.getApplication().executeOnPooledThread {
                    val r = runCatching {
                        DaemonClient.connect(java.nio.file.Paths.get(row.socket)).use {
                            // 라벨은 코어 규약을 탄다: DispatchMark("— asked by ")가 첫머리에
                            // 없으면 수신 쪽 셋이 조용히 빠진다 — 체이닝 금지 판정, 발신자 파싱,
                            // 회신 경로 지침(리뷰 F3; fleet.go 의 그 마크).
                            it.exchange(Request(
                                method = "hand",
                                name = "— asked by ide:" + project.name +
                                    " (사람이 IDE 플릿 판에서 보냄; 답은 hand-state 로 읽는다 — 회신 채널 없음)",
                                text = q, looking = looking,
                            ))
                        }
                    }.getOrElse { e -> tell(MagiBundle.msg("plan.ask.failed", e.message ?: MagiBundle.msg("common.noreason"))); return@executeOnPooledThread }
                    if (r.ok && !r.out.isNullOrBlank()) {
                        asked.add(Asked(row.socket, name, r.out!!, q))
                        tell(MagiBundle.msg("plan.ask.sent", name, r.out!!.takeLast(6)))
                        paintAskedNow()
                    } else tell(MagiBundle.msg("plan.ask.refused", r.error ?: MagiBundle.msg("common.noreason"))) // mid-turn 등 — 거절도 답이다
                }
            }
        }
        refreshTalks = { loadTalks() }
        refresh(); poll(); loadTalks(); loadModels()
        val timer = Timer(3_000) {
            if (toolWindow.isVisible) {
                refresh(); poll()
                // 죽은 콤보는 틱마다 되살려 본다(리뷰: 접었다 펴야만 풀리는 「영영 죽음」이었다).
                // 산 콤보는 안 두드린다 — 목록 새로고침은 펴는 순간과 동사 뒤의 일이다.
                if (!talk.isEnabled) loadTalks()
                if (!model.isEnabled) loadModels()
            }
        }.apply { isRepeats = true }
        timer.start()
        Disposer.register(toolWindow.disposable) { timer.stop() }
        project.messageBus.connect(toolWindow.disposable).subscribe(
            ToolWindowManagerListener.TOPIC,
            object : ToolWindowManagerListener {
                override fun toolWindowShown(shown: ToolWindow) {
                    if (shown.id == toolWindow.id) { refresh(); poll(); loadTalks(); loadModels() }
                }
            }
        )
    }

    /**
     * 끝난 자식을 한 번에 몇 줄까지 그리나.
     *
     * 이 판의 본업은 「지금 무엇이 돌고 있나」라, 지난 것이 그 위를 덮으면 판의 뜻이 바뀐다.
     * 한 턴이 자식을 수십 개 띄우는 것은 이 트리가 이미 겪은 모양이고(등록부가 한도를 두는
     * 이유가 그것이다), 여기도 같은 이유로 자른다.
     */
    private val pastKids = 6

    /**
     * 서브에이전트 한 줄 — 누르면 그 자식의 전사가 하단 독의 탭으로 선다.
     *
     * 라벨을 버튼으로 바꾸지 않는다: 이 판의 다른 줄은 전부 라벨이고, 하나만 버튼이면 그 줄이
     * 판에서 제일 시끄러워진다. 손 모양 커서와 툴팁으로 「누를 수 있다」를 말한다.
     */
    private fun kidRow(project: Project, text: String, sid: String): JBLabel =
        JBLabel(text).apply {
            foreground = Look.faint
            cursor = java.awt.Cursor.getPredefinedCursor(java.awt.Cursor.HAND_CURSOR)
            toolTipText = MagiBundle.msg("plan.kid.tip")
            addMouseListener(object : java.awt.event.MouseAdapter() {
                override fun mouseClicked(e: java.awt.event.MouseEvent) {
                    MagiTabs.open(project, sid, "⛐" + sid.takeLast(6))
                }
            })
        }

    /**
     * 이 데몬이 예약을 고칠 수 있는가 — 핸드셰이크에서 한 번 읽고 기억한다.
     *
     * 화면이 편집기를 그릴지 정하는 자리라 **부르기 전에** 알아야 한다. 없는 문을 부르고 거부를
     * 읽는 방식으로는 「낡은 빌드」와 「거부하는 엔진」을 못 가르는데, 화면은 그 둘에 다른 말을
     * 해야 한다(이 트리의 문 원칙 첫째).
     */
    private var canEditCron = false
    private var capsRead = false
    /** 이 데몬이 `context` 문을 답하나. 광고 없는 문은 두드리지 않는다. */
    private var canAskContext = false
    /** 문이 답한 창 사용량. EDT 밖에서 채우고 EDT 에서 읽으므로 volatile 이다. */
    @Volatile private var ctxFromDoor: dev.sayaya.magi.ide.usecase.Rows.Ctx? = null

    /**
     * 예약 하나를 고치는 판 — [job] 이 null 이면 새로 만든다.
     *
     * 이름은 **고칠 때 잠근다**: 이름이 잡의 정체이고, 여기서 바꾸면 「고치기」가 조용히
     * 「새로 만들고 옛것을 남기기」가 된다. 바꾸고 싶으면 지우고 다시 만드는 것이 그 뜻에 맞다.
     *
     * 스위치를 안 건드린 편집은 스위치를 안 보낸다(문의 세 갈래 `enabled`) — 말만 고치는 편집이
     * 꺼 둔 잡을 도로 켜면 안 된다.
     */
    private fun editCron(project: Project, job: CronRow?, ws: Workspace,
                         note: (String) -> Unit, after: () -> Unit) {
        val dlg = object : com.intellij.openapi.ui.DialogWrapper(project, true) {
            val name = com.intellij.ui.components.JBTextField(job?.name ?: "").apply {
                isEnabled = job == null
                columns = 18
            }
            val schedule = com.intellij.ui.components.JBTextField(job?.schedule ?: "@daily").apply { columns = 18 }
            val prompt = com.intellij.ui.components.JBTextArea(job?.prompt ?: "", 5, 40).apply {
                lineWrap = true; wrapStyleWord = true
            }
            // **묻거나 돈다 — 하나다.** 데몬이 둘 다인 잡을 거부하므로 화면도 그렇게 묻는다:
            // 라디오 둘이면 「둘 다 채웠다」가 만들어질 수 없고, 거부를 읽어서 배우지 않아도 된다.
            val runs = !job?.command.isNullOrBlank()
            val asks = javax.swing.JRadioButton(MagiBundle.msg("plan.schedule.asks"), !runs)
            val doesRun = javax.swing.JRadioButton(MagiBundle.msg("plan.schedule.runs"), runs)
            // 한 쌍으로 묶어야 배타가 성립한다 — 안 묶으면 둘 다 켜지고 그게 데몬이 거부하는 모양이다.
            val kind = javax.swing.ButtonGroup().apply { add(asks); add(doesRun) }
            val command = com.intellij.ui.components.JBTextField(job?.command ?: "", 24)
            val timeout = com.intellij.ui.components.JBTextField(job?.timeout ?: "", 8)
            fun sync() {
                prompt.isEnabled = asks.isSelected
                command.isEnabled = doesRun.isSelected
                timeout.isEnabled = doesRun.isSelected
            }
            val on = javax.swing.JCheckBox(MagiBundle.msg("plan.schedule.enabled"), job?.enabled ?: true)
            init {
                title = MagiBundle.msg(if (job == null) "plan.schedule.new" else "plan.schedule.edit")
                init()
            }
            override fun createCenterPanel(): javax.swing.JComponent =
                com.intellij.util.ui.FormBuilder.createFormBuilder()
                    .addLabeledComponent(MagiBundle.msg("plan.schedule.name"), name)
                    .addLabeledComponent(MagiBundle.msg("plan.schedule.when"), schedule)
                    .addComponent(asks)
                    .addLabeledComponent("", com.intellij.ui.components.JBScrollPane(prompt))
                    .addComponent(doesRun)
                    .addLabeledComponent(MagiBundle.msg("plan.schedule.cmd"), command)
                    .addLabeledComponent(MagiBundle.msg("plan.schedule.timeout"), timeout)
                    .addComponentToRightColumn(JBLabel(MagiBundle.msg("plan.schedule.cmd.why")).apply {
                        foreground = Look.faint
                    })
                    .addComponent(on)
                    // 사람이 쓰기 전에 규칙을 말한다 — 데몬이 거부한 뒤에 배우는 것보다 싸다.
                    .addComponentToRightColumn(JBLabel(MagiBundle.msg("plan.schedule.hint")).apply {
                        foreground = Look.faint
                    })
                    .panel
            override fun getPreferredFocusedComponent(): javax.swing.JComponent =
                when {
                    job == null -> name
                    runs -> command
                    else -> prompt
                }
        }
        // 고른 종류만 열려 있다 — 잠근 칸에 친 글자가 조용히 버려지지 않게.
        dlg.asks.addActionListener { dlg.sync() }
        dlg.doesRun.addActionListener { dlg.sync() }
        dlg.sync()
        if (!dlg.showAndGet()) return
        val n = dlg.name.text.trim()
        val sch = dlg.schedule.text.trim()
        val e = dev.sayaya.magi.ide.usecase.Schedules.edit(
            dlg.doesRun.isSelected, dlg.prompt.text, dlg.command.text, dlg.timeout.text)
        // 스위치를 안 건드렸으면 안 보낸다.
        val flag = if (job != null && dlg.on.isSelected == job.enabled) null else dlg.on.isSelected
        ws.onDaemon({ note(MagiBundle.msg("common.failed", it)) }) { c ->
            val r = c.setCron(n, sch, e.prompt, flag, e.command, e.timeout)
            if (!r.ok) note(MagiBundle.msg("common.notsent", r.error ?: MagiBundle.msg("common.noreason")))
            else SwingUtilities.invokeLater { after() }
        }
    }

    /** 토큰 수를 사람 눈금으로. 정수 나눗셈의 "0k" 를 안 만든다(1k 미만은 그대로). */
    private fun k(n: Int): String = if (n >= 1000) "${n / 1000}k" else "$n"

    /**
     * 플릿 한 행. 목격담은 흐리게+나이, 사람 기다리면 강조 — 그리고 **저쪽에 쌓인 대기**가
     * 있으면 센다(`waiting`): 남에게 청한 일이 어디서 기다리는지가 이 판의 절반이다.
     */
    /**
     * 창을 **무엇이** 채우나 — 총량 옆의 한 줄.
     *
     * 총량만 그리는 화면이 왜 문제인지는 코어가 적어 두었다: *"somebody looking at a nearly-full
     * bar reaches for the conversation, and on this harness **the conversation is routinely the
     * small half**."* 도구 카탈로그만으로 기본 로스터에서 6~7k 이라 대개 대화보다 크고, 그래서
     * 총량만 보고 대화를 접는 사람은 안 줄어드는 쪽을 접는다.
     *
     * **제 합에 대한 몫으로 그린다.** 다섯은 어림(chars/4)이라 `used` 와 안 더해진다 — 비율로는
     * 정직하고 총량으로는 아니다. 창의 %로 그리면 그 부정직을 화면에 옮기게 된다.
     *
     * 조각이 없거나(옛 데몬·스트림 낙하) 합이 0이면 **아무 말도 안 한다** — 모름을 0%로 그리지
     * 않는다는 이 판의 규칙 그대로다.
     */
    private fun makeup(p: dev.sayaya.magi.ide.model.ContextParts?): String {
        val sum = p?.sum() ?: 0
        if (p == null || sum <= 0) return ""
        val share = listOf(
            MagiBundle.msg("plan.usage.part.tools") to p.tools,
            MagiBundle.msg("plan.usage.part.results") to p.results,
            MagiBundle.msg("plan.usage.part.talk") to p.talk,
            MagiBundle.msg("plan.usage.part.calls") to p.calls,
            MagiBundle.msg("plan.usage.part.system") to p.system,
        ).filter { it.second > 0 }
            .sortedByDescending { it.second }
            .joinToString("  ") { "${it.first} ${it.second * 100 / sum}%" }
        return if (share.isBlank()) "" else MagiBundle.msg("plan.usage.makeup", share)
    }

    private fun fleetRow(r: RosterRow, crowded: Boolean = false): JBLabel {
        val name = r.name?.takeIf { it.isNotBlank() } ?: r.socket.substringAfterLast('/')
        val role = r.role?.takeIf { it.isNotBlank() }?.let { " · $it" }.orEmpty()
        // 컴패니언 상태 열거형 처리 (`internal/adapter/fleet/fleet.go`의 `State` 6종 대응):
        // 코어 설계 원칙: "nobody is listening and a turn was left open — a crash, a kill, a closed laptop. Every other view renders this identically to a finished session, which is why it is here."
        // 정상 종료(stopped)와 작업 처리 중 비정상 중단(abandoned)을 명확히 구분하여 렌더링합니다.
        // 미지의 신규 상태값이 전달될 경우 임의 추정 대신 원문 문자열을 그대로 노출합니다.
        val state = when (r.state) {
            "waiting" -> MagiBundle.msg("plan.companions.waiting")
            "working" -> MagiBundle.msg("plan.companions.working")
            "idle" -> ""
            "abandoned" -> MagiBundle.msg("plan.companions.abandoned")
            "stopped" -> MagiBundle.msg("plan.companions.stopped")
            "remote" -> MagiBundle.msg("plan.companions.remote")
            else -> r.state?.let { " — $it" }.orEmpty()
        }
        // 컴패니언 작업 부하(Load) 표시:
        // 코어 로드 산정 공식: "they decide where team-addressed work goes… load is Waiting + (1 if Handling)".
        // 대기 큐(`waiting`)뿐만 아니라 현재 작업 처리 중(`handling`) 여부를 함께 표기하여 수동 작업 위임 시 판단 근거를 제공합니다.
        val load = listOf(
            if (r.handling) MagiBundle.msg("plan.companions.busy") else "",
            if (r.waiting > 0) MagiBundle.msg("plan.companions.queue", r.waiting) else "",
        ).filter { it.isNotBlank() }.joinToString("")
        // 컴패니언 수행 가능 역할(does) 및 전체 지원 수(can) 가시화:
        // 코어 설계 의도: "Does NAMES those things… a name is enough to pick a companion out of a roster".
        // 라이브 실측(2026-09-10)에서 다중 컴패니언이 동일 모델('idle · sonnet')로 표시되어 식별 불가능하던 문제를 해결합니다.
        // `can`은 전체 기능 개수이고 `does`는 대표 샘플(최대 3개, `MaxDoes`)이므로 잔여 개수가 존재할 경우 '+N' 형식으로 표기합니다.
        val does = r.does.orEmpty().filter { it.isNotBlank() }
        val offers = if (does.isEmpty()) "" else {
            val head = does.take(3)
            val rest = maxOf(r.can, does.size) - head.size
            "  · " + head.joinToString(", ") + (if (rest > 0) " +$rest" else "")
        }
        val where = r.workdir?.takeIf { it.isNotBlank() }?.let { "  (" + it.substringAfterLast('/') + ")" }.orEmpty()
        // 가십 프로토콜 기반 마지막 목격 시각(초 단위 값을 자연어 경과 시간으로 변환).
        val seen = if (r.sighting) MagiBundle.msg("plan.companions.seen", RowText.ago(r.ageSeconds)) else ""
        val share = if (crowded) MagiBundle.msg("plan.companions.same") else "" // 동일 워크스페이스에 둘 이상 기동 시 파일 수정 충돌 주의 경고
        return JBLabel(name + role + state + load + offers + where + share + seen).apply {
            foreground = when {
                r.sighting -> Look.muted
                r.state == "waiting" -> Look.primary
                else -> Look.body
            }
            border = JBUI.Borders.empty(2, 0)
            toolTipText = r.socket
        }
    }
}
