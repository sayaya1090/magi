package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.application.runReadActionBlocking
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.vcs.changes.ChangeListManager
import com.intellij.openapi.vcs.ex.SimpleLocalLineStatusTracker
import com.intellij.openapi.vfs.LocalFileSystem
import javax.swing.SwingUtilities

/**
 * 컴패니언이 고친 파일의 **편집 화면 안에** 변경을 그린다.
 *
 * 사용자 교정: 「파일별 diff 리뷰도 마찬가지고. 이건 편집된 파일의 편집 인터페이스에 바로 그릴
 * 수 있잖아」 — 그래서 별도 판에 diff 를 그리는 대신, 그 파일을 열고 IDE 의 라인 상태 트래커를
 * 세운다. 거터에 변경 막대가 서고, 막대를 누르면 IDE 가 제 인라인 diff 팝업(되돌리기·복사
 * 포함)을 띄운다 — **우리는 아무것도 안 그린다.** 기준은 VCS 가 아는 이전 내용이고, 못 얻으면
 * 트래커를 안 세운다: 기준을 지어내면 「무엇이 바뀌었나」가 거짓이 된다.
 */
internal object EditMarkers {

    private val tracked = java.util.concurrent.ConcurrentHashMap<String, SimpleLocalLineStatusTracker>()

    /**
     * [rel] 워크스페이스 상대 경로의 파일을 열고 변경 막대를 세운다.
     * 무거운 것(VCS 이전 내용 읽기)은 풀드 스레드에서, 세우는 것만 EDT 에서.
     */
    fun show(project: Project, rel: String) {
        val base = project.basePath ?: return
        ApplicationManager.getApplication().executeOnPooledThread {
            // 경로는 절대일 수도 있다(코어가 그렇게 실어 보내는 자리가 있다) — refreshDisk 와
            // 같은 갈래다. 못 찾으면 **말한다**: 눌렀는데 아무 일도 안 나는 단추 금지(§0.5-7).
            val slashed = rel.replace('\\', '/')
            val abs = if (java.nio.file.Paths.get(slashed).isAbsolute) slashed else "$base/$slashed"
            val vf = LocalFileSystem.getInstance().refreshAndFindFileByPath(abs)
            if (vf == null) { tell(project, "그 파일을 못 찾았다 — $rel"); return@executeOnPooledThread }
            // content 는 디스크/네트워크를 탈 수 있어 EDT 금지다(이 저장소가 이미 문 자리에서 겪었다).
            val baseText = runCatching {
                ChangeListManager.getInstance(project).getChange(vf)?.beforeRevision?.content
            }.getOrNull()
            SwingUtilities.invokeLater {
                FileEditorManager.getInstance(project).openFile(vf, true)
                if (baseText == null) return@invokeLater // 기준을 모르면 막대를 안 세운다
                val doc = runReadActionBlocking {
                    FileDocumentManager.getInstance().getDocument(vf)
                } ?: return@invokeLater
                // IDE 가 이미 같은 기준으로 막대를 그리는 파일이면 **아무것도 안 세운다** —
                // 한 문서에 트래커 둘이면 거터가 겹친다(§0-5: IDE 에 있는 것은 안 만든다).
                val already = runCatching {
                    com.intellij.openapi.vcs.impl.LineStatusTrackerManager
                        .getInstance(project).getLineStatusTracker(doc) != null
                }.getOrDefault(false)
                if (already) return@invokeLater
                val t = runCatching {
                    tracked.computeIfAbsent(vf.path) {
                        SimpleLocalLineStatusTracker.createTracker(project, doc, vf)
                    }
                }.getOrElse {
                    tell(project, "이 IDE 에선 변경 막대를 못 세운다 — 파일만 열었다")
                    return@invokeLater
                }
                t.setBaseRevision(baseText)
            }
        }
    }

    private fun tell(project: Project, text: String) =
        com.intellij.notification.NotificationGroupManager.getInstance()
            .getNotificationGroup("magi")
            .createNotification(text, com.intellij.notification.NotificationType.WARNING)
            .notify(project)

    /** 창이 닫힐 때 — 트래커를 놓아 준다. 안 놓으면 문서에 리스너가 남는다. */
    fun release() {
        val all = tracked.values.toList()
        tracked.clear()
        SwingUtilities.invokeLater { all.forEach { runCatching { it.release() } } }
    }
}
