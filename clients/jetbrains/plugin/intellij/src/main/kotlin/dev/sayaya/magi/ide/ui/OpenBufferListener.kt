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
 * 지금 열어 둔 버퍼를 컴패니언에게 알린다.
 *
 * 이게 없으면 사람이 저장을 안 한 채 두었을 때 에이전트의 `read` 는 디스크를 읽고 낡은 내용을
 * 추론한다. 콘솔이 에디터에서 같은 것을 보내고(`/open-file`), 여기가 그 IDE 판이다.
 *
 * 모델 호출이 아니고 기록도 아니다 — 다음 턴이 주변 맥락으로 볼 뿐이라 실패해도 조용하다.
 *
 * **문이 조건만큼 열려야 한다.** 이 문이 파는 것은 「저장 안 한 내용」인데, 저장 안 한 내용이
 * 생기는 사건은 **타이핑**이지 탭 바꾸기가 아니다. 한때 이 클래스는 `selectionChanged` 에서만
 * 보냈고, 그래서 보낸 값은 **탭을 바꾼 순간의 스냅샷**이었다. 한 파일에서 십 분을 고치면
 * 모델은 십 분 전 글자를 「사람이 지금 편집 중인 것」으로 읽는다 — 맥락에서 가장 최근 자리를
 * 차지하는 슬롯에서.
 *
 * **낡음을 막는 코어의 장치는 이 클라이언트를 전제로 하지 않았다.** `app.ambientTTL` 의 주석이
 * 15분을 정당화하는 근거가 *"editing re-pushes every 600ms, so any live editor refreshes far
 * inside this"* 인데, 그 문장은 콘솔에 대해 참이고 예전의 이 클래스에 대해 거짓이었다. 그래서
 * 타이핑 중에도 15분이 지나면 버퍼가 조용히 사라졌다가 다음 탭 전환에 **옛 글자로** 되살아났다.
 * 남의 안전장치가 내 전제를 안 지키면 그 장치는 내 것이 아니다.
 *
 * **지우는 갈래가 없던 것이 나머지 반이다.** `app.SetOpenFile` 은 빈 텍스트를 「닫혔다」로 읽고,
 * 콘솔은 저장·취소·탭닫기마다 그것을 보낸다. 코어 주석이 그 없음을 이미 이름으로 부르고 있다 —
 * *"an old console that never learned to clear"*. 이 플러그인이 그것이었다: 탭을 닫아도 버퍼는
 * TTL 이 끊을 때까지 계속 「사람이 편집 중인 파일」로 턴마다 실려 갔다.
 */
class OpenBufferListener : FileEditorManagerListener {

    /**
     * 데몬이 들고 있는 것으로 아는 경로. 칸이 **하나**라(세션당 `openFile` 한 개) 닫힘을 받을 때
     * 그게 올라가 있는 파일일 때만 지운다 — 아니면 방금 올라간 남의 값을 지운다.
     *
     * EDT 에서만 읽고 쓴다: 세 진입점(선택 바뀜·디바운스 타이머·닫힘)이 전부 EDT 다.
     */
    private var standing: String? = null

    /**
     * 마지막 뜻만 이긴다. 지우기와 밀기가 **같은** 세대 번호를 쓰므로, 지운 뒤에 도착한 밀기도
     * 밀린 뒤에 도착한 지우기도 자기가 낡은 것을 알고 그만둔다. 콘솔의 `++openAt` 과 같은 수인데,
     * 저쪽은 밀기만 세고 이쪽은 둘 다 센다 — 보내는 일이 풀 스레드로 나가 순서가 안 보장된다.
     */
    private val gen = AtomicLong()

    /** 귀를 이미 붙인 파일. 스플릿에서 같은 파일이 두 번 열려도 두 번 안 붙인다. */
    private val heard = mutableSetOf<VirtualFile>()

    private var pending: Pair<Project, VirtualFile>? = null

    /**
     * 글자 하나마다 보내지 않는다. **콘솔과 같은 600ms** 이고, 같은 수인 것이 중요하다 —
     * 코어의 TTL 이 그 숫자를 근거로 15분을 고른다.
     */
    private val debounce = Timer(600) { pending?.let { (p, f) -> send(p, f.path, textOf(f)) } }
        .apply { isRepeats = false }

    override fun selectionChanged(event: FileEditorManagerEvent) {
        // 마지막 탭이 닫혀 고를 것이 없는 경우는 여기서 안 지운다 — `fileClosed` 가 지운다.
        // 지우는 갈래를 이 `return` **위**로 올리면 A→B 전환에서 B 를 지운다.
        val file = event.newFile ?: return
        listen(event.manager, file)
        send(event.manager.project, file.path, textOf(file) ?: return)
    }

    /**
     * 탭이 닫히면 데몬의 사본도 닫는다. 안 하면 그 버퍼가 「사람이 편집 중인 파일」로 세션의
     * 남은 턴마다 실려 간다 — 사람은 몇 시간 전에 그 파일을 떠났는데.
     */
    override fun fileClosed(source: FileEditorManager, file: VirtualFile) {
        if (standing != file.path) return
        send(source.project, file.path, "")
    }

    /**
     * 문서에 귀를 붙인다 — **탭이 사는 동안만.** 부모를 `FileEditor` 로 두면 탭이 닫힐 때
     * 플랫폼이 떼 준다. `Document` 는 프로젝트보다 오래 사는 물건이라 손으로 떼기로 하면
     * 언젠가 안 떼는 갈래가 생기고, 그때 죽은 프로젝트가 계속 보낸다.
     */
    private fun listen(manager: FileEditorManager, file: VirtualFile) {
        if (file in heard) return
        // getDocument 자체가 read-action 을 요구한다 — 실측 SEVERE 의 하위 프레임이
        // FileDocumentManagerBase.getDocument 였다(리뷰 F1 확정): textOf 만이 아니라
        // 이 맨몸 호출도 같은 병이다.
        val doc = com.intellij.openapi.application.runReadActionBlocking {
            FileDocumentManager.getInstance().getDocument(file)
        } ?: return
        val editor = manager.getSelectedEditor(file) ?: return
        heard.add(file)
        val project = manager.project
        doc.addDocumentListener(object : DocumentListener {
            override fun documentChanged(e: DocumentEvent) {
                // 안 보이는 스플릿에서 난 편집으로 「열어 둔 파일」을 바꿔치지 않는다.
                if (file !in manager.selectedFiles) return
                pending = project to file
                debounce.restart()
            }
        }, editor)
        Disposer.register(editor) { heard.remove(file) }
    }

    private fun textOf(file: VirtualFile): String? =
        // 디바운스 타이머는 EDT 에서 울리는데, 2026.1 스레딩 규칙은 EDT 라도 read-action 없이
        // 모델(Document)을 못 읽게 한다 — 샌드박스 라이브에서 이 줄이 SEVERE(ThreadingAssertions,
        // Plugin to blame: magi)로 잡혔다. runReadActionBlocking(명시적 **비취소** read)을
        // 골랐다: 600ms 에 한 번 Document.text 복사 한 번이라 취소 기계가 낄 이유가 없고,
        // deprecation 이 첫 대안으로 미는 ReadAction.nonBlocking 은 비동기라 이 함수의 동기
        // 반환 계약을 깬다(리뷰 F2 — 함수명을 잘못 적은 주석이 그 "개선"을 부를 뻔했다).
        com.intellij.openapi.application.runReadActionBlocking {
            FileDocumentManager.getInstance().getDocument(file)?.text
        }

    private fun send(project: Project, path: String, text: String?) {
        val body = text ?: return
        // 소켓을 **여기서 따로 계산하지 않는다.** 설정 디렉토리를 IDE 의 environ 으로 정하고
        // 있었는데, 창 쪽은 사람의 셸이 아는 값을 쓴다 — 셸에서 `MAGI_CONFIG_DIR` 을 쓰는
        // 기계에서는 열린 버퍼가 **다른 소켓**으로 갔다. 한 규칙을 두 곳이 한 벌씩 적어 두면
        // 안 재지는 쪽이 갈린다(이 저장소가 여러 번 겪은 그것).
        val sock = Workspace(project).socket() ?: return
        val mine = gen.incrementAndGet()
        standing = if (body.isEmpty()) null else path
        ApplicationManager.getApplication().executeOnPooledThread {
            if (gen.get() != mine) return@executeOnPooledThread
            Assist({ DaemonClient.connect(sock) }).setOpenFile(path, body)
        }
    }
}
