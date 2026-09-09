package dev.sayaya.magi.ide.ui

import com.intellij.openapi.fileEditor.FileEditor
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.ui.EditorNotificationPanel
import com.intellij.ui.EditorNotificationProvider
import java.util.function.Function
import javax.swing.JComponent

/**
 * 타이핑 중 코드 검토(LookWhileTyping) 피드백이 존재할 때 에디터 상단에 표시되는 알림 배너 ([EditorNotificationProvider]).
 *
 * 지적 사항이 없는 경우 배너를 노출하지 않으며, '닫기' 클릭 시 해당 파일의 캐시된 피드백을 제거하고 다음 입력 시 재검토를 수행한다.
 */
internal class LookBanner : EditorNotificationProvider {
    override fun collectNotificationData(
        project: Project,
        file: VirtualFile,
    ): Function<in FileEditor, out JComponent?>? {
        val note = LookWhileTyping.noteFor(project, file) ?: return null
        return Function { _ ->
            EditorNotificationPanel(EditorNotificationPanel.Status.Info).apply {
                text = "magi: " + note.lineSequence().first().take(160)
                createActionLabel(MagiBundle.msg("look.full")) { LookWhileTyping.showFull(project, file) }
                createActionLabel(MagiBundle.msg("look.close")) { LookWhileTyping.forget(project, file) }
            }
        }
    }
}
