package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.fileEditor.FileEditorManagerEvent
import com.intellij.openapi.fileEditor.FileEditorManagerListener
import com.intellij.openapi.fileEditor.FileDocumentManager
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.transport.SocketPath
import dev.sayaya.magi.ide.usecase.Assist
import java.nio.file.Paths

/**
 * 지금 열어 둔 버퍼를 컴패니언에게 알린다.
 *
 * 이게 없으면 사람이 저장을 안 한 채 두었을 때 에이전트의 `read` 는 디스크를 읽고 낡은 내용을
 * 추론한다. 콘솔이 에디터에서 같은 것을 보내고(`/open-file`), 여기가 그 IDE 판이다.
 *
 * 모델 호출이 아니고 기록도 아니다 — 다음 턴이 주변 맥락으로 볼 뿐이라 실패해도 조용하다.
 */
class OpenBufferListener : FileEditorManagerListener {

    override fun selectionChanged(event: FileEditorManagerEvent) {
        val project = event.manager.project
        val file = event.newFile ?: return
        val base = project.basePath ?: return
        val doc = FileDocumentManager.getInstance().getDocument(file)
        val text = doc?.text ?: return
        val sock = SocketPath.of(SocketPath.configDir(), Paths.get(base))
        ApplicationManager.getApplication().executeOnPooledThread {
            Assist({ DaemonClient.connect(sock) }).setOpenFile(file.path, text)
        }
    }

    companion object {
        fun install(manager: FileEditorManager) {
            manager.project.messageBus.connect()
                .subscribe(FileEditorManagerListener.FILE_EDITOR_MANAGER, OpenBufferListener())
        }
    }
}
