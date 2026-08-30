package dev.sayaya.magi.ide.ui

import com.intellij.icons.AllIcons
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.ui.AnimatedIcon

/**
 * **지금 훑어보기** — 웹 콘솔의 아이콘 단추와 같은 자리(사용자 지시). 자동(손 멈춤)이 꺼져
 * 있어도 이것은 답한다: 자동은 취향이고, 누른 것은 명시적 요청이다.
 *
 * 도는 동안 아이콘이 **스피너로 바뀐다** — 「눌렀는데 아무 일도 안 난다」와 「도는 중」이
 * 화면에서 같아 보이면 안 된다(사용자가 "동작중일 때 스피너 전환"으로 짚은 자리).
 */
class LookNowAction : AnAction() {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        val project = e.project
        // 메인 툴바에는 편집기 컨텍스트가 안 실린다 — 지금 열려 있는 파일을 직접 묻는다.
        // (우클릭에서는 실리므로 그쪽 값을 먼저 쓴다.)
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: project?.let { current(it) }
        val base = project?.basePath
        val mine = project != null && file != null &&
            base != null && file.path.startsWith(base + "/")
        e.presentation.isEnabledAndVisible = mine
        if (!mine) return
        val busy = LookWhileTyping.isRunning(project!!, file!!)
        e.presentation.icon = if (busy) AnimatedIcon.Default.INSTANCE else AllIcons.Actions.Preview
        e.presentation.text = if (busy) MagiBundle.msg("look.busy") else MagiBundle.msg("action.magi.lookNow.text")
        e.presentation.description = MagiBundle.msg("action.magi.lookNow.description")
    }

    /** 지금 편집기에 열려 있는 파일 — 툴바에서 부를 때의 대상. */
    private fun current(project: com.intellij.openapi.project.Project) =
        com.intellij.openapi.fileEditor.FileEditorManager.getInstance(project).selectedFiles.firstOrNull()

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: current(project) ?: return
        LookWhileTyping.askNow(project, file)
        // 누른 순간 아이콘이 스피너가 되게 — 다음 update 를 기다리지 않는다.
        LookWhileTyping.refreshIcons(project)
    }
}
