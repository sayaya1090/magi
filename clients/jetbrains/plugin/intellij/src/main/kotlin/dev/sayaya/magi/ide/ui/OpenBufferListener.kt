package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.editor.event.DocumentEvent
import com.intellij.openapi.editor.event.DocumentListener
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.fileEditor.FileEditorManagerEvent
import com.intellij.openapi.fileEditor.FileEditorManagerListener
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Disposer
import com.intellij.openapi.vfs.VirtualFile
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.transport.SocketPath
import dev.sayaya.magi.ide.usecase.Assist
import java.nio.file.Paths
import java.util.concurrent.atomic.AtomicLong
import javax.swing.Timer

/**
 * 현재 활성 에디터 버퍼의 미저장 내용을 백엔드 데몬에 실시간 동기화하는 리스너 ([FileEditorManagerListener]).
 *
 * 사용자가 저장하지 않은 편집 상태에서 에이전트가 디스크의 이전 내용을 판독하는 것을 방지하기 위해,
 * 웹 콘솔의 `/open-file`과 동일하게 활성 버퍼 내용을 백엔드에 지속적으로 전달한다.
 *
 * 데몬의 ambient TTL(15분)은 클라이언트의 600ms 주기 갱신을 전제로 설계되었으므로,
 * 탭 전환 이벤트뿐 아니라 타이핑 입력 시 600ms 디바운스 주기로 최신 텍스트를 재전송한다.
 * 또한 탭이 닫힐 때(`fileClosed`) 빈 문자열을 전송하여 데몬의 열린 파일 캐시를 즉시 해제한다 (`app.SetOpenFile`).
 */
class OpenBufferListener : FileEditorManagerListener {

    /**
     * 데몬에 현재 등록된 것으로 추적되는 파일 경로.
     * 세션당 단일 파일 슬롯이므로 탭 닫힘 이벤트 수신 시 현재 등록된 파일과 일치할 때만 해제 요청을 보낸다.
     * 세 이벤트 진입점(선택 변경, 디바운스 타이머, 탭 닫힘) 모두 EDT에서 실행되므로 EDT 스레드에서만 접근한다.
     */
    private var standing: String? = null

    /**
     * 비동기 전송 스케줄링 간 레이스 컨디션을 방지하기 위한 세대 번호.
     * 백그라운드 풀 스레드에서 비동기 전송 시 지연 수신된 이전 요청이 최신 상태를 덮어쓰지 않도록 차단한다.
     */
    private val gen = AtomicLong()

    /** 중복 리스너 부착 방지를 위한 등록 완료 파일 집합 (에디터 분할 배치 대응). */
    private val heard = mutableSetOf<VirtualFile>()

    private var pending: Pair<Project, VirtualFile>? = null

    /** 버퍼 전송 디바운스 타이머 (600ms, 코어 ambientTTL 갱신 주기 연동). */
    private val debounce = Timer(600) { pending?.let { (p, f) -> send(p, f.path, textOf(f)) } }
        .apply { isRepeats = false }

    override fun selectionChanged(event: FileEditorManagerEvent) {
        val file = event.newFile ?: return
        listen(event.manager, file)
        send(event.manager.project, file.path, textOf(file) ?: return)
    }

    /** 탭 닫힘 시 데몬의 열린 버퍼 캐시를 해제하기 위해 빈 문자열을 전송한다. */
    override fun fileClosed(source: FileEditorManager, file: VirtualFile) {
        if (standing != file.path) return
        send(source.project, file.path, "")
    }

    /**
     * 대상 문서에 편집 감지 리스너를 등록한다.
     * 메모리 누수를 방지하기 위해 리스너 수명주기는 [FileEditor]의 생명주기에 바인딩한다.
     */
    private fun listen(manager: FileEditorManager, file: VirtualFile) {
        if (file in heard) return
        // IntelliJ 2026.1 스레딩 규칙에 따라 getDocument 호출을 ReadAction 내에서 수행한다 (리뷰 F1).
        val doc = com.intellij.openapi.application.runReadActionBlocking {
            FileDocumentManager.getInstance().getDocument(file)
        } ?: return
        val editor = manager.getSelectedEditor(file) ?: return
        heard.add(file)
        val project = manager.project
        doc.addDocumentListener(object : DocumentListener {
            override fun documentChanged(e: DocumentEvent) {
                if (file !in manager.selectedFiles) return
                pending = project to file
                debounce.restart()
            }
        }, editor)
        Disposer.register(editor) { heard.remove(file) }
    }

    private fun textOf(file: VirtualFile): String? =
        // 2026.1 플랫폼의 EDT ThreadingAssertions 위반을 방지하기 위해 runReadActionBlocking을 사용한다 (리뷰 F2).
        com.intellij.openapi.application.runReadActionBlocking {
            FileDocumentManager.getInstance().getDocument(file)?.text
        }

    private fun send(project: Project, path: String, text: String?) {
        val body = text ?: return
        // 환경 변수 일관성을 보장하기 위해 단일화된 Workspace 소켓 경로를 참조한다.
        val sock = Workspace(project).socket() ?: return
        val mine = gen.incrementAndGet()
        standing = if (body.isEmpty()) null else path
        ApplicationManager.getApplication().executeOnPooledThread {
            if (gen.get() != mine) return@executeOnPooledThread
            Assist({ DaemonClient.connect(sock) }).setOpenFile(path, body)
        }
    }
}
