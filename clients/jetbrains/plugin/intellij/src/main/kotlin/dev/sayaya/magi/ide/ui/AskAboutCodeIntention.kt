package dev.sayaya.magi.ide.ui

import com.intellij.codeInsight.intention.IntentionAction
import com.intellij.openapi.editor.Editor
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindowManager
import com.intellij.psi.PsiFile
import dev.sayaya.magi.ide.model.FileRef

/**
 * Alt+Enter 의 「magi에게 물어보기」 — 이웃들의 인라인 프롬프트가 서는 그 자리에, 우리 몸에
 * 맞는 모양으로: 선택(또는 캐럿 줄)을 **참조로 첨부**하고 컴포저에 물음의 시작을 앉힌다.
 * 지시는 사람이 마저 쓰고 Enter — 편집은 컴패니언 손이 하고 diff 는 전사·승인 프롬프트가
 * 보인다(SURVEY §3 채택: 인라인 편집의 1단).
 *
 * 발췌 정확성의 규칙은 [AttachToChatAction] 과 같다 — 코어가 디스크에서 읽으므로 붙이기 전에
 * 저장한다. 그쪽이 리뷰로 산 교훈을 여기서 다시 사지 않는다.
 */
class AskAboutCodeIntention : IntentionAction {

    override fun getText() = MagiBundle.msg("intention.magi.text")
    override fun getFamilyName() = MagiBundle.msg("intention.magi.family")
    override fun startInWriteAction() = false

    /** 창 유무는 안 본다 — 툴윈도는 게으르고(plugin.xml 의 실측), 안 뜨는 항목은 배울 수도
     *  없다. 창이 없으면 [invoke] 가 열어 주고 멈춘다(이웃 [AttachToChatAction] 의 갈래). */
    override fun isAvailable(project: Project, editor: Editor?, file: PsiFile?): Boolean =
        editor != null && file?.virtualFile != null

    /** 프리뷰는 없음 — 부수효과(첨부·창 열기)가 프리뷰 사본에서 안 도는 것을 요행이 아니라
     *  계약으로. */
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
            window?.show() // 창이 서면 다음 Alt+Enter 가 통한다 — 허공에 참조를 쌓지 않는다
            return
        }
        // 고른 것이 없으면 **캐럿이 선 줄**이다 — 「이 코드」가 가리키는 것은 지금 그 줄이지
        // 파일 전체가 아니다(우클릭 쪽은 반대로 고른다 — [Attach.WhenBare] 가 그 갈림을 적는다).
        Attach.refs(editor, path, Attach.WhenBare.CaretLines).forEach(view::attach)
        // activate 의 콜백에서 채운다 — show 의 비동기 완료 전에 포커스를 청하면 안 앉는다(리뷰).
        val start = MagiBundle.msg("chat.prefill.code")
        window?.activate({ view.prefill(start) }, true) ?: view.prefill(start)
    }
}
