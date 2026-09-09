package dev.sayaya.magi.ide.ui

import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.wm.ToolWindowManager
import dev.sayaya.magi.ide.model.FileRef

/**
 * 에디터 선택 영역 또는 프로젝트 뷰의 파일을 대화 컨텍스트 참조([FileRef])로 등록하는 액션.
 *
 * 본문 전체를 복사하지 않고 파일 경로 및 라인 범위(`path:start-end`)만을 참조로 전달한다 (SURVEY §2).
 * 에디터에 선택 영역이 있으면 해당 라인 범위를, 없으면 파일 전체를 지정하며, 프로젝트 뷰에서는 선택된 모든 파일을 일괄 등록한다.
 */
class AttachToChatAction : AnAction(), com.intellij.openapi.project.DumbAware {

    // 팝업 메뉴 내 시각적 일관성을 확보하고 리소스 키 오타를 컴파일 타임에 검증하기 위해 코드에서 직접 아이콘을 지정한다 (2026-09-01 실측 피드백).
    init { templatePresentation.icon = com.intellij.icons.AllIcons.General.Add }

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    /** 에디터 및 가상 파일이 유효한 경우에만 액션을 활성화한다 (비파일 에디터 오작동 방지). */
    override fun update(e: AnActionEvent) {
        e.presentation.text = MagiEditorMenu.item(e, "action.magi.attach.text")
        e.presentation.description = MagiBundle.msg("action.magi.attach.description")
        val editorReady = e.getData(CommonDataKeys.EDITOR) != null &&
            e.getData(CommonDataKeys.VIRTUAL_FILE) != null
        e.presentation.isEnabledAndVisible = e.project != null &&
            (editorReady || !e.getData(CommonDataKeys.VIRTUAL_FILE_ARRAY).isNullOrEmpty())
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val view = MagiWindows.of(project)
        val editor = e.getData(CommonDataKeys.EDITOR)
        val out = mutableListOf<FileRef>()
        if (editor != null) {
            val path = e.getData(CommonDataKeys.VIRTUAL_FILE)?.path ?: return
            // 에디터 버퍼 저장 및 라인 범위 판정 로직을 [Attach] 모듈로 단일화하여 처리한다.
            out += Attach.refs(editor, path, Attach.WhenBare.WholeFile)
        } else {
            e.getData(CommonDataKeys.VIRTUAL_FILE_ARRAY)?.forEach { vf -> out += FileRef(vf.path) }
        }
        if (out.isEmpty()) return
        if (view == null) {
            // 대화 도구 창이 미생성 상태인 경우 창만 활성화하고 종료한다 (미초기화 상태에서의 참조 유실 방지).
            ToolWindowManager.getInstance(project).getToolWindow("magi")?.show()
            return
        }
        out.forEach(view::attach)
        ToolWindowManager.getInstance(project).getToolWindow("magi")?.show()
    }
}
