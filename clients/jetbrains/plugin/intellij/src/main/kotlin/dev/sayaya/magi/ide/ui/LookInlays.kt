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
 * 훑어본 말을 **그 줄 끝에 회색 글씨로** 붙인다 — 웹 콘솔이 같은 말을 줄 옆에 그리는 그
 * 모양이고(사용자 지시), IDE 에서 그 자리는 인레이다. 코어가 `<줄><TAB><지적>` 꼴로 주므로
 * (`internal/app/git.go` 의 LookOver 계약) 줄에 걸 수 있다 — 못 거는 말은 여기 오지 않고
 * 편집기 위 띠로 간다([LookBanner]).
 *
 * 우리 것만 지운다: 인레이를 손에 들고 있다가 지우지, 그 줄의 인레이를 쓸지 않는다 —
 * 남의 힌트(파라미터 이름, 타입)를 우리가 치우면 그건 남의 화면을 부수는 것이다.
 */
internal object LookInlays {

    private val mine = java.util.concurrent.ConcurrentHashMap<String, MutableList<Inlay<*>>>()

    private fun key(project: Project, file: VirtualFile) = project.locationHash + " " + file.path

    /**
     * EDT 에서 부른다. **못 건 말을 돌려준다** — 줄이 그새 사라졌거나 편집기가 없으면 걸 자리가
     * 없는데, 조용히 버리면 「할 말 없음」과 화면에서 같아진다. 부르는 쪽이 그것을 띠로 올린다.
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
        // 몇 개를 어디에 걸었는지 적는다 — 「안 뜬다」와 「안 왔다」와 「걸 자리가 없었다」는
        // 화면에서 같아 보이고, 그 셋은 사람이 할 일이 다르다.
        LOG.info("magi: 훑어본 말 ${notes.size} — 줄에 건 것 ${kept.size}, 못 건 것 ${missed.size}, 편집기 ${editors.size}")
        return missed
    }

    private val LOG = com.intellij.openapi.diagnostic.Logger.getInstance(LookInlays::class.java)

    fun clear(project: Project, file: VirtualFile) {
        mine.remove(key(project, file))?.forEach { runCatching { com.intellij.openapi.util.Disposer.dispose(it) } }
    }

    /** 회색 이탤릭 한 줄. 편집기 글꼴을 그대로 쓴다 — 코드 옆에 선 글은 코드처럼 보여야 한다. */
    private class Ghost(private val text: String) : EditorCustomElementRenderer {
        private fun shown() = "  " + text.trim()

        override fun calcWidthInPixels(inlay: Inlay<*>): Int {
            val ed = inlay.editor
            return ed.contentComponent.getFontMetrics(font(ed)).stringWidth(shown())
        }

        override fun paint(inlay: Inlay<*>, g: Graphics, r: Rectangle, attrs: TextAttributes) {
            val ed = inlay.editor
            g.font = font(ed)
            // **테마가 정한 힌트 색을 쓴다.** 처음엔 문서화 색을 집었는데 어두운 테마에서
            // 배경에 묻혔다(사용자 실측: "회색이 너무 어두워서 잘 안 보인다"). 인레이의 색은
            // 테마가 이미 정해 둔 롤이 있다 — 파라미터 힌트가 쓰는 그것이고, 어느 테마든
            // 그 테마가 「읽히되 앞에 안 나서는」 값으로 고른 색이다. 우리가 회색을 짐작하지
            // 않는다: 짐작한 회색은 다음 테마에서 다시 묻힌다.
            g.color = scheme(ed) ?: attrs.foregroundColor ?: ed.colorsScheme.defaultForeground
            val fm = g.fontMetrics
            g.drawString(shown(), r.x, r.y + (r.height - fm.height) / 2 + fm.ascent)
        }

        /** 테마의 인레이 색 — 힌트 롤 먼저, 없으면 인레이 기본 롤. */
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
