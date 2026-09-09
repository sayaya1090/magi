package dev.sayaya.magi.ide.ui

import com.intellij.codeInsight.intention.IntentionAction
import com.intellij.codeInsight.intention.preview.IntentionPreviewInfo
import com.intellij.openapi.editor.Editor
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.project.DumbAware
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindowManager
import com.intellij.psi.PsiFile
import dev.sayaya.magi.ide.model.FileRef

/**
 * 에디터 컨텍스트 액션(Alt+Enter, Show Context Actions)에 등록되는 magi 인텐션 그룹.
 *
 * 키보드 중심 작업 흐름을 지원하며, 팝업 목록의 과도한 확장을 방지하기 위해 컨텍스트별로 관련성 높은 인텐션만 선별적으로 노출한다:
 * - 코드 선택 영역 존재 시: [AttachIntention] (선택 블록을 다음 대화 참조로 추가)
 * - 코드 선택 영역 부재 시: [ReviewIntention] (현재 파일 전체 코드 검토 요청)
 * - 해당 파일에 대한 에이전트 수정 이력 존재 시: [WroteThisIntention] (수정 턴 추적)
 *
 * 도구 창 조작 및 첨부 등 사이드 이펙트를 수반하므로 인텐션 프리뷰([IntentionPreviewInfo.EMPTY])는 비활성화한다.
 */
internal abstract class MagiIntention : IntentionAction, DumbAware {
    override fun getFamilyName() = MagiBundle.msg("intention.magi.family")
    override fun startInWriteAction() = false
    override fun generatePreview(project: Project, editor: Editor, file: PsiFile) = IntentionPreviewInfo.EMPTY
}

/** 선택된 코드 블록을 다음 대화 메시지의 참조로 등록하는 인텐션. */
internal class AttachIntention : MagiIntention() {
    override fun getText() = MagiBundle.msg("intention.attach.text")

    override fun isAvailable(project: Project, editor: Editor?, file: PsiFile?) =
        editor != null && file?.virtualFile != null && editor.selectionModel.hasSelection()

    override fun invoke(project: Project, editor: Editor?, file: PsiFile?) {
        editor ?: return
        val path = file?.virtualFile?.path ?: return
        val view = MagiWindows.of(project)
            ?: return run { ToolWindowManager.getInstance(project).getToolWindow("magi")?.show() }
        Attach.refs(editor, path, Attach.WhenBare.Nothing).forEach(view::attach)
        ToolWindowManager.getInstance(project).getToolWindow("magi")?.show()
    }
}

/** 현재 파일에 대해 코드 검토를 요청하는 인텐션 (선택 영역이 없는 경우에만 활성화). */
internal class ReviewIntention : MagiIntention() {
    override fun getText() = MagiBundle.msg("intention.lookNow.text")

    override fun isAvailable(project: Project, editor: Editor?, file: PsiFile?): Boolean {
        val vf = file?.virtualFile ?: return false
        val base = project.basePath ?: return false
        return editor != null && editor.selectionModel.hasSelection().not() &&
            vf.path.startsWith("$base/")
    }

    override fun invoke(project: Project, editor: Editor?, file: PsiFile?) {
        val vf = file?.virtualFile ?: return
        LookWhileTyping.askNow(project, vf)
        LookWhileTyping.refreshIcons(project)
    }
}

/** 해당 파일에 대한 에이전트 수정 이력이 존재하는 경우에만 노출되는 수정 턴 추적 인텐션. */
internal class WroteThisIntention : MagiIntention() {
    override fun getText() = MagiBundle.msg("intention.wroteThis.text")

    override fun isAvailable(project: Project, editor: Editor?, file: PsiFile?): Boolean {
        val path = file?.virtualFile?.path ?: return false
        editor ?: return false
        val authors = MagiWindows.of(project)?.authors ?: return false
        return authors.of(path).isNotEmpty()
    }

    override fun invoke(project: Project, editor: Editor?, file: PsiFile?) {
        editor ?: return
        val path = file?.virtualFile?.path ?: return
        LookOverAction.show(project, WroteThisAction.report(project, path, editor.caretModel.logicalPosition.line + 1))
    }
}
