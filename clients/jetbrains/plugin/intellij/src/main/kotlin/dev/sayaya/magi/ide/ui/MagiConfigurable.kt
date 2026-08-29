package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.options.Configurable
import com.intellij.openapi.project.Project
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBTextField
import com.intellij.util.ui.JBUI
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
        row("승인 모드", permission)
        note("코어의 낱말 그대로 — ask 는 매번 묻고, auto 는 편집만 통과시킨다.")
        row("모델", model)
        note("목록은 데몬이 주고, 「지금 무엇인지는 데몬이 말하지 않는다」 — 고르면 바뀐다.")
        row("백엔드", backend)
        note("프로필 이름을 적으면 use-backend 로 간다. 목록을 주는 문은 아직 없다.")
        row("예약·크론", JBLabel("읽는 문이 없다 — 워크스페이스의 config.toml 이 원천이다."))
        row("서브에이전트 · 컨텍스트 창", JBLabel("쓰기 문이 없다 — 문이 생기면 칸이 된다."))
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
        workspace.onDaemon({ tell("데몬에 못 닿는다: $it") }) { comp -> pull(comp); tell(" ") }
    }

    /** 데몬이 아는 것을 화면으로. 모델 목록이 늦거나 없어도 나머지는 선다. */
    private fun pull(comp: Companion) {
        val perm = comp.facts().permission
        val m = comp.models()
        SwingUtilities.invokeLater {
            read = perm
            if (perm != null) permission.selectedItem = perm
            model.removeAllItems()
            model.addItem("")
            m.models?.forEach { model.addItem(it) }
            model.selectedItem = ""
            m.why?.let { tell("모델 목록: $it") }
        }
    }

    private fun tell(text: String) = SwingUtilities.invokeLater { said.text = text }
}
