package dev.sayaya.magi.ide.ui

import com.intellij.openapi.fileEditor.FileEditor
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.ui.EditorNotificationPanel
import com.intellij.ui.EditorNotificationProvider
import java.util.function.Function
import javax.swing.JComponent

/**
 * 타이핑 중 훑어보기가 **할 말이 있을 때만** 서는 띠. 편집기 위, IDE 가 원래 이런 말을 세우는
 * 자리다(§0-5: IDE 에 있는 것은 만들지 않는다).
 *
 * 할 말이 없으면 아무것도 안 선다 — 침묵이 이 기능의 절반이다. 「닫기」는 그 말을 지우고,
 * 다음에 손을 멈출 때 다시 묻는다.
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
