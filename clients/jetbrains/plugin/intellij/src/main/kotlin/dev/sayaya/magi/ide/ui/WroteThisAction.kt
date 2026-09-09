package dev.sayaya.magi.ide.ui

import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.ui.popup.JBPopupFactory
import com.intellij.openapi.wm.ToolWindowManager

/**
 * 현재 캐럿이 위치한 라인을 수정한 에이전트 턴(Turn) 및 해당 턴의 사용자 요청 프롬프트를 추적하는 액션.
 *
 * VCS Git Blame과 달리 커밋되지 않은 세션 내 편집 내역을 추적한다.
 * 마지막 편집 작업의 명시적 범위(`at`/`to`) 내에 위치하여 라인 밀림이 없는 것이 확실한 경우 정확한 턴을 특정하고,
 * 그 외의 경우에는 해당 파일 전체를 수정한 턴 목록을 폴백으로 안내한다 (§5-5).
 */
class WroteThisAction : AnAction(), com.intellij.openapi.project.DumbAware {

    // 팝업 메뉴 내 시각적 일관성을 확보하고 리소스 키 오타를 컴파일 타임에 검증하기 위해 코드에서 직접 아이콘을 지정한다 (2026-09-01 실측 피드백).
    init { templatePresentation.icon = com.intellij.icons.AllIcons.Vcs.History }

    override fun getActionUpdateThread() = ActionUpdateThread.EDT

    override fun update(e: AnActionEvent) {
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
         * 라인 수정 작성자 및 관련 턴 정보 리포트 문자열 생성.
         * 우클릭 팝업 액션과 Alt+Enter 인텐션 액션이 공통으로 사용한다.
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
