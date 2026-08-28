package dev.sayaya.magi.ide.ui

import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindow
import com.intellij.openapi.wm.ToolWindowFactory
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.content.ContentFactory
import dev.sayaya.magi.ide.usecase.Companion
import java.awt.GridLayout
import javax.swing.BoxLayout
import javax.swing.SwingUtilities

/**
 * 우측 판 — **무엇을 하기로 했나**.
 *
 * 콘솔의 그 자리를 옮긴 것이다(`docs/UI.md` §2.2: 840px 이상에서 대화 옆에 서고, sticky 다). 옆
 * 칼럼이 붙박이여야 하는 이유는 콘솔이 적어 두었다 — 스크롤에 흘러가는 계획은 다시 찾아야 하는
 * 계획이다. IDE 에서는 툴윈도가 원래 독립적이라 그 성질이 공짜로 따라온다.
 *
 * **오늘 서는 것은 사실 장 하나다.** 계획·건넨 일·예약·받은 지시는 읽기 문이 필요하고(설계 문서
 * §3·§5), 그때까지 이 판은 **비어 있는 대신 왜 비었는지 말한다.** 빈 기둥은 "아직 안 왔다"로
 * 읽히는데 실제로는 "이 컴패니언은 계획을 안 세웠다"이거나 "이 빌드에 그 문이 없다"이고, 셋은
 * 다른 사실이다(§0.5-7).
 */
class FactsToolWindow : ToolWindowFactory {

    override fun createToolWindowContent(project: Project, toolWindow: ToolWindow) {
        val view = View(project)
        toolWindow.contentManager.addContent(
            ContentFactory.getInstance().createContent(JBScrollPane(view.root), null, false)
        )
        view.refresh()
    }

    private class View(project: Project) {
        val root = JBPanel<JBPanel<*>>().apply { layout = BoxLayout(this, BoxLayout.Y_AXIS) }
        private val workspace = Workspace(project)

        private val doing = JBLabel(" ")
        private val permission = JBLabel(" ")
        private val session = JBLabel(" ")
        private val trouble = JBLabel(" ")

        init {
            root.add(card("지금", doing, "승인", permission, "대화", session))
            root.add(trouble)
            // 아직 문이 없어 못 채우는 장들. 이름을 세워 두는 것이 빈자리로 두는 것보다 낫다 —
            // 사람이 "이 화면이 원래 이만큼인가"를 묻지 않게 된다.
            for (name in listOf("계획", "건넨 일", "예약·크론", "받은 지시")) {
                root.add(JBLabel("$name — 데몬에 읽기 문이 생기면 온다(설계 문서 §3)."))
            }
        }

        private fun card(vararg pairs: Any): JBPanel<JBPanel<*>> {
            val p = JBPanel<JBPanel<*>>(GridLayout(0, 2, 8, 2))
            var i = 0
            while (i < pairs.size) {
                p.add(JBLabel(pairs[i] as String))
                p.add(pairs[i + 1] as JBLabel)
                i += 2
            }
            return p
        }

        fun refresh() = workspace.onDaemon({ say(trouble, it) }) { comp -> paint(comp.facts()) }

        /**
         * 모름과 없음을 갈라 그린다. `doing` 이 null 인 것은 "쉬는 중"이 아니라 **이 데몬이 말해
         * 주지 않았다**이고, 둘을 같은 글자로 그리면 화면이 모르는 것을 아는 척한다.
         */
        private fun paint(f: Companion.Facts) = SwingUtilities.invokeLater {
            doing.text = when {
                f.waiting != null -> "사람을 기다리는 중"
                f.doing != null -> f.doing
                else -> "도는 것 없음"
            }
            permission.text = f.permission ?: "데몬이 안 말했다"
            session.text = f.session
            trouble.text = " "
        }

        private fun say(label: JBLabel, text: String) = SwingUtilities.invokeLater { label.text = text }
    }
}
