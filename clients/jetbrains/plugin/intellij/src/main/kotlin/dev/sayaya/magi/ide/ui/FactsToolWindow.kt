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
import dev.sayaya.magi.ide.usecase.Companion
import java.awt.GridLayout
import javax.swing.BoxLayout
import javax.swing.SwingUtilities
import javax.swing.Timer

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
        // **한 번 그리고 마는 판은 열어 둔 채로 낡는다.** 이 판의 윗장은 「지금 무엇을 하나」인데,
        // 그리기 문(`refresh`)을 여는 자리가 여기 하나뿐이던 동안 그 "지금"은 **판을 처음 연 순간**을
        // 뜻했다. 눈꼬리의 한 줄(상태 표시줄)은 3초마다 다시 묻는데 그것을 자세히 보라고 만든 판이
        // 안 묻고 있었다 — 자세히 보는 쪽이 더 낡는 것은 앞뒤가 바뀐 것이다.
        //
        // 간격은 상태 표시줄과 같이 두고, **안 보이면 왕복을 안 한다.** 툴윈도는 한 번 만들어지면
        // 숨겨도 살아 있어서, 접어 둔 판이 조용히 소켓을 두드리는 것을 막는 조건이 필요하다.
        //
        // 그래서 판이 열려 있는 동안은 같은 사실을 두 자리가 각자 묻는다(여기와 상태 표시줄). 알고
        // 두는 중복이다 — 겹치는 창이 **사람이 보고 있는 동안**뿐이고, 데몬이 미는 쪽이 되면 폴링이
        // 통째로 데몬 안으로 들어가 같이 없어진다(README "폴링 간격은 콘솔이 실측한 값을 따른다").
        val timer = Timer(3_000) { if (toolWindow.isVisible) view.refresh() }.apply { isRepeats = true }
        timer.start()
        Disposer.register(toolWindow.disposable) { timer.stop() }
        // **문을 좁혔으면 좁힌 문이 다시 열리는 순간을 잡아야 한다.** 위의 `isVisible` 은 접어 둔
        // 판이 소켓을 두드리는 것을 막는데, 그것만 두면 한 시간 접어 뒀다 펴는 사람이 **한 시간 전
        // 사실을 「지금」으로** 읽는다 — 다음 틱까지 최대 3초. 접힌 동안 낡는 것은 아무도 안 보니
        // 결함이 아니고, 결함은 **보기 시작하는 순간**에 있다. 그 순간에 종이 하나 있다.
        project.messageBus.connect(toolWindow.disposable).subscribe(
            ToolWindowManagerListener.TOPIC,
            object : ToolWindowManagerListener {
                override fun toolWindowShown(shown: ToolWindow) {
                    if (shown.id == toolWindow.id) view.refresh()
                }
            }
        )
    }

    private class View(project: Project) {
        val root = JBPanel<JBPanel<*>>().apply { layout = BoxLayout(this, BoxLayout.Y_AXIS) }
        private val workspace = Workspace(project)

        private val doing = JBLabel(" ")
        private val permission = JBLabel(" ")
        private val session = JBLabel(" ")
        private val trouble = JBLabel(" ")

        private val outside = JBLabel(" ")

        init {
            root.add(card("지금", doing, "승인", permission, "대화", session))
            root.add(trouble)
            // 거절이 오기 전에 말한다. 컴패니언이 못 만지는 루트가 있으면 그 사실이 화면에 있어야
            // 하고, 없으면 이 줄은 비어 있다 — 없는 문제를 광고하지 않는다. 채우는 것은 `refresh`
            // 하나뿐이다. 여기서도 한 번 채우면 문이 둘이 되고, 둘이 되면 한쪽만 고치게 된다.
            root.add(outside)
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

        fun refresh() {
            // 데몬을 안 기다리는 줄이 먼저다. 못 붙는 프로젝트에서도 이 경고는 떠야 한다.
            sayOutside()
            workspace.onDaemon({ say(trouble, it) }) { comp -> paint(comp.facts()) }
        }

        /**
         * 워크스페이스 밖 컨텐트 루트를 센다. 데몬을 안 부르므로 붙기 전에도 뜬다 — 이 사실은
         * 데몬이 아니라 **IDE 가** 아는 것이다.
         *
         * **두 갈래를 다 쓴다.** 예전에는 빈 목록이면 그냥 돌아갔는데, 그러면 이 자리를 지우는
         * 코드가 어디에도 없어서 사람이 루트를 워크스페이스 안으로 옮겨도 경고가 그대로 서 있었다.
         * 「없으면 안 뜬다」는 **한 번도 안 쓴 것**이지 지운 것이 아니다 — 안 쓰는 것으로 지움을
         * 흉내내면 처음 한 번만 맞는다.
         */
        private fun sayOutside() {
            val out = workspace.rootsOutsideWorkspace()
            if (out.isEmpty()) return say(outside, " ")
            say(outside, "<html>이 컴패니언이 <b>못 만지는</b> 컨텐트 루트 ${out.size}개 — " +
                "워크스페이스는 프로젝트 디렉토리 하나다:<br/>" + out.joinToString("<br/>") + "</html>")
        }

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
