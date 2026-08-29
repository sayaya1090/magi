package dev.sayaya.magi.ide.ui

import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.wm.ToolWindowManager
import dev.sayaya.magi.ide.model.FileRef

/**
 * 「magi: 대화에 첨부」 — 에디터 선택영역 또는 프로젝트 뷰의 파일을 참조로 세운다.
 *
 * **본문을 복사하지 않는다.** 실리는 것은 경로와 줄범위뿐이고(SURVEY §2 채택 1·2), 발췌는
 * 코어가 보낼 때 렌더해 프롬프트와 함께 영속한다 — 이웃들의 붙여넣기-스냅샷과 달리, ambient
 * 가 이미 미는 신선한 버퍼를 에이전트가 읽는다. "지금 내가 보는 이 줄들"만은 컴패니언이 스스로
 * 알 수 없는 컨텍스트라는 것이 이 액션의 존재 이유다.
 *
 * 에디터에 선택이 있으면 `경로:시작-끝`, 없으면 파일 전체. 프로젝트 뷰에선 고른 파일들 전부.
 */
class AttachToChatAction : AnAction() {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    /** 켜짐과 실행이 같은 조건을 본다 — 눌러서 아무 일도 안 나는 메뉴는 없는 메뉴보다 나쁘다
     *  ([LookOverAction] 의 그 규칙; diff 뷰어·콘솔처럼 파일 없는 에디터에서 걸렸다). */
    override fun update(e: AnActionEvent) {
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
            // 발췌는 코어가 **디스크**에서 읽는다(`internal/app/refs.go` 의 `renderRef`). 버퍼로
            // 센 줄번호가 저장 안 한 디스크와 갈리면 다른 텍스트가 "에이전트가 본 것"으로
            // 영속된다(리뷰 실측) — 붙이는 순간 저장해 그 창을 닫는다.
            com.intellij.openapi.fileEditor.FileDocumentManager.getInstance().saveDocument(editor.document)
            val doc = editor.document
            // 캐럿마다 하나 — 멀티캐럿 선택의 나머지가 소리 없이 빠지면 "사라지는 첨부 없음"이
            // 클라이언트에서 깨진다. 줄은 에디터 셈법(1-기준 포함), 계약의 낱말 그대로(FileRef.Lines).
            val sels = editor.caretModel.allCarets.filter { it.hasSelection() }
            if (sels.isEmpty()) out += FileRef(path)
            else sels.forEach { c ->
                val from = doc.getLineNumber(c.selectionStart) + 1
                // 선택 끝이 줄머리에 걸치면 그 줄은 실제로 안 골라진 것이다.
                val endOff = (c.selectionEnd - 1).coerceAtLeast(c.selectionStart)
                val to = doc.getLineNumber(endOff) + 1
                out += FileRef(path, if (from == to) "$from" else "$from-$to")
            }
        } else {
            e.getData(CommonDataKeys.VIRTUAL_FILE_ARRAY)?.forEach { vf -> out += FileRef(vf.path) }
        }
        if (out.isEmpty()) return
        if (view == null) {
            // 대화 창이 아직 없다 — 열어 주고 끝낸다. 참조를 허공에 쌓아 두면 창이 설 때 남의
            // 것이 된 값이 실려 간다(모르는 부수효과 금지).
            ToolWindowManager.getInstance(project).getToolWindow("magi")?.show()
            return
        }
        out.forEach(view::attach)
        ToolWindowManager.getInstance(project).getToolWindow("magi")?.show()
    }
}
