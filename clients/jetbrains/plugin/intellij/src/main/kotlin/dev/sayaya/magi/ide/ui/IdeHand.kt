package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.command.WriteCommandAction
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.fileEditor.OpenFileDescriptor
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.LocalFileSystem
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.psi.PsiDocumentManager
import dev.sayaya.magi.ide.usecase.Hand
import java.nio.file.Paths
import java.util.concurrent.Callable
import java.util.concurrent.TimeUnit

/**
 * 손의 IDE 쪽 — 실제로 편집기를 움직이는 자리.
 *
 * **문서를 고치지 디스크를 고치지 않는다.** 그것이 이 도구가 존재하는 이유다. `WriteCommandAction`
 * 안에서 문서를 바꾸면 되돌리기 스택에 하나로 들어가고, 로컬 히스토리가 남고, 인스펙션이 다시 돌고,
 * 열린 편집기가 바로 갱신된다. 파일을 밖에서 쓰면 그 넷이 다 안 일어난다 — magi 의 `write` 가
 * 나쁘다는 뜻이 아니라, **IDE 가 열어 둔 파일에는 이쪽이 맞다**는 뜻이다(§5).
 *
 * 스레드가 까다롭다. MCP 요청은 HTTP 스레드에서 오고 IntelliJ 의 문서 쓰기는 **EDT + 쓰기 액션**
 * 안에서만 된다. 그래서 넘겨서 기다리되 **경계를 둔다** — 대화 상자 하나가 EDT 를 잡고 있으면
 * 에이전트가 영영 매달리는 대신 사유를 받고 다음으로 갈 수 있어야 한다.
 */
class IdeHand(private val project: Project) : Hand.Ide {

    override fun show(path: String, line: Int?): String = onEdt {
        val f = find(path) ?: return@onEdt "no such file in this project: $path"
        // 0-기반으로 바꾼다. 사람과 도구가 1부터 세고 IntelliJ 가 0부터 센다.
        val d = OpenFileDescriptor(project, f, ((line ?: 1) - 1).coerceAtLeast(0), 0)
        FileEditorManager.getInstance(project).openTextEditor(d, true)
        "opened ${f.path}" + (line?.let { " at line $it" } ?: "")
    }

    override fun replace(path: String, old: String, new: String, all: Boolean): String = onEdt {
        val f = find(path) ?: return@onEdt "no such file in this project: $path"
        val docs = com.intellij.openapi.fileEditor.FileDocumentManager.getInstance()
        val doc = docs.getDocument(f) ?: return@onEdt "not a text file: ${f.path}"
        val text = doc.text
        val hits = text.split(old).size - 1
        // 못 찾은 것과 여러 번 찾은 것을 **다르게** 말한다. 둘 다 "실패"로 뭉치면 에이전트가
        // 무엇을 고쳐야 할지 모른다 — 하나는 문자열이 틀린 것이고 하나는 좁히라는 뜻이다.
        if (hits == 0) return@onEdt "that text is not in ${f.path}"
        if (hits > 1 && !all) return@onEdt "that text appears $hits times in ${f.path} — narrow it, or pass replaceAll"
        WriteCommandAction.runWriteCommandAction(project, "magi: apply edit", null, {
            doc.setText(if (all) text.replace(old, new) else text.replaceFirst(old, new))
            PsiDocumentManager.getInstance(project).commitDocument(doc)
        })
        "replaced $hits occurrence(s) in ${f.path} — in the editor, so undo and inspections see it"
    }

    /**
     * IDE 가 지금 말하는 오류와 경고.
     *
     * **하이라이트를 읽지 인스펙션을 다시 돌리지 않는다.** 다시 돌리면 초 단위이고, 사람이 보고 있는
     * 것과 다른 답이 나올 수 있다 — 이 도구의 값은 「사람 화면에 밑줄로 그려진 그것」을 그대로 주는
     * 데 있다.
     *
     * ⚠ **없다는 것이 괜찮다는 뜻은 아니다.** 아직 분석이 안 끝난 파일도 아무것도 안 말한다. 그래서
     * 「없다」를 그렇게만 적지 않고 그 사정을 같이 적는다 — 조용한 성공을 「깨끗하다」로 읽으면
     * 에이전트가 빌드도 안 돌리고 넘어간다.
     *
     * 힌트와 정보는 건다. 그것은 IDE 의 제안이고, 섞으면 빌드를 멈추는 둘이 묻힌다.
     */
    override fun problems(path: String?): String = onEdt {
        val docs = com.intellij.openapi.fileEditor.FileDocumentManager.getInstance()
        val files: List<VirtualFile> = if (path != null) {
            listOf(find(path) ?: return@onEdt "no such file in this project: $path")
        } else {
            FileEditorManager.getInstance(project).openFiles.toList()
        }
        val lines = mutableListOf<String>()
        for (f in files) {
            val doc = docs.getDocument(f) ?: continue
            val found = com.intellij.codeInsight.daemon.impl.DaemonCodeAnalyzerImpl.getHighlights(
                doc, com.intellij.lang.annotation.HighlightSeverity.WARNING, project,
            )
            for (h in found) {
                val kind = if (h.severity >= com.intellij.lang.annotation.HighlightSeverity.ERROR) "error" else "warning"
                val line = doc.getLineNumber(h.startOffset) + 1
                val col = h.startOffset - doc.getLineStartOffset(line - 1) + 1
                val text = h.description ?: continue
                lines += "${f.path}:$line:$col $kind: $text"
            }
        }
        if (lines.isEmpty()) {
            return@onEdt if (path != null) {
                "no errors or warnings for $path — note that a file the IDE has not finished " +
                    "analysing also reports none"
            } else {
                "no errors or warnings in the open files right now"
            }
        }
        // 상한을 둔다. 리팩터링 중인 프로젝트는 수천 줄이 되고, 이것은 툴 결과로 실려 간다.
        val cap = 200
        if (lines.size <= cap) lines.joinToString("\n")
        else lines.take(cap).joinToString("\n") + "\n… and ${lines.size - cap} more"
    }

    /** 프로젝트 안의 파일만 찾는다. 밖을 열어 주면 워크스페이스 경계가 이 도구로 새 나간다. */
    private fun find(path: String): VirtualFile? {
        val abs = if (Paths.get(path).isAbsolute) path else "${project.basePath}/$path"
        val f = LocalFileSystem.getInstance().refreshAndFindFileByPath(abs) ?: return null
        val base = project.basePath ?: return null
        return f.takeIf { it.path.startsWith(base) }
    }

    /**
     * EDT 로 넘기고 기다린다. **무한히 기다리지 않는다** — 대화 상자가 EDT 를 잡고 있으면
     * 에이전트가 매달리는 대신 사유를 받아야 하고, 매달린 도구 호출은 턴 전체를 세운다.
     */
    private fun onEdt(work: () -> String): String {
        val task = java.util.concurrent.FutureTask(Callable { work() })
        ApplicationManager.getApplication().invokeLater(task)
        return try {
            task.get(20, TimeUnit.SECONDS)
        } catch (e: java.util.concurrent.TimeoutException) {
            task.cancel(true)
            "the IDE did not answer in 20s — something is holding its UI thread"
        }
    }
}
