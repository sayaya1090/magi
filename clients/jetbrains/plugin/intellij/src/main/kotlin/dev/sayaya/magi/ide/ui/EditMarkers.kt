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
 * 컴패니언이 수정한 파일의 변경 사항을 에디터 내부에 직접 인라인으로 렌더링한다.
 *
 * 사용자 피드백(「파일별 diff 리뷰도 마찬가지고. 이건 편집된 파일의 편집 인터페이스에 바로 그릴
 * 수 있잖아」)을 반영하여, 별도 패널에 diff를 출력하는 대신 대상 파일을 열고 플랫폼의 라인 상태 트래커
 * ([SimpleLocalLineStatusTracker])를 초기화한다. 거터에 변경 인디케이터가 표시되며, 인디케이터 클릭 시
 * 플랫폼 자체 인라인 diff 팝업(되돌리기 및 복사 기능 포함)이 호출되므로 플러그인이 UI 요소를 직접 그리지 않는다.
 * 변경 기준선(Base Revision)은 VCS가 보관하는 이전 수정본 내용이며, 기준선을 획득할 수 없는 경우 트래커를
 * 등록하지 않는다 (불확실한 기준선을 임의 생성하여 변경 내역 왜곡 방지, §0.5-7).
 */
internal object EditMarkers {

    private val tracked = java.util.concurrent.ConcurrentHashMap<String, SimpleLocalLineStatusTracker>()

    /**
     * [rel] 워크스페이스 상대 경로(또는 코어가 전달한 절대 경로)의 파일을 열고 변경 추적 마커를 등록한다.
     * 디스크/네트워크 I/O가 수반되는 VCS 이전 수정본 판독은 백그라운드 풀 스레드에서 비동기로 수행하고,
     * 트래커 초기화 및 에디터 표출만 EDT에서 수행한다.
     */
    fun show(project: Project, rel: String) {
        val base = project.basePath ?: return
        ApplicationManager.getApplication().executeOnPooledThread {
            // 코어가 절대 경로를 전달하는 경우에 대비하여 경로를 정규화한다.
            // 파일이 존재하지 않는 경우 알림을 표시한다 (무반응 버튼 금지 원칙, §0.5-7).
            val slashed = rel.replace('\\', '/')
            val abs = if (java.nio.file.Paths.get(slashed).isAbsolute) slashed else "$base/$slashed"
            val vf = LocalFileSystem.getInstance().refreshAndFindFileByPath(abs)
            if (vf == null) { tell(project, MagiBundle.msg("edit.notfound", rel)); return@executeOnPooledThread }
            // 디스크 및 VCS 저장소 접근은 EDT 블로킹을 방지하기 위해 백그라운드 스레드에서만 수행한다.
            val baseText = runCatching {
                ChangeListManager.getInstance(project).getChange(vf)?.beforeRevision?.content
            }.getOrNull()
            SwingUtilities.invokeLater {
                FileEditorManager.getInstance(project).openFile(vf, true)
                if (baseText == null) return@invokeLater // 유효한 기준선이 없으면 마커를 등록하지 않는다.
                val doc = runReadActionBlocking {
                    FileDocumentManager.getInstance().getDocument(vf)
                } ?: return@invokeLater
                // 플랫폼이 이미 동일한 문서를 추적하고 있는 경우 중복 등록하지 않는다 (거터 충돌 방지, §0-5).
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
                    tell(project, MagiBundle.msg("edit.nomarkers"))
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

    /** 툴윈도 또는 화면 종료 시 등록된 트래커 리소스를 해제하여 문서 리스너 누수를 방지한다. */
    fun release() {
        val all = tracked.values.toList()
        tracked.clear()
        SwingUtilities.invokeLater { all.forEach { runCatching { it.release() } } }
    }
}
