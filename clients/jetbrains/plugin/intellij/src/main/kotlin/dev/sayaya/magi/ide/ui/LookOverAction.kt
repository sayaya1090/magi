package dev.sayaya.magi.ide.ui

import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindowManager
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.components.JBTextArea
import com.intellij.ui.content.ContentFactory
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.usecase.Assist
import javax.swing.SwingUtilities

/**
 * 열어 둔 파일을 컴패니언에게 훑어보게 한다 — 콘솔의 `/look` 이 하는 그 일이고, 부르는 메서드도
 * 같다(`daemon.go` 의 `Client.LookOver`).
 *
 * **저장 안 한 내용을 보낸다.** 디스크가 아니라 편집기 버퍼를 읽는 이유는, 방금 고친 것을 봐 달라는
 * 것이 이 동작의 전부이기 때문이다. `read` 툴에게 시키면 디스크를 읽어 낡은 내용을 훑는다.
 *
 * 결과는 하단 독의 **둘째 탭**으로 간다. 풍선 알림에 넣지 않는 이유는 이것이 여러 문단짜리 글이고,
 * 읽는 중에 사라지면 다시 부르는 수밖에 없어서다.
 */
class LookOverAction : AnAction(), com.intellij.openapi.project.DumbAware {

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    /** 편집기와 파일이 있을 때만 보인다. 눌러서 아무 일도 안 나는 메뉴는 없는 메뉴보다 나쁘다. */
    override fun update(e: AnActionEvent) {
        // 글자는 **여기서** 못박는다: plugin.xml 의 번들 경로는 언어팩이 없을 때
        // JVM 기본 로케일로 새어 한국어가 뜬다(실측). MagiBundle 은 언어팩 유무로
        // 정하므로, 한 규칙으로 통일한다.
        e.presentation.text = MagiEditorMenu.item(e, "action.magi.lookOver.text")
        e.presentation.description = MagiBundle.msg("action.magi.lookOver.description")
        e.presentation.isEnabledAndVisible =
            e.project != null && e.getData(CommonDataKeys.EDITOR) != null && e.getData(CommonDataKeys.VIRTUAL_FILE) != null
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        val editor = e.getData(CommonDataKeys.EDITOR) ?: return
        val file = e.getData(CommonDataKeys.VIRTUAL_FILE) ?: return
        val text = editor.document.text
        val sock = Workspace(project).socket() ?: return show(project, MagiBundle.msg("chat.noworkspace"))

        show(project, MagiBundle.msg("chat.look.looking", file.name))
        ApplicationManager.getApplication().executeOnPooledThread {
            val said = runCatching { Assist({ DaemonClient.connect(sock) }).lookOver(file.path, text) }
                .getOrElse { MagiBundle.msg("chat.unreachable", it.message ?: MagiBundle.msg("common.noreason")) }
            // 빈 답과 못 물은 것을 가른다. 모델이 할 말이 없는 것과 데몬에 못 닿은 것은 다른 사건이다.
            show(project, said?.takeIf { it.isNotBlank() } ?: MagiBundle.msg("chat.look.nothing"))
        }
    }

    companion object {
        /**
         * 훑어본 글이 서는 자리 — 하단 독의 둘째 탭. 우클릭 액션과 **타이핑 중 훑어보기**가
         * 같은 자리를 쓴다(한 규칙, 한 벌): 같은 종류의 글이 두 자리에 서면 사람이 어디를
         * 봐야 하는지 배워야 한다.
         */
        fun show(project: Project, body: String) = SwingUtilities.invokeLater {
            val tw = ToolWindowManager.getInstance(project).getToolWindow("magi") ?: return@invokeLater
            val area = JBTextArea(body).apply { isEditable = false; lineWrap = true; wrapStyleWord = true }
            val cm = tw.contentManager
            cm.findContent(TAB)?.let { cm.removeContent(it, true) }
            val content = ContentFactory.getInstance().createContent(JBScrollPane(area), TAB, false)
            cm.addContent(content)
            cm.setSelectedContent(content)
            tw.activate(null)
        }

        /**
         * 탭 이름 — **번들에서 온다.** 여기 박아 두면 영어 IDE 의 하단 독에 한국어 탭 하나가
         * 선다(가이드라인 검토 G5). `const` 를 뗀 것은 그 사유다: 값이 로케일에 달렸다.
         */
        val TAB: String get() = MagiBundle.msg("chat.look.tab")
    }
}
