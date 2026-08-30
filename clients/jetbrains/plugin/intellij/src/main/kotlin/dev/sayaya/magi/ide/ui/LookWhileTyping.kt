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
 * **타이핑 중 훑어보기** — 손을 잠시 멈추면 컴패니언이 지금 버퍼를 읽고, 잘못돼 보이는 것이
 * 있으면 그것만 말한다. 할 말이 없으면 **아무 말도 하지 않는다.**
 *
 * 웹 콘솔이 같은 것을 갖고 있고(files.look), 부르는 문도 같다 — look-over. 새 문은 없다.
 *
 * **기본은 꺼짐이다.** 멈출 때마다 백엔드를 한 번 쓰는 비용은 타이핑하는 사람이 **고를** 일이지
 * 겪을 일이 아니다(웹이 같은 판단). 그리고 이 스위치는 컴패니언의 상태가 아니라 **이 화면의
 * 취향**이라 데몬이 아니라 프로젝트 로컬에 산다 — 웹도 브라우저-로컬로 둔다.
 *
 * 저장하지 않는다. 대화에도 안 보낸다 — 답은 그 파일 위 띠에만 뜬다.
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
            // 꺼면 회색 글씨도 같이 걷힌다 — 끈 기능의 말이 남아 있으면 그것이 거짓이다.
            ApplicationManager.getApplication().invokeLater {
                com.intellij.openapi.fileEditor.FileEditorManager.getInstance(project).openFiles
                    .forEach { LookInlays.clear(project, it) }
            }
            EditorNotifications.getInstance(project).updateAllNotifications()
        }
    }

    /**
     * 파일마다 마지막으로 들은 말. 없는 파일은 띠가 안 선다(할 말 없음 = 침묵).
     * 열쇠에 프로젝트를 섞는다 — 같은 경로를 두 프로젝트가 열면 남의 말이 뜬다.
     */
    private val said = java.util.concurrent.ConcurrentHashMap<String, String>()

    /** 「전체 보기」가 여는 원문 — 줄에 건 것까지 다 들어 있다. */
    private val full = java.util.concurrent.ConcurrentHashMap<String, String>()

    /** 지금 훑는 중인 파일들 — 아이콘이 스피너로 바뀌는 근거다(도는 것을 도는 것으로 그린다). */
    private val running = java.util.concurrent.ConcurrentHashMap.newKeySet<String>()

    fun isRunning(project: Project, file: VirtualFile): Boolean = running.contains(key(project, file))

    /**
     * 사람이 지금 보자고 한 것 — 아이콘 단추가 부른다. 디바운스를 안 기다리고 바로 묻는다.
     * 스위치가 꺼져 있어도 **부르면 답한다**: 자동은 취향이고 이것은 명시적 요청이다.
     */
    fun askNow(project: Project, file: VirtualFile) = Ears(project).ask(file, force = true)

    private fun key(project: Project, file: VirtualFile) = project.locationHash + " " + file.path

    fun noteFor(project: Project, file: VirtualFile): String? = said[key(project, file)]

    /** 띠는 「전체 보기」로 원문을 연다 — 줄에 건 것까지 다 들어 있다. */
    fun showFull(project: Project, file: VirtualFile) {
        val note = full[key(project, file)] ?: said[key(project, file)] ?: return
        LookOverAction.show(project, note)
    }

    private val LOG = com.intellij.openapi.diagnostic.Logger.getInstance(LookWhileTyping::class.java)

    /**
     * 아이콘이 상태를 따라오게 한다. 툴바는 제 주기로 update 를 부르므로 보통은 저절로
     * 바뀌지만, 끝난 순간을 바로 보이게 편집기 띠도 함께 새로고침한다.
     */
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

    /** 프로젝트와 함께 죽는 수명 — 귀는 프로젝트가 닫히면 떨어진다. */
    fun scope(project: Project): Disposable = project.getService(Scope::class.java)

    @com.intellij.openapi.components.Service(com.intellij.openapi.components.Service.Level.PROJECT)
    internal class Scope : Disposable {
        override fun dispose() = Unit
    }

    /**
     * 모든 문서의 편집을 듣는 귀. 파일 선택 이벤트가 아니라 멀티캐스터인 이유는 [LookStartup]
     * 주석에 있다 — 이미 열려 있던 파일이 안 들리던 실측.
     */
    internal class Ears(private val project: Project) : DocumentListener {
        private val gen = java.util.concurrent.atomic.AtomicLong()
        private var pending: VirtualFile? = null

        /** 800ms — 자동완성(400ms)보다 길다. 읽고 판단하는 호출이라 더 비싸다. */
        private val debounce = Timer(800) { pending?.let { ask(it) } }.apply { isRepeats = false }

        override fun documentChanged(e: DocumentEvent) {
            if (!enabled(project)) return
            val file = FileDocumentManager.getInstance().getFile(e.document) ?: return
            val base = project.basePath ?: return
            // 워크스페이스 밖(라이브러리 소스, 다른 루트)은 묻지 않는다 — 코어의 감옥이 거절할
            // 경로를 굳이 보내지 않는다.
            if (!file.path.startsWith(base + "/")) return
            forget(project, file) // 글자가 바뀌면 옛말은 낡았다
            pending = file
            debounce.restart()
        }

        fun ask(file: VirtualFile, force: Boolean = false) {
            if (!force && !enabled(project)) return
            val k = key(project, file)
            if (!running.add(k)) return // 이미 도는 중 — 두 번 묻지 않는다
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
                // **침묵과 고장을 가른다.** 둘 다 화면에는 「아무것도 안 뜸」이라 로그가 유일한
                // 갈림이다(사용자가 「안 뜨는데?」로 물은 자리 — 그때 둘 중 무엇이었는지 알 길이
                // 없었다).
                asked.onFailure { LOG.info("magi: 훑어보기가 실패했다 — " + file.name, it) }
                val out = asked.getOrNull()
                if (asked.isSuccess && out.isNullOrBlank()) LOG.info("magi: 훑어봤고 할 말이 없었다 — " + file.name)
                // 늦게 온 답은 버린다 — 그새 더 쳤으면 그 답은 옛 버퍼의 것이다.
                if (mine != gen.get()) return@executeOnPooledThread
                val note = out?.trim()?.takeIf { it.isNotEmpty() } ?: return@executeOnPooledThread
                val cut = dev.sayaya.magi.ide.usecase.LookNotes.split(note)
                val anchored = cut.anchored
                val loose = cut.loose
                // 줄에 걸리는 말은 그 줄 옆에, 나머지만 띠에 — 웹이 줄 옆에 그리는 그 모양이다.
                full[key(project, file)] = note
                ApplicationManager.getApplication().invokeLater {
                    // 줄이 사라졌으면(그새 지웠다) 그 말은 걸 자리가 없다 — **삼키지 않고**
                    // 띠로 올린다. 못 건 말을 조용히 버리면 「할 말 없음」과 같아 보인다.
                    val missed = LookInlays.show(project, file, anchored)
                    val rest = (listOf(loose) + missed).filter { it.isNotBlank() }.joinToString("\n")
                    if (rest.isBlank()) said.remove(key(project, file)) else said[key(project, file)] = rest
                    EditorNotifications.getInstance(project).updateNotifications(file)
                }
            }
        }
    }
}
