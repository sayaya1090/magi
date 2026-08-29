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
import dev.sayaya.magi.ide.model.RosterRow
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

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val workspace = Workspace(project)
        val plan = JBPanel<JBPanel<*>>().apply {
            layout = BoxLayout(this, BoxLayout.Y_AXIS)
            border = JBUI.Borders.empty(8, 12)
        }
        val work = JBPanel<JBPanel<*>>().apply {
            layout = BoxLayout(this, BoxLayout.Y_AXIS)
            border = JBUI.Borders.empty(0, 12)
        }
        val fleet = JBPanel<JBPanel<*>>().apply {
            layout = BoxLayout(this, BoxLayout.Y_AXIS)
            border = JBUI.Borders.empty(0, 12)
        }
        val cronPane = JBPanel<JBPanel<*>>().apply {
            layout = BoxLayout(this, BoxLayout.Y_AXIS)
            border = JBUI.Borders.empty(0, 12)
        }
        val ctx = JBLabel(" ").apply { foreground = Look.faint; border = JBUI.Borders.empty(2, 12) }
        val talk = JComboBox<String>()
        val model = JComboBox<String>()
        // 사건 라벨 하나 — 뒤 사건이 앞 사건을 덮는 그 무늬인 것을 알고 둔다(리뷰 지적). 수준이
        // 안 섞여 원판(사유가 수준에 지워짐)보다 약하고, 유닛2의 상태점 재편에서 자리째 재론한다.
        val said = JBLabel(" ").apply { foreground = Look.faint; border = JBUI.Borders.empty(2, 12) }
        // 낡음을 지금인 양 두지 않는다(결함 모양 #10 「낡았는데 자신만만」): 폴이 실패하면 판을
        // 지우는 대신 — 마지막 값은 여전히 값이다 — 그 사실을 말로 세운다.
        val stale = JBLabel("데몬에 못 닿는다 — 아래는 마지막으로 읽힌 값이다").apply {
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
        // 국소함수는 전방 참조가 안 된다 — 단추 리스너(위)가 목록 새로고침(아래)을 불러야
        // 해서 손잡이로 잇는다. 선언 뒤에 실체가 앉는다.
        var refreshTalks: () -> Unit = {}
        /** 콤보의 「제목 (s_…끝6)」 표시에서 id 를 되찾는 지도. 렌더된 문장에서 파내지 않는다. */
        var talkIds: Map<String, String> = emptyMap()

        model.addActionListener {
            if (painting) return@addActionListener
            val pick = model.selectedItem as? String ?: return@addActionListener
            workspace.onDaemon({ tell("못 갔다: $it") }) { comp ->
                val r = comp.setModel(pick)
                tell(if (r.ok) "모델을 바꿨다: $pick" else "안 갔다: ${r.error ?: "사유 없음"}")
            }
        }
        talk.addActionListener {
            if (painting) return@addActionListener
            val label = talk.selectedItem as? String ?: return@addActionListener
            val id = talkIds[label] ?: return@addActionListener
            workspace.onDaemon({ tell("못 갔다: $it") }) { comp ->
                if (comp.facts().session == id) return@onDaemon // 이미 그 대화다
                val r = comp.resume(id)
                // 갈아타기의 나머지 절반은 대화 창이 한다 — session.moved 를 보고 새 대화에 붙는다.
                tell(if (r.ok) "옮겼다 → ${id.takeLast(6)}" else "안 갔다: ${r.error ?: "사유 없음"}")
            }
        }
        val fresh = JButton("새 대화").apply {
            addActionListener {
                workspace.onDaemon({ tell("못 갔다: $it") }) { comp ->
                    val r = comp.newSession()
                    // 턴이 도는 중이면 데몬이 거부한다 — 인터럽트 먼저라는 계약을 그대로 보인다.
                    tell(if (r.ok) "새 대화: ${r.session?.takeLast(6) ?: ""}" else "안 갔다: ${r.error ?: "사유 없음"}")
                    if (r.ok) refreshTalks() // 동사 뒤엔 목록이 낡았다 — 새 대화가 콤보에 서야 한다
                }
            }
        }
        val compact = JButton("대화 요약해 접기").apply {
            addActionListener {
                workspace.onDaemon({ tell("못 갔다: $it") }) { comp ->
                    val r = comp.compact()
                    tell(if (r.ok) "접으라고 보냈다." else "안 갔다: ${r.error ?: "사유 없음"}")
                }
            }
        }

        val controls = JBPanel<JBPanel<*>>().apply {
            layout = BoxLayout(this, BoxLayout.Y_AXIS)
            add(stale)
            add(Look.gutter("대기·작업"))
            add(work)
            add(Look.gutter("플릿"))
            add(fleet)
            add(Look.gutter("예약"))
            add(cronPane)
            add(Look.gutter("계기"))
            add(ctx)
            add(Look.gutter("컨트롤"))
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
            plan.removeAll()
            val v = MagiWindows.of(project)
            val steps = v?.plan()
            when {
                steps == null -> plan.add(JBLabel("아직 모른다 — 대화 창이 전사에 붙으면 온다").apply {
                    foreground = Look.faint
                })
                steps.isEmpty() -> plan.add(JBLabel("컴패니언이 계획을 안 세웠다").apply {
                    foreground = Look.faint
                })
                else -> {
                    val done = steps.count { it.status == "completed" }
                    plan.add(Look.gutter("계획  $done/${steps.size}"))
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
                "컨텍스트 ${"%.0f".format(it.percent)}%  (${k(it.tokens)}/${k(it.window)})"
            } ?: "컨텍스트 — 아직 모른다(턴이 돌면 온다)"
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
            SwingUtilities.invokeLater {
                if (my != pollSeq.get()) return@invokeLater // 더 새 틱이 이미 섰다 — 낡은 그림 금지
                stale.isVisible = false
                work.removeAll()
                val queued = j?.queued.orEmpty()
                val bgRunning = j?.background.orEmpty().filter { it.running }
                val kids = j?.children.orEmpty().filter { it.running }
                // 모름과 없음을 가른다(§0-3, 리뷰 실측): 문 없는 옛 데몬은 jobs 가 아예 안 온다 —
                // 그것을 "기다리는 것 없음"으로 그리면 화면이 모르는 것을 아는 척한다. 현행 데몬은
                // 빈 목록이라도 Jobs 를 실어 보낸다(answerJobs) — null 은 정확히 버전 스큐다.
                if (j == null) {
                    work.add(JBLabel("이 데몬엔 jobs 문이 없다" +
                        (jr.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")).apply {
                        foreground = Look.faint
                    })
                } else if (queued.isEmpty() && bgRunning.isEmpty() && kids.isEmpty()) {
                    work.add(JBLabel("기다리는 것 없음").apply { foreground = Look.faint })
                }
                queued.forEach { q ->
                    // 사람 말 먼저, 그다음 건넨 일 — 차례는 데몬이 정했고 여기는 그대로 그린다.
                    val head = if (q.kind == "handover") "↤ ${q.from ?: "누군가"}: " else "· "
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
                            toolTipText = "이 잡을 세운다"
                            addActionListener {
                                workspace.onDaemon({ tell("못 갔다: $it") }) { c2 ->
                                    val kr = c2.killJob(b.id)
                                    if (!kr.ok) tell("안 갔다: ${kr.error ?: "사유 없음"}")
                                }
                            }
                        }, BorderLayout.EAST)
                    })
                }
                kids.forEach { c ->
                    work.add(JBLabel("⛐ ${c.task?.take(60) ?: c.id}").apply { foreground = Look.faint })
                }
                fleet.removeAll()
                val rows = r.roster
                when {
                    rows == null -> fleet.add(JBLabel("이 데몬엔 roster 문이 없다" +
                        (r.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")).apply {
                        foreground = Look.faint
                    })
                    rows.isEmpty() -> fleet.add(JBLabel("이름 댈 컴패니언이 없다").apply { foreground = Look.faint })
                    else -> rows.sortedBy { it.sighting }.forEach { row -> fleet.add(fleetRow(row)) }
                }
                work.revalidate(); work.repaint(); fleet.revalidate(); fleet.repaint()
                cronPane.removeAll()
                // 모름≠없음의 갈림은 cron 필드가 아니라 ok 다(리뷰 실측): 빈 목록은 omitempty 로
                // 통째 생략돼 null 로 오므로, null 을 「문 없음」으로 읽으면 예약 없는 보통 데몬이
                // 영영 버전 스큐 문구를 뒤집어쓴다. 문이 없으면 ok=false + error 가 온다.
                val crons = if (cr.ok) cr.cron.orEmpty() else null
                when {
                    crons == null -> cronPane.add(JBLabel("이 데몬엔 cron 문이 없다" +
                        (cr.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")).apply {
                        foreground = Look.faint
                    })
                    crons.isEmpty() -> cronPane.add(JBLabel("예약이 없다").apply { foreground = Look.faint })
                    else -> crons.forEach { j ->
                        // 고장 행이 이 판이 표시해야 하는 행이다 — 다른 어떤 화면도 다시 언급 안 한다.
                        val line = when {
                            !j.problem.isNullOrBlank() -> "✗ ${j.name} — ${j.problem}"
                            !j.enabled -> "○ ${j.name} — 꺼짐"
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
        }
        }

        // 대화 목록은 매 틱이 아니라 펴는 순간과 동사 뒤에만 — 스토어 훑기를 3초마다 시키지 않는다.
        fun loadTalks() = workspace.onDaemon({}) { comp ->
            val sr = comp.sessions()
            // 모름≠없음의 갈림은 ok 다 — 빈 목록은 omitempty 로 통째 생략돼 null 로 온다
            // (cron 이 판 그 함정의 sessions 판). 문이 없을 때만 ok=false 다.
            val list = if (sr.ok) sr.sessions.orEmpty() else null
            if (list == null) {
                // 눌리게 그려놓고 아무 일도 안 나는 콤보를 두지 않는다(M3: 불가능한 동작은 비활성).
                SwingUtilities.invokeLater {
                    talk.isEnabled = false
                    talk.toolTipText = "이 데몬엔 sessions 문이 없다" +
                        (sr.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")
                }
                return@onDaemon
            }
            val now = comp.facts().session
            SwingUtilities.invokeLater {
                talk.isEnabled = true
                talk.toolTipText = null
                painting = true
                talkIds = list.associate { row ->
                    val label = (row.title?.take(40)?.ifBlank { null } ?: "(제목 없음)") + "  ·" + row.id.takeLast(6)
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
                        !mr.ok -> "이 데몬엔 models 문이 없다" +
                            (mr.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")
                        mr.why != null -> "모델 목록을 못 받았다 — " + mr.why!!.lineSequence().first().take(80)
                        else -> "백엔드가 모델을 하나도 안 줬다"
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

    /** 토큰 수를 사람 눈금으로. 정수 나눗셈의 "0k" 를 안 만든다(1k 미만은 그대로). */
    private fun k(n: Int): String = if (n >= 1000) "${n / 1000}k" else "$n"

    /**
     * 플릿 한 행. 목격담은 흐리게+나이, 사람 기다리면 강조 — 그리고 **저쪽에 쌓인 대기**가
     * 있으면 센다(`waiting`): 남에게 청한 일이 어디서 기다리는지가 이 판의 절반이다.
     */
    private fun fleetRow(r: RosterRow): JBLabel {
        val name = r.name?.takeIf { it.isNotBlank() } ?: r.socket.substringAfterLast('/')
        val role = r.role?.takeIf { it.isNotBlank() }?.let { " · $it" }.orEmpty()
        val state = when (r.state) {
            "waiting" -> " — 사람을 기다린다"
            "working" -> " — 도는 중"
            "idle" -> ""
            else -> r.state?.let { " — $it" }.orEmpty()
        }
        val load = if (r.waiting > 0) "  · 대기 ${r.waiting}" else ""
        val where = r.workdir?.takeIf { it.isNotBlank() }?.let { "  (" + it.substringAfterLast('/') + ")" }.orEmpty()
        val seen = if (r.sighting) "  · ${r.ageSeconds}s 전 목격" else ""
        return JBLabel(name + role + state + load + where + seen).apply {
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
