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
class LookNowAction : AnAction(), com.intellij.openapi.project.DumbAware {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        val project = e.project
        // 메인 툴바에는 편집기 컨텍스트가 안 실린다 — 지금 열려 있는 파일을 직접 묻는다.
        // (우클릭에서는 실리므로 그쪽 값을 먼저 쓴다.)
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: project?.let { current(it) }
        val base = project?.basePath
        val mine = project != null && file != null &&
            base != null && file.path.startsWith(base + "/")
        // **툴바에서는 사라지지 않는다.** 못 쓸 때 숨으면 옆 아이콘들이 그때마다 밀리고,
        // 메인 툴바는 사람이 직접 배치하는 자리라 배치가 프로젝트 상태에 따라 움직이면 근육
        // 기억이 안 선다(가이드라인 G12). 회색으로 서 있으면 설명이 툴팁으로 남아 「왜 못
        // 누르나」에 답할 자리도 생긴다. 우클릭 메뉴는 반대다 — 못 쓸 항목이 남의 메뉴
        // 바닥에 회색으로 쌓이면 그것대로 소음이라, 거기서는 그대로 숨는다.
        e.presentation.isVisible = e.isFromActionToolbar || mine
        e.presentation.isEnabled = mine
        e.presentation.icon = AllIcons.Actions.Preview
        e.presentation.description = MagiBundle.msg("action.magi.lookNow.description")
        e.presentation.text = MagiEditorMenu.item(e, "action.magi.lookNow.text")
        if (!mine) return
        val busy = LookWhileTyping.isRunning(project, file)
        if (busy) {
            e.presentation.icon = AnimatedIcon.Default.INSTANCE
            e.presentation.text = MagiBundle.msg("look.busy")
        }
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
