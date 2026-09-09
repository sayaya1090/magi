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
 * IntelliJ 에디터 조작 및 검사 기능을 수행하는 IDE 측 핸드(Hand) 구현체.
 *
 * 디스크 파일 직접 수정 대신 IDE 문서(`Document`)를 수정하여 작업한다.
 * `WriteCommandAction` 컨텍스트 내에서 문서를 수정함으로써 단일 Undo 스택 등록, 로컬 히스토리 보존,
 * 인스펙션 자동 재수행, 열린 에디터 실시간 동기화를 보장한다 (§5 규칙).
 *
 * MCP 요청 스레드(HTTP)와 IntelliJ 쓰기 액션(EDT + Write Lock) 간의 스레드 동기화를 처리하며,
 * 모달 다이얼로그 등으로 인한 UI 스레드 교착 시 에이전트 무한 대기를 방지하기 위해 20초 타임아웃 경계를 둔다.
 */
class IdeHand(private val project: Project) : Hand.Ide {

    override fun show(path: String, line: Int?): String = onEdt {
        val f = find(path) ?: return@onEdt "no such file in this project: $path"
        // 1-based 라인 번호를 IntelliJ의 0-based 에디터 오프셋으로 변환
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
        // 문자열 미발견과 다중 발견을 분리하여 에이전트에게 명확한 교정 가이드를 제공
        if (hits == 0) return@onEdt "that text is not in ${f.path}"
        if (hits > 1 && !all) return@onEdt "that text appears $hits times in ${f.path} — narrow it, or pass replaceAll"
        WriteCommandAction.runWriteCommandAction(project, "magi: apply edit", null, {
            doc.setText(if (all) text.replace(old, new) else text.replaceFirst(old, new))
            PsiDocumentManager.getInstance(project).commitDocument(doc)
        })
        "replaced $hits occurrence(s) in ${f.path} — in the editor, so undo and inspections see it"
    }

    /**
     * IDE 에디터에 현재 표시 중인 오류 및 경고 목록을 조회한다.
     *
     * 인스펙션을 재실행하지 않고 현재 에디터 하이라이팅 캐시(`DaemonCodeAnalyzerImpl.getHighlights`)를 직접 읽어 즉시 반환한다.
     * 분석이 아직 완료되지 않은 파일 역시 오류가 보고되지 않으므로, 미발견 시 분석 진행 가능성에 대한 주의 문구를 포함한다.
     * 빌드 차단 요소를 명확히 구분하기 위해 정보(Information) 및 힌트(Hint) 수준은 제외하고 Warning 이상만 수집한다.
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
        // 대규모 프로젝트 리팩터링 시 도구 결과 페이로드 폭증을 방지하기 위해 최대 200건으로 상한을 제한한다.
        val cap = 200
        if (lines.size <= cap) lines.joinToString("\n")
        else lines.take(cap).joinToString("\n") + "\n… and ${lines.size - cap} more"
    }

    /** 프로젝트 작업 영역 내의 가상 파일만 조회한다 (작업 영역 외부 파일 접근 차단). */
    private fun find(path: String): VirtualFile? {
        val abs = if (Paths.get(path).isAbsolute) path else "${project.basePath}/$path"
        val f = LocalFileSystem.getInstance().refreshAndFindFileByPath(abs) ?: return null
        val base = project.basePath ?: return null
        return f.takeIf { it.path.startsWith(base) }
    }

    /**
     * 작업을 EDT 스레드로 디스패치하고 최대 20초간 대기한다.
     * UI 스레드가 모달 대화상자 등으로 차단된 경우 20초 후 타임아웃 예외를 반환하여 에이전트 블로킹을 해제한다.
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
