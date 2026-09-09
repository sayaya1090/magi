package dev.sayaya.magi.ide.ui

import com.intellij.openapi.editor.Editor
import com.intellij.openapi.editor.EditorCustomElementRenderer
import com.intellij.openapi.editor.EditorFactory
import com.intellij.openapi.editor.Inlay
import com.intellij.openapi.editor.markup.TextAttributes
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import java.awt.Graphics
import java.awt.Rectangle

/**
 * 코드 검토(LookOver) 피드백을 에디터 행 끝 인라인 인레이(Inlay)로 렌더링한다.
 *
 * 코어 데몬의 LookOver 출력 계약(`<line>\t<comment>`, `internal/app/git.go`)에 따라 특정 라인 끝에 요소를 배치하며,
 * 행 위치 매핑이 불가능하거나 에디터가 열려 있지 않은 피드백은 반환되어 [LookBanner] 상단 알림 배너로 전달된다.
 *
 * 플러그인이 생성한 인레이 인스턴스만을 추적([mine])하여 해제함으로써 타 플러그인이나 IDE 기본 인레이(매개변수 힌트, 타입 어노테이션 등)와의 충돌을 방지한다.
 */
internal object LookInlays {

    private val mine = java.util.concurrent.ConcurrentHashMap<String, MutableList<Inlay<*>>>()

    private fun key(project: Project, file: VirtualFile) = project.locationHash + " " + file.path

    /**
     * EDT에서 호출된다.
     * 문서 변경으로 라인이 유실되었거나 에디터 인스턴스가 없어 인레이를 부착하지 못한 피드백 목록을 반환한다 (호출부에서 배너로 폴백 처리).
     */
    fun show(project: Project, file: VirtualFile, notes: List<Pair<Int, String>>): List<String> {
        clear(project, file)
        val doc = FileDocumentManager.getInstance().getDocument(file)
            ?: return notes.map { (n, t) -> "${n}행: $t" }
        val editors = EditorFactory.getInstance().getEditors(doc, project)
        if (editors.isEmpty()) return notes.map { (n, t) -> "${n}행: $t" }
        val kept = mutableListOf<Inlay<*>>()
        val missed = mutableListOf<String>()
        for ((line, text) in notes) {
            val idx = line - 1
            if (idx < 0 || idx >= doc.lineCount) { missed += "${line}행: $text"; continue }
            val end = doc.getLineEndOffset(idx)
            var placed = false
            for (editor in editors) {
                editor.inlayModel.addAfterLineEndElement(end, false, Ghost(text))?.let {
                    kept += it; placed = true
                }
            }
            if (!placed) missed += "${line}행: $text"
        }
        if (kept.isNotEmpty()) mine[key(project, file)] = kept
        // 인레이 부착 상태 로깅 (전체 수, 부착 성공 수, 미배치 수, 에디터 수)
        LOG.info("magi: 훑어본 말 ${notes.size} — 줄에 건 것 ${kept.size}, 못 건 것 ${missed.size}, 편집기 ${editors.size}")
        return missed
    }

    private val LOG = com.intellij.openapi.diagnostic.Logger.getInstance(LookInlays::class.java)

    fun clear(project: Project, file: VirtualFile) {
        mine.remove(key(project, file))?.forEach { runCatching { com.intellij.openapi.util.Disposer.dispose(it) } }
    }

    /** 에디터 폰트 기반 이탤릭 텍스트 렌더러. 코드와의 조화를 위해 현재 에디터 폰트 설정을 계승한다. */
    private class Ghost(private val text: String) : EditorCustomElementRenderer {
        private fun shown() = "  " + text.trim()

        override fun calcWidthInPixels(inlay: Inlay<*>): Int {
            val ed = inlay.editor
            return ed.contentComponent.getFontMetrics(font(ed)).stringWidth(shown())
        }

        override fun paint(inlay: Inlay<*>, g: Graphics, r: Rectangle, attrs: TextAttributes) {
            val ed = inlay.editor
            g.font = font(ed)
            // 에디터 테마의 인레이 전경색을 사용한다.
            // 어두운 테마에서 문서화 주석 색상이 배경에 묻히는 실측 문제(2026-09-01)를 해결하기 위해
            // 플랫폼 테마가 보장하는 파라미터 힌트/인레이 기본 롤 색상을 우선 적용한다.
            g.color = scheme(ed) ?: attrs.foregroundColor ?: ed.colorsScheme.defaultForeground
            val fm = g.fontMetrics
            g.drawString(shown(), r.x, r.y + (r.height - fm.height) / 2 + fm.ascent)
        }

        /** 에디터 테마의 인레이 전경색 조회 (INLINE_PARAMETER_HINT 우선, 부재 시 INLAY_DEFAULT 폴백). */
        private fun scheme(ed: Editor): java.awt.Color? {
            val s = ed.colorsScheme
            return s.getAttributes(
                com.intellij.openapi.editor.DefaultLanguageHighlighterColors.INLINE_PARAMETER_HINT,
            )?.foregroundColor
                ?: s.getAttributes(
                    com.intellij.openapi.editor.DefaultLanguageHighlighterColors.INLAY_DEFAULT,
                )?.foregroundColor
        }

        private fun font(ed: Editor) =
            ed.colorsScheme.getFont(com.intellij.openapi.editor.colors.EditorFontType.ITALIC)
    }
}
