package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.options.Configurable
import com.intellij.openapi.project.Project
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBTextField
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.RosterRow
import dev.sayaya.magi.ide.usecase.Activity
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.Markup
import java.awt.GridBagConstraints
import java.awt.GridBagLayout
import java.awt.Insets
import javax.swing.JComboBox
import javax.swing.JComponent
import javax.swing.SwingUtilities

/**
 * Settings › Tools › magi — 슬래시 커맨드로 하던 **설정**(남는 상태)의 자리다. 무엇이 설정이고
 * 무엇이 행동인지는 `docs/UI.ko.md` §5 의 표가 가른다.
 *
 * **값은 데몬에 산다.** `PersistentStateComponent` 를 안 쓰는 것이 이 화면의 첫 결정이다(§5.1) —
 * IDE 에 한 벌 더 두면 IDE 가 아는 값과 데몬이 아는 값이 갈라지고, 갈라지면 화면이 거짓말한다.
 * 그래서 열 때 읽고([reset]), 적용이 쓰고([apply]), 쓰고 나서 다시 읽어 확인한다.
 *
 * **문이 없는 것은 편집 불가로 그린다.** 백엔드 프로필(`/providers`)·서브에이전트·컨텍스트 창은
 * 데몬에 쓰기 문이 없다 — 편집 가능하게 그리면 사람이 바꾸고 안 바뀐다(§0-3). 값이 보이는 문이
 * 생기면 그때 칸이 된다. 자동완성도 여기 안 산다: 스위치는 magi 의 `[autocomplete]` 하나이고
 * **플러그인이 두 번째 스위치를 만들지 않는다**(`MagiInlineCompletion` 의 KDoc 이 그 규칙이다).
 *
 * **모름을 지금 값인 양 그리지 않는다.** 데몬은 지금 승인 모드는 말해 주지만(`status`) 지금
 * 모델은 말해 주지 않는다 — `models` 는 고를 수 있는 목록이지 현재가 아니다(`Companion.facts` 의
 * 같은 사유). 그래서 모델 칸은 「바꾸기 전까지 무엇인지 모른다」를 그대로 적는다.
 */
class MagiConfigurable(private val project: Project) : Configurable {

    private val workspace by lazy { Workspace(project) }

    // ── 「지금」 장 — 우측 독(magi.facts)에 살던 사실들. 사용자 결정(2026-08-29)으로 이
    // 화면 안에 접혔다: 상시 감시는 상태 표시줄이 하고 있고, 자세히 보는 판은 열 때 읽으면
    // 된다 — 설정 화면은 열림이 곧 reset() 이라 「펴는 순간 다시 묻기」가 구조로 공짜다.
    private val doing = JBLabel(" ")
    private val perm = JBLabel(" ")
    private val sessionL = JBLabel(" ").apply { font = Look.mono() }
    private val outside = JBLabel(" ").apply { foreground = Look.warn }
    private val fleet = JBPanel<JBPanel<*>>().apply {
        layout = javax.swing.BoxLayout(this, javax.swing.BoxLayout.Y_AXIS)
    }

    private val permission = JComboBox(arrayOf("ask", "auto", "allow", "deny"))
    private val model = JComboBox<String>().apply { isEditable = true }
    private val backend = JBTextField()
    private val said = JBLabel(" ").apply { foreground = Look.faint }

    /** 마지막으로 데몬에서 읽은 값. [isModified] 는 화면과 이것을 견준다 — IDE 저장분이 아니다. */
    private var read: String? = null

    override fun getDisplayName() = "magi"

    override fun createComponent(): JComponent {
        val p = JBPanel<JBPanel<*>>(GridBagLayout()).apply { border = JBUI.Borders.empty(8, 12) }
        var y = 0
        fun head(text: String) {
            p.add(Look.gutter(text), GridBagConstraints().apply {
                gridx = 0; gridy = y; gridwidth = 2; anchor = GridBagConstraints.LINE_START
                insets = Insets(if (y == 0) 0 else 14, 0, 2, 0)
            })
            y++
        }
        fun row(name: String, c: JComponent) {
            p.add(JBLabel(name).apply { foreground = Look.faint }, GridBagConstraints().apply {
                gridx = 0; gridy = y; anchor = GridBagConstraints.LINE_START; insets = Insets(4, 0, 4, 12)
            })
            p.add(c, GridBagConstraints().apply {
                gridx = 1; gridy = y; weightx = 1.0; fill = GridBagConstraints.HORIZONTAL
                anchor = GridBagConstraints.LINE_START; insets = Insets(4, 0, 4, 0)
            })
            y++
        }
        fun note(text: String) {
            p.add(JBLabel("<html><i>${Markup.text(text)}</i></html>").apply { foreground = Look.faint }, GridBagConstraints().apply {
                gridx = 1; gridy = y; anchor = GridBagConstraints.LINE_START; insets = Insets(0, 0, 8, 0)
            })
            y++
        }
        head("지금")
        row("하는 일", doing)
        row("승인", perm)
        row("대화", sessionL)
        row(" ", outside)
        head("설정")
        row("승인 모드", permission)
        note("코어의 낱말 그대로 — ask 는 매번 묻고, auto 는 편집만 통과시킨다.")
        row("모델", model)
        note("목록은 데몬이 주고, 「지금 무엇인지는 데몬이 말하지 않는다」 — 고르면 바뀐다.")
        row("백엔드", backend)
        note("프로필 이름을 적으면 use-backend 로 간다. 목록을 주는 문은 아직 없다.")
        row("예약·크론", JBLabel("읽는 문이 없다 — 워크스페이스의 config.toml 이 원천이다."))
        row("서브에이전트 · 컨텍스트 창", JBLabel("쓰기 문이 없다 — 문이 생기면 칸이 된다."))
        head("플릿")
        p.add(fleet, GridBagConstraints().apply {
            gridx = 0; gridy = y; gridwidth = 2; weightx = 1.0
            fill = GridBagConstraints.HORIZONTAL; anchor = GridBagConstraints.LINE_START
        })
        y++
        p.add(said, GridBagConstraints().apply {
            gridx = 0; gridy = y; gridwidth = 2; anchor = GridBagConstraints.LINE_START
            insets = Insets(12, 0, 0, 0)
        })
        return p
    }

    override fun isModified(): Boolean =
        (permission.selectedItem as? String) != read ||
            (model.selectedItem as? String).orEmpty().isNotBlank() ||
            backend.text.isNotBlank()

    /**
     * 쓴다 — 그리고 **다시 읽는다.** 쓴 값이 아니라 읽은 값을 화면에 남겨야, 데몬이 거절했거나
     * 다르게 알아들은 것이 그대로 보인다. 소켓은 EDT 밖에서.
     */
    override fun apply() {
        val mode = permission.selectedItem as? String
        val pick = (model.selectedItem as? String).orEmpty().trim()
        val prof = backend.text.trim()
        workspace.onDaemon({ tell("못 갔다: $it") }) { comp ->
            val gripes = mutableListOf<String>()
            if (mode != null && mode != read) comp.setPermission(mode).also {
                if (!it.ok) gripes += "승인: ${it.error ?: "사유 없음"}"
            }
            if (pick.isNotBlank()) comp.setModel(pick).also {
                if (!it.ok) gripes += "모델: ${it.error ?: "사유 없음"}"
            }
            if (prof.isNotBlank()) comp.useBackend(prof).also {
                if (!it.ok) gripes += "백엔드: ${it.error ?: "사유 없음"}"
            }
            pull(comp)
            tell(if (gripes.isEmpty()) "적용했다." else gripes.joinToString(" · "))
            SwingUtilities.invokeLater { model.selectedItem = ""; backend.text = "" }
        }
    }

    override fun reset() {
        sayOutside() // 데몬을 안 기다리는 줄이 먼저다 — 못 붙는 워크스페이스에서도 이 경고는 선다
        workspace.onDaemon({ tell("데몬에 못 닿는다: $it") }) { comp -> pull(comp); tell(" ") }
    }

    /**
     * 컴패니언이 못 만지는 컨텐트 루트. 데몬이 아니라 IDE 가 아는 사실이라 붙기 전에도 답이
     * 있고, **빈 갈래도 쓴다** — 안 쓰는 것으로 지움을 흉내내면 처음 한 번만 맞는다.
     */
    private fun sayOutside() {
        val out = workspace.rootsOutsideWorkspace()
        if (out.isEmpty()) return say(outside, " ")
        say(outside, "<html>못 만지는 컨텐트 루트 ${out.size}개 — 워크스페이스는 프로젝트 디렉토리 하나다:<br/>" +
            out.joinToString("<br/>") { Markup.text(it) } + "</html>")
    }

    private fun say(label: JBLabel, text: String) = SwingUtilities.invokeLater { label.text = text }

    /** 플릿을 다시 그린다. 문이 없는 데몬(옛 빌드)이면 그 사실을 적는다 — 빈 판은 「없다」와
     *  「못 물었다」를 못 가른다. 목격담은 흐리게, 나이와 함께. */
    private fun paintFleet(r: Response) = SwingUtilities.invokeLater {
        fleet.removeAll()
        val rows = r.roster
        when {
            rows == null -> fleet.add(JBLabel("이 데몬엔 roster 문이 없다" +
                (r.error?.let { " — " + it.lineSequence().first().take(80) } ?: "")).apply { foreground = Look.faint })
            rows.isEmpty() -> fleet.add(JBLabel("이 머신이 이름 댈 컴패니언이 없다").apply { foreground = Look.faint })
            else -> rows.sortedBy { it.sighting }.forEach { row -> fleet.add(fleetRow(row)) }
        }
        fleet.revalidate(); fleet.repaint()
    }

    private fun fleetRow(r: RosterRow): JBLabel {
        val name = r.name?.takeIf { it.isNotBlank() } ?: r.socket.substringAfterLast('/')
        val role = r.role?.takeIf { it.isNotBlank() }?.let { " · $it" }.orEmpty()
        val state = when (r.state) {
            "waiting" -> " — 사람을 기다린다"
            "working" -> " — 도는 중"
            "idle" -> ""
            else -> r.state?.let { " — $it" }.orEmpty()
        }
        val where = r.workdir?.takeIf { it.isNotBlank() }?.let { "  (" + it.substringAfterLast('/') + ")" }.orEmpty()
        val seen = if (r.sighting) "  · ${r.ageSeconds}s 전 목격" else ""
        return JBLabel(name + role + state + where + seen).apply {
            foreground = when {
                r.sighting -> Look.muted
                r.state == "waiting" -> Look.primary
                else -> Look.body
            }
            border = JBUI.Borders.empty(2, 0)
            toolTipText = r.socket
        }
    }

    /** 데몬이 아는 것을 화면으로. 모델 목록이 늦거나 없어도 나머지는 선다. */
    private fun pull(comp: Companion) {
        val f = comp.facts()
        SwingUtilities.invokeLater {
            doing.text = when (val a = Activity.of(f)) {
                Activity.Waiting -> "사람을 기다리는 중"
                is Activity.Doing -> a.what
                Activity.Unsaid -> "도는 것 없음"
            }
            perm.text = f.permission ?: "데몬이 안 말했다"
            sessionL.text = f.session
        }
        paintFleet(comp.roster())
        val m = comp.models()
        SwingUtilities.invokeLater {
            read = f.permission
            if (f.permission != null) permission.selectedItem = f.permission
            model.removeAllItems()
            model.addItem("")
            m.models?.forEach { model.addItem(it) }
            model.selectedItem = ""
            m.why?.let { tell("모델 목록: $it") }
        }
    }

    private fun tell(text: String) = SwingUtilities.invokeLater { said.text = text }
}
