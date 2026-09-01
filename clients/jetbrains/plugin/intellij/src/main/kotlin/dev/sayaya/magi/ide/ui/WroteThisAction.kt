package dev.sayaya.magi.ide.ui

import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.ui.popup.JBPopupFactory
import com.intellij.openapi.wm.ToolWindowManager

/**
 * 이 줄을 어느 턴이 썼고, 그 턴은 무엇을 하라는 요청이었나.
 *
 * IDE 의 blame 과 다른 질문이다 — 커밋 하나에 턴이 여럿 들어 있고, 커밋 안 된 편집에는 blame 이
 * 답하지 않는다.
 *
 * **못 짚으면 못 짚는다고 말한다.** 소리 나게 답할 수 있는 것은 마지막 편집이 `at`/`to` 로 짚은
 * 범위뿐이다 — 그 뒤에 아무것도 안 왔으므로 줄이 안 밀렸다는 것이 확실한 유일한 경우다. 그 밖에는
 * **파일을 건드린 턴 목록**을 대신 내놓는다. 좁은 답을 넓게 말하면 틀린 줄을 가리키게 되고,
 * §5-5 가 그것을 금한다.
 */
class WroteThisAction : AnAction(), com.intellij.openapi.project.DumbAware {

    // 메뉴에 넷이 나란히 서는데 하나만 아이콘이 있으면 나머지 셋이 빈칸처럼 보인다(사용자
    // 실측 2026-09-01). 이 줄을 누가 썼나 — 내력. 이 액션이 실제로 묻는 것이 그것이다.
    //
    // XML 이 아니라 여기서 준다. `icon="AllIcons.X.Y"` 는 이름이 틀려도 런타임 경고 한 줄이고,
    // 그 경고를 보는 사람은 없다 — 아이콘이 안 뜨는 것으로만 드러난다. 코드면 컴파일이 잡는다.
    init { templatePresentation.icon = com.intellij.icons.AllIcons.Vcs.History }

    override fun getActionUpdateThread() = ActionUpdateThread.EDT

    override fun update(e: AnActionEvent) {
        // 글자는 **여기서** 못박는다: plugin.xml 의 번들 경로는 언어팩이 없을 때
        // JVM 기본 로케일로 새어 한국어가 뜬다(실측). MagiBundle 은 언어팩 유무로
        // 정하므로, 한 규칙으로 통일한다.
        e.presentation.text = MagiEditorMenu.item(e, "action.magi.wroteThis.text")
        e.presentation.description = MagiBundle.msg("action.magi.wroteThis.description")
        e.presentation.isEnabledAndVisible =
            e.project != null && e.getData(CommonDataKeys.EDITOR) != null && e.getData(CommonDataKeys.VIRTUAL_FILE) != null
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val editor = e.getData(CommonDataKeys.EDITOR) ?: return
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: return
        say(e, report(project, file.path, editor.caretModel.logicalPosition.line + 1))
    }

    companion object {
        /**
         * 「이 줄을 누가 썼나」에 답할 글. **아는 만큼을 내놓고 모르는 자리를 이름 붙여 말한다** —
         * 줄을 못 짚는다고 아무 말도 안 하는 것이 이 액션이 없애려던 그 침묵이다.
         *
         * 액션과 인텐션(Alt+Enter)이 같은 글을 쓴다. 두 벌로 적으면 한쪽만 고치는 날이 온다.
         */
        fun report(project: com.intellij.openapi.project.Project, path: String, line: Int): String {
            val view = MagiWindows.of(project) ?: return MagiBundle.msg("chat.wrote.nowindow")
            val a = view.authors
            a.wrote(path, line)?.let { pinned ->
                val why = pinned.asked?.let { MagiBundle.msg("chat.wrote.asked", it) }
                    ?: MagiBundle.msg("chat.wrote.noask")
                return MagiBundle.msg("chat.wrote.pinned", line, pinned.seq, pinned.tool.orEmpty()) + why
            }
            val all = a.of(path)
            if (all.isEmpty()) return MagiBundle.msg("chat.wrote.untouched")
            return buildString {
                append(MagiBundle.msg("chat.wrote.moved")).append('\n')
                append(MagiBundle.msg("chat.wrote.turns", all.size)).append('\n')
                all.takeLast(6).forEach { t ->
                    append("  #").append(t.seq).append(' ').append(t.tool)
                    t.lines?.let { append(" (").append(it.first).append('-').append(it.last).append(')') }
                    append("  ").append(t.asked?.take(70) ?: MagiBundle.msg("chat.wrote.unknownask")).append('\n')
                }
            }
        }
    }

    private fun say(e: AnActionEvent, text: String) {
        JBPopupFactory.getInstance()
            .createMessage(text)
            .showInBestPositionFor(e.dataContext)
    }
}
