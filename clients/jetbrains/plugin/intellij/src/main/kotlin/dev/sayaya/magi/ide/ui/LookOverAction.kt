package dev.sayaya.magi.ide.ui

import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.wm.ToolWindowManager
import com.intellij.ui.components.JBScrollPane
import com.intellij.ui.content.ContentFactory
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.usecase.Assist
import dev.sayaya.magi.ide.usecase.LookNotes
import com.intellij.openapi.fileEditor.OpenFileDescriptor
import com.intellij.openapi.vfs.VirtualFile
import java.awt.event.MouseAdapter
import java.awt.event.MouseEvent
import javax.swing.SwingUtilities

/**
 * 현재 열려 있는 에디터 버퍼의 코드를 컴패니언에게 검토 요청하는 액션.
 *
 * 디스크가 아닌 에디터의 실시간 문서 버퍼 텍스트를 전달하여 미저장 변경사항까지 검토 대상에 포함한다.
 * 검토 결과는 알림 풍선 대신 하단 도구 창의 보조 탭으로 표출하여 긴 텍스트의 가독성과 보존성을 확보한다.
 */
class LookOverAction : AnAction(), com.intellij.openapi.project.DumbAware {

    // 팝업 메뉴 내 시각적 일관성을 확보하고 리소스 키 오타를 컴파일 타임에 검증하기 위해 코드에서 직접 아이콘을 지정한다 (2026-09-01 실측 피드백).
    init { templatePresentation.icon = com.intellij.icons.AllIcons.General.InspectionsEye }

    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    /** 에디터 및 가상 파일이 유효한 경우에만 액션을 활성화한다. */
    override fun update(e: AnActionEvent) {
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
            // 모델의 지적 사항 부재(정상)와 데몬 연결 실패를 명확히 분기하여 표출
            show(project, said?.takeIf { it.isNotBlank() } ?: MagiBundle.msg("chat.look.nothing"), file)
        }
    }

    companion object {
        /**
         * 검토 결과 표출용 도구 창 탭.
         * 우클릭 액션과 [LookWhileTyping]의 '전체 보기' 액션이 동일 탭을 공유하여 사용자 혼선을 방지한다.
         */
        fun show(project: Project, body: String, file: VirtualFile? = null) = SwingUtilities.invokeLater {
            val tw = ToolWindowManager.getInstance(project).getToolWindow("magi") ?: return@invokeLater
            // 파일을 알면 줄에 걸리는 말을 번호째 세우고, 그 줄을 누르면 편집기의 그 줄로 간다.
            // 예전엔 답을 받은 그대로 적어 `11\t…` 의 번호가 눌리지 않는 글자였다(2026-09-28 스크린샷).
            val split = file?.let { LookNotes.split(body) }?.takeIf { it.anchored.isNotEmpty() }
            val rows = split?.let { LookNotes.listing(it) }
            val text = if (rows == null) body else
                MagiBundle.msg("chat.look.head", file.name, split.anchored.size) + "\n\n" +
                    rows.joinToString("\n") { it.second }
            val area = WordArea(text).apply {
                isEditable = false; lineWrap = true; wrapStyleWord = true
                border = com.intellij.util.ui.JBUI.Borders.empty(6, 10) // 글자가 틀에 붙어 있었다(같은 스크린샷)
            }
            if (rows != null) area.addMouseListener(object : MouseAdapter() {
                override fun mouseClicked(ev: MouseEvent) {
                    val row = runCatching { area.getLineOfOffset(area.viewToModel2D(ev.point)) }.getOrNull() ?: return
                    val line = rows.getOrNull(row - HEAD_ROWS)?.first ?: return
                    OpenFileDescriptor(project, file, line - 1, 0).navigate(true)
                }
            })
            val cm = tw.contentManager
            cm.findContent(TAB)?.let { cm.removeContent(it, true) }
            val content = ContentFactory.getInstance().createContent(JBScrollPane(area), TAB, false)
            cm.addContent(content)
            cm.setSelectedContent(content)
            tw.activate(null)
        }

        /** 머리 줄과 빈 줄 — 탭의 행 번호에서 [LookNotes.listing] 의 번호로 가는 차이. */
        private const val HEAD_ROWS = 2

        /** 동적 다국어 로케일을 지원하는 검토 탭 타이틀 (가이드라인 검토 G5). */
        val TAB: String get() = MagiBundle.msg("chat.look.tab")
    }
}
