package dev.sayaya.magi.ide.ui

import com.intellij.openapi.Disposable
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.application.runReadActionBlocking
import com.intellij.openapi.editor.event.DocumentEvent
import com.intellij.openapi.editor.event.DocumentListener
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.ui.EditorNotifications
import dev.sayaya.magi.ide.transport.DaemonClient
import dev.sayaya.magi.ide.usecase.Assist
import javax.swing.Timer

/**
 * 타이핑 중 코드 검토(LookWhileTyping) 제어기.
 *
 * 사용자의 입력 유휴(800ms 디바운스) 감지 시 현재 에디터 버퍼를 조회하여 백엔드 데몬의 `look-over` 엔드포인트를 호출하고,
 * 지적 사항이 있는 경우에만 행별 인레이 및 상단 배너로 안내한다. 지적 사항이 없으면 아무런 메시지도 노출하지 않는다.
 *
 * 웹 콘솔의 `files.look` 기능과 동일하며 동일한 `look-over` API를 호출한다.
 * 리뷰 호출 비용을 고려하여 기본값은 비활성화(`false`)로 설정되며, 설정값은 데몬이 아닌 프로젝트 로컬 환경([LocalPrefs])에 영속화된다.
 * 생성된 검토 의견은 트랜스크립트 세션에 기록되지 않고 에디터 뷰에만 일시적으로 표시된다.
 */
internal object LookWhileTyping {

    const val KEY = "magi.lookWhileTyping"

    fun enabled(project: Project): Boolean = LocalPrefs.look(project)

    fun setEnabled(project: Project, on: Boolean) {
        LocalPrefs.setLook(project, on)
        if (!on) {
            val mark = project.locationHash + " "
            said.keys.removeIf { it.startsWith(mark) }
            full.keys.removeIf { it.startsWith(mark) }
            // 기능 비활성화 시 열려 있는 에디터의 모든 인레이를 제거한다.
            ApplicationManager.getApplication().invokeLater {
                com.intellij.openapi.fileEditor.FileEditorManager.getInstance(project).openFiles
                    .forEach { LookInlays.clear(project, it) }
            }
            EditorNotifications.getInstance(project).updateAllNotifications()
        }
    }

    /**
     * 파일별 최종 검토 피드백 캐시. 지적 사항이 없는 파일은 알림 배너를 표시하지 않는다.
     * 복수 프로젝트에서 동일 파일 경로를 열람할 때의 충돌을 방지하기 위해 `project.locationHash + path` 복합 키를 사용한다.
     */
    private val said = java.util.concurrent.ConcurrentHashMap<String, String>()

    /** 전체 보기 액션용 원본 피드백 캐시 (라인별 인레이 텍스트 포함). */
    private val full = java.util.concurrent.ConcurrentHashMap<String, String>()

    /** 현재 검토가 진행 중인 파일 집합 (UI 진행 스피너 표시 기준). */
    private val running = java.util.concurrent.ConcurrentHashMap.newKeySet<String>()

    fun isRunning(project: Project, file: VirtualFile): Boolean = running.contains(key(project, file))

    /**
     * 수동 즉시 검토 실행. 디바운스 대기 없이 즉시 데몬에 검토를 요청하며, 자동 기능 비활성화 상태에서도 명시적 요청은 즉시 수행된다.
     */
    fun askNow(project: Project, file: VirtualFile) = Ears(project).ask(file, force = true)

    private fun key(project: Project, file: VirtualFile) = project.locationHash + " " + file.path

    fun noteFor(project: Project, file: VirtualFile): String? = said[key(project, file)]

    /** 상단 배너의 '전체 보기' 클릭 시 전체 검토 피드백 원문을 다이얼로그로 표시한다. */
    fun showFull(project: Project, file: VirtualFile) {
        val note = full[key(project, file)] ?: said[key(project, file)] ?: return
        LookOverAction.show(project, note)
    }

    private val LOG = com.intellij.openapi.diagnostic.Logger.getInstance(LookWhileTyping::class.java)

    /** 검토 완료 시점에 에디터 배너 알림을 즉시 갱신하여 UI 상태를 동기화한다. */
    internal fun refreshIcons(project: Project) {
        EditorNotifications.getInstance(project).updateAllNotifications()
    }

    fun forget(project: Project, file: VirtualFile) {
        LOG.info("magi: 훑어본 말을 걷는다 — " + file.name)
        said.remove(key(project, file))
        full.remove(key(project, file))
        ApplicationManager.getApplication().invokeLater { LookInlays.clear(project, file) }
        EditorNotifications.getInstance(project).updateNotifications(file)
    }

    /** 프로젝트 라이프사이클에 바인딩된 Disposable 스코프. */
    fun scope(project: Project): Disposable = project.getService(Scope::class.java)

    @com.intellij.openapi.components.Service(com.intellij.openapi.components.Service.Level.PROJECT)
    internal class Scope : Disposable {
        override fun dispose() = Unit
    }

    /**
     * 문서 변경 감지 DocumentListener.
     * 파일 전환 이벤트 대신 멀티캐스터를 사용하여 이미 열려 있는 에디터 버퍼의 변경 사항까지 누락 없이 감지한다 ([LookStartup] 참조).
     */
    internal class Ears(private val project: Project) : DocumentListener {
        private val gen = java.util.concurrent.atomic.AtomicLong()
        private var pending: VirtualFile? = null

        /** 디바운스 인터벌 800ms (LLM 추론 비용을 고려하여 인라인 자동완성 400ms 대비 길게 책정). */
        private val debounce = Timer(800) { pending?.let { ask(it) } }.apply { isRepeats = false }

        override fun documentChanged(e: DocumentEvent) {
            if (!enabled(project)) return
            val file = FileDocumentManager.getInstance().getFile(e.document) ?: return
            val base = project.basePath ?: return
            // 프로젝트 작업 디렉토리 외부 경로(라이브러리 소스, 외부 파일 등)는 데몬 샌드박스 거절 대상이므로 검토 대상에서 제외한다.
            if (!file.path.startsWith(base + "/")) return
            forget(project, file) // 버퍼 내용 변경 시 기존 검토 피드백 무효화
            pending = file
            debounce.restart()
        }

        fun ask(file: VirtualFile, force: Boolean = false) {
            if (!force && !enabled(project)) return
            val k = key(project, file)
            if (!running.add(k)) return // 중복 실행 방지
            val sock = Workspace(project).socket() ?: return
            val text = runReadActionBlocking {
                FileDocumentManager.getInstance().getDocument(file)?.text
            } ?: return
            val base = project.basePath ?: return
            val rel = file.path.removePrefix(base + "/")
            val mine = gen.incrementAndGet()
            ApplicationManager.getApplication().executeOnPooledThread {
                val asked = runCatching { Assist({ DaemonClient.connect(sock) }).lookOver(rel, text) }
                running.remove(k)
                ApplicationManager.getApplication().invokeLater { refreshIcons(project) }
                // 지적 사항 없음(정상 침묵)과 네트워크/엔진 오류로 인한 무응답을 구분하여 로깅
                asked.onFailure { LOG.info("magi: 훑어보기가 실패했다 — " + file.name, it) }
                val out = asked.getOrNull()
                if (asked.isSuccess && out.isNullOrBlank()) LOG.info("magi: 훑어봤고 할 말이 없었다 — " + file.name)
                // 요청 이후 추가 입력이 발생한 경우(세대 번호 불일치) 이전 결과는 폐기한다.
                if (mine != gen.get()) return@executeOnPooledThread
                val note = out?.trim()?.takeIf { it.isNotEmpty() } ?: return@executeOnPooledThread
                val cut = dev.sayaya.magi.ide.usecase.LookNotes.split(note)
                val anchored = cut.anchored
                val loose = cut.loose
                // 라인 앵커가 존재하는 피드백은 인라인 인레이로, 비앵커 피드백은 상단 배너로 분기한다.
                full[key(project, file)] = note
                ApplicationManager.getApplication().invokeLater {
                    // 라인 삭제로 인레이 배치가 실패한 피드백은 누락하지 않고 상단 배너로 폴백 표출한다.
                    val missed = LookInlays.show(project, file, anchored)
                    val rest = (listOf(loose) + missed).filter { it.isNotBlank() }.joinToString("\n")
                    if (rest.isBlank()) said.remove(key(project, file)) else said[key(project, file)] = rest
                    EditorNotifications.getInstance(project).updateNotifications(file)
                }
            }
        }
    }
}
