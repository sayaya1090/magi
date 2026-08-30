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
 * Alt+Enter(Show Context Actions)에 서는 것들 — 사용자 요청.
 *
 * **왜 우클릭 메뉴와 둘 다인가.** 두 자리는 사람이 다르게 온다: 우클릭은 마우스가 이미 가 있을
 * 때고, Alt+Enter 는 손이 자판에 있을 때다. 이웃들도 둘 다에 선다.
 *
 * **다만 넷을 다 세우지는 않는다.** Alt+Enter 는 남들도 채우는 **순위 목록**이라, 파일마다 우리
 * 줄이 넷이면 우클릭 메뉴에서 방금 없앤 그 소음을 여기 다시 만드는 것이다. 그래서 각자 **할 일이
 * 있을 때만** 선다: 첨부는 고른 것이 있을 때, 검토는 고른 것이 없을 때(파일 전체가 대상이라),
 * 「누가 썼나」는 이 창이 그 줄에 대해 **아는 것이 있을 때**. 보통 두 줄이 선다.
 *
 * 미리보기는 없다([IntentionPreviewInfo.EMPTY]) — 이것들의 일은 편집이 아니라 첨부·창 열기라,
 * 프리뷰 사본에서 부수효과가 안 도는 것을 요행이 아니라 계약으로 둔다([AskAboutCodeIntention]).
 */
internal abstract class MagiIntention : IntentionAction, DumbAware {
    override fun getFamilyName() = MagiBundle.msg("intention.magi.family")
    override fun startInWriteAction() = false
    override fun generatePreview(project: Project, editor: Editor, file: PsiFile) = IntentionPreviewInfo.EMPTY
}

/** 고른 코드를 다음 메시지의 참조로 — 우클릭 「채팅에 추가」와 같은 일, 같은 자리에서. */
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

/** 이 파일을 지금 훑는다 — 고른 것이 없을 때만 선다(고른 것이 있으면 첨부가 그 자리를 쓴다). */
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

/**
 * 이 줄을 어느 작업이 썼나. **아는 것이 있을 때만 선다** — 「모른다」만 말하려고 Alt+Enter 의
 * 한 줄을 차지하지 않는다(우클릭 메뉴 쪽은 물어볼 자리라 그때도 답한다).
 */
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
