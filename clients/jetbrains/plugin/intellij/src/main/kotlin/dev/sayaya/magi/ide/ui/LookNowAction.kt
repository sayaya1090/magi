package dev.sayaya.magi.ide.ui

import com.intellij.icons.AllIcons
import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.ui.AnimatedIcon

/**
 * 현재 열려 있는 파일에 대해 코드 검토([LookWhileTyping])를 즉시 요청하는 액션.
 *
 * 자동 검토 기능 비활성화 상태에서도 명시적 요청을 즉시 수행한다.
 * 검토가 진행 중인 동안 액션 아이콘을 스피너([AnimatedIcon.Default.INSTANCE])로 전환하여 비동기 실행 상태를 명확히 표시한다.
 */
class LookNowAction : AnAction(), com.intellij.openapi.project.DumbAware {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        val project = e.project
        // 메인 툴바 실행 시에는 에디터 컨텍스트가 주입되지 않으므로 현재 활성 에디터 파일을 직접 조회한다.
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: project?.let { current(it) }
        val base = project?.basePath
        val mine = project != null && file != null &&
            base != null && file.path.startsWith(base + "/")
        // 메인 툴바 아이콘 위치 변동으로 인한 사용자 조작 혼선을 방지하기 위해 툴바에서는 숨기지 않고 비활성화(disabled) 상태를 유지한다 (가이드라인 G12).
        // 반면 우클릭 컨텍스트 메뉴에서는 불필요한 시각적 노이즈를 방지하기 위해 숨김 처리한다.
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

    /** 현재 에디터에 열려 있는 활성 가상 파일 반환. */
    private fun current(project: com.intellij.openapi.project.Project) =
        com.intellij.openapi.fileEditor.FileEditorManager.getInstance(project).selectedFiles.firstOrNull()

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: current(project) ?: return
        LookWhileTyping.askNow(project, file)
        // 클릭 즉시 진행 스피너가 표시되도록 UI 알림을 갱신한다.
        LookWhileTyping.refreshIcons(project)
    }
}
