package dev.sayaya.magi.ide.ui

import com.intellij.codeInsight.intention.IntentionAction
import com.intellij.openapi.editor.Editor
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindowManager
import com.intellij.psi.PsiFile
import dev.sayaya.magi.ide.model.FileRef

/**
 * 에디터 컨텍스트 액션(Alt+Enter)용 'magi에게 물어보기' 인텐션 액션.
 *
 * 선택된 코드 블록(또는 캐럿 라인)을 파일 참조([FileRef])로 첨부하고, 하단 도구 창 입력 필드에 질문 접두사를 프리필한다 (SURVEY §3).
 * 참조 정확성을 보장하기 위해 코어 데몬이 디스크에서 읽기 전 미저장 버퍼를 플러시한다 ([AttachToChatAction] 동일 원칙).
 */
class AskAboutCodeIntention : IntentionAction {

    override fun getText() = MagiBundle.msg("intention.magi.text")
    override fun getFamilyName() = MagiBundle.msg("intention.magi.family")
    override fun startInWriteAction() = false

    /** 도구 창 지연 생성 특성을 고려하여 에디터와 파일이 유효하면 항상 사용 가능으로 노출한다. */
    override fun isAvailable(project: Project, editor: Editor?, file: PsiFile?): Boolean =
        editor != null && file?.virtualFile != null

    /** 사이드 이펙트(창 활성화 및 첨부 칩 추가)를 수반하므로 인텐션 프리뷰는 비활성화한다. */
    override fun generatePreview(
        project: Project, editor: Editor, file: PsiFile,
    ): com.intellij.codeInsight.intention.preview.IntentionPreviewInfo =
        com.intellij.codeInsight.intention.preview.IntentionPreviewInfo.EMPTY

    override fun invoke(project: Project, editor: Editor?, file: PsiFile?) {
        editor ?: return
        val path = file?.virtualFile?.path ?: return
        val window = ToolWindowManager.getInstance(project).getToolWindow("magi")
        val view = MagiWindows.of(project)
        if (view == null) {
            window?.show() // 도구 창이 미생성 상태인 경우 창을 활성화한다.
            return
        }
        // 선택 영역이 없는 경우 현재 캐럿이 위치한 라인을 기본 참조로 지정한다 ([Attach.WhenBare.CaretLines]).
        Attach.refs(editor, path, Attach.WhenBare.CaretLines).forEach(view::attach)
        // 도구 창 활성화 비동기 완료 시점에 프리필 텍스트를 주입하고 포커스를 부여한다.
        val start = MagiBundle.msg("chat.prefill.code")
        window?.activate({ view.prefill(start) }, true) ?: view.prefill(start)
    }
}
