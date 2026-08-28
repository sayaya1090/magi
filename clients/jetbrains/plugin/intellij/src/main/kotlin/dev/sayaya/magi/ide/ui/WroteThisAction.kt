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
class WroteThisAction : AnAction() {

    override fun getActionUpdateThread() = ActionUpdateThread.EDT

    override fun update(e: AnActionEvent) {
        e.presentation.isEnabledAndVisible =
            e.project != null && e.getData(CommonDataKeys.EDITOR) != null && e.getData(CommonDataKeys.VIRTUAL_FILE) != null
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val editor = e.getData(CommonDataKeys.EDITOR) ?: return
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: return
        val line = editor.caretModel.logicalPosition.line + 1 // 사람이 세는 방식으로

        val view = MagiWindows.of(project)
        if (view == null) {
            say(e, "magi 창이 아직 안 열렸다 — 전사를 안 받고 있으니 답할 자료가 없다.")
            return
        }
        val a = view.authors
        val pinned = a.wrote(file.path, line)
        if (pinned != null) {
            val why = pinned.asked?.let { "\n요청: " + it } ?: "\n그 턴의 요청은 이 창이 받은 범위 밖이라 모른다."
            say(e, "이 줄(" + line + ")은 #" + pinned.seq + " 의 " + pinned.tool + " 이 썼다." + why)
            return
        }
        val all = a.of(file.path)
        if (all.isEmpty()) {
            say(e, "이 창이 받은 전사 안에서는 이 파일을 건드린 턴이 없다.\n(창이 열린 뒤의 것만 안다 — 그전은 모른다.)")
            return
        }
        // 여기가 이 액션의 요점이다. 줄을 못 짚는다고 아무 말도 안 하는 것이 아니라, 아는 만큼을
        // 내놓고 **모르는 자리를 이름 붙여 말한다.**
        say(e, buildString {
            append("이 줄은 못 짚는다 — 뒤에 온 편집이 줄을 밀어냈다.\n")
            append("이 파일을 건드린 턴 " + all.size + "개:\n")
            all.takeLast(6).forEach { t ->
                append("  #" + t.seq + " " + t.tool)
                t.lines?.let { append(" (" + it.first + "-" + it.last + " 당시)") }
                append("  " + (t.asked?.take(70) ?: "요청 모름") + "\n")
            }
        })
    }

    private fun say(e: AnActionEvent, text: String) {
        JBPopupFactory.getInstance()
            .createMessage(text)
            .showInBestPositionFor(e.dataContext)
    }
}
