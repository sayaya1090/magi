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
class AttachToChatAction : AnAction(), com.intellij.openapi.project.DumbAware {

    // 메뉴에 넷이 나란히 서는데 하나만 아이콘이 있으면 나머지 셋이 빈칸처럼 보인다(사용자
    // 실측 2026-09-01). 대화에 **더한다** — 더하기. 첨부라는 말보다 하는 일에 가깝다.
    //
    // XML 이 아니라 여기서 준다. `icon="AllIcons.X.Y"` 는 이름이 틀려도 런타임 경고 한 줄이고,
    // 그 경고를 보는 사람은 없다 — 아이콘이 안 뜨는 것으로만 드러난다. 코드면 컴파일이 잡는다.
    init { templatePresentation.icon = com.intellij.icons.AllIcons.General.Add }

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    /** 켜짐과 실행이 같은 조건을 본다 — 눌러서 아무 일도 안 나는 메뉴는 없는 메뉴보다 나쁘다
     *  ([LookOverAction] 의 그 규칙; diff 뷰어·콘솔처럼 파일 없는 에디터에서 걸렸다). */
    override fun update(e: AnActionEvent) {
        // 글자는 **여기서** 못박는다: plugin.xml 의 번들 경로는 언어팩이 없을 때
        // JVM 기본 로케일로 새어 한국어가 뜬다(실측). MagiBundle 은 언어팩 유무로
        // 정하므로, 한 규칙으로 통일한다.
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
            // 뜨는 규칙은 한 자리에 있다([Attach]) — 저장·멀티캐럿·줄 표기가 세 곳에 흩어져
            // 있었고, 그중 하나만 고치는 날이 오게 두지 않는다.
            out += Attach.refs(editor, path, Attach.WhenBare.WholeFile)
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
