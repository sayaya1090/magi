package dev.sayaya.magi.ide.ui

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
 * 우측 독 — 계획과 계기판. 자리 기준은 사용자가 문장으로 세웠다(2026-08-29): **설정보다 자주
 * 쓰지만 채팅보다 덜 쓰는 컨트롤의 집.** 위에서 아래로 — 계획, 대기·작업, 플릿, 예약 자리,
 * 계기(컨텍스트), 컨트롤(대화 드롭다운·새 대화·모델·컴팩트).
 *
 * 원천이 갈린다. 계기는 전사 스트림([Rows]: 계획·모델은 사실이라 재생되고, 컨텍스트는 전이라
 * 다시 붙으면 모른다 — 그 모름을 0% 로 그리지 않는다). 목록과 동사는 데몬 문(`jobs`·`roster`·
 * `sessions`·`session-new`·`set-model`·`compact`). 다시 묻는 종 둘(보이는 동안 3초 + 펴는
 * 순간)은 옛 사실 판이 실측으로 산 그대로다.
 */
class PlanToolWindow : ToolWindowFactory {
    /**
     * 이 프로젝트에 이 창이 해당하나 — 규약이 요구하는 판정이다(UI Guidelines · Tool window:
     * "don't display the button when the window doesn't apply to the project setup").
     *
     * **얕게 본다.** 「데몬이 살아 있나」로 재면 데몬을 나중에 켜는 보통 흐름에서 버튼이
     * 영영 안 서고, 그러면 켜러 갈 자리도 없다. 워크스페이스가 될 수 있는 자리인가(=경로가
     * 있나)까지만 묻는다 — 웰컴 화면이나 경로 없는 임시 프로젝트에서만 안 선다.
     */
    override fun shouldBeAvailable(project: Project) = project.basePath != null

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val workspace = Workspace(project)
        // 세로로 쌓는 판 — **정렬을 컨테이너가 강제한다.** BoxLayout Y축은 자식을 alignmentX
        // 로 눕히는데 기본이 0.5(가운데)라, 판보다 좁은 자식(JBLabel 은 max=pref 라 안
        // 늘어난다)이 가운데로 밀려 왼쪽에 유령 마진이 선다(사용자 실측: "대기작업부터
        // 컨트롤까지 좌측에 이상한 마진"). 정렬 스윕을 재구축 자리마다 한 줄씩 두는 판은
        // 다음 구역이 또 빠뜨린다(리뷰 실측: controls 만 쓸었고 계획 판에 같은 기전이
        // 남아 있었다) — 자식이 언제 서든 add 가 정렬을 세우면 빠뜨릴 자리가 없다.
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
        // 폭을 항목에서 뗀다 — 긴 대화 제목 하나가 판을 벌리지 않게(Look.narrow 주석).
        val talk = Look.narrowCombo<String>()
        val model = Look.narrowCombo<String>(16)
        // 사건 라벨 하나 — 뒤 사건이 앞 사건을 덮는 그 무늬인 것을 알고 둔다(리뷰 지적). 수준이
        // 안 섞여 원판(사유가 수준에 지워짐)보다 약하고, 유닛2의 상태점 재편에서 자리째 재론한다.
        val said = JBLabel(" ").apply { foreground = Look.faint; border = JBUI.Borders.empty(2, 12) }
        // 낡음을 지금인 양 두지 않는다(결함 모양 #10 「낡았는데 자신만만」): 폴이 실패하면 판을
        // 지우는 대신 — 마지막 값은 여전히 값이다 — 그 사실을 말로 세운다.
        val stale = JBLabel(MagiBundle.msg("plan.stale")).apply {
            foreground = Look.warn
            border = JBUI.Borders.empty(2, 12)
            isVisible = false
        }
        fun tell(t: String) = SwingUtilities.invokeLater { said.text = t }

        // 사람이 고른 것과 판이 다시 채우는 것을 가른다 — 가르지 않으면 리프레시마다 동사가 나간다.
        var painting = false
        // 늦게 온 완료를 버린다(리뷰: 느린 성공 틱이 빠른 실패 틱의 낡음-배너를 지우고 죽기 전
        // 값을 지금인 양 세웠다). 각 틱이 번호를 들고, 자기보다 새 틱이 있으면 그리지 않는다.
        val pollSeq = java.util.concurrent.atomic.AtomicLong()

        /**
         * 이 창에서 다른 컴패니언에게 건넨 일들 — 접수증과 함께. 조종은 계약 경계 그대로
         * **그 컴패니언의 소켓**으로 간다(hand/hand-state — docs/CLIENTS §2). 창이 사는 동안만
         * 기억한다: 접수증의 원본은 저쪽 데몬이고, 여기는 물어볼 열쇠만 든다.
         */
        class Asked(val socket: String, val who: String, val receipt: String, val ask: String) {
            /** 마지막으로 알게 된 문장 — 종결 후에도 폴 없이 그린다. */
            @Volatile var line: String? = null
            /** 더 안 물어본다: done/over, 또는 저쪽이 접수증을 모른다(재시작·만료 — Handed 계약). */
            @Volatile var over = false
            /**
             * 답을 받고 끝났나. **색이 이 사실에 달렸다** — 전에는 그려진 글자에 번역된
             * 「답: 」이 들었는지로 판정했고, 그 판정은 그 키에 자리표시자가 생기는 순간
             * 조용히 거짓이 된다. 사실은 글자에서 되읽지 않고 사실로 들고 다닌다.
             */
            @Volatile var answered = false
        }
        val asked = java.util.Collections.synchronizedList(mutableListOf<Asked>())
        // 국소함수는 전방 참조가 안 된다 — 단추 리스너(위)가 목록 새로고침(아래)을 불러야
        // 해서 손잡이로 잇는다. 선언 뒤에 실체가 앉는다.
        var refreshTalks: () -> Unit = {}
        // 아래 둘도 같은 무늬의 지역 var 다 — 클래스 프로퍼티로 두면 **팩토리가 애플리케이션
        // 싱글턴**이라(플랫폼: ToolWindowEP 가 한 번 만들어 캐시) 두 프로젝트가 서로의 판을
        // 그리고 서로의 이름으로 hand 를 보낸다(리뷰 F2 — Project 누수까지).
        var askOf: (RosterRow) -> Unit = {}
        var paintAsked: (Long) -> Unit = {}
        /** 콤보의 「제목 (s_…끝6)」 표시에서 id 를 되찾는 지도. 렌더된 문장에서 파내지 않는다. */
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
            ctx.text = v?.contextNow()?.let {
                MagiBundle.msg("plan.usage.ctx", "%.0f%%  (%s/%s)".format(it.percent, k(it.tokens), k(it.window)))
            } ?: MagiBundle.msg("plan.usage.none")
            v?.modelNow()?.let { now ->
                painting = true
                if ((0 until model.itemCount).none { model.getItemAt(it) == now }) model.addItem(now)
                model.selectedItem = now
                painting = false
            }
            plan.revalidate(); plan.repaint()
        }

        // 데몬 왕복들 — EDT 밖에서 묻고 EDT 로 그린다. 실패는 조용히: 3초마다 우는 판은 사람이
        // 끄고, 못 붙음의 보고는 상태 표시줄이 이미 한다.
        fun poll() {
            val my = pollSeq.incrementAndGet()
            workspace.onDaemon({ if (my == pollSeq.get()) SwingUtilities.invokeLater { stale.isVisible = true } }) { comp ->
            val jr = comp.jobs()
            val j = jr.jobs
            val r = comp.roster()
            val cr = comp.cron()
            // 끝난 자식은 등록부에 없다 — 로그가 아는 것을 문에 묻는다. 문 없는 데몬은 null 을
            // 주고, 그때 이 판은 도는 것만 그린다(모름을 없음으로 그리지 않는다는 그 규칙).
            val past = comp.children().children.orEmpty()
            SwingUtilities.invokeLater {
                if (my != pollSeq.get()) return@invokeLater // 더 새 틱이 이미 섰다 — 낡은 그림 금지
                stale.isVisible = false
                work.removeAll()
                val queued = j?.queued.orEmpty()
                val bgRunning = j?.background.orEmpty().filter { it.running }
                val kids = j?.children.orEmpty().filter { it.running }
                // 모름과 없음을 가른다(§0-3, 리뷰 실측): 문 없는 옛 데몬은 jobs 가 아예 안 온다 —
                // 그것을 MagiBundle.msg("plan.tasks.none")으로 그리면 화면이 모르는 것을 아는 척한다. 현행 데몬은
                // 빈 목록이라도 Jobs 를 실어 보낸다(answerJobs) — null 은 정확히 버전 스큐다.
                if (j == null) {
                    work.add(JBLabel(MagiBundle.msg("plan.tasks.nodoor") +
                        (jr.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")).apply {
                        foreground = Look.faint
                    })
                } else if (queued.isEmpty() && bgRunning.isEmpty() && kids.isEmpty() && past.isEmpty()) {
                    work.add(JBLabel(MagiBundle.msg("plan.tasks.none")).apply { foreground = Look.faint })
                }
                queued.forEach { q ->
                    // 사람 말 먼저, 그다음 건넨 일 — 차례는 데몬이 정했고 여기는 그대로 그린다.
                    val head = if (q.kind == "handover") "↤ ${q.from ?: MagiBundle.msg("plan.someone")}: " else "· "
                    work.add(JBLabel(head + (q.text?.lineSequence()?.firstOrNull() ?: "")).apply {
                        foreground = if (q.kind == "handover") Look.accent else Look.body
                    })
                }
                bgRunning.forEach { b ->
                    // 도는 잡 옆의 ✕ — job-kill 문. removed=false 는 이미-없음이라 조용히 지나간다.
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
                                    if (!kr.ok) tell(MagiBundle.msg("common.notsent", kr.error ?: MagiBundle.msg("common.noreason")))
                                }
                            }
                        }, BorderLayout.EAST)
                    })
                }
                // 서브에이전트 — 도는 것 먼저, 그다음 **끝난 것**.
                //
                // 도는 것은 `jobs` 등록부가 더 잘 안다(무엇을 시켰는지, 몇 걸음인지). 끝난 것은
                // 그 등록부에서 사라지므로 `children` 문이 답한다 — 회의가 닫히면 참가자 방이
                // 정확히 그렇게 빠진다(ForgetSubagent). 둘을 합쳐 그리되 id 로 겹치는 것은
                // 도는 쪽을 남긴다.
                //
                // 줄은 **누를 수 있다.** 자식이 무엇을 했는지는 그 자식의 전사에 있고, 전사 문은
                // 자식 id 도 받는다 — 여기 없던 것은 문이 아니라 누를 자리였다.
                val running = kids.map { it.id }.toSet()
                kids.forEach { c ->
                    work.add(kidRow(project, "⛐ " + (c.task?.take(60) ?: c.id), c.id))
                }
                past.filter { it.id !in running }.take(pastKids).forEach { c ->
                    // 역할이 있으면 그것이 이름이다 — 회의 방과 델리게이트를 가르는 유일한 사실.
                    val what = c.title?.take(52)?.ifBlank { null } ?: c.id.takeLast(6)
                    val who = c.agent?.ifBlank { null }?.let { "$it · " } ?: ""
                    work.add(kidRow(project, "⛒ $who$what", c.id))
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
                        })
                    }
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

    /** 토큰 수를 사람 눈금으로. 정수 나눗셈의 "0k" 를 안 만든다(1k 미만은 그대로). */
    private fun k(n: Int): String = if (n >= 1000) "${n / 1000}k" else "$n"

    /**
     * 플릿 한 행. 목격담은 흐리게+나이, 사람 기다리면 강조 — 그리고 **저쪽에 쌓인 대기**가
     * 있으면 센다(`waiting`): 남에게 청한 일이 어디서 기다리는지가 이 판의 절반이다.
     */
    private fun fleetRow(r: RosterRow, crowded: Boolean = false): JBLabel {
        val name = r.name?.takeIf { it.isNotBlank() } ?: r.socket.substringAfterLast('/')
        val role = r.role?.takeIf { it.isNotBlank() }?.let { " · $it" }.orEmpty()
        val state = when (r.state) {
            "waiting" -> MagiBundle.msg("plan.companions.waiting")
            "working" -> MagiBundle.msg("plan.companions.working")
            "idle" -> ""
            else -> r.state?.let { " — $it" }.orEmpty()
        }
        val load = if (r.waiting > 0) MagiBundle.msg("plan.companions.queue", r.waiting) else ""
        val where = r.workdir?.takeIf { it.isNotBlank() }?.let { "  (" + it.substringAfterLast('/') + ")" }.orEmpty()
        val seen = if (r.sighting) MagiBundle.msg("plan.companions.seen", r.ageSeconds) else ""
        val share = if (crowded) MagiBundle.msg("plan.companions.same") else "" // 같은 워크스페이스에 둘 이상 — 충돌 주의
        return JBLabel(name + role + state + load + where + share + seen).apply {
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
